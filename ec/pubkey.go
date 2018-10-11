package ec

import (
	"bvchain/util"

	"strconv"
	"fmt"
	"bytes"
	"crypto/ecdsa"
	"halftwo/mangos/xstr"
	secp256k1 "github.com/btcsuite/btcd/btcec"
)

const PubKeyPrefix = "BvP"

type PubKey [33]byte


func EcdsaToPubKey(pub *ecdsa.PublicKey) (p PubKey) {
	if pub.Curve != secp256k1.S256() {
		panic("PublicKey curve is not secp256k1")
	}
	copy(p[:], (*secp256k1.PublicKey)(pub).SerializeCompressed())
	return
}

func (p *PubKey) IsZero() bool {
	return xstr.IndexNotByte(p[:], 0) == -1
}

func (p *PubKey) ToEcdsa() (*ecdsa.PublicKey, error) {
	key, err := secp256k1.ParsePubKey(p[:], secp256k1.S256())
	if err != nil {
		return nil, err
	}
	return key.ToECDSA(), nil
}

func (p *PubKey) Address() (addr Address) {
	s256 := Sum256(p[:])
	copy(addr[:], s256[:])
	return
}

func (p *PubKey) Equal(p2 PubKey) bool {
	return bytes.Equal(p[:], p2[:])
}

func (p PubKey) String() string {
	return util.BytesToBase32Sum(p[:], PubKeyPrefix, 4, true)
}


func StringToPubKey(s string) (p PubKey, err error) {
	b, err := util.Base32SumToBytes(s, PubKeyPrefix, 4, true)
	if err != nil {
		return
	}

	if len(b) != len(p) {
		err = fmt.Errorf("invalid PubKey string")
		return
	}

	copy(p[:], b)
	return
}

func (p *PubKey) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(p.String())), nil
}

func (p *PubKey) UnmarshalJSON(bz []byte) error {
	if len(bz) < 2 || bz[0] != '"' || bz[len(bz)-1] != '"' {
		return fmt.Errorf("Invalid PubKey string");
	}

	pkey, err := StringToPubKey(string(bz[1:len(bz)-1]))
	if err != nil {
		return err
	}

	*p = pkey
	return nil
}

