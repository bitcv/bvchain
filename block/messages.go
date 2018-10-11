package block

import (
	"bvchain/p2p"

	"fmt"
	"reflect"
)

func init() {
	man := p2p.TheMK.RegisterChannel(p2p.BLOCK_CHANNEL)
	man.Register(p2p.MessageCode(0x01), reflect.TypeOf((*_StatusRequestMessage)(nil)))
	man.Register(p2p.MessageCode(0x02), reflect.TypeOf((*_StatusInfoMessage)(nil)))
	man.Register(p2p.MessageCode(0x03), reflect.TypeOf((*_BlockRequestMessage)(nil)))
	man.Register(p2p.MessageCode(0x04), reflect.TypeOf((*_BlockMissingMessage)(nil)))
	man.Register(p2p.MessageCode(0x05), reflect.TypeOf((*_BlockDataMessage)(nil)))
}


type _StatusRequestMessage struct {
	Height int64
	Hash []byte
}

func (m *_StatusRequestMessage) String() string {
	return fmt.Sprintf("[StatusRequest %v %x]", m.Height, m.Hash)
}


type _StatusInfoMessage struct {
	Height int64
	Hash []byte
}

func (m *_StatusInfoMessage) String() string {
	return fmt.Sprintf("[StatusInfo %v %x]", m.Height, m.Hash)
}

type _BlockRequestMessage struct {
	Height int64
	Hash []byte
}

func (m *_BlockRequestMessage) String() string {
	return fmt.Sprintf("[BlockRequest %v %x]", m.Height, m.Hash)
}


type _BlockMissingMessage struct {
	Height int64
	Hash []byte
}

func (m *_BlockMissingMessage) String() string {
	return fmt.Sprintf("[BlockMissing %v %x]", m.Height, m.Hash)
}


type _BlockDataMessage struct {
	Block *Block
}

func (m *_BlockDataMessage) String() string {
	return fmt.Sprintf("[BlockData %v %x]", m.Block.Height, m.Block._hash)
}


