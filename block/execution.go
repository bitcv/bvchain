package block

import (
	"bvchain/tx"
	"bvchain/util/log"
	"bvchain/state"
	"bvchain/chain"
	"bvchain/consensus"

	"fmt"
)

//-----------------------------------------------------------------------------
// BlockExecutor handles block execution and state updates.
// It exposes ApplyBlock(), which validates & executes the block, updates state w/ ABCI responses,
// then commits and updates the mempool atomically, then saves state.

// BlockExecutor provides the context and accessories for properly executing a block.
type BlockExecutor struct {
	sdb *state.StateDb
	trxPool *tx.TrxPool
	maint *consensus.Maintainer

	logger log.Logger
}

// NewBlockExecutor returns a new BlockExecutor with a NopEventBus.
// Call SetEventBus to provide one.
func NewBlockExecutor(params *chain.ChainParams, sdb *state.StateDb, trxPool *tx.TrxPool, logger log.Logger) *BlockExecutor {
	return &BlockExecutor{
		sdb: sdb,
		trxPool: trxPool,
		maint: consensus.NewMaintainer(params, sdb),
		logger: logger,
	}
}

// ValidateBlock validates the given block against the given state.
// If the block is invalid, it returns an error.
// Validation does not mutate state, but does require historical information from the stateDB,
// ie. to verify evidence from a validator at an old height.
func (bexec *BlockExecutor) ValidateBlock(cst *state.ChainState, blk *Block) error {
	if blk.Height != cst.LastBlockHeight + 1 {
		return fmt.Errorf("Wrong Block.Header.Height. Expected %v, got %v", cst.LastBlockHeight+1, blk.Height)
	}

	if blk.PrevHash != cst.LastBlockHash {
		return fmt.Errorf("Wrong Block.Header.PrevHash. Expected %v, got %v", cst.LastBlockHash, blk.PrevHash)
	}
	// TODO
	return nil
}

// ApplyBlock validates the block against the state, executes it against the app,
// fires the relevant events, commits the app, and saves the new state and responses.
// It's the only function that needs to be called
// from outside this package to process and commit an entire block.
// It takes a blockId to avoid recomputing the parts hash.
func (bexec *BlockExecutor) ApplyBlock(cst *state.ChainState, blk *Block) (*state.ChainState, error) {

	if err := bexec.ValidateBlock(cst, blk); err != nil {
		return cst, ErrInvalidBlock(err)
	}

	for _, tx := range blk.Transactions {
		bexec.sdb.ApplyTransaction(tx)
	}

	maintNeeded := cst.NextMaintenanceTime <= blk.Timestamp
	if maintNeeded {
		bexec.maint.PerformChainMaintenance(blk.Height, blk.Timestamp)
	}

//	SaveState(bexec.tr, cst)

	return cst, nil
}

