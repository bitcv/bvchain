package util

import (
	"math"
	"math/rand"
	crand "crypto/rand"
	"encoding/binary"
	"halftwo/mangos/crock32"
)

const _MAX_INT64_FLOAT = float64(math.MaxInt64)

var MyRand *rand.Rand

func init() {
	MyRand = NewRand()
}

func RandomBytes(size int) []byte {
	bz := make([]byte, size)
	if _, err := crand.Read(bz); err != nil {
		MyRand.Read(bz)
	}
	return bz
}

func FillRandomBytes(bz []byte) {
	if _, err := crand.Read(bz); err != nil {
		MyRand.Read(bz)
	}
}

func GenerateRandomId(size int) string {
	if size <= 0 {
		return ""
	}

	ilen := crock32.DecodeLen(size)
	if ilen < 4 {
		ilen = 4
	}

	in := RandomBytes(ilen)
	u32 := binary.BigEndian.Uint32(in)

	out := make([]byte, size)
	crock32.EncodeLower(out, in)
	out[0] = crock32.AlphabetLower[10 + (u32 / (math.MaxUint32 / 22 + 1))]
	return string(out)
}

func RandomInt64() int64 {
	var buf [8]byte
	if _, err := crand.Read(buf[:]); err != nil {
		panic(err)
	}
	buf[0] &= 0x7f
	return int64(binary.BigEndian.Uint64(buf[:]))
}

func RandomInt32() int32 {
	var buf [4]byte
	if _, err := crand.Read(buf[:]); err != nil {
		panic(err)
	}
	buf[0] &= 0x7f
	return int32(binary.BigEndian.Uint32(buf[:]))
}

func RandomInt() int {
	u := uint(RandomInt64())
	return int(u << 1 >> 1)
}

func RandomInt32n(n int32) int32 {
	if n & (n-1) == 0 { // n is power of two, can mask
		return RandomInt32() & (n - 1)
	}

	max := int32((1 << 31) - 1 - (1<<31)%uint32(n))
	v := RandomInt32()
	for v > max {
		v = RandomInt32()
	}
	return v % n
}

func RandomInt64n(n int64) int64 {
	if n & (n-1) == 0 { // n is power of two, can mask
		return RandomInt64() & (n - 1)
	}

	max := int64((1 << 63) - 1 - (1<<63)%uint64(n))
	v := RandomInt64()
	for v > max {
		v = RandomInt64()
	}
	return v % n
}

func RandomIntn(n int) int {
	if n <= 0 {
		panic("n must be positive")
	}
	if n <= (1<<31) - 1 {
		return int(RandomInt32n(int32(n)))
	}
	return int(RandomInt64n(int64(n)))
}

func RandomFloat64() float64 {
	return float64(RandomInt64()) / _MAX_INT64_FLOAT
}

func NewRand() *rand.Rand {
	return rand.New(rand.NewSource(RandomInt64()))
}

