package db

import (
	"bvchain/util/log"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/errors"
	"github.com/syndtr/goleveldb/leveldb/filter"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/iterator"
	"github.com/syndtr/goleveldb/leveldb/util"
)


type LevelDb struct {
	file string
	ldb *leveldb.DB
}

var _ KvDb = (*LevelDb)(nil)

func NewLevelDb(file string, cache int, handles int) (*LevelDb, error) {
	if cache < 16 {
		cache = 16
	}
	if handles < 16 {
		handles = 16
	}

	ldb, err := leveldb.OpenFile(file, &opt.Options{
		OpenFilesCacheCapacity: handles,
		BlockCacheCapacity:     cache / 2 * opt.MiB,
		WriteBuffer:            cache / 4 * opt.MiB,
		Filter:                 filter.NewBloomFilter(10),
	})

	if _, corrupted := err.(*errors.ErrCorrupted); corrupted {
		ldb, err = leveldb.RecoverFile(file, nil)
	}

	if err != nil {
		return nil, err
	}

	db := &LevelDb{
		file: file,
		ldb: ldb,
	}

	return db, nil
}

func (db *LevelDb) Close() {
	err := db.ldb.Close()
	if err != nil {
		log.Error(err.Error())
	}
}

func (db *LevelDb) Path() string {
	return db.file
}

func (db *LevelDb) Put(key []byte, value []byte) error {
	return db.ldb.Put(key, value, nil)
}

func (db *LevelDb) Has(key []byte) (bool, error) {
	return db.ldb.Has(key, nil)
}

func (db *LevelDb) Get(key []byte) ([]byte, error) {
	value, err := db.ldb.Get(key, nil)
	if err != nil {
		return nil, err
	}
	return value, nil
}

func (db *LevelDb) Remove(key []byte) error {
        return db.ldb.Delete(key, nil)
}

func (db *LevelDb) NewIterator() iterator.Iterator {
	return db.ldb.NewIterator(nil, nil)
}

func (db *LevelDb) NewIteratorWithPrefix(prefix []byte) iterator.Iterator {
	return db.ldb.NewIterator(util.BytesPrefix(prefix), nil)
}


func (db *LevelDb) NewBatch() KvBatch {
	return &_DbBatch{ldb:db.ldb, lb:new(leveldb.Batch)}
}

type _DbBatch struct {
	ldb *leveldb.DB
	lb *leveldb.Batch
	size int
}

func (b *_DbBatch) Put(key, value []byte) error {
	b.lb.Put(key, value)
	b.size += len(key) + len(value)
	return nil
}

func (b *_DbBatch) Write() error {
	return b.ldb.Write(b.lb, nil)
}

func (b *_DbBatch) DataSize() int {
	return b.size
}

func (b *_DbBatch) Reset() {
	b.lb.Reset()
	b.size = 0
}

