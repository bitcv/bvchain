package p2p

import (
	"bvchain/ec"
	"bvchain/util"
	"bvchain/util/log"
	"bvchain/p2p/cxn"
	"bvchain/cfg"

	"fmt"
	"net"
)

func RandomNodeId() NodeId {
	r := util.RandomBytes(32)
	return NodeId(util.BytesToBase32Sum(r, "", 0, false))
}

func AddPeerToAdapter(ap *Adapter, peer Peer) {
	ap.peers.Add(peer)
}

func CreateRandomPeer(outgoing bool) *_Peer {
	addr, netAddr := CreateRoutableAddr()
	p := &_Peer{
		pc: &_PeerConnection{
			outgoing: outgoing,
		},
		nodeInfo: NodeInfo{
			NodeId: netAddr.NodeId,
			ListenAddr: netAddr.DialString(),
		},
		mc: &cxn.MxConn{},
	}
	p.SetLogger(log.TestingLogger().With("peer", addr))
	return p
}

func CreateRoutableAddr() (addr string, netAddr *NetAddress) {
	for {
		var err error
		id := RandomNodeId()
		addr = fmt.Sprintf("%s@%v.%v.%v.%v:26656", id, util.RandomIntn(256), util.RandomIntn(256), util.RandomIntn(256), util.RandomIntn(256))
		netAddr, err = NewNetAddressString(addr)
		if err != nil {
			panic(err)
		}
		if netAddr.Routable() {
			break
		}
	}
	return
}

//------------------------------------------------------------------
// Connects adapters via arbitrary net.Conn. Used for testing.

const _TEST_HOST = "localhost"

// MakeConnectedAdapters returns n adapters, connected according to the connect func.
// If connect==Connect2Adapters, the adapters will be fully connected.
// initAdapter defines how the i'th adapter should be initialized (ie. with what reactors).
// NOTE: panics if any adapter fails to start.
func MakeConnectedAdapters(config *cfg.P2pConfig, n int, initAdapter func(int, *Adapter) *Adapter, connect func([]*Adapter, int, int)) []*Adapter {
	adapters := make([]*Adapter, n)
	for i := 0; i < n; i++ {
		adapters[i] = MakeAdapter(config, i, _TEST_HOST, "123.123.123", initAdapter)
	}

	if err := StartAdapters(adapters); err != nil {
		panic(err)
	}

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			connect(adapters, i, j)
		}
	}

	return adapters
}

// Connect2Adapters will connect adapters i and j via net.Pipe().
// Blocks until a connection is established.
// NOTE: caller ensures i and j are within bounds.
func Connect2Adapters(adapters []*Adapter, i, j int) {
	ap1 := adapters[i]
	ap2 := adapters[j]

	c1, c2 := net.Pipe()

	doneCh := make(chan struct{})
	go func() {
		err := ap1.addPeerWithConnection(c1)
		if err != nil {
			panic(err)
		}
		doneCh <- struct{}{}
	}()
	go func() {
		err := ap2.addPeerWithConnection(c2)
		if err != nil {
			panic(err)
		}
		doneCh <- struct{}{}
	}()
	<-doneCh
	<-doneCh
}

func (ap *Adapter) addPeerWithConnection(conn net.Conn) error {
	pc, err := newConnectionIncoming(ap.config, ap.nodeKey.PrivKey, conn)
	if err != nil {
		if err := conn.Close(); err != nil {
			ap.Logger.Error("Error closing connection", "err", err)
		}
		return err
	}
	if err = ap.addPeerConn(pc); err != nil {
		pc.CloseConn()
		return err
	}

	return nil
}

// StartAdapters calls ap.Start() for each given adapter.
// It returns the first encountered error.
func StartAdapters(adapters []*Adapter) error {
	for _, s := range adapters {
		err := s.Start() // start adapter and reactors
		if err != nil {
			return err
		}
	}
	return nil
}

func MakeAdapter(config *cfg.P2pConfig, i int, chainId, version string, initAdapter func(int, *Adapter) *Adapter) *Adapter {
	// new Adapter, add reactors
	// TODO: let the config be passed in?
	nodeKey := &NodeKey{
		PrivKey: ec.NewPrivKey(),
	}
	ap := NewAdapter(config)
	ap.SetLogger(log.TestingLogger())
	ap = initAdapter(i, ap)
	ni := &NodeInfo{
		NodeId:     nodeKey.NodeId(),
		ChainId:    chainId,
		Version:    version,
		ListenAddr: fmt.Sprintf("127.0.0.1:%d", util.RandomIntn(64512)+1023),
	}
	for ch := range ap.reactorsByCh {
		ni.Channels = append(ni.Channels, ch)
	}
	ap.SetNodeInfo(ni)
	ap.SetNodeKey(nodeKey)
	return ap
}
