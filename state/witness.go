package state

import (
	"bvchain/ec"
	"bvchain/obj"

	"fmt"
	"math"
	"bytes"
)

const MAX_WITNESS_URL_LEN = 255


type WitnessState struct {
	WitnessId int64		// positive 
	MyVoterId int64		// 0 or negative
        Address ec.Address
        Url string
        VoteCount uint64
        MissedCount uint64
        LastConfirmedBlockHeight uint64
}

func (w *WitnessState) Clone() *WitnessState {
	w2 := *w
	return &w2
}

func (w *WitnessState) SetUrl(url string, sdb *StateDb) {
	if w.Url != url {
		sdb.journal.Append(_WitnessUrlChange{
			vid: w.WitnessId,
			prev: w.Url,
		})
		w.Url = url
	}
}

func (w *WitnessState) Marshal() []byte {
	var bf bytes.Buffer
	s := obj.NewSerializer(&bf)
	s.WriteVarint(w.WitnessId)
	s.WriteVarint(w.MyVoterId)
	s.WriteVariableBytes(w.Address[:])
	s.WriteVariableBytes([]byte(w.Url))
	s.WriteUvarint(w.VoteCount)
	s.WriteUvarint(w.MissedCount)
	s.WriteUvarint(w.LastConfirmedBlockHeight)
	return bf.Bytes()
}

func (w *WitnessState) Unmarshal(bz []byte) error {
	bf := bytes.NewBuffer(bz)
	d := obj.NewDeserializer(bf)
	w.WitnessId = d.ReadVarintAndCheck(1, math.MaxInt64)
	w.MyVoterId = d.ReadVarintAndCheck(math.MinInt64, 0)

	buf := d.ReadVariableBytes(32, nil)
	if len(buf) != len(w.Address) {
		return fmt.Errorf("Invalid Address unmarshaled for WitnessState")
	}
	copy(w.Address[:], buf)

	w.Url = string(d.ReadVariableBytes(MAX_WITNESS_URL_LEN, nil))
	w.VoteCount = d.ReadUvarint()
	w.MissedCount = d.ReadUvarint()
	w.LastConfirmedBlockHeight = d.ReadUvarint()
	if d.N < len(bz) {
		return fmt.Errorf("More data than expected when unmarshaling WitnessState data")
	}
	return d.Err
}

