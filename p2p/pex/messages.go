package pex

import (
	"bvchain/p2p"

	"fmt"
	"reflect"
)


func init() {
	man := p2p.TheMK.RegisterChannel(p2p.PEX_CHANNEL)
	man.Register(p2p.MessageCode(0x01), reflect.TypeOf((*_PexRequestMessage)(nil)))
	man.Register(p2p.MessageCode(0x02), reflect.TypeOf((*_PexAddrsMessage)(nil)))
}

// A _PexRequestMessage requests additional peer addresses.
type _PexRequestMessage struct {
}

func (m *_PexRequestMessage) String() string {
	return "[PexRequest]"
}

// A message with announced peer addresses.
type _PexAddrsMessage struct {
	Addrs []*p2p.NetAddress	`json:"addrs"`
}

func (m *_PexAddrsMessage) String() string {
	return fmt.Sprintf("[PexAddrs %v]", m.Addrs)
}

