package op

import (
	"bvchain/obj"
	"bvchain/chain"

	"math/big"
)


type VoteWitnessOperation struct {
	NumWitness uint32
	WitnessIds []int64
}

func (o *VoteWitnessOperation) Kind() Kind {
	return OP_VoteWitness
}

func (o *VoteWitnessOperation) GasPrice() *big.Int {
	return big.NewInt(1)
}

func (o *VoteWitnessOperation) Gas() uint64 {
	return 10000*10000*5	// TEST
}

func (o *VoteWitnessOperation) Cost() *big.Int {
	return new(big.Int).SetUint64(o.Gas())
}

func (o *VoteWitnessOperation) Serialize(s *obj.Serializer) {
	s.WriteByte(byte(o.Kind()))
	s.WriteUvarint(uint64(o.NumWitness))

	s.WriteUvarint(uint64(len(o.WitnessIds)))
	for _, id := range o.WitnessIds {
		s.WriteUvarint(uint64(id))
	}
}

func (o *VoteWitnessOperation) Deserialize(d *obj.Deserializer) {
	d.ReadByteAndCheck(byte(o.Kind()))
	o.NumWitness = uint32(d.ReadUvarintAndCheck(0, chain.MAX_WITNESS_COUNT))
	k := int(d.ReadUvarintAndCheck(0, chain.MAX_WITNESS_COUNT))
	o.WitnessIds = make([]int64, k)
	for i := 0; i < k; i++ {
		id := int64(d.ReadUvarint())
		o.WitnessIds[i] = id
	}
}

