package merkle

import (
	"bvchain/ec"
)

// SimpleHashFromTwoHashes is the basic operation of the Merkle tree: Hash(left | right).
func SimpleHashFromTwoHashes(left, right []byte) []byte {
	hasher := ec.New256Hasher()
	hasher.Write(left)
	hasher.Write(right)
	return hasher.Sum(nil)
}

// SimpleHashFromHashers computes a Merkle tree from items that can be hashed.
func SimpleHashFromHashers(items []Hasher) []byte {
	hashes := make([][]byte, len(items))
	for i, item := range items {
		hash := item.Hash()
		hashes[i] = hash
	}
	return simpleHashFromHashes(hashes)
}

// SimpleHashFromMap computes a Merkle tree from sorted map.
// Like calling SimpleHashFromHashers with
// `item = []byte(Hash(key) | Hash(value))`,
// sorted by `item`.
func SimpleHashFromMap(m map[string]Hasher) []byte {
	sm := newSimpleMap()
	for k, v := range m {
		sm.Set(k, v)
	}
	return sm.Hash()
}

//----------------------------------------------------------------

// Expects hashes!
func simpleHashFromHashes(hashes [][]byte) []byte {
	k := len(hashes)
	switch k {
	case 0:
		return nil
	case 1:
		return hashes[0]
	default:
		m := (k+1)/2
		left := simpleHashFromHashes(hashes[:m])
		right := simpleHashFromHashes(hashes[m:])
		return SimpleHashFromTwoHashes(left, right)
	}
}

