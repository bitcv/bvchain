package op

import (
	"bvchain/obj"

	"fmt"
	"bytes"
	"math/big"
	"halftwo/mangos/xstr"
)

type Kind uint8

const (
	OP_Transfer = Kind(0x01)
	OP_SetupWitness = Kind(0x80)
	OP_VoteWitness = Kind(0x81)
)

type Operation interface {
	Kind() Kind
	GasPrice() *big.Int
	Gas() uint64
	Cost() *big.Int
	Serialize(s *obj.Serializer)
	Deserialize(d *obj.Deserializer)
}


type Result interface {
}


func MarshalOperation(o Operation) ([]byte, error) {
	var buf []byte
	w := xstr.NewBytesWriter(&buf)
	s := obj.NewSerializer(w)
	o.Serialize(s)
	return buf, s.Err
}

func UnmarshalOperation(bz []byte) (Operation, int, error) {
	if len(bz) < 1 {
		return nil, 0, fmt.Errorf("Can't unmarshal empty blob to operation")
	}

	var o Operation
	switch Kind(bz[0]) {
	case OP_Transfer:
		o = &TransferOperation{}
	case OP_SetupWitness:
		o = &SetupWitnessOperation{}
	case OP_VoteWitness:
		o = &VoteWitnessOperation{}
	default:
                return nil, 0, fmt.Errorf("Unknown Operation")
	}

	bf := bytes.NewBuffer(bz)
	d := obj.NewDeserializer(bf)
	o.Deserialize(d)
	if d.Err != nil {
		return nil, d.N, d.Err
	}
        return o, d.N, nil
}

