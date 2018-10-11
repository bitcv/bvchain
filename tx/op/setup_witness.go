package op

import (
	"bvchain/obj"

	"math/big"
)

const MAX_URL_LEN = 255

type SetupWitnessOperation struct {
	Url string
}

func (o *SetupWitnessOperation) Kind() Kind {
	return OP_SetupWitness
}

func (o *SetupWitnessOperation) GasPrice() *big.Int {
	return big.NewInt(1)
}

func (o *SetupWitnessOperation) Gas() uint64 {
	return 10000*10000*1000	// TEST
}

func (o *SetupWitnessOperation) Cost() *big.Int {
	return new(big.Int).SetUint64(o.Gas())
}

func (o *SetupWitnessOperation) Serialize(s *obj.Serializer) {
	s.WriteByte(byte(o.Kind()))
	s.WriteVariableBytes([]byte(o.Url))
}

func (o *SetupWitnessOperation) Deserialize(d *obj.Deserializer) {
	d.ReadByteAndCheck(byte(o.Kind()))
	o.Url = string(d.ReadVariableBytes(MAX_URL_LEN, nil))
}

