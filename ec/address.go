package ec

import (
	"bvchain/util"

	"fmt"
	"bytes"
)

const _MAINNET_ADDRESS_PREFIX = "BvA"
const _TESTNET_ADDRESS_PREFIX = "bVa"
var _isMainNet = true

type Address [20]byte

func (a *Address) SetBytes(b []byte) {
	if len(b) != 20 {
		panic("Length of address bytes must be 20")
	}
	copy(a[:], b)
}

func (a *Address) Compare(a2 Address) int {
	return bytes.Compare(a[:], a2[:])
}

func (a *Address) Equal(a2 Address) bool {
	return bytes.Equal(a[:], a2[:])
}

func BytesToAddress(b []byte) Address {
	var a Address
	a.SetBytes(b)
	return a
}

func getAddressPrefix() string {
	if _isMainNet {
		return _MAINNET_ADDRESS_PREFIX
	}
	return _TESTNET_ADDRESS_PREFIX
}

func (a Address) String() string {
	return util.BytesToBase32Sum(a[:], getAddressPrefix(), 4, true)
}

func StringToAddress(s string) (a Address, err error) {
	b, err := util.Base32SumToBytes(s, getAddressPrefix(), 4, true)
	if err != nil {
		return
	}

	if len(b) != len(a) {
		err = fmt.Errorf("invalid Address string")
		return
	}

	copy(a[:], b)
	return
}

func SetMainNet(main bool) {
	_isMainNet = main
}

