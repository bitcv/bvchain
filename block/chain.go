package block

import (
	"bvchain/ec"
	"bvchain/tx"
	"bvchain/cfg"
	"bvchain/util/log"
	"bvchain/chain"
	"bvchain/state"
	"bvchain/db"
	"bvchain/trie"

	"os"
	"path"
)

type BlockChain struct {
	config *cfg.Config
	params *chain.ChainParams
	cst *state.ChainState
	bstore *BlockStore
	cdb *trie.CacheDb
	sdb *state.StateDb
	trxPool *tx.TrxPool
	bexec *BlockExecutor
	logger log.Logger
}


func NewBlockChain(cfgfile string) (*BlockChain, error) {
	config, err := cfg.LoadConfig(cfgfile)
	if err != nil {
		return nil, err
	}

	logger := log.New(os.Stderr)

	blockDbPath := path.Join(config.DbPath, "block")
	blockKvdb, err := db.NewLevelDb(blockDbPath, 0, 0)
	if err != nil {
		return nil, err
	}
	bstore := NewBlockStore(blockKvdb)

	stateDbPath := path.Join(config.DbPath, "state")
	stateKvdb, err := db.NewLevelDb(stateDbPath, 0, 0)
	if err != nil {
		return nil, err
	}

	height := bstore.Height()
	hash := bstore.LoadHash(height)
	root := ec.BytesToHash256(hash)

	cst, err := state.NewChainState(stateKvdb, height)
	if err != nil {
		return nil, err
	}

	params := chain.DefaultChainParams()

	cdb := trie.NewCacheDb(stateKvdb)
	sdb, err := state.NewStateDb(root, cdb)
	if err != nil {
		return nil, err
	}

	trxPool := tx.NewTrxPool(config.TrxPool, height)

	bexec := NewBlockExecutor(params, sdb, trxPool, logger)

	bc := &BlockChain{
		config: config,
		params: params,
		cst: cst,
		trxPool: trxPool,
		bstore: bstore,
		cdb: cdb,
		sdb: sdb,
		bexec: bexec,
		logger: logger,
	}
	return bc, nil
}


func (bc *BlockChain) StateDbAt(root ec.Hash256) (*state.StateDb, error) {
	return state.NewStateDb(root, bc.cdb)
}


