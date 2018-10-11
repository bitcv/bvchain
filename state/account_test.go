package state

import (
	"bvchain/util"

	"testing"
	"math/big"
)

func TestAccount(t *testing.T) {
	var x Account
	x.Nonce = uint64(util.RandomInt64())
	x.Vid = util.RandomInt64()
	x.Balance = new(big.Int).SetBytes(util.RandomBytes(util.RandomIntn(32)))
	x.StoreRoot = util.RandomBytes(util.RandomIntn(32))
	x.CodeHash = util.RandomBytes(util.RandomIntn(32))

	bz := x.Marshal()
	y, err := UnmarshalAccount(bz)

	if err != nil || !x.equal(y) {
		t.Fatalf("Marshal or unmarshal error")
	}
}

