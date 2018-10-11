package ec

import (
	"fmt"
	"hash"
	"sync"
	"encoding/hex"
	"golang.org/x/crypto/sha3"
)

type Hash512 [64]byte
type Hash256 [32]byte

func New512Hasher() hash.Hash {
	return sha3.New512()
}

func New256Hasher() hash.Hash {
	return sha3.New256()
}


var _s512Pool = sync.Pool {
        New: func() interface{} {
                return New512Hasher()
        },
}

func Sum512(msg []byte) (hh Hash512) {
        hasher := _s512Pool.Get().(hash.Hash)
        defer _s512Pool.Put(hasher)

        hasher.Reset()
        hasher.Write(msg)
	hasher.Sum(hh[:0])
        return
}

var _s256Pool = sync.Pool {
        New: func() interface{} {
                return New256Hasher()
        },
}

func Sum256(msg []byte) (hh Hash256) {
        hasher := _s256Pool.Get().(hash.Hash)
        defer _s256Pool.Put(hasher)

        hasher.Reset()
        hasher.Write(msg)
	hasher.Sum(hh[:0])
	return
}

// right aligned
// parameter to must 0 initialized
func bytes_slice2array(from, to []byte) {
	if len(from) > len(to) {
		k := len(from) - len(to)
		from = from[k:]
	}

	copy(to[len(to)-len(from):], from)
}

func BytesToHash512(b []byte) (h Hash512) {
	bytes_slice2array(b, h[:])
	return
}

func BytesToHash256(b []byte) (h Hash256) {
	bytes_slice2array(b, h[:])
	return
}


func musthex2bytes(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(fmt.Sprintf("ec: invalid hex string, s=%s", s))
	}
	return b
}

func MustHexToHash512(s string) (h Hash512) {
	return BytesToHash512(musthex2bytes(s))

}

func MustHexToHash256(s string) (h Hash256) {
	return BytesToHash256(musthex2bytes(s))
}


func (h Hash512) String() string {
	return hex.EncodeToString(h[:])
}

func (h Hash256) String() string {
	return hex.EncodeToString(h[:])
}

