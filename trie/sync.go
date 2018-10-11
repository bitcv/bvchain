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
	"bvchain/ec"
	"bvchain/db"

	"errors"
	"fmt"
	"gopkg.in/karalabe/cookiejar.v2/collections/prque"
)

// ErrNotRequested is returned by the trie sync when it's requested to process a
// node it did not request.
var ErrNotRequested = errors.New("not requested")

// ErrAlreadyProcessed is returned by the trie sync when it's requested to process a
// node it already processed previously.
var ErrAlreadyProcessed = errors.New("already processed")

// _SyncRequest represents a scheduled or already in-flight state retrieval request.
type _SyncRequest struct {
	hash ec.Hash256 // Hash of the node data content to retrieve
	data []byte      // Data content of the node, cached until all subtrees complete
	raw  bool        // Whether this is a raw entry (code) or a trie node

	parents []*_SyncRequest // Parent state nodes referencing this entry (notify all upon completion)
	depth   int        // Depth level within the trie the node is located to prioritise DFS
	deps    int        // Number of dependencies before allowed to commit this node

	callback LeafCallback // Callback to invoke if a leaf node it reached on this branch
}

// SyncResult is a simple list to return missing nodes along with their request
// hashes.
type SyncResult struct {
	Hash ec.Hash256 // Hash of the originally unknown trie node
	Data []byte     // Data content of the retrieved node
}

// _SyncMemBatch is an in-memory buffer of successfully downloaded but not yet
// persisted data items.
type _SyncMemBatch struct {
	batch map[ec.Hash256][]byte // In-memory membatch of recently completed items
	order []ec.Hash256          // Order of completion to prevent out-of-order data loss
}

// newSyncMemBatch allocates a new memory-buffer for not-yet persisted trie nodes.
func newSyncMemBatch() *_SyncMemBatch {
	return &_SyncMemBatch{
		batch: make(map[ec.Hash256][]byte),
		order: make([]ec.Hash256, 0, 256),
	}
}

// Sync is the main state trie synchronisation scheduler, which provides yet
// unknown trie hashes to retrieve, accepts node data associated with said hashes
// and reconstructs the trie step by step until all is done.
type Sync struct {
	kvdb db.KvDb		// Persistent database to check for existing entries
	membatch *_SyncMemBatch	// Memory buffer to avoid frequent database writes
	requests map[ec.Hash256]*_SyncRequest // Pending requests pertaining to a key hash
	queue *prque.Prque	// Priority queue with the pending requests
}

// NewSync creates a new trie data download scheduler.
func NewSync(root ec.Hash256, kvdb db.KvDb, callback LeafCallback) *Sync {
	ts := &Sync{
		kvdb: kvdb,
		membatch: newSyncMemBatch(),
		requests: make(map[ec.Hash256]*_SyncRequest),
		queue: prque.New(),
	}
	ts.AddSubTrie(root, 0, ec.Hash256{}, callback)
	return ts
}

// AddSubTrie registers a new trie to the sync code, rooted at the designated parent.
func (s *Sync) AddSubTrie(root ec.Hash256, depth int, parent ec.Hash256, callback LeafCallback) {
	// Short circuit if the trie is empty or already known
	if root == emptyRootHash || root == (ec.Hash256{}) {
		return
	}
	if _, ok := s.membatch.batch[root]; ok {
		return
	}
	key := root[:]
	blob, _ := s.kvdb.Get(key)
	if local, err := DecodeNode(blob, 0, key); local != nil && err == nil {
		return
	}
	// Assemble the new sub-trie sync request
	req := &_SyncRequest{
		hash: root,
		depth: depth,
		callback: callback,
	}
	// If this sub-trie has a designated parent, link them together
	if parent != (ec.Hash256{}) {
		ancestor := s.requests[parent]
		if ancestor == nil {
			panic(fmt.Sprintf("sub-trie ancestor not found: %x", parent))
		}
		ancestor.deps++
		req.parents = append(req.parents, ancestor)
	}
	s.schedule(req)
}

// AddRawEntry schedules the direct retrieval of a state entry that should not be
// interpreted as a trie node, but rather accepted and stored into the database
// as is. This method's goal is to support misc state metadata retrievals (e.g.
// contract code).
func (s *Sync) AddRawEntry(hash ec.Hash256, depth int, parent ec.Hash256) {
	// Short circuit if the entry is empty or already known
	if hash == (ec.Hash256{}) {
		return
	}
	if _, ok := s.membatch.batch[hash]; ok {
		return
	}
	if ok, _ := s.kvdb.Has(hash[:]); ok {
		return
	}
	// Assemble the new sub-trie sync request
	req := &_SyncRequest{
		hash:  hash,
		raw:   true,
		depth: depth,
	}
	// If this sub-trie has a designated parent, link them together
	if parent != (ec.Hash256{}) {
		ancestor := s.requests[parent]
		if ancestor == nil {
			panic(fmt.Sprintf("raw-entry ancestor not found: %x", parent))
		}
		ancestor.deps++
		req.parents = append(req.parents, ancestor)
	}
	s.schedule(req)
}

// Missing retrieves the known missing nodes from the trie for retrieval.
func (s *Sync) Missing(max int) []ec.Hash256 {
	requests := []ec.Hash256{}
	for !s.queue.Empty() && (max == 0 || len(requests) < max) {
		requests = append(requests, s.queue.PopItem().(ec.Hash256))
	}
	return requests
}

// Process injects a batch of retrieved trie nodes data, returning if something
// was committed to the database and also the index of an entry if processing of
// it failed.
func (s *Sync) Process(results []SyncResult) (bool, int, error) {
	committed := false

	for i, item := range results {
		// If the item was not requested, bail out
		request := s.requests[item.Hash]
		if request == nil {
			return committed, i, ErrNotRequested
		}
		if request.data != nil {
			return committed, i, ErrAlreadyProcessed
		}
		// If the item is a raw entry request, commit directly
		if request.raw {
			request.data = item.Data
			s.commit(request)
			committed = true
			continue
		}
		// Decode the node data content and update the request
		node, err := DecodeNode(item.Data, 0, item.Hash[:])
		if err != nil {
			return committed, i, err
		}
		request.data = item.Data

		// Create and schedule a request for all the children nodes
		requests, err := s.children(request, node)
		if err != nil {
			return committed, i, err
		}
		if len(requests) == 0 && request.deps == 0 {
			s.commit(request)
			committed = true
			continue
		}
		request.deps += len(requests)
		for _, child := range requests {
			s.schedule(child)
		}
	}
	return committed, 0, nil
}

// Commit flushes the data stored in the internal membatch out to persistent
// storage, returning the number of items written and any occurred error.
func (s *Sync) Commit(kvdb db.KvDb) (int, error) {
	for i, key := range s.membatch.order {
		if err := kvdb.Put(key[:], s.membatch.batch[key]); err != nil {
			return i, err
		}
	}
	written := len(s.membatch.order)

	// Drop the membatch data and return
	s.membatch = newSyncMemBatch()
	return written, nil
}

// Pending returns the number of state entries currently pending for download.
func (s *Sync) Pending() int {
	return len(s.requests)
}

// schedule inserts a new state retrieval request into the fetch queue. If there
// is already a pending request for this node, the new request will be discarded
// and only a parent reference added to the old one.
func (s *Sync) schedule(req *_SyncRequest) {
	// If we're already requesting this node, add a new reference and stop
	if old, ok := s.requests[req.hash]; ok {
		old.parents = append(old.parents, req.parents...)
		return
	}
	// Schedule the request for future retrieval
	s.queue.Push(req.hash, float32(req.depth))
	s.requests[req.hash] = req
}

// children retrieves all the missing children of a state trie entry for future
// retrieval scheduling.
func (s *Sync) children(req *_SyncRequest, object _Node) ([]*_SyncRequest, error) {
	var children []_Node
	if node, ok := object.(*_BranchNode); ok {
		for _, child := range node.Children {
			if child == nil {
				continue
			}
			children = append(children, child)
		}
	}

	// Iterate over the children, and request all unknown ones
	requests := make([]*_SyncRequest, 0, len(children))
	for _, child := range children {
		if req.callback != nil {
			/* TODO
			if node, ok := child.(_ValueNode); ok {
				if err := req.callback(node, req.hash); err != nil {
					return nil, err
				}
			}
			*/
		}

		if node, ok := child.(_HashNode); ok {
			hash := ec.Hash256(node)
			if _, ok := s.membatch.batch[hash]; ok {
				continue
			}
			if ok, _ := s.kvdb.Has(hash[:]); ok {
				continue
			}
			requests = append(requests, &_SyncRequest{
				hash: hash,
				parents: []*_SyncRequest{req},
				depth: req.depth + 1,
				callback: req.callback,
			})
		}
	}
	return requests, nil
}

// commit finalizes a retrieval request and stores it into the membatch. If any
// of the referencing parent requests complete due to this commit, they are also
// committed themselves.
func (s *Sync) commit(req *_SyncRequest) (err error) {
	// Write the node content to the membatch
	s.membatch.batch[req.hash] = req.data
	s.membatch.order = append(s.membatch.order, req.hash)

	delete(s.requests, req.hash)

	// Check all parents for completion
	for _, parent := range req.parents {
		parent.deps--
		if parent.deps == 0 {
			if err := s.commit(parent); err != nil {
				return err
			}
		}
	}
	return nil
}

