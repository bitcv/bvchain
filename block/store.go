package block

import (
	"bvchain/db"
	"bvchain/util"

	"encoding/binary"
	"sync/atomic"
	"halftwo/mangos/vbs"
	"halftwo/mangos/xerr"
)

// NB: BlockStore methods will panic if they encounter errors
// deserializing loaded data, indicating probable corruption on disk.

type BlockStore struct {
	kvdb db.KvDb
	height int64	// atomic
}

// NewBlockStore returns a new BlockStore with the given DB,
// initialized to the last height that was committed to the DB.
func NewBlockStore(kvdb db.KvDb) *BlockStore {
	st := LoadBlockState(kvdb)
	return &BlockStore{
		kvdb: kvdb,
		height: st.Height,
	}
}

// Height returns the last known contiguous block height.
func (bs *BlockStore) Height() int64 {
	return atomic.LoadInt64(&bs.height)
}

func (bs *BlockStore) LoadHash(height int64) []byte {
	bz, err := bs.kvdb.Get(height2HashKey(height))
	if err != nil {
		return nil
	}

	var hash []byte
	err = vbs.Unmarshal(bz, &hash)
	if err != nil {
		panic(xerr.Trace(err, "Error unmarshal hash"))
	}
	return hash
}

func (bs *BlockStore) LoadBlockByHeight(height int64) *Block {
	hash := bs.LoadHash(height)
	if hash == nil {
		return nil
	}
	return bs._loadBlock(height, hash)
}

// LoadBlock returns the block with the given height and hash (can be nil).
// If no block is found for that height, it returns nil.
func (bs *BlockStore) LoadBlock(height int64, hash []byte) *Block {
	if len(hash) == 0 {
		hash = bs.LoadHash(height)
		if hash == nil {
			return nil
		}
	}

	return bs._loadBlock(height, hash)
}

func (bs *BlockStore) _loadBlock(height int64, hash []byte) *Block {
	util.Assert(len(hash) == 32)

	bz, err := bs.kvdb.Get(blockKey(height, hash))
	if err != nil {
		return nil
	}

	block := Block{}
	err = vbs.Unmarshal(bz, &block)	// TODO
	if err != nil {
		panic(xerr.Trace(err, "Error unmarshaling block"))
	}
	return &block
}

// SaveBlock persists the given block to the underlying kvdb.
func (bs *BlockStore) SaveBlock(block *Block) {
	if block == nil {
		panic("BlockStore can only save a non-nil block")
	}

	bz, err := vbs.Marshal(block)
	if err != nil {
		panic("vbs.Marshal() failed")
	}

	key := blockKey(block.Height, block.Hash())
	bs.kvdb.Put(key, bz)

	if block.Height == atomic.LoadInt64(&bs.height) + 1 {
		height := atomic.AddInt64(&bs.height, 1)
		_BlockState{Height:height}.Save(bs.kvdb)
	}
}

//-----------------------------------------------------------------------------
func hash2HeightKey(hash []byte) []byte {
	buf := make([]byte, 1 + len(hash))
	buf[0] = 'h'
	return append(buf[:1], hash...)
}

func height2HashKey(height int64) []byte {
	buf := make([]byte, 9)
	buf[0] = 'H'
	binary.BigEndian.PutUint64(buf[1:], uint64(height))
	return buf[:9]
}

func blockKey(height int64, hash []byte) []byte {
	buf := make([]byte, 9 + len(hash))
	buf[0] = 'B'
	binary.BigEndian.PutUint64(buf[1:], uint64(height))
	return append(buf[:9], hash...)
}

//-----------------------------------------------------------------------------

var blockStateKey = []byte("blockState")

type _BlockState struct {
	Height int64
}

// Save persists the blockDb state to the database as VBS.
func (ss _BlockState) Save(kvdb db.KvDb) {
	bz, err := vbs.Marshal(ss)
	if err != nil {
		panic(xerr.Trace(err, "Could not marshal _BlockState"))
	}
	kvdb.Put(blockStateKey, bz)
}

// LoadBlockState returns the _BlockState as loaded from disk.
// If no _BlockState was previously persisted, it returns the zero value.
func LoadBlockState(kvdb db.KvDb) _BlockState {
	bz, err := kvdb.Get(blockStateKey)
	if len(bz) == 0 || err != nil {
		return _BlockState{
			Height: 0,
		}
	}

	ss := _BlockState{}
	err = vbs.Unmarshal(bz, &ss)
	if err != nil {
		panic(xerr.Trace(err, "Could not unmarshal _BlockState"))
	}
	return ss
}


