package tx

import (
	"bvchain/tx/op"
	"bvchain/ec"
	"bvchain/obj"

	"bytes"
	"math/big"
	"halftwo/mangos/xstr"
)


type Transaction struct {
	op.Operation
	Nonce uint64
	Signature ec.Signature

	_address ec.Address
}

func (t *Transaction) SenderAddress() ec.Address {
	return t._address
}

func (t *Transaction) GasPrice() *big.Int {
	return t.Operation.GasPrice()
}

func (t *Transaction) Gas() uint64 {
	return t.Operation.Gas()
}

func (t *Transaction) Cost() *big.Int {
	return t.Operation.Cost()
}

func MarshalTransaction(t *Transaction) []byte {
	var buf []byte
	w := xstr.NewBytesWriter(&buf)
	s := obj.NewSerializer(w)
	t.Operation.Serialize(s)
	s.WriteUvarint(t.Nonce)
	s.WriteVariableBytes(t.Signature[:])
	return buf
}

func UnmarshalTransaction(bz []byte) (*Transaction, error) {
	operation, k, err := op.UnmarshalOperation(bz)
	if err != nil {
		return nil, err
	}

	t := &Transaction{Operation:operation}
	bf := bytes.NewBuffer(bz[k:])
	d := obj.NewDeserializer(bf)
	t.Nonce = d.ReadUvarint()
	d.ReadVariableBytes(len(t.Signature), t.Signature[:])
	if d.Err != nil {
		return nil, d.Err
	}

	pubkey, err := t.Signature.RecoverMessagePubKey(bz[:k])
	if err != nil {
		return nil, err
	}
	t._address = pubkey.Address()
	return t, nil
}

