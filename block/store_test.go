package block

import (
	"bvchain/util/log"
	"bvchain/db"

	"bytes"
	"fmt"
	"runtime/debug"
	"strings"
	"testing"
	"time"
	"halftwo/mangos/vbs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadBlockState(t *testing.T) {
	db := db.NewMemDb()

	bs := &_BlockState{Height: 1000}
	bs.Save(db)

	bs2 := LoadBlockState(db)

	assert.Equal(t, *bs, bs2, "expected the retrieved DBs to match")
}

func TestNewBlockStore(t *testing.T) {
	db := db.NewMemDb()
	db.Put(blockStateKey, []byte(`{"height": "10000"}`))
	bs := NewBlockStore(db)
	require.Equal(t, int64(10000), bs.Height(), "failed to properly parse blockstore")

	panicCausers := []struct {
		data    []byte
		wantErr string
	}{
		{[]byte("artful-doger"), "not unmarshal bytes"},
		{[]byte(" "), "unmarshal bytes"},
	}

	for i, tt := range panicCausers {
		// Expecting a panic here on trying to parse an invalid blockStore
		_, _, panicErr := doFn(func() (interface{}, error) {
			db.Put(blockStateKey, tt.data)
			_ = NewBlockStore(db)
			return nil, nil
		})
		require.NotNil(t, panicErr, "#%d panicCauser: %q expected a panic", i, tt.data)
		assert.Contains(t, panicErr.Error(), tt.wantErr, "#%d data: %q", i, tt.data)
	}

	db.Put(blockStateKey, nil)
	bs = NewBlockStore(db)
	assert.Equal(t, bs.Height(), int64(0), "expecting nil bytes to be unmarshaled alright")
}

func freshBlockStore() (*BlockStore, db.KvDb) {
	db := db.NewMemDb()
	return NewBlockStore(db), db
}

var st, _ = makeStateAndBlockStore(log.New(new(bytes.Buffer)))
var block = makeBlock(1, st)

// TODO: This test should be simplified ...

func TestBlockStoreSaveLoadBlock(t *testing.T) {
	st, bs := makeStateAndBlockStore(log.New(new(bytes.Buffer)))
	require.Equal(t, bs.Height(), int64(0), "initially the height should be zero")

	// check there are no blocks at various heights
	noBlockHeights := []int64{0, -1, 100, 1000, 2}
	for i, height := range noBlockHeights {
		if g := bs.LoadBlockByHeight(height); g != nil {
			t.Errorf("#%d: height(%d) got a block; want nil", i, height)
		}
	}

	// save a block
	block := makeBlock(bs.Height()+1, st)
	bs.SaveBlock(block)
	require.Equal(t, bs.Height(), block.Header.Height, "expecting the new height to be changed")

	header1 := &Header{
		Height: 1,
		Timestamp: time.Now().Unix(),
	}
	tmp := *header1
	header2 := &tmp
	header2.Height = 4

	// End of setup, test data

	tuples := []struct {
		block      *Block
		wantErr    bool
		wantPanic  string

		corruptBlockInDb      bool
	}{
		{
			block:      newBlock(header1),
		},

		{
			block:     nil,
			wantPanic: "only save a non-nil block",
		},

		{
			block:     newBlock(header2),
			wantPanic: "only save contiguous blocks", // and incomplete and uncontiguous parts
		},

		{
			block:     newBlock(header1),
			wantPanic: "only save complete block", // incomplete parts
		},

		{
			block:     newBlock(header1),
			wantPanic:        "unmarshal to types.BlockMeta failed",
			corruptBlockInDb: true, // Corrupt the DB's block entry
		},
	}

	type quad struct {
		block  *Block
	}

	for i, tuple := range tuples {
		bs, db := freshBlockStore()
		res, err, panicErr := doFn(func() (interface{}, error) {
			bs.SaveBlock(tuple.block)
			if tuple.block == nil {
				return nil, nil
			}

			if tuple.corruptBlockInDb {
				db.Put(height2HashKey(tuple.block.Height), []byte("block-bogus-hash"))
			}
			bBlock := bs.LoadBlockByHeight(tuple.block.Height)

			return &quad{block: bBlock,}, nil
		})

		if subStr := tuple.wantPanic; subStr != "" {
			if panicErr == nil {
				t.Errorf("#%d: want a non-nil panic", i)
			} else if got := panicErr.Error(); !strings.Contains(got, subStr) {
				t.Errorf("#%d:\n\tgotErr: %q\nwant substring: %q", i, got, subStr)
			}
			continue
		}

		if tuple.wantErr {
			if err == nil {
				t.Errorf("#%d: got nil error", i)
			}
			continue
		}

		assert.Nil(t, panicErr, "#%d: unexpected panic", i)
		assert.Nil(t, err, "#%d: expecting a non-nil error", i)
		qua, ok := res.(*quad)
		if !ok || qua == nil {
			t.Errorf("#%d: got nil quad back; gotType=%T", i, res)
			continue
		}
	}
}

func TestBlockFetchAtHeight(t *testing.T) {
	st, bs := makeStateAndBlockStore(log.New(new(bytes.Buffer)))
	require.Equal(t, bs.Height(), int64(0), "initially the height should be zero")
	block := makeBlock(bs.Height()+1, st)

	bs.SaveBlock(block)
	require.Equal(t, bs.Height(), block.Header.Height, "expecting the new height to be changed")

	blockAtHeight := bs.LoadBlockByHeight(bs.Height())
	bz1, _ := vbs.Marshal(block)
	bz2, _ := vbs.Marshal(blockAtHeight)
	require.Equal(t, bz1, bz2)
	require.Equal(t, block.Hash(), blockAtHeight.Hash(),
		"expecting a successful load of the last saved block")

	blockAtHeightPlus1 := bs.LoadBlockByHeight(bs.Height() + 1)
	require.Nil(t, blockAtHeightPlus1, "expecting an unsuccessful load of Height()+1")
	blockAtHeightPlus2 := bs.LoadBlockByHeight(bs.Height() + 2)
	require.Nil(t, blockAtHeightPlus2, "expecting an unsuccessful load of Height()+2")
}

func doFn(fn func() (interface{}, error)) (res interface{}, err error, panicErr error) {
	defer func() {
		if r := recover(); r != nil {
			switch e := r.(type) {
			case error:
				panicErr = e
			case string:
				panicErr = fmt.Errorf("%s", e)
			default:
				if st, ok := r.(fmt.Stringer); ok {
					panicErr = fmt.Errorf("%s", st)
				} else {
					panicErr = fmt.Errorf("%s", debug.Stack())
				}
			}
		}
	}()

	res, err = fn()
	return res, err, panicErr
}

func newBlock(hdr *Header) *Block {
	return &Block{
		Header: *hdr,
	}
}
