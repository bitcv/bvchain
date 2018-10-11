package block

import (
	"bvchain/obj"

	"io"
)

func (h *Header) Serialize(w io.Writer) error {
	s := obj.Serializer{W:w}
	s.WriteBigEndian(h.Height)
	s.WriteBigEndian(h.Timestamp)
	s.Write(h.PrevHash[:])
	s.Write(h.TrxRoot[:])
	s.Write(h.StateRoot[:])
	s.WriteBigEndian(h.WitnessId)
	s.Write(h.Signature[:])
	return s.Err
}

func (h *Header) Deserialize(r io.Reader) error {
	d := obj.Deserializer{R:r}
	d.ReadBigEndian(&h.Height)
	d.ReadBigEndian(&h.Timestamp)
	d.Read(h.PrevHash[:])
	d.Read(h.TrxRoot[:])
	d.Read(h.StateRoot[:])
	d.ReadBigEndian(&h.WitnessId)
	d.Read(h.Signature[:])
	return d.Err
}

func (b *Block) Serialize(w io.Writer) error {
	b.Header.Serialize(w)
	// TODO
	return nil
}

func (b *Block) Deserialize(r io.Reader) error {
	b.Header.Deserialize(r)
	// TODO
	return nil
}

