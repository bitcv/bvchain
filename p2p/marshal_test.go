package p2p

import (
	"testing"

	"fmt"
	"reflect"
)


func initMessages() MarshalKeeper {
	mk := NewMarshalKeeper()
	man := mk.RegisterChannel(PEX_CHANNEL)
	man.Register(MessageCode(0x00), reflect.TypeOf((*_PexRequestMessage)(nil)))
	man.Register(MessageCode(0x01), reflect.TypeOf((*_PexAddrsMessage)(nil)))
	return mk
}

func TestMarshal(t *testing.T) {
	mk := initMessages()
	addr, _ := NewNetAddressString("ktva2nzvvjawz2f364r75yqrymz0p4a8vgg0v7n1wmaven9v9dw0@194.25.184.197:57014")
	bz := mk.MustMarshal(PEX_CHANNEL, &_PexAddrsMessage{Addrs:[]*NetAddress{addr}})
	msg, err := mk.Unmarshal(PEX_CHANNEL, bz)
	t.Log(msg)
	if err != nil {
		t.Error(err)
	}
}

type _PexRequestMessage struct {
}

func (m *_PexRequestMessage) String() string {
	return "[PexRequest]"
}

type _PexAddrsMessage struct {
	Addrs []*NetAddress `json:"addrs"`
}

func (m *_PexAddrsMessage) String() string {
	return fmt.Sprintf("[PexAddrs %v]", m.Addrs)
}

