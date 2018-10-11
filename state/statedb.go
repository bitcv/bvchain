package state

import (
	"bvchain/ec"
	"bvchain/trie"
	"bvchain/util/log"

	"fmt"
	"sort"
	"math/big"
	"encoding/binary"
)

type _Revision struct {
	id int
	journalIndex int
}

type StateDb struct {
	cdb *trie.CacheDb
	tr *trie.Trie
	cst *ChainState
	accountStates map[ec.Address]*_AccountState
	accountDirty map[ec.Address]struct{}
	witnesses map[int64]*WitnessState
	voters map[int64]*VoterState
	dbErr error

	refund uint64

	journal *_Journal
	revisions []_Revision
	nextRevisionId int
}


func _idKey(bz []byte, prefix string, id int64) []byte {
	var idbuf [8]byte
	binary.BigEndian.PutUint64(idbuf[:], uint64(id))

	bz = bz[:0]
	bz = append(bz, prefix...)
	bz = append(bz, idbuf[:]...)
	return bz
}

func witnessKey(bz []byte, id int64) []byte {
	return _idKey(bz, "witness:", id)
}

func voterKey(bz []byte, id int64) []byte {
	return _idKey(bz, "voter:", id)
}

func accountKey(bz []byte, address []byte) []byte {
	bz = bz[:0]
	bz = append(bz, "account:"...)
	bz = append(bz, address...)
	return bz
}

func NewStateDb(root ec.Hash256, cdb *trie.CacheDb) (*StateDb, error) {
	tr, err := trie.NewWithCacheDb(root, cdb)
	if err != nil {
		return nil, err
	}
	cst := LoadChainState(tr)
	sdb := &StateDb{
		cdb: cdb,
		tr: tr,
		cst: cst,
		accountStates: make(map[ec.Address]*_AccountState),
		accountDirty: make(map[ec.Address]struct{}),
		witnesses: make(map[int64]*WitnessState),
		voters: make(map[int64]*VoterState),
		journal: newJournal(),
	}
	return sdb, nil
}

func (sdb *StateDb) Reset(root ec.Hash256) error {
	tr, err := trie.NewWithCacheDb(root, sdb.cdb)
	if err != nil {
		return err
	}
	cst := LoadChainState(tr)

	sdb.tr = tr
	sdb.cst = cst
	sdb.accountStates = make(map[ec.Address]*_AccountState)
	sdb.accountDirty = make(map[ec.Address]struct{})
	sdb.witnesses = make(map[int64]*WitnessState)
	sdb.voters = make(map[int64]*VoterState)
	sdb.journal = newJournal()
	sdb.clearJournalAndRefund()
	return nil
}

func (sdb *StateDb) ChainState() *ChainState {
	return sdb.cst
}

// Exist reports whether the given account address exists in the state.
func (sdb *StateDb) Exist(addr ec.Address) bool {
	return sdb.getAccountState(addr) != nil
}

// Empty returns whether the state object is either non-existent or empty
func (sdb *StateDb) Empty(addr ec.Address) bool {
	as := sdb.getAccountState(addr)
	return as == nil || as.empty()
}

func (sdb *StateDb) GetAccount(addr ec.Address) *Account {
	as := sdb.getAccountState(addr)
	if as == nil {
		return nil
	}
	return &as.account
}

func (sdb *StateDb) AddBalance(addr ec.Address, amount *big.Int) {
	as := sdb.getOrCreateAccountState(addr)
	if as != nil {
		as.AddBalance(amount)
	}
}

func (sdb *StateDb) SetNonce(addr ec.Address, nonce uint64) {
	as := sdb.getOrCreateAccountState(addr)
	if as != nil {
		as.SetNonce(nonce)
	}
}

func (sdb *StateDb) getOrCreateAccountState(addr ec.Address) *_AccountState {
	as := sdb.getAccountState(addr)
	if as == nil {
		as, _ = sdb.createAccountState(addr)
	}
	return as
}

func (sdb *StateDb) getAccountState(addr ec.Address) *_AccountState {
	if s := sdb.accountStates[addr]; s != nil {
		if s.removed {
			return nil
		}
		return s
	}

	var buf [40]byte
	bz := sdb.tr.Get(accountKey(buf[:], addr[:]))
	if len(bz) == 0 {
		return nil
	}

	var account Account
	if err := account.Unmarshal(bz); err != nil {
		log.Error("Failed to decode state object", "addr", addr, "err", err)
		return nil
	}

	s := newAccountState(sdb, addr, &account)
	sdb.setAccountState(s)
	return s
}

func (sdb *StateDb) setAccountState(as *_AccountState) {
	sdb.accountStates[as.Address()] = as
}

func (sdb *StateDb) removeAccountState(as *_AccountState) {
	as.removed = true
	addr := as.Address()
	sdb.tr.Remove(addr[:])
}

func (sdb *StateDb) updateAccountState(as *_AccountState) {
	addr := as.Address()
	bz := as.Marshal()

	var buf [40]byte
	key := accountKey(buf[:], addr[:])
	sdb.tr.Update(key, bz)
}

func (sdb *StateDb) createAccountState(addr ec.Address) (as, prev *_AccountState) {
	prev = sdb.getAccountState(addr)
	as = newAccountState(sdb, addr, &Account{})
	as.setNonce(0) // sets the object to dirty
	if prev == nil {
		sdb.journal.Append(_CreateAccountChange{addr: &addr})
	} else {
		sdb.journal.Append(_ResetAccountChange{prev: prev})
	}
	sdb.setAccountState(as)
	return as, prev
}

func (sdb *StateDb) CreateAccount(addr ec.Address) {
	as, prev := sdb.createAccountState(addr)
	if prev != nil {
		as.setBalance(prev.account.Balance)
	}
}

func (sdb *StateDb) GetWitnessState(id int64) *WitnessState {
	if w := sdb.witnesses[id]; w != nil {
		return w
	}

	var buf [32]byte
	key := witnessKey(buf[:], id)
	bz := sdb.tr.Get(key)
	if len(bz) == 0 {
		return nil
	}

	w := new(WitnessState)
	if err := w.Unmarshal(bz); err != nil {
		log.Error("Failed to decode witness object", "id", id, "err", err)
		return nil
	}

	sdb.witnesses[id] = w
	return w
}

func (sdb *StateDb) createWitnessState(addr ec.Address, voterId int64) *WitnessState{
	sdb.cst.LastAllocatedWitnessId++
	id := sdb.cst.LastAllocatedWitnessId
	w := &WitnessState{
		WitnessId: id,
		MyVoterId: voterId,
		Address: addr,
	}
	sdb.journal.Append(_CreateWitnessChange{addr: &addr, vid: id, voterId: voterId,})
	sdb.setWitnessState(w)
	// TODO
	return w
}

func (sdb *StateDb) setWitnessState(w *WitnessState) {
	sdb.witnesses[w.WitnessId] = w
}

func (sdb *StateDb) updateWitnessState(w *WitnessState) {
	var buf [32]byte
	key := witnessKey(buf[:], w.WitnessId)
	bz := w.Marshal()
	sdb.tr.Update(key, bz)
}


func (sdb *StateDb) GetVoterState(id int64) *VoterState {
	if v := sdb.voters[id]; v != nil {
		return v
	}

	var buf [32]byte
	key := voterKey(buf[:], id)
	bz := sdb.tr.Get(key)
	if len(bz) == 0 {
		return nil
	}

	v := new(VoterState)
	if err := v.Unmarshal(bz); err != nil {
		log.Error("Failed to decode witness object", "id", id, "err", err)
		return nil
	}

	sdb.voters[id] = v
	return v
}

func (sdb *StateDb) createVoterState(addr ec.Address) *VoterState {
	sdb.cst.LastAllocatedVoterId--
	id := sdb.cst.LastAllocatedVoterId
	v := &VoterState{
		VoterId: id,
		Address: addr,
	}
	sdb.journal.Append(_CreateVoterChange{addr: &addr, vid: id,})
	sdb.setVoterState(v)
	// TODO
	return v
}

func (sdb *StateDb) setVoterState(v *VoterState) {
	sdb.voters[v.VoterId] = v
}

func (sdb *StateDb) removeVoterState(id int64) {
	var buf [32]byte
	key := voterKey(buf[:], id)
	sdb.tr.Remove(key)
}

func (sdb *StateDb) updateVoterState(v *VoterState) {
	var buf [32]byte
	key := voterKey(buf[:], v.VoterId)
	bz := v.Marshal()
	sdb.tr.Update(key, bz)
}


func (sdb *StateDb) GetRefund() uint64 {
	return sdb.refund
}

func (sdb *StateDb) AddRefund(gas uint64) {
	sdb.journal.Append(_RefundChange{prev: sdb.refund})
	sdb.refund += gas
}

func (sdb *StateDb) setError(err error) {
	sdb.dbErr = err
}

func (sdb *StateDb) clearJournalAndRefund() {
	sdb.journal = newJournal()
	sdb.revisions = sdb.revisions[:0]
	sdb.refund = 0
}

// Snapshot returns an identifier for the current revision of the state.
func (sdb *StateDb) Snapshot() int {
	id := sdb.nextRevisionId
	sdb.nextRevisionId++
	sdb.revisions = append(sdb.revisions, _Revision{id, sdb.journal.Length()})
	return id
}

// RevertToSnapshot reverts all state changes made since the given revision.
func (sdb *StateDb) RevertToSnapshot(revid int) {
	// Find the snapshot in the stack of valid snapshots.
	idx := sort.Search(len(sdb.revisions), func(i int) bool {
		return sdb.revisions[i].id >= revid
	})

	if idx == len(sdb.revisions) || sdb.revisions[idx].id != revid {
		panic(fmt.Errorf("revision id %v cannot be reverted", revid))
	}
	snapshot := sdb.revisions[idx].journalIndex

	// Replay the journal to undo changes and remove invalidated snapshots
	sdb.journal.Revert(sdb, snapshot)
	sdb.revisions = sdb.revisions[:idx]
}

func (sdb *StateDb) Commit(removeEmptyAccounts bool) (root ec.Hash256, err error) {
	defer sdb.clearJournalAndRefund()

	for addr := range sdb.journal.addrDirties {
		sdb.accountDirty[addr] = struct{}{}
	}

	for addr, as := range sdb.accountStates {
		_, dirty := sdb.accountDirty[addr]
		if !dirty {
			continue
		}

		delete(sdb.accountDirty, addr)
		if removeEmptyAccounts && as.empty() {
			sdb.removeAccountState(as)
		} else {
			sdb.updateAccountState(as)
		}
	}

	for vid := range sdb.journal.vidDirties {
		if vid > 0 {
			w := sdb.witnesses[vid]
			if w != nil {
				sdb.updateWitnessState(w)
			}
		} else {
			v := sdb.voters[vid]
			if v != nil {
				sdb.updateVoterState(v)
			}
		}
	}

	SaveChainState(sdb.tr, sdb.cst)

	// Write trie changes.                                                                                                                                          
	root, err = sdb.tr.Commit(func(leaf []byte, parent ec.Hash256) error {
		var account Account
		if err := account.Unmarshal(leaf); err != nil {
			return err
		}

		return nil
	})
	//log.Debug("Trie cache stats after commit", "misses", trie.CacheMisses(), "unloads", trie.CacheUnloads())
	return root, err
}


