package state

import (
	"bvchain/chain"
	"bvchain/genesis"
	"bvchain/ec"
	"bvchain/db"

	"bytes"
	"fmt"
	"io/ioutil"
	"halftwo/mangos/vbs"
)

// ChainState is a short description of the latest committed block of the chain.
// It keeps all information necessary to validate new blocks,
// including the last validator set and the chain params.
// All fields are exposed so the struct can be easily serialized,
// but none of them should be mutated directly.
// Instead, use state.Clone() or state.NextState(...).
// NOTE: not goroutine-safe.
type ChainState struct {
	// Immutable
	ChainId string

	// LastBlockHeight=0 at genesis (ie. block(H=0) does not exist)
	LastBlockHeight int64
	LastBlockHash ec.Hash256
	LastBlockTime int64

	LastBlockWitnessId int64
	LastBlockAbsoluteSlot int64
	NextMaintenanceTime int64

	DuringMaintenance bool

	LastAllocatedWitnessId int64	// positive
	LastAllocatedVoterId int64	// negative

	// Witnesses are persisted to the database separately every time they change,
	// so we can query for historical validator sets.
	// Note that if s.LastBlockHeight causes a valset change,
	// we set s.LastHeightWsetChanged = s.LastBlockHeight + 1
	Wset *chain.WitnessSet
	LastHeightWsetChanged int64

	// Chain parameters used for validating blocks.
	// Changes returned by EndBlock and updated after Commit.
	ChainParams *chain.ChainParams
	LastHeightChainParamsChanged int64

	// Merkle root of the results from executing prev block
	LastResultsHash []byte
}

func NewChainState(kvdb db.KvDb, blockHeight int64) (*ChainState, error) {
	if blockHeight == 0 {
	}

	// TODO
	cst := &ChainState{}
	return cst, nil
}

// Clone makes a copy of the ChainState for mutating.
func (cst *ChainState) Clone() *ChainState {
	st2 := *cst
	st2.Wset = cst.Wset.Clone()
	st2.ChainParams = cst.ChainParams.Clone()
	return &st2
}

// Equals returns true if the States are identical.
func (cst *ChainState) Equal(cst2 *ChainState) bool {
	bz, bz2 := cst.Bytes(), cst2.Bytes()
	return bytes.Equal(bz, bz2)
}

// Bytes serializes the ChainState using vbs.
func (cst *ChainState) Bytes() []byte {
	bz, err := vbs.Marshal(cst)
	if err != nil {
		panic(err)
	}
	return bz
}

// IsEmpty returns true if the ChainState is equal to the empty ChainState.
func (cst *ChainState) IsEmpty() bool {
	return cst.Wset == nil || cst.Wset.Size() == 0
}

//------------------------------------------------------------------------
// Genesis

// MakeGenesisStateFromFile reads and unmarshals state from the given
// file.
//
// Used during replay and in tests.
func MakeGenesisStateFromFile(genDocFile string) (*ChainState, error) {
	genDoc, err := MakeGenesisDocFromFile(genDocFile)
	if err != nil {
		return nil, err
	}
	return MakeGenesisState(genDoc)
}

// MakeGenesisDocFromFile reads and unmarshals genesis doc from the given file.
func MakeGenesisDocFromFile(genDocFile string) (*genesis.Document, error) {
	genDocJson, err := ioutil.ReadFile(genDocFile)
	if err != nil {
		return nil, fmt.Errorf("Couldn't read GenesisDoc file: %v", err)
	}
	genDoc, err := genesis.DocumentFromJson(genDocJson)
	if err != nil {
		return nil, fmt.Errorf("Error reading GenesisDoc: %v", err)
	}
	return genDoc, nil
}

// MakeGenesisState creates state from genesis.Document.
func MakeGenesisState(genDoc *genesis.Document) (*ChainState, error) {
	err := genDoc.ValidateAndComplete()
	if err != nil {
		return nil, fmt.Errorf("Error in genesis file: %v", err)
	}

	lastWitnessId := int64(0)
	ws := make([]*chain.Witness, len(genDoc.Witnesses))
	for i, w := range genDoc.Witnesses {
		lastWitnessId++
		id := lastWitnessId
		address := w.PubKey.Address()
		ws[i] = &chain.Witness{ Id:id, Address:address, }
	}

	st := &ChainState{
		ChainId: genDoc.ChainId,

		LastBlockHeight: 0,
		LastBlockTime:   genDoc.GenesisTime,

		Wset:            chain.NewWitnessSet(ws),
		LastHeightWsetChanged: 1,
		LastBlockWitnessId: lastWitnessId,

		ChainParams: genDoc.ChainParams.Clone(),
		LastHeightChainParamsChanged: 1,
	}

	return st, nil
}

