package p2p

import (
	"bvchain/util"
	"bvchain/p2p/cxn"
)

type Reactor interface {
	util.Service

	SetAdapter(*Adapter)

	AddPeer(peer Peer)
	RemovePeer(peer Peer, reason interface{})

	GetChannels() []*cxn.ChannelDescriptor
	ReceiveMessage(peer Peer, chId byte, msgBytes []byte)
}

type BaseReactor struct {
	util.BaseService
	Adapter *Adapter
}

var _ Reactor = (*BaseReactor)(nil)

func (br *BaseReactor) Init(name string, impl Reactor) {
	br.BaseService.Init(nil, name, impl)
}

func (br *BaseReactor) SetAdapter(adapter *Adapter) {
	br.Adapter = adapter
}

func (*BaseReactor) AddPeer(peer Peer) {
}

func (*BaseReactor) RemovePeer(peer Peer, reason interface{}) {
}

func (*BaseReactor) GetChannels() []*cxn.ChannelDescriptor {
	panic("The real reactor must implement GetChannels() method")
	return nil
}

func (*BaseReactor) ReceiveMessage(peer Peer, chId byte, msgBytes []byte) {
	panic("The real reactor must implement ReceiveMessage() method")
}

