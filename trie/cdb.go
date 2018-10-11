package trie

import (
	"bvchain/db"
	"bvchain/ec"
	"bvchain/util"
	"bvchain/util/log"
	"bvchain/obj"

	"sync"
	"halftwo/mangos/xstr"
)

const _BATCH_DATA_SIZE = 100 * 1024

// CacheDb for is a writing cache to the underlying db.KvDb
type CacheDb struct {
	kvdb db.KvDb
	items map[ec.Hash256]*_CacheItem
	itemSize int
	lock sync.RWMutex
}

type _CacheItem struct {
	node _RawNode
	blobSize uint16
//	ancestor uint32
//	children map[ec.Hash256]uint32
}

func sizeOfItem(it *_CacheItem) int {
	return int(it.blobSize) + 256	// it's a rough guess
}

type _RawNode interface {
	GetBlob() []byte
}

type _RawHashNode ec.Hash256

type _RawBlobNode []byte

type _RawLeafNode struct {
	Path []byte
	Value []byte
}

type _RawBranchNode struct {
	Path []byte
	Value []byte
	Children [16]_RawNode
}

func (n _RawHashNode) GetBlob() []byte { util.CantReachHere(); return nil }

func (n _RawBlobNode) GetBlob() []byte { return n[:] }

func (n *_RawLeafNode) GetBlob() []byte {
	// TODO: almost repeated code, ugly
	var blob []byte
	w := xstr.NewBytesWriter(&blob)
	s := &obj.Serializer{W:w}

	compact := path2compact(n.Path, nil)
	s.WriteByte(NODE_Leaf)
	s.Write(compact)
	s.Write(n.Value)
	return blob
}

func (n *_RawBranchNode) GetBlob() []byte {
	// TODO: almost repeated code, ugly
	var blob []byte
	w := xstr.NewBytesWriter(&blob)
	s := &obj.Serializer{W:w}

	var buf [33]byte
	compact := path2compact(n.Path, buf[:])

	s.WriteByte(NODE_Branch)
	s.Write(compact)

	for i, child := range n.Children {
		if child ==  nil {
			continue
		}

		switch child := child.(type) {
		case _RawHashNode:
			s.WriteByte(byte(i*2))
			s.Write(child[:])

		case _RawBlobNode:
			s.WriteByte(byte(i*2+1))
			s.WriteByte(byte(len(child)))
			s.Write(child[:])

		default:
			util.CantReachHere()
		}
	}

	if len(n.Value) > 0 {
		s.WriteByte(byte(32))
		s.Write(n.Value)
	}
	return blob
}


func NewCacheDb(kvdb db.KvDb) *CacheDb {
	return &CacheDb{
		kvdb: kvdb,
		items: make(map[ec.Hash256]*_CacheItem),
	}
}

func (cdb *CacheDb) NodeKeys() []ec.Hash256 {
	cdb.lock.RLock()
	defer cdb.lock.RUnlock()

	keys := make([]ec.Hash256, 0, len(cdb.items))
	for key := range cdb.items {
		if key == (ec.Hash256{}) {
			continue
		}

		keys = append(keys, key)
	}
	return keys
}

func (cdb *CacheDb) GetNodeBlob(hash ec.Hash256) []byte {
	cdb.lock.Lock()
	it := cdb.items[hash]
	cdb.lock.Unlock()

	if it != nil {
		return it.node.GetBlob()
	}

	blob, err := cdb.kvdb.Get(hash[:])
	if err != nil || blob == nil {
		return nil
	}
	return blob
}

func (cdb *CacheDb) GetNode(hash ec.Hash256, cacheGen uint16) (n _Node) {
	cdb.lock.Lock()
	it := cdb.items[hash]
	defer cdb.lock.Unlock()

	if it != nil {
		return expandFromRawNode(it.node, cacheGen)
	}

	blob, err := cdb.kvdb.Get(hash[:])
	if err != nil || blob == nil {
		return nil
	}

	n, err = DecodeNode(blob, cacheGen, hash[:])
	if err != nil {
		panic(err)
		return nil
	}
	return n
}

func makeRawNode(node _Node) _RawNode {
	switch node := node.(type) {
	case *_LeafNode:
		return &_RawLeafNode {
			Path: node.Path,
			Value: node.Value,
		}

	case *_BranchNode:
		rn := &_RawBranchNode {
			Path: node.Path,
			Value: node.Value,
		}

		for i, child := range node.Children {
			if child != nil {
				child.assertCacheInfo()
				if child.blobSize() < 32 {
					blob := EncodeNode(child)
					rn.Children[i] = _RawBlobNode(blob)
				} else {
					hash := child.cachedHash()
					rn.Children[i] = _RawHashNode(ec.BytesToHash256(hash))
				}
			}
		}
		return rn
	}
	return nil
}

func expandFromRawNode(rn _RawNode, cacheGen uint16) _Node {
	switch rn := rn.(type) {
	case *_RawLeafNode:
		return &_LeafNode {
			Path: rn.Path,
			Value: rn.Value,
			ci: _CacheInfo{gen:cacheGen},
		}

	case *_RawBranchNode:
		node := &_BranchNode {
			Path: rn.Path,
			Value: rn.Value,
			ci: _CacheInfo{gen:cacheGen},
		}
		for i, child := range rn.Children {
			if child != nil {
				switch child := child.(type) {
				case _RawHashNode:
					node.Children[i] = _HashNode(child)

				case _RawBlobNode:
					node.Children[i] = MustDecodeNode(child[:], cacheGen, nil)
				}
			}
		}
		return node
	}
	return nil
}


func (cdb *CacheDb) insert(hash ec.Hash256, node _Node, blob []byte) {
	if _, ok := cdb.items[hash]; ok {
		return
	}

	rn := makeRawNode(node)
	if rn == nil {
		return
	}

	it := &_CacheItem {
		node: rn,
		blobSize: uint16(len(blob)),
	}

	cdb.items[hash] = it
	cdb.itemSize += sizeOfItem(it)
}


func (cdb *CacheDb) Commit(hash ec.Hash256, report bool) error {
	if err := cdb.commit(hash, report); err != nil {
		return err
	}

	cdb.lock.Lock()
	cdb._uncache(hash)
	cdb.lock.Unlock()
	return nil
}

func (cdb *CacheDb) commit(hash ec.Hash256, report bool) error {
	cdb.lock.RLock()
	defer cdb.lock.RUnlock()

	batch := cdb.kvdb.NewBatch()
	if err := cdb._doBatch(hash, batch); err != nil {
		log.Error("Failed to commit trie from CacheDb", "err", err)
		return err
	}

	if err := batch.Write(); err != nil {
		log.Error("Failed to write trie to disk", "err", err)
		return err
	}
	return nil
}

func (cdb *CacheDb) _doBatch(hash ec.Hash256, batch db.KvBatch) error {
	it := cdb.items[hash]
	if it == nil {
		return nil
	}

	if node, ok := it.node.(*_RawBranchNode); ok {
		for _, child := range node.Children {
			if child == nil {
				continue
			}

			hashNode, ok := child.(_RawHashNode)
			if ok {
				hh := ec.Hash256(hashNode)
				if err := cdb._doBatch(hh, batch); err != nil {
					return err
				}
			}
		}
	}

	if err := batch.Put(hash[:], it.node.GetBlob()); err != nil {
		return err
	}

	if batch.DataSize() >= _BATCH_DATA_SIZE {
		if err := batch.Write(); err != nil {
			return err
		}
		batch.Reset()
	}
	return nil
}

func (cdb *CacheDb) _uncache(hash ec.Hash256) {
	it := cdb.items[hash]
	if it == nil {
		return
	}

	if node, ok := it.node.(*_RawBranchNode); ok {
		for _, child := range node.Children {
			if child == nil {
				continue
			}

			hashNode, ok := child.(_RawHashNode)
			if ok {
				cdb._uncache(ec.Hash256(hashNode))
			}
		}
	}

	delete(cdb.items, hash)
	cdb.itemSize -= sizeOfItem(it)
}

func (cdb *CacheDb) FlushUntil(limit int) {
	// TODO
}

