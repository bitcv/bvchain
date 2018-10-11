package op

import (
	"bvchain/ec"
	"bvchain/obj"

	"math/big"
)


const MAX_PAYLOAD_SIZE = 1024 * 1


type TransferOperation struct {
        Price        *big.Int
        GasLimit     uint64
        Recipient    ec.Address
        Amount       *big.Int
        Payload      []byte
}

func (o *TransferOperation) Kind() Kind {
	return OP_Transfer
}

func (o *TransferOperation) GasPrice() *big.Int {
	return o.Price
}

func (o *TransferOperation) Gas() uint64 {
	return o.GasLimit
}

func (o *TransferOperation) Cost() *big.Int {
	total := new(big.Int).Mul(o.Price, new(big.Int).SetUint64(o.GasLimit))
        total.Add(total, o.Amount)
        return total
}

func (o *TransferOperation) Serialize(s *obj.Serializer) {
	s.WriteByte(byte(o.Kind()))
	s.WriteBigInt(o.Price)
	s.WriteUvarint(o.GasLimit)
	s.WriteVariableBytes(o.Recipient[:])
	s.WriteBigInt(o.Amount)
	s.WriteVariableBytes(o.Payload)
}

func (o *TransferOperation) Deserialize(d *obj.Deserializer) {
	d.ReadByteAndCheck(byte(o.Kind()))
	o.Price = d.ReadBigInt()
	o.GasLimit = d.ReadUvarint()
	d.ReadVariableBytes(len(o.Recipient), o.Recipient[:])
	o.Amount = d.ReadBigInt()
	o.Payload = d.ReadVariableBytes(MAX_PAYLOAD_SIZE, nil)
}

