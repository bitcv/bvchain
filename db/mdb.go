package db

import (
	"errors"
	"sync"
)

type MemDb struct {
	dict map[string][]byte
	lock sync.RWMutex
}

var _ KvDb = (*MemDb)(nil)

func NewMemDb() *MemDb {
	return &MemDb{
		dict: make(map[string][]byte),
	}
}

func NewMemDbWithCap(size int) *MemDb {
	return &MemDb{
		dict: make(map[string][]byte, size),
	}
}

func copyBytes(b []byte) []byte {
	x := make([]byte, len(b))
	copy(x, b)
	return x
}

func (db *MemDb) Put(key []byte, value []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	db.dict[string(key)] = copyBytes(value)
	return nil
}

func (db *MemDb) Has(key []byte) (bool, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	_, ok := db.dict[string(key)]
	return ok, nil
}

func (db *MemDb) Get(key []byte) ([]byte, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if value, ok := db.dict[string(key)]; ok {
		return value, nil
	}
	return nil, errors.New("not found")
}

func (db *MemDb) Keys() [][]byte {
	db.lock.RLock()
	defer db.lock.RUnlock()

	keys := make([][]byte, 0, len(db.dict))
	for key := range db.dict {
		keys = append(keys, []byte(key))
	}
	return keys
}

func (db *MemDb) Remove(key []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	delete(db.dict, string(key))
	return nil
}

func (db *MemDb) Close() {
	db.dict = nil
}

func (db *MemDb) NewBatch() KvBatch {
	return &_MemBatch{mdb:db}
}

func (db *MemDb) Len() int { return len(db.dict) }


type _KV struct{
	k []byte
	v []byte
}

type _MemBatch struct {
	mdb     *MemDb
	items []_KV
	size   int
}

func (b *_MemBatch) Put(key, value []byte) error {
	b.items = append(b.items, _KV{copyBytes(key), copyBytes(value)})
	b.size += len(key) + len(value)
	return nil
}

func (b *_MemBatch) Write() error {
	b.mdb.lock.Lock()
	defer b.mdb.lock.Unlock()

	for _, item := range b.items {
		b.mdb.dict[string(item.k)] = item.v
	}
	return nil
}

func (b *_MemBatch) DataSize() int {
	return b.size
}

func (b *_MemBatch) Reset() {
	b.items = b.items[:0]
	b.size = 0
}

