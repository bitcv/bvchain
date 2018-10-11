package state

import (
	"bvchain/ec"

	"math/big"
)

// journalEntry is a modification entry in the state change journal that can be
// reverted on demand.
type _JournalEntry interface {
	// revert undoes the changes introduced by this journal entry.
	revert(*StateDb)

	// Address returns the address modified by this journal entry.
	Address() *ec.Address

	// Vid returns the vid modified by this journal entry.
	Vid() int64
}

// journal contains the list of state modifications applied since the last state
// commit. These are tracked to be able to be reverted in case of an execution
// exception or revertal request.
type _Journal struct {
	entries []_JournalEntry
	addrDirties map[ec.Address]int
	vidDirties map[int64]int
}

// newJournal create a new initialized journal.
func newJournal() *_Journal {
	return &_Journal{
		addrDirties: make(map[ec.Address]int),
		vidDirties: make(map[int64]int),
	}
}

// append inserts a new modification entry to the end of the change journal.
func (j *_Journal) Append(entry _JournalEntry) {
	j.entries = append(j.entries, entry)
	if addr := entry.Address(); addr != nil {
		j.addrDirties[*addr]++
	}

	if vid := entry.Vid(); vid != 0 {
		j.vidDirties[vid]++
	}
}

// revert undoes a batch of journalled modifications along with any reverted
// dirty handling too.
func (j *_Journal) Revert(sdb *StateDb, snapshot int) {
	for i := len(j.entries) - 1; i >= snapshot; i-- {
		entry := j.entries[i]

		// Undo the changes made by the operation
		entry.revert(sdb)

		// Drop any dirty tracking induced by the change
		if addr := entry.Address(); addr != nil {
			if j.addrDirties[*addr]--; j.addrDirties[*addr] == 0 {
				delete(j.addrDirties, *addr)
			}
		}

		if vid := entry.Vid(); vid != 0 {
			if j.vidDirties[vid]--; j.vidDirties[vid] == 0 {
				delete(j.vidDirties, vid)
			}
		}
	}
	j.entries = j.entries[:snapshot]
}

func (j *_Journal) Dirty(addr ec.Address) {
	j.addrDirties[addr]++
}

// length returns the current number of entries in the journal.
func (j *_Journal) Length() int {
	return len(j.entries)
}

// Changes to the account trie.

type _BaseChange struct {
}

func (_BaseChange) Address() *ec.Address { return nil }
func (_BaseChange) Vid() int64 { return 0 }

type _CreateAccountChange struct {
	_BaseChange
	addr *ec.Address
}

type _ResetAccountChange struct {
	_BaseChange
	prev *_AccountState
}

type _SuicideChange struct {
	_BaseChange
	addr *ec.Address
	prev bool // whether account had already suicided
	prevBalance *big.Int
}

// Changes to individual accounts.
type _BalanceChange struct {
	_BaseChange
	addr *ec.Address
	prev *big.Int
}

type _NonceChange struct {
	_BaseChange
	addr *ec.Address
	prev uint64
}

type _VidChange struct {
	_BaseChange
	addr *ec.Address
	prev int64
}

type _StorageChange struct {
	_BaseChange
	addr *ec.Address
	key ec.Hash256
	prevValue ec.Hash256
}

type _CodeChange struct {
	_BaseChange
	addr *ec.Address
	prevCode []byte
	prevHash []byte
}

// Changes to other state values.
type _RefundChange struct {
	_BaseChange
	prev uint64
}

type _TouchChange struct {
	_BaseChange
	addr *ec.Address
	prev bool
	prevDirty bool
}

type _CreateWitnessChange struct {
	_BaseChange
	addr *ec.Address
	vid int64
	voterId int64
}

type _CreateVoterChange struct {
	_BaseChange
	addr *ec.Address
	vid int64
}

type _WitnessUrlChange struct {
	_BaseChange
	vid int64
	prev string
}

type _VoterNumWitnessChange struct {
	_BaseChange
	vid int64
	prev uint32
}

type _VoterWitnessIdsChange struct {
	_BaseChange
	vid int64
	prev []int64
}

func (ch _CreateAccountChange) revert(sdb *StateDb) {
	delete(sdb.accountStates, *ch.addr)
	delete(sdb.accountDirty, *ch.addr)
}

func (ch _CreateAccountChange) Address() *ec.Address {
	return ch.addr
}

func (ch _ResetAccountChange) revert(sdb *StateDb) {
	sdb.setAccountState(ch.prev)
}


func (ch _SuicideChange) revert(sdb *StateDb) {
	obj := sdb.getAccountState(*ch.addr)
	if obj != nil {
		obj.suicided = ch.prev
		obj.setBalance(ch.prevBalance)
	}
}

func (ch _SuicideChange) Address() *ec.Address {
	return ch.addr
}


func (ch _TouchChange) revert(sdb *StateDb) {
}

func (ch _TouchChange) Address() *ec.Address {
	return ch.addr
}

func (ch _BalanceChange) revert(sdb *StateDb) {
	sdb.getAccountState(*ch.addr).setBalance(ch.prev)
}

func (ch _BalanceChange) Address() *ec.Address {
	return ch.addr
}

func (ch _NonceChange) revert(sdb *StateDb) {
	sdb.getAccountState(*ch.addr).setNonce(ch.prev)
}

func (ch _NonceChange) Address() *ec.Address {
	return ch.addr
}

func (ch _VidChange) revert(sdb *StateDb) {
	sdb.getAccountState(*ch.addr).setVid(ch.prev)
}

func (ch _VidChange) Address() *ec.Address {
	return ch.addr
}

func (ch _StorageChange) revert(sdb *StateDb) {
	sdb.getAccountState(*ch.addr).setState(ch.key, ch.prevValue)
}

func (ch _StorageChange) Address() *ec.Address {
	return ch.addr
}

func (ch _RefundChange) revert(sdb *StateDb) {
	sdb.refund = ch.prev
}

func (ch _CreateWitnessChange) revert(sdb *StateDb) {
	sdb.getAccountState(*ch.addr).setVid(ch.voterId)
	sdb.cst.LastAllocatedWitnessId = ch.vid - 1
	delete(sdb.witnesses, ch.vid)
}

func (ch _CreateWitnessChange) Address() *ec.Address {
	return ch.addr
}

func (ch _CreateWitnessChange) Vid() int64 {
	return ch.vid
}

func (ch _CreateVoterChange) revert(sdb *StateDb) {
	sdb.getAccountState(*ch.addr).setVid(0)
	sdb.cst.LastAllocatedVoterId = ch.vid + 1
	delete(sdb.voters, ch.vid)
}

func (ch _CreateVoterChange) Address() *ec.Address {
	return ch.addr
}

func (ch _CreateVoterChange) Vid() int64 {
	return ch.vid
}

func (ch _WitnessUrlChange) revert(sdb *StateDb) {
	w := sdb.GetWitnessState(ch.vid)
	w.Url = ch.prev
}

func (ch _WitnessUrlChange) Vid() int64 {
	return ch.vid
}

func (ch _VoterNumWitnessChange) revert(sdb *StateDb) {
	v := sdb.GetVoterState(ch.vid)
	v.NumWitness = ch.prev
}

func (ch _VoterNumWitnessChange) Vid() int64 {
	return ch.vid
}

func (ch _VoterWitnessIdsChange) revert(sdb *StateDb) {
	v := sdb.GetVoterState(ch.vid)
	v.WitnessIds = ch.prev
}

func (ch _VoterWitnessIdsChange) Vid() int64 {
	return ch.vid
}

