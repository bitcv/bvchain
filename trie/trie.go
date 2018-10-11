package trie

import (
	"bvchain/ec"
	"bvchain/db"
	"bvchain/util"

	"io"
	"fmt"
	"math"
	"bytes"
)

var emptyRootHash = ec.Sum256([]byte{})

const MaxKeyLen = 127


type Trie struct {
	cdb *CacheDb
	rootNode _Node
	cacheGen uint16
	cacheLimit int16
}

func New(rootHash ec.Hash256, kvdb db.KvDb) (*Trie, error) {
        if kvdb == nil {
                panic("trie.New called without a database")
        }

	cdb := NewCacheDb(kvdb)
	return NewWithCacheDb(rootHash, cdb)
}

func NewWithCacheDb(rootHash ec.Hash256, cdb *CacheDb) (*Trie, error) {
        if cdb == nil {
                panic("trie.New called without a database")
        }

        tr := &Trie{
                cdb: cdb,
		cacheGen: 1,
        }

        if rootHash != (ec.Hash256{}) && rootHash != emptyRootHash {
                node := tr.resolveHash(rootHash[:])
                if node == nil {
                        return nil, fmt.Errorf("trie: can't find rootHash=%s", rootHash.String())
                }
                tr.rootNode = node
        }
        return tr, nil
}

func (tr *Trie) CacheDb() *CacheDb {
	return tr.cdb
}

func (tr *Trie) SetCacheLimit(limit int) {
	if limit < math.MaxInt16 && limit > 0 {
		tr.cacheLimit = int16(limit)
	} else {
		tr.cacheLimit = math.MaxInt16
	}
}

func (tr *Trie) NodeIterator(start []byte) NodeIterator {
	return newNodeIterator(tr, start)
}

func (tr *Trie) dirtyCI() _CacheInfo {
	return _CacheInfo{gen:tr.cacheGen, dirty:true}
}

func (tr *Trie) newLeafNode(path []byte, value []byte) *_LeafNode {
	return &_LeafNode{Path:path, Value:value, ci:tr.dirtyCI(), }
}

func (tr *Trie) newBranchNode(path []byte) *_BranchNode {
	return &_BranchNode{Path:path, ci:tr.dirtyCI(), }
}


// RootHash returns the root hash of the Trie.
func (tr *Trie) RootHash() (h ec.Hash256) {
	if tr.rootNode == nil {
		copy(h[:], emptyRootHash[:])
		return h
	}

	hash := tr.rootNode.cachedHash()
	if len(hash) != 32 {
		h := newNodeHasher(tr.cdb, 0, 0)
		hash, _, _, _ = h.DoHash(tr.rootNode, true)
	}
	return ec.BytesToHash256(hash)
}

func (tr *Trie) RootHashBytes() []byte {
	hash := tr.RootHash()
	return hash[:]
}

func key2path(key []byte) []byte {
        size := len(key) * 2
        path := make([]byte, size)
        for i, b := range key {
                path[i*2] = b / 16
                path[i*2+1] = b % 16
        }
        return path
}

func path2key(path []byte) []byte {
	n := len(path)
	if n % 2 != 0 {
		panic("path is not multiple of 2")
	}
	k := n / 2

	key := make([]byte, k)
	for i := 0; i < k; i++ {
		hi := path[i*2]
		low := path[i*2+1]
		if hi >= 16 || low >= 16 {
			panic("invalid path, element >= 16")
		}
		key[i] = (hi << 4) + low
	}
	return key
}

// Don't modify the content of value.
func (tr *Trie) Get(key []byte) []byte {
	path := key2path(key)
	node, value, resolved := tr.get(tr.rootNode, path)
	if resolved {
		tr.rootNode = node
	}
	return value
}

// Update will modify or insert a node in the tr.
// Don't modify the content of value after this function.
func (tr *Trie) Update(key []byte, value []byte) {
	if len(key) > MaxKeyLen {
		panic("trie: the key is too long")
	}

	path := key2path(key)
	var node _Node
	if len(value) == 0 {
		node, _ = tr.remove(tr.rootNode, path)
	} else {
		node, _ = tr.insert(tr.rootNode, path, value)
	}
	tr.rootNode = node
}

func (tr *Trie) Remove(key []byte) {
	path := key2path(key)
	node, _ := tr.remove(tr.rootNode, path)
	tr.rootNode = node
}

func (tr *Trie) get(origNode _Node, path []byte) (newNode _Node, value []byte, resolved bool) {
	switch n := origNode.(type) {
	case nil:
		return nil, nil, false

	case _HashNode, _BlobNode:
		realNode := tr.resolve(n)
		if realNode == nil {
			util.CantReachHere()
			return n, nil, true
		}
		newNode, value, _ := tr.get(realNode, path)
		return newNode, value, true

	case *_LeafNode:
		if bytes.Equal(path, n.Path) {
			return n, n.Value, false
		}
		return n, nil, false

	case *_BranchNode:
		if len(n.Path) > 0 {
			if !bytes.HasPrefix(path, n.Path) {
				return n, nil, false
			}
			path = path[len(n.Path):]
		}

		if len(path) == 0 {
			return n, n.Value, false
		}

		idx := path[0]
		newNode, value, resolved := tr.get(n.Children[idx], path[1:])
		if resolved {
			n.Children[idx] = newNode
			n.ci.gen = tr.cacheGen
		}
		return n, value, resolved

	default:
		panic(fmt.Sprintf("invalid node %T", origNode))
	}
	// Can't reach here
	return nil, nil, false
}

func concat(a ...[]byte) []byte {
	n := 0
	for _, x := range a {
		n += len(x)
	}

	r := make([]byte, n)
	r = r[:0]
	for _, x := range a {
		r = append(r, x...)
	}
        return r
}

func (tr *Trie) remove(origNode _Node, path []byte) (newNode _Node, dirty bool) {
	switch n := origNode.(type) {
	case nil:
		return nil, false

	case _HashNode, _BlobNode:
		realNode := tr.resolve(n)
		if realNode == nil {
			util.CantReachHere()
			return nil, true
		}
		return tr.remove(realNode, path)

	case *_LeafNode:
		if bytes.Equal(path, n.Path) {
			return nil, true
		}
		return n, false

	case *_BranchNode:
		if len(n.Path) > 0 {
			if !bytes.HasPrefix(path, n.Path) {
				return n, false
			}
			path = path[len(n.Path):]
		}

		if len(path) == 0 {
			n.Value = nil
		} else {
			idx := path[0]
			child, dirty := tr.remove(n.Children[idx], path[1:])
			if !dirty {
				return n, false
			}
			n.Children[idx] = child
		}
		n.ci = tr.dirtyCI()

		pos := -1
		count := 0
		for i, c := range n.Children {
			if c != nil {
				count++
				if pos == -1 {
					pos = i
				}
			}
		}

		if count > 1 || (count == 1 && len(n.Value) > 0) {
			return n, true
		}

		if len(n.Value) > 0 {
			return tr.newLeafNode(nil, n.Value), true
		}

		// pos >= 0
		child := tr.resolve(n.Children[pos])
		if child == nil {
			// not found in the db
			return nil, true
		}

		switch child := child.(type) {
		case *_LeafNode:
			newpath := concat(n.Path, []byte{byte(pos)}, child.Path)
			return tr.newLeafNode(newpath, child.Value), true

		case *_BranchNode:
			newpath := concat(n.Path, []byte{byte(pos)}, child.Path)
			child.Path = newpath
			child.ci = tr.dirtyCI()
			return child, true
		}

	default:
		panic(fmt.Sprintf("invalid node %T", origNode))
	}
	// Can't reach here
	return nil, true
}

func prefixLen(a []byte, b []byte) int {
	n := len(a)
	if n > len(b) {
		n = len(b)
	}

	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

func (tr *Trie) insert(origNode _Node, path []byte, value []byte) (newNode _Node, dirty bool) {
	switch n := origNode.(type) {
	case nil:
		return tr.newLeafNode(path, value), true

	case _HashNode, _BlobNode:
		realNode := tr.resolve(n)
		if realNode == nil {
			util.CantReachHere()
			return nil, false
		}
		return tr.insert(realNode, path, value)

	case *_LeafNode:
		if bytes.Equal(path, n.Path) {
			if bytes.Equal(n.Value, value) {
				return n, false
			}
			n.Value = value
			n.ci = tr.dirtyCI()
			return n, true
		}

		plen := prefixLen(path, n.Path)
		branch := tr.newBranchNode(path[:plen])
		branch.AddChildValue(path[plen:], value, tr.dirtyCI())
		branch.AddChildValue(n.Path[plen:], n.Value, tr.dirtyCI())
		return branch, true

	case *_BranchNode:
		plen := prefixLen(path, n.Path)
		if plen < len(n.Path) {
			branch := tr.newBranchNode(path[:plen])
			branch.AddChildValue(path[plen:], value, tr.dirtyCI())
			branch.AddChildBranch(n.Path[plen:], n, tr.dirtyCI())
			return branch, true
		}
		path = path[plen:]

		if len(path) == 0 {
			if bytes.Equal(n.Value, value) {
				return n, false
			}
			n.Value = value
		} else {
			idx := path[0]
			child, dirty := tr.insert(n.Children[idx], path[1:], value)
			if !dirty {
				return n, false
			}
			n.Children[idx] = child
		}
		n.ci = tr.dirtyCI()
		return n, true

	default:
		panic(fmt.Sprintf("invalid node %T", origNode))
	}
	// Can't reach here
	return nil, true
}

func (tr *Trie) dump(w io.Writer) {
	n := tr.rootNode
	if n == nil {
		fmt.Fprintf(w, "<nil>\n")
	} else {
		n.dump(w, 0)
	}
}


func (tr *Trie) resolve(n _Node) _Node {
	switch n := n.(type) {
	case _HashNode:
		return tr.resolveHash(n[:])

	case _BlobNode:
		decoded, err := DecodeNode(n[:], tr.cacheGen, nil)
		if err != nil {
			util.CantReachHere()
		}
		return decoded
	}
	return n
}

func (tr *Trie) resolveHash(hash []byte) _Node {
	hh := ec.BytesToHash256(hash)
	n := tr.cdb.GetNode(hh, tr.cacheGen)
	return n
}


type LeafCallback func(leaf []byte, parent ec.Hash256) error


// NB: onleaf is not used
func (tr *Trie) Commit(onleaf LeafCallback) (root ec.Hash256, err error) {
        if tr.cdb == nil {
                panic("commit called on tr with nil database")
        }

	if tr.rootNode == nil {
		return
	}

	h := newNodeHasher(tr.cdb, tr.cacheGen, tr.cacheLimit)
	hash, folded, _, err := h.DoHash(tr.rootNode, true)
	tr.rootNode = folded
	tr.cacheGen++
	return ec.BytesToHash256(hash), err
}

