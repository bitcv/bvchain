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
	"bvchain/util"

	"bytes"
	"errors"
	"container/heap"
)

// Iterator is a key-value trie iterator that traverses a Trie.
type Iterator struct {
	nodeIter NodeIterator

	Key   []byte // Current data key on which the iterator is positioned on
	Value []byte // Current data value on which the iterator is positioned on
	Err   error
}

// NewIterator creates a new key-value iterator from a node iterator
func NewIterator(nodeIter NodeIterator) *Iterator {
	return &Iterator{
		nodeIter: nodeIter,
	}
}

// Next moves the iterator forward one key-value entry.
func (it *Iterator) Next() bool {
	for it.nodeIter.Next(true) {
		if it.nodeIter.HasValue() {
			it.Key = it.nodeIter.LeafKey()
			it.Value = it.nodeIter.LeafValue()
			return true
		}
	}
	it.Key = nil
	it.Value = nil
	it.Err = it.nodeIter.Error()
	return false
}

// Prove generates the Merkle proof for the leaf node the iterator is currently
// positioned on.
func (it *Iterator) Prove() [][]byte {
	return it.nodeIter.LeafProof()
}

// NodeIterator is an iterator to traverse the trie pre-order.
type NodeIterator interface {
	// Next moves the iterator to the next node. If the parameter is false, any child
	// nodes will be skipped.
	Next(bool) bool

	// Error returns the error status of the iterator.
	Error() error

	// Hash returns the hash of the current node.
	Hash() ec.Hash256

	// ParentHash returns the hash of the parent of the current node. The hash may be the one
	// grandparent if the immediate parent is an internal node with no hash.
	ParentHash() ec.Hash256

	// Path returns the hex-encoded path to the current node.
	// Callers must not retain references to the return value after calling Next.
	// For leaf nodes, the last element of the path is the 'terminator symbol' 0x10.
	Path() []byte

	// HasValue returns true iff the current node is a leaf node.
	HasValue() bool

	// LeafKey returns the key of the leaf. The method panics if the iterator is not
	// positioned at a leaf. Callers must not retain references to the value after
	// calling Next.
	LeafKey() []byte

	// LeafValue returns the content of the leaf. The method panics if the iterator
	// is not positioned at a leaf. Callers must not retain references to the value
	// after calling Next.
	LeafValue() []byte

	// LeafProof returns the Merkle proof of the leaf. The method panics if the
	// iterator is not positioned at a leaf. Callers must not retain references
	// to the value after calling Next.
	LeafProof() [][]byte
}

// _NodeIterState represents the iteration state at one particular node of the
// trie, which can be resumed at a later invocation.
type _NodeIterState struct {
	node _Node        // Trie node being iterated
	hash ec.Hash256 // Hash of the node being iterated (nil if not standalone)
	parentHash ec.Hash256 // Hash of the first full ancestor node (nil if current is the root)
	index   int         // Child to be processed next
	pathlen int         // Length of the path to this node
}

type _NodeIterator struct {
	tr    *Trie                // Trie being iterated
	stack []*_NodeIterState // Hierarchy of trie nodes persisting the iteration state
	path  []byte               // Path to the current node
	err   error                // Failure set in case of an internal error in the iterator
}

// _ErrIteratorEnd is stored in _NodeIterator.err when iteration is done.
var _ErrIteratorEnd = errors.New("end of iteration")

// _SeekError is stored in _NodeIterator.err if the initial seek has failed.
type _SeekError struct {
	key []byte
	err error
}

func (e _SeekError) Error() string {
	return "seek error: " + e.err.Error()
}

func newNodeIterator(tr *Trie, start []byte) NodeIterator {
	rootHash := tr.RootHash()
        if rootHash == (ec.Hash256{}) || rootHash == emptyRootHash {
		return &_NodeIterator{err:_ErrIteratorEnd,}
	}
	it := &_NodeIterator{tr: tr}
	it.err = it.seek(start)
	return it
}

func (it *_NodeIterator) Hash() ec.Hash256 {
	if len(it.stack) == 0 {
		return ec.Hash256{}
	}
	return it.stack[len(it.stack)-1].hash
}

func (it *_NodeIterator) ParentHash() ec.Hash256 {
	if len(it.stack) == 0 {
		return ec.Hash256{}
	}
	return it.stack[len(it.stack)-1].parentHash
}

func (it *_NodeIterator) HasValue() bool {
	n := len(it.stack)
	if n > 0 {
		val := it.stack[n-1].node.GetValue()
		return len(val) > 0
	}
	return false
}

func (it *_NodeIterator) LeafKey() []byte {
	n := len(it.stack)
	if n > 0 {
		val := it.stack[n-1].node.GetValue()
		if len(val) > 0 {
			return path2key(it.path)
		}
	}
	panic("not at leaf")
}

func (it *_NodeIterator) LeafValue() []byte {
	n := len(it.stack)
	if n > 0 {
		val := it.stack[n-1].node.GetValue()
		if len(val) > 0 {
			return val
		}
	}
	panic("not at leaf")
}

func (it *_NodeIterator) LeafProof() [][]byte {
	panic("XXX: not implemented")

	n := len(it.stack)
	if n > 0 {
		if _, ok := it.stack[n-1].node.(*_LeafNode); ok {
			proofs := make([][]byte, 0, n)
			/*
			for i, item := range it.stack[:n-1] {
				// TODO
			}
			*/
			return proofs
		}
	}
	panic("not at leaf")
}

func (it *_NodeIterator) Path() []byte {
	return it.path
}

func (it *_NodeIterator) Error() error {
	if it.err == _ErrIteratorEnd {
		return nil
	}
	if seekErr, ok := it.err.(_SeekError); ok {
		return seekErr.err
	}
	return it.err
}

// Next moves the iterator to the next node, returning whether there are any
// further nodes. In case of an internal error this method returns false and
// sets the Error field to the encountered failure. If `descend` is false,
// skips iterating over any subnodes of the current node.
func (it *_NodeIterator) Next(descend bool) bool {
	if it.err == _ErrIteratorEnd {
		return false
	}
	if seekErr, ok := it.err.(_SeekError); ok {
		if it.err = it.seek(seekErr.key); it.err != nil {
			return false
		}
	}
	// Otherwise step forward with the iterator and report any errors.
	state, parentIndex, path, err := it.peek(descend)
	it.err = err
	if it.err != nil {
		return false
	}
	it.push(state, parentIndex, path)
	return true
}

func (it *_NodeIterator) seek(prefix []byte) error {
	keyPath := key2path(prefix)
	// Move forward until we're just before the closest match to key.
	for {
		state, parentIndex, path, err := it.peek(bytes.HasPrefix(keyPath, it.path))
		if err == _ErrIteratorEnd {
			return _ErrIteratorEnd
		} else if err != nil {
			return _SeekError{prefix, err}
		} else if bytes.Compare(path, keyPath) >= 0 {
			return nil
		}
		it.push(state, parentIndex, path)
	}
}

// peek creates the next state of the iterator.
func (it *_NodeIterator) peek(descend bool) (*_NodeIterState, *int, []byte, error) {
	if len(it.stack) == 0 {
		// Initialize the iterator if we've just started.
		rootHash := it.tr.RootHash()
		state := &_NodeIterState{node: it.tr.rootNode, index: -1}
		if rootHash != emptyRootHash {
			state.hash = rootHash
		}
		path, err := state.resolve(it.tr, nil)
		return state, nil, path, err
	}

	if !descend {
		// If we're skipping children, pop the current node first
		it.pop()
	}

	// Continue iteration to the next child
	for len(it.stack) > 0 {
		parent := it.stack[len(it.stack)-1]
		ancestor := parent.hash
		if (ancestor == ec.Hash256{}) {
			ancestor = parent.parentHash
		}
		state, path := it.nextChild(parent, ancestor)
		if state != nil {
			path, err := state.resolve(it.tr, path)
			if err != nil {
				return parent, &parent.index, path, err
			}
			return state, &parent.index, path, nil
		}
		// No more child nodes, move back up.
		it.pop()
	}
	return nil, nil, nil, _ErrIteratorEnd
}

func (st *_NodeIterState) resolve(tr *Trie, path []byte) ([]byte, error) {
	switch n := st.node.(type) {
	case _HashNode:
		resolved := tr.resolveHash(n[:])
		if resolved == nil {
			return path, &MissingNodeError{NodeHash:ec.Hash256(n), Path:path}
		}
		st.node = resolved
		st.hash = ec.Hash256(n)

	case _BlobNode:
		resolved, err := DecodeNode(n[:], tr.cacheGen, nil)
		if err != nil {
			util.CantReachHere()
		}
		st.node = resolved
		st.hash = ec.Hash256{}
	}

	switch node := st.node.(type) {
	case *_LeafNode:
		path = append(path, node.Path...)
	case *_BranchNode:
		path = append(path, node.Path...)
	}
	return path, nil
}

func (it *_NodeIterator) nextChild(parent *_NodeIterState, ancestor ec.Hash256) (*_NodeIterState, []byte) {
	node, ok := parent.node.(*_BranchNode)
	if ok {
		for i := parent.index + 1; i < len(node.Children); i++ {
			child := node.Children[i]
			if child != nil {
				blobSize := child.blobSize()
				if blobSize == 0 {
					h := newNodeHasher(nil, 0, 0)
					h.DoHash(child, false)
				}

				hash := child.cachedHash()
				state := &_NodeIterState{
					hash: ec.BytesToHash256(hash),
					node: child,
					parentHash: ancestor,
					index: -1,
					pathlen: len(it.path),
				}
				path := append(it.path, byte(i))
				parent.index = i - 1
				return state, path
			}
		}
	}
	return nil, nil
}

func (it *_NodeIterator) push(state *_NodeIterState, parentIndex *int, path []byte) {
	it.path = path
	it.stack = append(it.stack, state)
	if parentIndex != nil {
		*parentIndex++
	}
}

func (it *_NodeIterator) pop() {
	n := len(it.stack)
	parent := it.stack[n-1]
	it.path = it.path[:parent.pathlen]
	it.stack = it.stack[:n-1]
}

func comparePathValue(a, b NodeIterator) int {
	if cmp := bytes.Compare(a.Path(), b.Path()); cmp != 0 {
		return cmp
	}

	av := a.HasValue()
	bv := b.HasValue()

	if av && !bv {
		return -1
	} else if !av && bv {
		return 1
	}

	if av && bv {
		return bytes.Compare(a.LeafValue(), b.LeafValue())
	}

	return 0
}

type _DiffIterator struct {
	a, b  NodeIterator // Nodes returned are those in b - a.
	eof   bool         // Indicates a has run out of elements
	count int          // Number of nodes scanned on either trie
}

// NewDiffIterator constructs a NodeIterator that iterates over elements in b that
// are not in a. Returns the iterator, and a pointer to an integer recording the number
// of nodes seen.
func NewDiffIterator(a, b NodeIterator) (NodeIterator, *int) {
	a.Next(true)
	it := &_DiffIterator{
		a: a,
		b: b,
	}
	return it, &it.count
}

func (it *_DiffIterator) Hash() ec.Hash256 {
	return it.b.Hash()
}

func (it *_DiffIterator) ParentHash() ec.Hash256 {
	return it.b.ParentHash()
}

func (it *_DiffIterator) HasValue() bool {
	return it.b.HasValue()
}

func (it *_DiffIterator) LeafKey() []byte {
	return it.b.LeafKey()
}

func (it *_DiffIterator) LeafValue() []byte {
	return it.b.LeafValue()
}

func (it *_DiffIterator) LeafProof() [][]byte {
	return it.b.LeafProof()
}

func (it *_DiffIterator) Path() []byte {
	return it.b.Path()
}

func (it *_DiffIterator) Next(bool) bool {
	// Invariants:
	// - We always advance at least one element in b.
	// - At the start of this function, a's path is lexically greater than b's.
	if !it.b.Next(true) {
		return false
	}
	it.count++

	if it.eof {
		// a has reached eof, so we just return all elements from b
		return true
	}

	for {
		switch comparePathValue(it.a, it.b) {
		case -1:
			// b jumped past a; advance a
			if !it.a.Next(true) {
				it.eof = true
				return true
			}
			it.count++
		case 1:
			// b is before a
			return true
		case 0:
			isDiff := (it.a.Hash() != it.b.Hash()) || it.a.Hash() == ec.Hash256{}
			if !it.b.Next(isDiff) {
				return false
			}
			it.count++
			if !it.a.Next(isDiff) {
				it.eof = true
				return true
			}
			it.count++
		}
	}
}

func (it *_DiffIterator) Error() error {
	if err := it.a.Error(); err != nil {
		return err
	}
	return it.b.Error()
}

type _NodeIteratorHeap []NodeIterator

func (h _NodeIteratorHeap) Len() int            { return len(h) }
func (h _NodeIteratorHeap) Less(i, j int) bool  { return comparePathValue(h[i], h[j]) < 0 }
func (h _NodeIteratorHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *_NodeIteratorHeap) Push(x interface{}) { *h = append(*h, x.(NodeIterator)) }
func (h *_NodeIteratorHeap) Pop() interface{} {
	n := len(*h)
	x := (*h)[n-1]
	*h = (*h)[0 : n-1]
	return x
}

type _UnionIterator struct {
	items *_NodeIteratorHeap // Nodes returned are the union of the ones in these iterators
	count int               // Number of nodes scanned across all tries
}

// NewUnionIterator constructs a NodeIterator that iterates over elements in the union
// of the provided NodeIterators. Returns the iterator, and a pointer to an integer
// recording the number of nodes visited.
func NewUnionIterator(iters []NodeIterator) (NodeIterator, *int) {
	h := make(_NodeIteratorHeap, len(iters))
	copy(h, iters)
	heap.Init(&h)

	ui := &_UnionIterator{items: &h}
	return ui, &ui.count
}

func (it *_UnionIterator) Hash() ec.Hash256 {
	return (*it.items)[0].Hash()
}

func (it *_UnionIterator) ParentHash() ec.Hash256 {
	return (*it.items)[0].ParentHash()
}

func (it *_UnionIterator) HasValue() bool {
	return (*it.items)[0].HasValue()
}

func (it *_UnionIterator) LeafKey() []byte {
	return (*it.items)[0].LeafKey()
}

func (it *_UnionIterator) LeafValue() []byte {
	return (*it.items)[0].LeafValue()
}

func (it *_UnionIterator) LeafProof() [][]byte {
	return (*it.items)[0].LeafProof()
}

func (it *_UnionIterator) Path() []byte {
	return (*it.items)[0].Path()
}

// Next returns the next node in the union of tries being iterated over.
//
// It does this by maintaining a heap of iterators, sorted by the iteration
// order of their next elements, with one entry for each source trie. Each
// time Next() is called, it takes the least element from the heap to return,
// advancing any other iterators that also point to that same element. These
// iterators are called with descend=false, since we know that any nodes under
// these nodes will also be duplicates, found in the currently selected iterator.
// Whenever an iterator is advanced, it is pushed back into the heap if it still
// has elements remaining.
//
// In the case that descend=false - eg, we're asked to ignore all subnodes of the
// current node - we also advance any iterators in the heap that have the current
// path as a prefix.
func (it *_UnionIterator) Next(descend bool) bool {
	if len(*it.items) == 0 {
		return false
	}

	// Get the next key from the union
	least := heap.Pop(it.items).(NodeIterator)

	// Skip over other nodes as long as they're identical, or, if we're not descending, as
	// long as they have the same prefix as the current node.
	for len(*it.items) > 0 && ((!descend && bytes.HasPrefix((*it.items)[0].Path(), least.Path())) || comparePathValue(least, (*it.items)[0]) == 0) {
		skipped := heap.Pop(it.items).(NodeIterator)
		isDiff := skipped.Hash() != least.Hash() || skipped.Hash() == ec.Hash256{}
		if skipped.Next(isDiff) {
			it.count++
			// If there are more elements, push the iterator back on the heap
			heap.Push(it.items, skipped)
		}
	}
	if least.Next(descend) {
		it.count++
		heap.Push(it.items, least)
	}
	return len(*it.items) > 0
}

func (it *_UnionIterator) Error() error {
	for i := 0; i < len(*it.items); i++ {
		if err := (*it.items)[i].Error(); err != nil {
			return err
		}
	}
	return nil
}

