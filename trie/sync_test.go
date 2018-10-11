// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package trie

import (
	"bvchain/db"
	"bvchain/ec"
	"bvchain/util"

	"bytes"
	"testing"
)

// makeTestTrie create a sample test trie to test node-wise reconstruction.
func makeTestTrie() (*CacheDb, *Trie, map[string][]byte) {
	// Create an empty trie
	cdb := NewCacheDb(db.NewMemDb())
	tr, _ := NewWithCacheDb(ec.Hash256{}, cdb)

	// Fill it with some arbitrary data
	content := make(map[string][]byte)
	for i := byte(0); i < 255; i++ {
		// Map the same data under multiple keys
		key, val := util.LeftPadBytes([]byte{1, i}, 32), []byte{i}
		content[string(key)] = val
		tr.Update(key, val)

		key, val = util.LeftPadBytes([]byte{2, i}, 32), []byte{i}
		content[string(key)] = val
		tr.Update(key, val)

		// Add some other data to inflate the trie
		for j := byte(3); j < 13; j++ {
			key, val = util.LeftPadBytes([]byte{j, i}, 32), []byte{j, i}
			content[string(key)] = val
			tr.Update(key, val)
		}
	}
	tr.Commit(nil)

	// Return the generated trie
	return cdb, tr, content
}

// checkTrieContents cross references a reconstructed trie with an expected data
// content map.
func checkTrieContents(t *testing.T, cdb *CacheDb, root []byte, content map[string][]byte) {
	// Check root availability and trie contents
	tr, err := NewWithCacheDb(ec.BytesToHash256(root), cdb)
	if err != nil {
		t.Fatalf("failed to create trie at %x: %v", root, err)
	}
	if err := checkTrieConsistency(cdb, ec.BytesToHash256(root)); err != nil {
		t.Fatalf("inconsistent trie at %x: %v", root, err)
	}
	for key, val := range content {
		if have := tr.Get([]byte(key)); !bytes.Equal(have, val) {
			t.Errorf("entry %x: content mismatch: have %x, want %x", key, have, val)
		}
	}
}

// checkTrieConsistency checks that all nodes in a trie are indeed present.
func checkTrieConsistency(cdb *CacheDb, root ec.Hash256) error {
	// Create and iterate a trie rooted in a subnode
	tr, err := NewWithCacheDb(root, cdb)
	if err != nil {
		return nil // Consider a non existent state consistent
	}
	it := tr.NodeIterator(nil)
	for it.Next(true) {
	}
	return it.Error()
}

// Tests that an empty trie is not scheduled for syncing.
func TestEmptySync(t *testing.T) {
	cdbA := NewCacheDb(db.NewMemDb())
	cdbB := NewCacheDb(db.NewMemDb())
	emptyA, _ := NewWithCacheDb(ec.Hash256{}, cdbA)
	emptyB, _ := NewWithCacheDb(emptyRootHash, cdbB)

	for i, tr := range []*Trie{emptyA, emptyB} {
		if req := NewSync(tr.RootHash(), db.NewMemDb(), nil).Missing(1); len(req) != 0 {
			t.Errorf("test %d: content requested for empty trie: %v", i, req)
		}
	}
}

// Tests that given a root hash, a trie can sync iteratively on a single thread,
// requesting retrieval tasks and returning all of them in one go.
func TestIterativeSyncIndividual(t *testing.T) { testIterativeSync(t, 1) }
func TestIterativeSyncBatched(t *testing.T)    { testIterativeSync(t, 100) }

func testIterativeSync(t *testing.T, batch int) {
	// Create a random trie to copy
	srcDb, srcTrie, srcData := makeTestTrie()

	// Create a destination trie and sync with the scheduler
	kvdb := db.NewMemDb()
	cdb := NewCacheDb(kvdb)
	sched := NewSync(srcTrie.RootHash(), kvdb, nil)

	queue := append([]ec.Hash256{}, sched.Missing(batch)...)
	for len(queue) > 0 {
		results := make([]SyncResult, len(queue))
		for i, hash := range queue {
			data := srcDb.GetNodeBlob(hash)
			if data == nil {
				t.Fatalf("failed to retrieve node data for %x", hash)
			}
			results[i] = SyncResult{hash, data}
		}
		if _, index, err := sched.Process(results); err != nil {
			t.Fatalf("failed to process result #%d: %v", index, err)
		}
		if index, err := sched.Commit(kvdb); err != nil {
			t.Fatalf("failed to commit data #%d: %v", index, err)
		}
		queue = append(queue[:0], sched.Missing(batch)...)
	}
	// Cross check that the two tries are in sync
	checkTrieContents(t, cdb, srcTrie.RootHashBytes(), srcData)
}

// Tests that the trie scheduler can correctly reconstruct the state even if only
// partial results are returned, and the others sent only later.
func TestIterativeDelayedSync(t *testing.T) {
	// Create a random trie to copy
	srcDb, srcTrie, srcData := makeTestTrie()

	// Create a destination trie and sync with the scheduler
	kvdb := db.NewMemDb()
	cdb := NewCacheDb(kvdb)
	sched := NewSync(srcTrie.RootHash(), kvdb, nil)

	queue := append([]ec.Hash256{}, sched.Missing(10000)...)
	for len(queue) > 0 {
		// Sync only half of the scheduled nodes
		results := make([]SyncResult, len(queue)/2+1)
		for i, hash := range queue[:len(results)] {
			data := srcDb.GetNodeBlob(hash)
			if data == nil {
				t.Fatalf("failed to retrieve node data for %x", hash)
			}
			results[i] = SyncResult{hash, data}
		}
		if _, index, err := sched.Process(results); err != nil {
			t.Fatalf("failed to process result #%d: %v", index, err)
		}
		if index, err := sched.Commit(kvdb); err != nil {
			t.Fatalf("failed to commit data #%d: %v", index, err)
		}
		queue = append(queue[len(results):], sched.Missing(10000)...)
	}
	// Cross check that the two tries are in sync
	checkTrieContents(t, cdb, srcTrie.RootHashBytes(), srcData)
}

// Tests that given a root hash, a trie can sync iteratively on a single thread,
// requesting retrieval tasks and returning all of them in one go, however in a
// random order.
func TestIterativeRandomSyncIndividual(t *testing.T) { testIterativeRandomSync(t, 1) }
func TestIterativeRandomSyncBatched(t *testing.T)    { testIterativeRandomSync(t, 100) }

func testIterativeRandomSync(t *testing.T, batch int) {
	// Create a random trie to copy
	srcDb, srcTrie, srcData := makeTestTrie()

	// Create a destination trie and sync with the scheduler
	kvdb := db.NewMemDb()
	cdb := NewCacheDb(kvdb)
	sched := NewSync(srcTrie.RootHash(), kvdb, nil)

	queue := make(map[ec.Hash256]struct{})
	for _, hash := range sched.Missing(batch) {
		queue[hash] = struct{}{}
	}
	for len(queue) > 0 {
		// Fetch all the queued nodes in a random order
		results := make([]SyncResult, 0, len(queue))
		for hash := range queue {
			data := srcDb.GetNodeBlob(hash)
			if data == nil {
				t.Fatalf("failed to retrieve node data for %x", hash)
			}
			results = append(results, SyncResult{hash, data})
		}
		// Feed the retrieved results back and queue new tasks
		if _, index, err := sched.Process(results); err != nil {
			t.Fatalf("failed to process result #%d: %v", index, err)
		}
		if index, err := sched.Commit(kvdb); err != nil {
			t.Fatalf("failed to commit data #%d: %v", index, err)
		}
		queue = make(map[ec.Hash256]struct{})
		for _, hash := range sched.Missing(batch) {
			queue[hash] = struct{}{}
		}
	}
	// Cross check that the two tries are in sync
	checkTrieContents(t, cdb, srcTrie.RootHashBytes(), srcData)
}

// Tests that the trie scheduler can correctly reconstruct the state even if only
// partial results are returned (Even those randomly), others sent only later.
func TestIterativeRandomDelayedSync(t *testing.T) {
	// Create a random trie to copy
	srcDb, srcTrie, srcData := makeTestTrie()

	// Create a destination trie and sync with the scheduler
	kvdb := db.NewMemDb()
	cdb := NewCacheDb(kvdb)
	sched := NewSync(srcTrie.RootHash(), kvdb, nil)

	queue := make(map[ec.Hash256]struct{})
	for _, hash := range sched.Missing(10000) {
		queue[hash] = struct{}{}
	}
	for len(queue) > 0 {
		// Sync only half of the scheduled nodes, even those in random order
		results := make([]SyncResult, 0, len(queue)/2+1)
		for hash := range queue {
			data := srcDb.GetNodeBlob(hash)
			if data == nil {
				t.Fatalf("failed to retrieve node data for %x", hash)
			}
			results = append(results, SyncResult{hash, data})

			if len(results) >= cap(results) {
				break
			}
		}
		// Feed the retrieved results back and queue new tasks
		if _, index, err := sched.Process(results); err != nil {
			t.Fatalf("failed to process result #%d: %v", index, err)
		}
		if index, err := sched.Commit(kvdb); err != nil {
			t.Fatalf("failed to commit data #%d: %v", index, err)
		}
		for _, result := range results {
			delete(queue, result.Hash)
		}
		for _, hash := range sched.Missing(10000) {
			queue[hash] = struct{}{}
		}
	}
	// Cross check that the two tries are in sync
	checkTrieContents(t, cdb, srcTrie.RootHashBytes(), srcData)
}

// Tests that a trie sync will not request nodes multiple times, even if they
// have such references.
func TestDuplicateAvoidanceSync(t *testing.T) {
	// Create a random trie to copy
	srcDb, srcTrie, srcData := makeTestTrie()

	// Create a destination trie and sync with the scheduler
	kvdb := db.NewMemDb()
	cdb := NewCacheDb(kvdb)
	sched := NewSync(srcTrie.RootHash(), kvdb, nil)

	queue := append([]ec.Hash256{}, sched.Missing(0)...)
	requested := make(map[ec.Hash256]struct{})

	for len(queue) > 0 {
		results := make([]SyncResult, len(queue))
		for i, hash := range queue {
			data := srcDb.GetNodeBlob(hash)
			if data == nil {
				t.Fatalf("failed to retrieve node data for %x", hash)
			}
			if _, ok := requested[hash]; ok {
				t.Errorf("hash %x already requested once", hash)
			}
			requested[hash] = struct{}{}

			results[i] = SyncResult{hash, data}
		}
		if _, index, err := sched.Process(results); err != nil {
			t.Fatalf("failed to process result #%d: %v", index, err)
		}
		if index, err := sched.Commit(kvdb); err != nil {
			t.Fatalf("failed to commit data #%d: %v", index, err)
		}
		queue = append(queue[:0], sched.Missing(0)...)
	}
	// Cross check that the two tries are in sync
	checkTrieContents(t, cdb, srcTrie.RootHashBytes(), srcData)
}

// Tests that at any point in time during a sync, only complete sub-tries are in
// the database.
func TestIncompleteSync(t *testing.T) {
	// Create a random trie to copy
	srcDb, srcTrie, _ := makeTestTrie()

	// Create a destination trie and sync with the scheduler
	kvdb := db.NewMemDb()
	cdb := NewCacheDb(kvdb)
	sched := NewSync(srcTrie.RootHash(), kvdb, nil)

	added := []ec.Hash256{}
	queue := append([]ec.Hash256{}, sched.Missing(1)...)
	for len(queue) > 0 {
		// Fetch a batch of trie nodes
		results := make([]SyncResult, len(queue))
		for i, hash := range queue {
			data := srcDb.GetNodeBlob(hash)
			if data == nil {
				t.Fatalf("failed to retrieve node data for %x", hash)
			}
			results[i] = SyncResult{hash, data}
		}
		// Process each of the trie nodes
		if _, index, err := sched.Process(results); err != nil {
			t.Fatalf("failed to process result #%d: %v", index, err)
		}
		if index, err := sched.Commit(kvdb); err != nil {
			t.Fatalf("failed to commit data #%d: %v", index, err)
		}
		for _, result := range results {
			added = append(added, result.Hash)
		}
		// Check that all known sub-tries in the synced trie are complete
		for _, root := range added {
			if err := checkTrieConsistency(cdb, root); err != nil {
				t.Fatalf("trie inconsistent: %v", err)
			}
		}
		// Fetch the next batch to retrieve
		queue = append(queue[:0], sched.Missing(1)...)
	}
	// Sanity check that removing any node from the database is detected
	for _, node := range added[1:] {
		key := node[:]
		value, _ := kvdb.Get(key)

		kvdb.Remove(key)
		if err := checkTrieConsistency(cdb, added[0]); err == nil {
			t.Fatalf("trie inconsistency not caught, missing: %x", key)
		}
		kvdb.Put(key, value)
	}
}
