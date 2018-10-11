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

const PrivKeyPrefix = "BvK"

type PrivKey [32]byte

func NewPrivKey() (k PrivKey) {
	priv, err := secp256k1.NewPrivateKey(secp256k1.S256())
	if err != nil {
		panic(err)
	}
	copy(k[:], priv.Serialize())
	return
}

func BytesToPrivKey(key []byte) (k PrivKey) {
	if len(key) < 32 {
		panic("length of key too short")
	}
	copy(k[:], key)
	return
}

func EcdsaToPrivKey(priv *ecdsa.PrivateKey) (k PrivKey) {
	if priv.Curve != secp256k1.S256() {
		panic("PrivateKey curve is not secp256k1")
	}
	copy(k[:], (*secp256k1.PrivateKey)(priv).Serialize())
	return
}

func (k *PrivKey) IsZero() bool {
	return xstr.IndexNotByte(k[:], 0) == -1
}

func (k *PrivKey) ToEcdsa() *ecdsa.PrivateKey {
        key, _ := secp256k1.PrivKeyFromBytes(secp256k1.S256(), k[:])
	return key.ToECDSA()
}

func (k *PrivKey) SignHash(hash []byte) (sg Signature, err error) {
	if len(hash) != 32 {
		panic("len of hash must be 32")
	}
        priv, _ := secp256k1.PrivKeyFromBytes(secp256k1.S256(), k[:])
        sig, err := secp256k1.SignCompact(secp256k1.S256(), priv, hash, false)
        if err != nil {
		return
        }
	copy(sg[:], sig)
        return
}

func (k *PrivKey) SignMessage(msg []byte) (sg Signature, err error) {
	hash := Sum256(msg)
	return k.SignHash(hash[:])
}

func (k *PrivKey) PubKey() (p PubKey) {
        _, pub := secp256k1.PrivKeyFromBytes(secp256k1.S256(), k[:])
        copy(p[:], pub.SerializeCompressed())
	return
}

func (k *PrivKey) SharedSecret(p PubKey) ([]byte, error) {
	pubkey, err := p.ToEcdsa()
	if err != nil {
		return nil, err
	}

	x, y := pubkey.Curve.ScalarMult(pubkey.X, pubkey.Y, k[:])
	var buf [64]byte
	xstr.RightAlignedCopy(buf[:32], x.Bytes())
	xstr.RightAlignedCopy(buf[32:], y.Bytes())
	h := Sum512(buf[:])
	return h[:], nil
}

func (k *PrivKey) Equal(k2 PrivKey) bool {
	return bytes.Equal(k[:], k2[:])
}

func (k PrivKey) String() string {
	return util.BytesToBase32Sum(k[:], PrivKeyPrefix, 4, true)
}

func StringToPrivKey(s string) (k PrivKey, err error) {
	b, err := util.Base32SumToBytes(s, PrivKeyPrefix, 4, true)
	if err != nil {
		return
	}

	if len(b) != len(k) {
		err = fmt.Errorf("invalid PrivKey string")
		return
	}

	copy(k[:], b)
	return
}

func (k *PrivKey) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(k.String())), nil
}

func (k *PrivKey) UnmarshalJSON(bz []byte) error {
	if len(bz) < 2 || bz[0] != '"' || bz[len(bz)-1] != '"' {
		return fmt.Errorf("Invalid PrivKey string");
	}

	key, err := StringToPrivKey(string(bz[1:len(bz)-1]))
	if err != nil {
		return err
	}

	*k = key
	return nil
}

