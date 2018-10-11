package state

import (
	"bvchain/trie"
	"bvchain/util"
	"bvchain/genesis"

	"fmt"
	"halftwo/mangos/vbs"
)

// database keys
var _stateKey = []byte("ChainStateKey")


// LoadChainStateFromDbOrGenesisFile loads the most recent state from the database,
// or creates a new one from the given genesisFilePath and persists the result
// to the database.
func LoadChainStateFromDbOrGenesisFile(tr *trie.Trie, genesisFilePath string) (*ChainState, error) {
	state := LoadChainState(tr)
	if state == nil || state.IsEmpty() {
		var err error
		state, err = MakeGenesisStateFromFile(genesisFilePath)
		if err != nil {
			return state, err
		}
		SaveChainState(tr, state)
	}

	return state, nil
}

// LoadChainStateFromDbOrGenesisDoc loads the most recent state from the database,
// or creates a new one from the given genesisDoc and persists the result
// to the database.
func LoadChainStateFromDbOrGenesisDoc(tr *trie.Trie, genesisDoc *genesis.Document) (*ChainState, error) {
	state := LoadChainState(tr)
	if state == nil || state.IsEmpty() {
		var err error
		state, err = MakeGenesisState(genesisDoc)
		if err != nil {
			return state, err
		}
		SaveChainState(tr, state)
	}

	return state, nil
}

// LoadChainState loads the ChainState from the database.
func LoadChainState(tr *trie.Trie) *ChainState {
	return loadChainState(tr, _stateKey)
}

func loadChainState(tr *trie.Trie, key []byte) *ChainState {
	buf := tr.Get(key)
	if len(buf) == 0 {
		return nil
	}

	state := ChainState{}
	err:= vbs.Unmarshal(buf, &state)
	if err != nil {
		util.Exit(fmt.Sprintf(`LoadChainState: Data has been corrupted or its spec has changed: %v\n`, err))
	}

	return &state
}

// SaveChainState persists the ChainState and the ChainParams to the database.
func SaveChainState(tr *trie.Trie, state *ChainState) {
	tr.Update(_stateKey, state.Bytes())
}

