package merkle

import (
	"bvchain/util"
	"bytes"
	"testing"
)

type testItem []byte

func (tI testItem) Hash() []byte {
	return []byte(tI)
}

func mutateBytes(bz []byte) []byte {
	if len(bz) == 0 {
		panic("Cannot mutate an empty bytes")
	}

	bz2 := make([]byte, len(bz))
	copy(bz2, bz)

	// Try a random mutation
	switch util.RandomInt() % 2 {
	case 0: // Mutate a single byte
		bz2[util.RandomInt()%len(bz)] += byte(util.RandomInt()%255 + 1)
	case 1: // Remove an arbitrary byte
		pos := util.RandomInt() % len(bz)
		bz2 = append(bz2[:pos], bz2[pos+1:]...)
	}
	return bz2
}

func TestSimpleProof(t *testing.T) {

	total := 100

	items := make([]Hasher, total)
	for i := 0; i < total; i++ {
		items[i] = testItem(util.RandomBytes(32))
	}

	rootHash := SimpleHashFromHashers(items)

	rootHash2, proofs := SimpleProofsFromHashers(items)

	if !bytes.Equal(rootHash, rootHash2) {
		t.Errorf("Unmatched root hashes: %X vs %X", rootHash, rootHash2)
	}

	// For each item, check the trail.
	for i, item := range items {
		itemHash := item.Hash()
		proof := proofs[i]

		// Verify success
		ok := proof.Verify(i, total, itemHash, rootHash)
		if !ok {
			t.Errorf("Verification failed for index %v.", i)
		}

		// Wrong item index should make it fail
		{
			ok = proof.Verify((i+1)%total, total, itemHash, rootHash)
			if ok {
				t.Errorf("Expected verification to fail for wrong index %v.", i)
			}
		}

		// Trail too long should make it fail
		origAunts := proof.Aunts
		proof.Aunts = append(proof.Aunts, util.RandomBytes(32))
		{
			ok = proof.Verify(i, total, itemHash, rootHash)
			if ok {
				t.Errorf("Expected verification to fail for wrong trail length.")
			}
		}
		proof.Aunts = origAunts

		// Trail too short should make it fail
		proof.Aunts = proof.Aunts[0 : len(proof.Aunts)-1]
		{
			ok = proof.Verify(i, total, itemHash, rootHash)
			if ok {
				t.Errorf("Expected verification to fail for wrong trail length.")
			}
		}
		proof.Aunts = origAunts

		// Mutating the itemHash should make it fail.
		ok = proof.Verify(i, total, mutateBytes(itemHash), rootHash)
		if ok {
			t.Errorf("Expected verification to fail for mutated leaf hash")
		}

		// Mutating the rootHash should make it fail.
		ok = proof.Verify(i, total, itemHash, mutateBytes(rootHash))
		if ok {
			t.Errorf("Expected verification to fail for mutated root hash")
		}
	}
}
