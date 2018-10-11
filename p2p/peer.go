package p2p

import (
	"bvchain/util"
	"bvchain/util/log"
	"bvchain/p2p/cxn"

	"fmt"
	"net"
	"sync"
)


const PeerStateKey = "PeerState"

type Peer interface {
	util.Service

	NodeInfo() *NodeInfo

	NodeId() NodeId
	RemoteIp() net.IP
	IsOutgoing() bool
	IsPersistent() bool

	ConnStatus() *cxn.ConnStatus

	Send(byte, []byte) bool
	TrySend(byte, []byte) bool

	Set(string, interface{})
	Get(string) interface{}
}

type _Peer struct {
	util.BaseService

	nodeInfo NodeInfo
	pc *_PeerConnection
	mc *cxn.MxConn

	data map[string]interface{}
	mtx sync.Mutex
}

// SetLogger implements BaseService.
func (p *_Peer) SetLogger(l log.Logger) {
	p.Logger = l
	p.mc.SetLogger(l)
}

// OnStart implements BaseService.
func (p *_Peer) OnStart() error {
	if err := p.BaseService.OnStart(); err != nil {
		return err
	}
	err := p.mc.Start()
	return err
}

// OnStop implements BaseService.
func (p *_Peer) OnStop() {
	p.BaseService.OnStop()
	p.mc.Stop()
}

//---------------------------------------------------
// Implements Peer

// NodeId returns the peer's NodeId - the hex encoded hash of its pubkey.
func (p *_Peer) NodeId() NodeId {
	return p.nodeInfo.NodeId
}

// IsOutgoing returns true if the connection is outgoing, false otherwise.
func (p *_Peer) IsOutgoing() bool {
	return p.pc.outgoing
}

// IsPersistent returns true if the peer is persitent, false otherwise.
func (p *_Peer) IsPersistent() bool {
	return p.pc.persistent
}

// NodeInfo returns a copy of the peer's NodeInfo.
func (p *_Peer) NodeInfo() *NodeInfo {
	return &p.nodeInfo
}

// Status returns the peer's ConnectionStatus.
func (p *_Peer) ConnStatus() *cxn.ConnStatus {
	return p.mc.Status()
}

// Send msg bytes to the channel identified by chId byte. Returns false if the
// send queue is full after timeout, specified by MConnection.
func (p *_Peer) Send(chId byte, msgBytes []byte) bool {
	if !p.IsRunning() {
		// see Adapter#Broadcast, where we fetch the list of peers and loop over
		// them - while we're looping, one peer may be removed and stopped.
		return false
	}
	return p.mc.Send(chId, msgBytes)
}

// TrySend msg bytes to the channel identified by chId byte. Immediately returns
// false if the send queue is full.
func (p *_Peer) TrySend(chId byte, msgBytes []byte) bool {
	if !p.IsRunning() {
		return false
	}
	return p.mc.TrySend(chId, msgBytes)
}

func (p *_Peer) Get(key string) (value interface{}) {
	p.mtx.Lock()
	value = p.data[key]
	p.mtx.Unlock()
	return
}

func (p *_Peer) Set(key string, value interface{}) {
	p.mtx.Lock()
	p.data[key] = value
	p.mtx.Unlock()
}


//---------------------------------------------------
// methods used by the Adapter

// CloseConn should be called by the Adapter if the peer was created but never
// started.
// Addr returns peer's remote network address.
func (p *_Peer) Addr() net.Addr {
	return p.pc.conn.RemoteAddr()
}

func (p *_Peer) RemoteIp() net.IP {
	return p.pc.RemoteIp()
}

// CanSend returns true if the send queue is not full, false otherwise.
func (p *_Peer) CanSend(chId byte) bool {
	if !p.IsRunning() {
		return false
	}
	return p.mc.CanSend(chId)
}

// String representation.
func (p *_Peer) String() string {
	if p.pc.outgoing {
		return fmt.Sprintf("Peer{%v %v out}", p.pc, p.NodeId())
	}

	return fmt.Sprintf("Peer{%v %v in}", p.pc, p.NodeId())
}

func newPeer(
        pc *_PeerConnection,
        mConfig cxn.MxConfig,
        nodeInfo *NodeInfo,
        reactorsByCh map[byte]Reactor,
        chDescs []*cxn.ChannelDescriptor,
        onPeerError func(Peer, interface{}),
) *_Peer {
	p := &_Peer{
		pc: pc,
		nodeInfo: *nodeInfo,
		data: make(map[string]interface{}),
	}
	p.BaseService.Init(nil, "_Peer", p)

        p.mc = createMxConn(
                pc.conn,
                p,
                reactorsByCh,
                chDescs,
                onPeerError,
                mConfig,
        )
	return p
}

func createMxConn(conn net.Conn,
	p *_Peer,
	reactorsByCh map[byte]Reactor,
	chDescs []*cxn.ChannelDescriptor,
	onPeerError func(Peer, interface{}),
	config cxn.MxConfig,
) *cxn.MxConn {

	onReceive := func(chId byte, msgBytes []byte) {
		reactor := reactorsByCh[chId]
		if reactor == nil {
			// Note that its ok to panic here as it's caught in the conn._recover,
			// which does onPeerError.
			panic(fmt.Sprintf("Unknown channel %X", chId))
		}
		reactor.ReceiveMessage(p, chId, msgBytes)
	}

	onError := func(r interface{}) {
		onPeerError(p, r)
	}

	return cxn.NewMxConnWithConfig(
		conn,
		chDescs,
		onReceive,
		onError,
		config,
	)
}

