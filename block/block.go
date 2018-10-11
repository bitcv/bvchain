package block

import (
	"bvchain/ec"
	"bvchain/tx"
)

type Id struct {
	Height int64
	Hash ec.Hash256
}

type Header struct {
	Height int64			// 8
	Timestamp int64			// 8
	PrevHash ec.Hash256		// 32
	TrxRoot ec.Hash256		// 32
	StateRoot ec.Hash256		// 32
	WitnessId int64			// 8

	Signature ec.Signature		// 65

	_hash []byte
}

type Block struct {
	Header
	Transactions []tx.Transaction
}


func (h *Header) BlockId() Id {
	id := Id{Height:h.Height}
	copy(id.Hash[:], h.Hash())
	return id
}

func (b *Header) Hash() []byte {
	if len(b._hash) == 0 {
		hasher := ec.New256Hasher()
		// TODO
		b._hash = hasher.Sum(nil)
	}
	return b._hash
}

