// Copyright 2014 The go-ethereum Authors
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
	"bvchain/ec"
	"bvchain/db"
	"bvchain/util"

	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

func TestIterator(t *testing.T) {
	tr := newEmpty()
	vals := []_StrKV{
		{"do", "verb"},
		{"ether", "wookiedoo"},
		{"horse", "stallion"},
		{"shaman", "horse"},
		{"doge", "coin"},
		{"dog", "puppy"},
		{"somethingveryoddindeedthis is", "myothernodedata"},
		{"d", "letter"},
	}
	all := make(map[string]string)
	for _, val := range vals {
		all[val.k] = val.v
		tr.Update([]byte(val.k), []byte(val.v))
	}
	tr.Commit(nil)

	found := make(map[string]string)
	it := NewIterator(tr.NodeIterator(nil))
	for it.Next() {
		t.Logf("KV %q=%q\n", it.Key, it.Value)
		found[string(it.Key)] = string(it.Value)
	}

	for k, v := range all {
		if found[k] != v {
			t.Errorf("iterator value mismatch for %q: got %q want %q", k, found[k], v)
		}
	}
}

func TestIteratorLargeData(t *testing.T) {
	type _BytesKV struct {
		k, v []byte
		t    bool
	}

	tr := newEmpty()
	vals := make(map[string]*_BytesKV)

	for i := byte(0); i < 255; i++ {
		value := &_BytesKV{util.LeftPadBytes([]byte{i}, 32), []byte{i}, false}
		value2 := &_BytesKV{util.LeftPadBytes([]byte{10, i}, 32), []byte{i}, false}
		tr.Update(value.k, value.v)
		tr.Update(value2.k, value2.v)
		vals[string(value.k)] = value
		vals[string(value2.k)] = value2
	}

	it := NewIterator(tr.NodeIterator(nil))
	for it.Next() {
		vals[string(it.Key)].t = true
	}

	var untouched []*_BytesKV
	for _, value := range vals {
		if !value.t {
			untouched = append(untouched, value)
		}
	}

	if len(untouched) > 0 {
		t.Errorf("Missed %d nodes", len(untouched))
		for _, value := range untouched {
			t.Error(value)
		}
	}
}

// Tests that the node iterator indeed walks over the entire database contents.
func TestNodeIteratorCoverage(t *testing.T) {
	// Create some arbitrary test trie to iterate
	cdb, tr, _ := makeTestTrie()

	// Gather all the node hashes found by the iterator
	hashes := make(map[ec.Hash256]struct{})
	for it := tr.NodeIterator(nil); it.Next(true); {
		if it.Hash() != (ec.Hash256{}) {
			hashes[it.Hash()] = struct{}{}
		}
	}
	// Cross check the hashes and the database itself
	for hash := range hashes {
		if bz := cdb.GetNodeBlob(hash); bz == nil {
			t.Errorf("failed to retrieve reported node %x", hash)
		}
	}
	for hash, obj := range cdb.items {
		if obj != nil && hash != (ec.Hash256{}) {
			if _, ok := hashes[hash]; !ok {
				t.Errorf("state entry not reported %x", hash)
			}
		}
	}
	for _, key := range cdb.kvdb.(*db.MemDb).Keys() {
		if _, ok := hashes[ec.BytesToHash256(key)]; !ok {
			t.Errorf("state entry not reported %x", key)
		}
	}
}

type _StrKV struct{ k, v string }

var _testdata1 = []_StrKV{
	{"bar", "b"},
	{"barb", "ba"},
	{"bard", "bc"},
	{"bars", "bb0123456789abcdefghijklmnopqrstuvwxyz"},
	{"fab", "z"},
	{"foo", "a"},
	{"food", "abcdefghijklmnopqrstuvwxyz0123456789"},
	{"foos", "aa"},
}

var _testdata2 = []_StrKV{
	{"aardvark", "c"},
	{"bar", "b"},
	{"barb", "bd"},
	{"bars", "be"},
	{"fab", "z"},
	{"foo", "a"},
	{"foos", "aa"},
	{"food", "abcdefghijklmnopqrstuvwxyz0123456789"},
	{"jars", "d"},
}

func TestIteratorSeek(t *testing.T) {
	tr := newEmpty()
	for _, val := range _testdata1 {
		tr.Update([]byte(val.k), []byte(val.v))
	}

	// Seek to the middle.
	it := NewIterator(tr.NodeIterator([]byte("fab")))
	if err := checkIteratorOrder(_testdata1[4:], it); err != nil {
		t.Fatal(err)
	}

	// Seek to a non-existent key.
	it = NewIterator(tr.NodeIterator([]byte("barc")))
	if err := checkIteratorOrder(_testdata1[2:], it); err != nil {
		t.Fatal(err)
	}

	// Seek beyond the end.
	it = NewIterator(tr.NodeIterator([]byte("z")))
	if err := checkIteratorOrder(nil, it); err != nil {
		t.Fatal(err)
	}
}

func checkIteratorOrder(want []_StrKV, it *Iterator) error {
	for it.Next() {
		if len(want) == 0 {
			return fmt.Errorf("didn't expect any more values, got key %q", it.Key)
		}
		if !bytes.Equal(it.Key, []byte(want[0].k)) {
			return fmt.Errorf("wrong key: got %q, want %q", it.Key, want[0].k)
		}
		want = want[1:]
	}
	if len(want) > 0 {
		return fmt.Errorf("iterator ended early, want key %q", want[0])
	}
	return nil
}

func TestDiffIterator(t *testing.T) {
	tr1 := newEmpty()
	for _, val := range _testdata1 {
		tr1.Update([]byte(val.k), []byte(val.v))
	}
	tr1.Commit(nil)

	tr2 := newEmpty()
	for _, val := range _testdata2 {
		tr2.Update([]byte(val.k), []byte(val.v))
	}
	tr2.Commit(nil)

	found := make(map[string]string)
	di, _ := NewDiffIterator(tr1.NodeIterator(nil), tr2.NodeIterator(nil))
	it := NewIterator(di)
	for it.Next() {
		t.Logf("KV %q=%q\n", it.Key, it.Value)
		found[string(it.Key)] = string(it.Value)
	}

	all := []_StrKV{
		{"aardvark", "c"},
		{"barb", "bd"},
		{"bars", "be"},
		{"jars", "d"},
	}
	for _, item := range all {
		if found[item.k] != item.v {
			t.Errorf("iterator value mismatch for %q: got %q want %q", item.k, found[item.k], item.v)
		}
	}
	if len(found) != len(all) {
		t.Errorf("iterator count mismatch: got %d values, want %d", len(found), len(all))
	}
}

func TestUnionIterator(t *testing.T) {
	tr1 := newEmpty()
	for _, val := range _testdata1 {
		tr1.Update([]byte(val.k), []byte(val.v))
	}
	tr1.Commit(nil)

	tr2 := newEmpty()
	for _, val := range _testdata2 {
		tr2.Update([]byte(val.k), []byte(val.v))
	}
	tr2.Commit(nil)

	di, _ := NewUnionIterator([]NodeIterator{tr1.NodeIterator(nil), tr2.NodeIterator(nil)})
	it := NewIterator(di)

	all := []_StrKV{
		{"aardvark", "c"},
		{"bar", "b"},
		{"barb", "ba"},
		{"barb", "bd"},
		{"bard", "bc"},
		{"bars", "bb0123456789abcdefghijklmnopqrstuvwxyz"},
		{"bars", "be"},
		{"fab", "z"},
		{"foo", "a"},
		{"food", "abcdefghijklmnopqrstuvwxyz0123456789"},
		{"foos", "aa"},
		{"jars", "d"},
	}

	for i, kv := range all {
		if !it.Next() {
			t.Errorf("Iterator ends prematurely at element %d", i)
		}
		if kv.k != string(it.Key) {
			t.Errorf("iterator value mismatch for element %d: got key %q want %q", i, it.Key, kv.k)
		}
		if kv.v != string(it.Value) {
			t.Errorf("iterator value mismatch for element %d: got value %q want %q", i, it.Value, kv.v)
		}
	}
	if it.Next() {
		t.Errorf("Iterator returned extra values.")
	}
}

func TestIteratorNoDups(t *testing.T) {
	var tr Trie
	for _, val := range _testdata1 {
		tr.Update([]byte(val.k), []byte(val.v))
	}
	checkIteratorNoDups(t, tr.NodeIterator(nil), nil)
}

// This test checks that nodeIterator.Next can be retried after inserting missing trie nodes.
func TestIteratorContinueAfterErrorDisk(t *testing.T)    { testIteratorContinueAfterError(t, false) }
func TestIteratorContinueAfterErrorMemonly(t *testing.T) { testIteratorContinueAfterError(t, true) }

func testIteratorContinueAfterError(t *testing.T, memOnly bool) {
	kvdb := db.NewMemDb()
	cdb := NewCacheDb(kvdb)

	tr, _ := NewWithCacheDb(ec.Hash256{}, cdb)
	for _, val := range _testdata1 {
		tr.Update([]byte(val.k), []byte(val.v))
	}
	tr.Commit(nil)

	if !memOnly {
		cdb.Commit(tr.RootHash(), true)
	}
	wantNodeCount := checkIteratorNoDups(t, tr.NodeIterator(nil), nil)

	var diskKeys [][]byte
	var memKeys []ec.Hash256
	if memOnly {
		memKeys = cdb.NodeKeys()
	} else {
		diskKeys = kvdb.Keys()
	}
	for i := 0; i < 20; i++ {
		// Create trie that will load all nodes from DB.
		tr, _ := NewWithCacheDb(tr.RootHash(), cdb)

		// Remove a random node from the database. It can't be the root node
		// because that one is already loaded.
		var rkey ec.Hash256
		var rval []byte
		var robj *_CacheItem
		for {
			if memOnly {
				rkey = memKeys[rand.Intn(len(memKeys))]
			} else {
				copy(rkey[:], diskKeys[rand.Intn(len(diskKeys))])
			}
			if rkey != tr.RootHash() {
				break
			}
		}
		if memOnly {
			robj = cdb.items[rkey]
			delete(cdb.items, rkey)
		} else {
			rval, _ = kvdb.Get(rkey[:])
			kvdb.Remove(rkey[:])
		}
		// Iterate until the error is hit.
		seen := make(map[string]bool)
		it := tr.NodeIterator(nil)
		checkIteratorNoDups(t, it, seen)
		missing, ok := it.Error().(*MissingNodeError)
		if !ok || missing.NodeHash != rkey {
			t.Fatal("didn't hit missing node, got", it.Error())
		}

		// Add the node back and continue iteration.
		if memOnly {
			cdb.items[rkey] = robj
		} else {
			kvdb.Put(rkey[:], rval)
		}
		checkIteratorNoDups(t, it, seen)
		if it.Error() != nil {
			t.Fatal("unexpected error", it.Error())
		}
		if len(seen) != wantNodeCount {
			t.Fatal("wrong node iteration count, got", len(seen), "want", wantNodeCount)
		}
	}
}

// Similar to the test above, this one checks that failure to create nodeIterator at a
// certain key prefix behaves correctly when Next is called. The expectation is that Next
// should retry seeking before returning true for the first time.
func TestIteratorContinueAfterSeekErrorDisk(t *testing.T) {
	testIteratorContinueAfterSeekError(t, false)
}
func TestIteratorContinueAfterSeekErrorMemonly(t *testing.T) {
	testIteratorContinueAfterSeekError(t, true)
}

func testIteratorContinueAfterSeekError(t *testing.T, memOnly bool) {
	// Commit test trie to db, then remove the node containing "bars".
	kvdb := db.NewMemDb()
	cdb := NewCacheDb(kvdb)

	tr0, _ := NewWithCacheDb(ec.Hash256{}, cdb)
	for _, val := range _testdata1 {
		tr0.Update([]byte(val.k), []byte(val.v))
	}
	root, _ := tr0.Commit(nil)
	if !memOnly {
		cdb.Commit(root, true)
	}

	if false {	// ON-OFF: print nodes that have has value
		it := tr0.NodeIterator(nil)
		for it.Next(true) {
			if it.HasValue() {
				fmt.Println(it.Hash(), it.Path(), it.LeafValue())
			}
		}
	}

	barNodeHash := ec.MustHexToHash256("7540b831600496d9855aee250adcbf6b4a8f353df831c813cfb711f76cd2a8d6")

	var barNodeBlob []byte
	var barNodeObj  *_CacheItem
	if memOnly {
		barNodeObj = cdb.items[barNodeHash]
		delete(cdb.items, barNodeHash)
	} else {
		barNodeBlob, _ = kvdb.Get(barNodeHash[:])
		kvdb.Remove(barNodeHash[:])
	}
	// Create a new iterator that seeks to "bars". Seeking can't proceed because
	// the node is missing.
	tr, _ := NewWithCacheDb(root, cdb)
	it := tr.NodeIterator([]byte("bars"))
	missing, ok := it.Error().(*MissingNodeError)
	if !ok {
		t.Fatal("want MissingNodeError, got", it.Error())
	} else if missing.NodeHash != barNodeHash {
		t.Fatal("wrong node missing")
	}
	// Reinsert the missing node.
	if memOnly {
		cdb.items[barNodeHash] = barNodeObj
	} else {
		kvdb.Put(barNodeHash[:], barNodeBlob)
	}
	// Check that iteration produces the right set of values.
	if err := checkIteratorOrder(_testdata1[3:], NewIterator(it)); err != nil {
		t.Fatal(err)
	}
}

func checkIteratorNoDups(t *testing.T, it NodeIterator, seen map[string]bool) int {
	if seen == nil {
		seen = make(map[string]bool)
	}
	for it.Next(true) {
		if seen[string(it.Path())] {
			t.Fatalf("iterator visited node path %x twice", it.Path())
		}
		seen[string(it.Path())] = true
	}
	return len(seen)
}

