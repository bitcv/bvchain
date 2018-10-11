package state

import (
	"bvchain/ec"
	"bvchain/obj"

	"fmt"
	"math"
	"bytes"
)


type VoterState struct {
	VoterId int64		// negative
	MyWitnessId int64	// 0 or positive
	Address ec.Address
	NumWitness uint32
	WitnessIds []int64
}

func (v *VoterState) SetNumWitness(num uint32, sdb *StateDb) {
	if v.NumWitness != num {
		sdb.journal.Append(_VoterNumWitnessChange{
			vid: v.VoterId,
			prev: v.NumWitness,
		})
		v.NumWitness = num
	}
}

func (v *VoterState) SetWitnessIds(ids []int64, sdb *StateDb) {
	sdb.journal.Append(_VoterWitnessIdsChange{
		vid: v.VoterId,
		prev: v.WitnessIds,
	})
	v.WitnessIds = ids
}

func (v *VoterState) Marshal() []byte {
	var bf bytes.Buffer
	s := obj.NewSerializer(&bf)
	s.WriteVarint(v.VoterId)
	s.WriteVarint(v.MyWitnessId)
	s.WriteVariableBytes(v.Address[:])
	s.WriteUvarint(uint64(v.NumWitness))
	s.WriteUvarint(uint64(len(v.WitnessIds)))
	for _, id := range v.WitnessIds {
		s.WriteVarint(id)
	}
	return bf.Bytes()
}

func (v *VoterState) Unmarshal(bz []byte) error {
	bf := bytes.NewBuffer(bz)
	d := obj.NewDeserializer(bf)
	v.VoterId = d.ReadVarintAndCheck(math.MinInt64, -1)
	v.MyWitnessId = d.ReadVarintAndCheck(0, math.MaxInt64)

	buf := d.ReadVariableBytes(32, nil)
	if len(buf) != len(v.Address) {
		return fmt.Errorf("Invalid Address unmarshaled for VoterState")
	}
	copy(v.Address[:], buf)

	v.NumWitness = uint32(d.ReadUvarint())

	n := int(d.ReadUvarint())
	if n < 0 {
		return fmt.Errorf("Invalid WitnessIds unmarshaled for VoterState")
	}

	v.WitnessIds = make([]int64, n)
	for i := 0; i < n; i++ {
		id := d.ReadVarint()
		if id <= 0 {
			return fmt.Errorf("Invalid WitnessIds unmarshaled for VoterState")
		}
		v.WitnessIds[i] = id
	}
	if d.N < len(bz) {
		return fmt.Errorf("More data than expected when unmarshaling VoterState data")
	}
	return d.Err
}

