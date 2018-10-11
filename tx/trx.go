package tx

import (
	"bvchain/ec"
)

type Trx []byte
type Trxs []Trx


func (trx Trx) Hash() []byte {
	hash := ec.Sum256(trx)
	return hash[:]
}

