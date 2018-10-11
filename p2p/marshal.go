package p2p

import (
	"bvchain/util"

	"fmt"
	"bytes"
	"reflect"
	"halftwo/mangos/vbs"
)

type MessageCode byte

var TheMK = NewMarshalKeeper()

type MarshalKeeper interface {
	RegisterChannel(ch byte) ChannelMan
	GetChannelMan(ch byte) ChannelMan
	MustMarshal(ch byte, msg interface{}) []byte
	Unmarshal(ch byte, bz []byte) (interface{}, error)
}

type ChannelMan interface {
	Register(MessageCode, reflect.Type) error
	Marshal(interface{}) ([]byte, error)
	Unmarshal([]byte) (interface{}, error)
}

type _ChannelMan struct {
	c2t map[MessageCode]reflect.Type
	t2c map[reflect.Type]MessageCode
}

type _MarshalKeeper struct {
	ch2man map[byte]ChannelMan
}


func NewMarshalKeeper() MarshalKeeper {
	mk := &_MarshalKeeper{}
	mk.ch2man = make(map[byte]ChannelMan)
	return mk
}

func (mk *_MarshalKeeper) MustMarshal(ch byte, msg interface{}) []byte {
	man := mk.GetChannelMan(ch)
	if man == nil {
		panic(fmt.Errorf("Unknown channel(%d)", ch))
	}

	bz, err := man.Marshal(msg)
	util.AssertNoError(err)
	return bz
}

func (mk *_MarshalKeeper) Unmarshal(ch byte, bz []byte) (interface{}, error) {
	man := mk.GetChannelMan(ch)
	if man == nil {
		return nil, fmt.Errorf("Unknown channel(%d)", ch)
	}
	return man.Unmarshal(bz)
}

func (mk *_MarshalKeeper) GetChannelMan(ch byte) ChannelMan {
	return mk.ch2man[ch]
}

func (mk *_MarshalKeeper) RegisterChannel(ch byte) ChannelMan {
	if _, ok := mk.ch2man[ch]; ok {
		panic(fmt.Sprintf("ChannelId(%d) already registered", ch))
	}
	man := &_ChannelMan{make(map[MessageCode]reflect.Type), make(map[reflect.Type]MessageCode)}
	mk.ch2man[ch] = man
	return man
}

func (cm *_ChannelMan) Register(c MessageCode, t reflect.Type) error {
	if _, ok := cm.c2t[c]; ok {
		return fmt.Errorf("MessageCode(%x) already registered", c)
	}
	if _, ok := cm.t2c[t]; ok {
		return fmt.Errorf("Message already registered, type=%v", t)
	}
	cm.c2t[c] = t
	cm.t2c[t] = c
	return nil
}

func (cm *_ChannelMan) Marshal(msg interface{}) ([]byte, error) {
	t := reflect.TypeOf(msg)
	c, ok := cm.t2c[t]
	if !ok {
		panic(fmt.Sprintf("Unregistered msg type %v", msg))
	}

	buf := bytes.NewBuffer([]byte{})
        enc := vbs.NewEncoder(buf)
	buf.WriteByte(byte(c))
	err := enc.Encode(msg)
	return buf.Bytes(), err
}

func (cm *_ChannelMan) Unmarshal(bz []byte) (interface{}, error) {
	if len(bz) == 0 {
		panic("No data to unmarshal")
	}

	t, ok := cm.c2t[MessageCode(bz[0])]
	if !ok {
		return nil, fmt.Errorf("Invalid MessageCode(%x)", bz[0])
	}

	ptr := reflect.New(t)
	err := vbs.Unmarshal(bz[1:], ptr.Interface())
	if err != nil {
		return nil, err
	}
	msg := ptr.Elem().Interface()
	return msg, nil
}

