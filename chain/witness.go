package chain

import (
	"bvchain/ec"

	"sort"
)

type Witness struct {
	Id int64 `json:"id"`
	Address ec.Address `json:"addr"`
	Url string `json:"url"`
}

func NewWitness(pubKey ec.PubKey) *Witness {
        return &Witness{ Address:pubKey.Address() }
}

func (w *Witness) Clone() *Witness {
	clone := *w
	return &clone
}


type WitnessSet struct {
	Wss []*Witness	`json:"wss"`
}


func NewWitnessSet(witnesses []*Witness) *WitnessSet {
        wss := make([]*Witness, len(witnesses))
        for i, w := range witnesses {
                wss[i] = w.Clone()
        }
        sort.Slice(wss, func (i, j int) bool {
		return wss[i].Id < wss[i].Id
	})
	return &WitnessSet{ Wss:wss }
}

func (set *WitnessSet) HasAddress(address ec.Address) bool {
	for _, ws := range set.Wss {
		if ws.Address == address {
			return true
		}
	}
	return false
}

func (set *WitnessSet) HasId(id int64) bool {
        k := sort.Search(len(set.Wss), func(i int) bool {
                return id <= set.Wss[i].Id
        })
        return k < len(set.Wss) && id == set.Wss[k].Id
}

func (set *WitnessSet) GetById(id int64) (idx int, w *Witness) {
        k := sort.Search(len(set.Wss), func(i int) bool {
                return id <= set.Wss[i].Id
        })
        if k < len(set.Wss) && id == set.Wss[k].Id {
		return k, set.Wss[k]
	}
	return -1, nil
}

func (set *WitnessSet) GetByIndex(idx int) *Witness {
        if idx < 0 || idx >= len(set.Wss) {
                return nil
        }
        return set.Wss[idx]
}

func (set *WitnessSet) Size() int {
        return len(set.Wss)
}

func (set *WitnessSet) Clone() *WitnessSet {
        wss2 := make([]*Witness, len(set.Wss))
	for i, w := range set.Wss {
		wss2[i] = w.Clone()
	}
	return NewWitnessSet(wss2)
}

