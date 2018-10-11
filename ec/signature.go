package ec

import (
	"bvchain/util"

	"strconv"
	"fmt"
	"bytes"
	"math/big"
	"crypto/ecdsa"
	"halftwo/mangos/xstr"
	secp256k1 "github.com/btcsuite/btcd/btcec"
)

const SignaturePrefix = "BvS"

var (
	secp256k1N, _  = new(big.Int).SetString("fffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141", 16)
	secp256k1Nhalf = new(big.Int).Div(secp256k1N, big.NewInt(2))
)

type Signature [65]byte


func (sg *Signature) IsZero() bool {
	return xstr.IndexNotByte(sg[:], 0) == -1
}

func (sg *Signature) RecoverHashPubKey(hash []byte) (p PubKey, err error) {
	if len(hash) != 32 {
		panic("len of hash must be 32")
	}
	pub, _, err := secp256k1.RecoverCompact(secp256k1.S256(), sg[:], hash)
	if err != nil {
		return
	}
	bytes := (*secp256k1.PublicKey)(pub).SerializeCompressed()
	copy(p[:], bytes)
	return
}

func (sg *Signature) RecoverMessagePubKey(msg []byte) (p PubKey, err error) {
	hash := Sum256(msg)
	return sg.RecoverHashPubKey(hash[:])
}

func (sg *Signature) VerifyHashPubKey(hash []byte, p PubKey) bool {
	if len(hash) != 32 {
		panic("len of hash must be 32")
	}
	pub, err := secp256k1.ParsePubKey(p[:], secp256k1.S256())
	if err != nil {
		return false
	}
	return sg.verifyEcdsaPublicKey(hash, (*ecdsa.PublicKey)(pub))
}

func (sg *Signature) VerifyMessagePubKey(msg []byte, p PubKey) bool {
	hash := Sum256(msg)
	return sg.VerifyHashPubKey(hash[:], p)
}

func (sg *Signature) verifyEcdsaPublicKey(hash []byte, pub *ecdsa.PublicKey) bool {
	R := new(big.Int).SetBytes(sg[1:33])
	S := new(big.Int).SetBytes(sg[33:65])
        return ecdsa.Verify(pub, hash, R, S)
}

func (sg *Signature) Equal(s2 Signature) bool {
	return bytes.Equal(sg[:], s2[:])
}

func (sg Signature) String() string {
	return util.BytesToBase32Sum(sg[:], SignaturePrefix, 4, true)
}


func StringToSignature(str string) (sg Signature, err error) {
	b, err := util.Base32SumToBytes(str, SignaturePrefix, 4, true)
	if err != nil {
		return
	}

	if len(b) != len(sg) {
		err = fmt.Errorf("invalid PubKey string")
		return
	}

	copy(sg[:], b)
	return
}

func (s *Signature) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(s.String())), nil
}

func (s *Signature) UnmarshalJSON(bz []byte) error {
	if len(bz) < 2 || bz[0] != '"' || bz[len(bz)-1] != '"' {
		return fmt.Errorf("Invalid Signature string");
	}

	sig, err := StringToSignature(string(bz[1:len(bz)-1]))
	if err != nil {
		return err
	}

	*s = sig
	return nil
}

