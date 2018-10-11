package p2p

import (
	"bvchain/ec"
	"bvchain/cfg"
	"bvchain/p2p/cxn"
	"bvchain/util/log"

	golog "log"
	"net"
	"testing"
	"time"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testCh = 0x01


func TestPeerBasic(t *testing.T) {
	assert, require := assert.New(t), require.New(t)

	// simulate remote peer
	rp := &_RemotePeer{PrivKey: ec.NewPrivKey(), Config: _config}
	rp.Start()
	defer rp.Stop()

	p, err := createOutgoingPeerAndPerformHandshake(rp.Addr(), _config, cxn.DefaultMxConfig())
	require.Nil(err)

	err = p.Start()
	require.Nil(err)
	defer p.Stop()

	assert.True(p.IsRunning())
	assert.True(p.IsOutgoing())
	assert.False(p.IsPersistent())
	p.pc.persistent = true
	assert.True(p.IsPersistent())
	assert.Equal(rp.Addr().DialString(), p.Addr().String())
	assert.Equal(rp.NodeId(), p.NodeId())
}

func TestPeerSend(t *testing.T) {
	assert, require := assert.New(t), require.New(t)

	config := _config

	// simulate remote peer
	rp := &_RemotePeer{PrivKey: ec.NewPrivKey(), Config: config}
	rp.Start()
	defer rp.Stop()

	p, err := createOutgoingPeerAndPerformHandshake(rp.Addr(), config, cxn.DefaultMxConfig())
	require.Nil(err)

	err = p.Start()
	require.Nil(err)

	defer p.Stop()

	assert.True(p.CanSend(testCh))
	assert.True(p.Send(testCh, []byte("Asylum")))
}

func createOutgoingPeerAndPerformHandshake(
	addr *NetAddress,
	config *cfg.P2pConfig,
	mxConfig cxn.MxConfig,
) (*_Peer, error) {
	chDescs := []*cxn.ChannelDescriptor{
		{Id: testCh, Priority: 1},
	}
	reactorsByCh := map[byte]Reactor{testCh: NewTestReactor(chDescs, true)}
	pk := ec.NewPrivKey()
	pc, err := newConnectionOutgoing(config, pk, addr, false)
	if err != nil {
		return nil, err
	}

	myInfo := &NodeInfo{
		NodeId:     addr.NodeId,
		ChainId:    "testing",
		Version:    "123.123.123",
		Channels: []byte{testCh},
	}
	nodeInfo, err := pc.Handshake(myInfo, 1*time.Second)
	if err != nil {
		return nil, err
	}

	p := newPeer(pc, mxConfig, nodeInfo, reactorsByCh, chDescs, func(p Peer, r interface{}) {})
	p.SetLogger(log.TestingLogger().With("peer", addr))
	return p, nil
}

type _RemotePeer struct {
	PrivKey    ec.PrivKey
	Config     *cfg.P2pConfig
	addr       *NetAddress
	quit       chan struct{}
	channels   []byte
	listenAddr string
}

func (rp *_RemotePeer) Addr() *NetAddress {
	return rp.addr
}

func (rp *_RemotePeer) NodeId() NodeId {
	return PubKeyToNodeId(rp.PrivKey.PubKey())
}

func (rp *_RemotePeer) Start() {
	if rp.listenAddr == "" {
		rp.listenAddr = "127.0.0.1:0"
	}

	l, e := net.Listen("tcp", rp.listenAddr) // any available address
	if e != nil {
		golog.Fatalf("net.Listen tcp :0: %+v", e)
	}
	rp.addr = NewNetAddress(rp.NodeId(), l.Addr())
	rp.quit = make(chan struct{})
	if rp.channels == nil {
		rp.channels = []byte{testCh}
	}
	go rp.accept(l)
}

func (rp *_RemotePeer) Stop() {
	close(rp.quit)
}

func (rp *_RemotePeer) accept(l net.Listener) {
	conns := []net.Conn{}

	for {
		conn, err := l.Accept()
		if err != nil {
			golog.Fatalf("Failed to accept conn: %+v", err)
		}

		pc, err := newConnectionIncoming(rp.Config, rp.PrivKey, conn)
		if err != nil {
			golog.Fatalf("Failed to create a peer: %+v", err)
		}

		myInfo := &NodeInfo{
			NodeId:      rp.Addr().NodeId,
			ChainId:    "testing",
			Version:    "123.123.123",
			ListenAddr: l.Addr().String(),
			Channels:   rp.channels,
		}

		_, err = pc.Handshake(myInfo, 1*time.Second)
		if err != nil {
			golog.Fatalf("Failed to perform handshake: %+v", err)
		}

		conns = append(conns, conn)

		select {
		case <-rp.quit:
			for _, conn := range conns {
				if err := conn.Close(); err != nil {
					golog.Fatal(err)
				}
			}
			return
		default:
		}
	}
}

