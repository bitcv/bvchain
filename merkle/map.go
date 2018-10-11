package merkle

import (
	"bvchain/ec"

	"bytes"
	"sort"
	"halftwo/mangos/vbs"
)

type KvPair struct {
	Key []byte
	Value []byte
}

type KvPairs []KvPair

func (kvs KvPairs) Len() int { return len(kvs) }

func (kvs KvPairs) Less(i, j int) bool {
	r := bytes.Compare(kvs[i].Key, kvs[j].Key)
	if r < 0 {
		return true
	} else if r == 0 {
                return bytes.Compare(kvs[i].Value, kvs[j].Value) < 0
	}
	return false
}

func (kvs KvPairs) Swap(i, j int) { kvs[i], kvs[j] = kvs[j], kvs[i] }


// Merkle tree from a map.
// Leaves are `hash(key) | hash(value)`.
// Leaves are sorted before Merkle hashing.
type simpleMap struct {
	kvs    KvPairs
	sorted bool
}

func newSimpleMap() *simpleMap {
	return &simpleMap{
		kvs:    nil,
		sorted: false,
	}
}

// Set hashes the key and value and appends it to the kv pairs.
func (sm *simpleMap) Set(key string, value Hasher) {
	sm.sorted = false

	// The value is hashed, so you can
	// check for equality with a cached value (say)
	// and make a determination to fetch or not.
	hash := value.Hash()

	sm.kvs = append(sm.kvs, KvPair{Key:[]byte(key), Value:hash})
}

// Hash Merkle root hash of items sorted by key
// (UNSTABLE: and by value too if duplicate key).
func (sm *simpleMap) Hash() []byte {
	sm.Sort()
	return hashKvPairs(sm.kvs)
}

func (sm *simpleMap) Sort() {
	if sm.sorted {
		return
	}
	sort.Sort(sm.kvs)
	sm.sorted = true
}

// Returns a copy of sorted KvPairs.
// NOTE these contain the hashed key and value.
func (sm *simpleMap) KvPairs() KvPairs {
	sm.Sort()
	kvs := make([]KvPair, len(sm.kvs))
	copy(kvs, sm.kvs)
	return kvs
}

func (kv KvPair) Hash() []byte {
	hasher := ec.New256Hasher()
	enc := vbs.NewEncoder(hasher)
	err := enc.Encode(kv.Key)
	if err != nil {
		panic(err)
	}
	err = enc.Encode(kv.Value)
	if err != nil {
		panic(err)
	}
	return hasher.Sum(nil)
}

func hashKvPairs(kvs []KvPair) []byte {
	hashers := make([]Hasher, len(kvs))
	for i, kvp := range kvs {
		hashers[i] = KvPair(kvp)
	}
	return SimpleHashFromHashers(hashers)
}

