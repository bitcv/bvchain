package obj

import (
	"io"
	"fmt"
	"math/big"
	"encoding/binary"
	"halftwo/mangos/xerr"
)

func UvarintSize(n uint64) int {
	var buf [binary.MaxVarintLen64]byte
	return binary.PutUvarint(buf[:], n)
}

type Serializer struct {
	W io.Writer
	N int
	Err error
}

func NewSerializer(w io.Writer) *Serializer {
	return &Serializer{W:w}
}

func (s *Serializer) SetError(err error) {
	if s.Err == nil {
		s.Err = err
	}
}

func (s *Serializer) WriteBigEndian(data interface{}) {
	if s.Err == nil {
		s.Err = binary.Write(s.W, binary.BigEndian, data)
		if s.Err == nil {
			s.N += binary.Size(data)
		}
	}
}

func (s *Serializer) WriteLittleEndian(data interface{}) {
	if s.Err == nil {
		s.Err = binary.Write(s.W, binary.LittleEndian, data)
		if s.Err == nil {
			s.N += binary.Size(data)
		}
	}
}

func (s *Serializer) Write(data []byte) {
	if s.Err == nil && len(data) > 0 {
		var n int
		n, s.Err = s.W.Write(data)
		s.N += n
		if s.Err == nil && n != len(data) {
			s.Err = xerr.Trace(fmt.Errorf("Write less than expected data"))
		}
	}
}

func (s *Serializer) WriteByte(b byte) {
	s.Write([]byte{b})
}

func (s *Serializer) WriteUvarint(n uint64) {
	if s.Err == nil {
		var buf [binary.MaxVarintLen64]byte
		k := binary.PutUvarint(buf[:], n)
		s.Write(buf[:k])
	}
}

func (s *Serializer) WriteVarint(n int64) {
	if s.Err == nil {
		var buf [binary.MaxVarintLen64]byte
		k := binary.PutVarint(buf[:], n)
		s.Write(buf[:k])
	}
}

func (s *Serializer) WriteBigInt(n *big.Int) {
	if s.Err == nil {
		if n == nil {
			s.WriteVarint(0)
		} else {
			bz := n.Bytes()
			k := len(bz)
			if n.Sign() < 0 {
				k = -k
			}
			s.WriteVarint(int64(k))
			s.Write(bz)
		}
	}
}

func (s *Serializer) WriteVariableBytes(data []byte) {
	if s.Err == nil {
		s.WriteUvarint(uint64(len(data)))
		if s.Err == nil {
			s.Write(data)
		}
	}
}


type Deserializer struct {
	R io.Reader
	N int
	Err error
}

func NewDeserializer(r io.Reader) *Deserializer {
	return &Deserializer{R:r}
}

func (d *Deserializer) SetError(err error) {
	if d.Err == nil {
		d.Err = err
	}
}

func (d *Deserializer) ReadBigEndian(data interface{}) {
	if d.Err == nil {
		d.Err = binary.Read(d.R, binary.BigEndian, data)
		if d.Err == nil {
			d.N += binary.Size(data)
		}
	}
}

func (d *Deserializer) ReadLittleEndian(data interface{}) {
	if d.Err == nil {
		d.Err = binary.Read(d.R, binary.LittleEndian, data)
		if d.Err == nil {
			d.N += binary.Size(data)
		}
	}
}

func (d *Deserializer) Read(data []byte) {
	if d.Err == nil && len(data) > 0 {
		var n int
		n, d.Err = io.ReadFull(d.R, data)
		d.N += n
		if d.Err == nil && n != len(data) {
			d.Err = xerr.Trace(fmt.Errorf("Read less than expected data"))
		}
	}
}

func (d *Deserializer) ReadByte() byte {
	var b [1]byte
	d.Read(b[:])
	return b[0]
}

func (d *Deserializer) ReadByteAndCheck(expected byte) {
	b := d.ReadByte()
	if d.Err == nil && b != expected {
		d.Err = xerr.Trace(fmt.Errorf("Read byte %d instead of expected %d", b, expected))
	}
}


func (d *Deserializer) ReadUvarint() (k uint64) {
	if d.Err == nil {
		if br, ok := d.R.(io.ByteReader); ok {
			k, d.Err = binary.ReadUvarint(br)
			if d.Err == nil {
				var buf [binary.MaxVarintLen64]byte
				d.N += binary.PutUvarint(buf[:], k)
				return k
			}
		} else {
			var buf [binary.MaxVarintLen64]byte
			for m := 1; m < len(buf); m++ {
				var x int
				x, d.Err = d.R.Read(buf[m-1:m])
				if d.Err != nil {
					return 0
				} else if x != 1 {
					d.Err = xerr.Trace(fmt.Errorf("Read less than expected"))
					return 0
				}

				k, x = binary.Uvarint(buf[:m])
				if x > 0 {
					d.N += x
					return k
				}
			}
		}
	}
	return k
}

func (d *Deserializer) ReadUvarintAndCheck(min, max uint64) (k uint64) {
	k = d.ReadUvarint()
	if d.Err == nil && (k < min || k > max) {
		d.Err = xerr.Trace(fmt.Errorf("Varint (%d) out of range [%d, %d]", k, min, max))
	}
	return k
}

func (d *Deserializer) ReadVarint() (k int64) {
	if d.Err == nil {
		u := d.ReadUvarint()
		k = int64(u >> 1)
		if u & 0x01 != 0 {
			k = ^k
		}
	}
	return k
}

func (d *Deserializer) ReadVarintAndCheck(min, max int64) (k int64) {
	k = d.ReadVarint()
	if d.Err == nil && (k < min || k > max) {
		d.Err = xerr.Trace(fmt.Errorf("Varint (%d) out of range [%d, %d]", k, min, max))
	}
	return k
}

func (d *Deserializer) ReadBigInt() (n *big.Int) {
	if d.Err == nil {
		k := d.ReadVarint()
		negative := k < 0
		if negative {
			k = -k
		}

		if d.Err == nil {
			if k > 0 {
				bz := make([]byte, k)
				d.Read(bz)
				n = new(big.Int).SetBytes(bz)
			} else {
				n = big.NewInt(0)
			}

			if negative {
				n = new(big.Int).Neg(n)
			}
		}
	}
	return n
}

func (d *Deserializer) ReadVariableBytes(max int, data []byte) []byte {
	if d.Err == nil {
		k := d.ReadUvarint()
		if d.Err != nil {
			return nil
		}

		if k > uint64(max) {
			d.Err = xerr.Trace(fmt.Errorf("Length of data greater than expected"))
			return nil
		}

		if uint64(cap(data)) < k {
			data = make([]byte, k)
		}
		data = data[:k]

		if k > 0 {
			d.Read(data)
		}

		if d.Err == nil {
			return data
		}
	}
	return nil
}


