package consensus

import (
	"bvchain/state"
	"bvchain/chain"
	"bvchain/util"

	"sort"
)

type Maintainer struct {
	params *chain.ChainParams
	sdb *state.StateDb
	cst *state.ChainState
	currentWss []*chain.Witness
}

func NewMaintainer(params *chain.ChainParams, sdb *state.StateDb) *Maintainer {
	m := &Maintainer{
		params: params,
		sdb: sdb,
	}

	//TODO
	m.cst = nil
	return m
}


func (m *Maintainer) UpdateActiveWitnessSet() {
	// TODO
}

func (m *Maintainer) UpdateDynamicData() {
	// TODO
}

func (m *Maintainer) GetScheduledWitness(slot int) *chain.Witness {
	n := m.cst.LastBlockAbsoluteSlot + int64(slot)
	return m.currentWss[n % int64(len(m.currentWss))]
}

func (m *Maintainer) GetTimeBySlot(slot int) int64 {
	if slot == 0 {
		return 0
	}

	interval := m.params.BlockInterval

	head_aslot := m.cst.LastBlockTime / int64(interval)
	head_time := head_aslot * int64(interval)
	if m.cst.DuringMaintenance {
		slot += m.params.MaintenanceSkipSlots
	}

	return head_time + int64(slot * interval)
}

func (m *Maintainer) GetSlotByTime(when int64) int {
	firstTime := m.GetTimeBySlot(1)
	if when < firstTime {
		return 0
	}

	interval := m.params.BlockInterval
	return int(when - firstTime) / interval + 1
}

func (m *Maintainer) UpdateWitnessSchedule() {
	cst := m.cst
	num := cst.Wset.Size()
	if cst.LastBlockHeight % int64(num) == 0 {
		wss := m.currentWss[:0]
		for i := 0; i < num; i++ {
			wss = append(wss, cst.Wset.GetByIndex(i))
		}

		now_hi := uint64(cst.LastBlockTime) << 32
		for i := 0; i < num; i++ {
			k := now_hi + uint64(i) * 2685821657736338717
			k ^= (k >> 12);
			k ^= (k << 25);
			k ^= (k >> 27);
			k *= 2685821657736338717

			jmax := uint64(num - i)
			j := i + int(k % jmax)
			wss[i], wss[j] = wss[j], wss[i]
		}

		m.currentWss = wss
	}
}

type _WitnessVote struct {
	wid int64
	vote int64
}


func (m *Maintainer) PerformChainMaintenance(blockHeight int64, blockTime int64) {

	lastWitnessId := m.cst.LastAllocatedWitnessId
	lastVoterId := m.cst.LastAllocatedVoterId
	maxWitnessCount := m.params.MaxWitnessCount
	minWitnessCount := m.params.MinWitnessCount

	voteTally := make([]_WitnessVote, lastWitnessId)
	for i := 0; i < len(voteTally); i++ {
		voteTally[i].wid = int64(i + 1)
	}
	numWitnessTally := make([]int64, maxWitnessCount)
	totalVotingStake := int64(0)
	for i := int64(-1); i >= lastVoterId; i-- {
		voter := m.sdb.GetVoterState(i)
		account := m.sdb.GetAccount(voter.Address)
		votingStake := account.Balance.Int64()

		if voter.MyWitnessId > 0 {
			wid := voter.MyWitnessId
			util.Assert(account.Vid == wid)
			if wid > lastWitnessId {
				continue
			}
			voteTally[wid-1].vote += votingStake
			totalVotingStake += votingStake
		} else {
			util.Assert(account.Vid == i)
			num := int(voter.NumWitness)
			if num == 0 {
				num = len(voter.WitnessIds)
			}

			wids := voter.WitnessIds

			for _, wid := range wids {
				if wid <= 0 || wid > lastWitnessId {
					continue
				}
				voteTally[wid-1].vote += votingStake
			}
			totalVotingStake += votingStake

			if num > maxWitnessCount {
				num = maxWitnessCount
			} else if num < 1 {
				num = 1
			}

			numWitnessTally[num-1] += votingStake
		}
	}

	stakeTarget := totalVotingStake / 2
	numWitness := 0
	for stake := int64(0); stake < stakeTarget && numWitness < len(numWitnessTally); numWitness++ {
		stake += numWitnessTally[numWitness]
	}

	if numWitness % 2 == 0 && numWitness < maxWitnessCount {
		numWitness++
	}

	if numWitness < minWitnessCount {
		numWitness = minWitnessCount
	}

	sort.Slice(voteTally, func (i, j int) bool {
		if voteTally[i].vote > voteTally[j].vote {
			return true
		} else if voteTally[i].vote == voteTally[j].vote {
			return voteTally[i].wid < voteTally[j].wid
		}
		return false
	})

	wss := make([]*chain.Witness, 0, numWitness)
	for i := 0; i < len(voteTally); i++ {
		id := voteTally[i].wid
		cand := m.sdb.GetWitnessState(id)
		account := m.sdb.GetAccount(cand.Address)
		if account.Vid != id {
			panic("The witnessId and address not match")
		}

		witness := &chain.Witness{Id:cand.WitnessId, Address:cand.Address, Url:cand.Url}
		wss = append(wss, witness)
		if len(wss) == numWitness {
			break
		}
	}
	m.cst.Wset = chain.NewWitnessSet(wss)

	// TODO
	nextMaintTime := m.cst.NextMaintenanceTime
	if nextMaintTime <= blockTime {
		maintInterval := int64(m.params.MaintenanceInterval)
		if blockHeight == 1 {
			nextMaintTime = (blockTime / maintInterval + 1) * maintInterval
		} else {
			k := (m.cst.LastBlockTime - nextMaintTime) / maintInterval
			nextMaintTime += (k + 1) * maintInterval
		}
	}

	// TODO
	m.cst.NextMaintenanceTime = nextMaintTime
}

