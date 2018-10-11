package trie

import (
	"bvchain/ec"
	"bvchain/obj"
	"bvchain/util"

	"sync"
	"io"
	"hash"
	"bytes"
	"strings"
	"fmt"
	"math"
	"halftwo/mangos/xstr"
)

const MAX_BLOB_SIZE = math.MaxUint16

type _Node interface {
	DoEncode(h *NodeHasher) (_Node, error)
	DoDecode(blob []byte, cacheGen uint16, hash []byte) error

	GetValue() []byte

	cinfo() *_CacheInfo

	assertCacheInfo()
	cachedHash() []byte
	blobSize() int
	canUnload(cacheGen uint16, cacheLimit int16) bool
	dump(w io.Writer, level int)
}


// _HashNode is not a real node, it's a place holder for child node in _BranchNode.
// Its value is the hash of the encoded blob of child node.
type _HashNode ec.Hash256

type _BlobNode []byte

type _LeafNode struct {
	Path []byte
	Value []byte
	ci _CacheInfo
}

type _BranchNode struct {
	Path []byte
	Value []byte
	Children [16]_Node	// children can be _LeafNode or _BranchNode
	ci _CacheInfo
}

type _CacheInfo struct {
	hash []byte
	blobSize uint16
	gen uint16
	dirty bool
}

const (
	NODE_Leaf	= byte(0x20)	// low 5 bits reserved for path
	NODE_Branch	= byte(0x40)	// low 5 bits reserved for path
)

var sha3Pool = sync.Pool {
	New: func() interface{} {
		return ec.New256Hasher()
	},
}

func sha3sum(blob []byte) []byte {
	hasher := sha3Pool.Get().(hash.Hash)
	defer sha3Pool.Put(hasher)

	hasher.Reset()
	hasher.Write(blob)
	return hasher.Sum(nil)
}


func (n _HashNode) cinfo() *_CacheInfo { return nil }
func (n _BlobNode) cinfo() *_CacheInfo { return nil }
func (n *_LeafNode) cinfo() *_CacheInfo { return &n.ci }
func (n *_BranchNode) cinfo() *_CacheInfo { return &n.ci }


func (n _HashNode) GetValue() []byte { return nil }
func (n _BlobNode) GetValue() []byte { return nil }
func (n *_LeafNode) GetValue() []byte { return n.Value }
func (n *_BranchNode) GetValue() []byte { return n.Value }


func (n _HashNode) assertCacheInfo() { /* do nothing */ }
func (n _BlobNode) assertCacheInfo() { /* do nothing */ }
func (n *_LeafNode) assertCacheInfo() { util.Assert(n.ci.blobSize > 0) }
func (n *_BranchNode) assertCacheInfo() { util.Assert(n.ci.blobSize > 0) }

func (n _HashNode) cachedHash() []byte { return n[:] }
func (n _BlobNode) cachedHash() []byte { return nil }
func (n *_LeafNode) cachedHash() []byte { return n.ci.hash }
func (n *_BranchNode) cachedHash() []byte { return n.ci.hash }

func (n _HashNode) blobSize() int { return MAX_BLOB_SIZE }	// XXX
func (n _BlobNode) blobSize() int { return len(n) }
func (n *_LeafNode) blobSize() int { return int(n.ci.blobSize) }
func (n *_BranchNode) blobSize() int { return int(n.ci.blobSize) }


type NodeHasher struct {
	blob []byte
	cdb *CacheDb
	cacheGen uint16
	cacheLimit int16
}


func newNodeHasher(cdb *CacheDb, cacheGen uint16, cacheLimit int16) *NodeHasher {
	h := &NodeHasher{
		cdb: cdb,
		cacheGen: cacheGen,
		cacheLimit: cacheLimit,
	}
	return h
}

func (h *NodeHasher) DoHash(n _Node, force bool) ([]byte, _Node, int, error) {
	switch n := n.(type) {
	case _HashNode:
		return n[:], n, MAX_BLOB_SIZE, nil
	case _BlobNode:
		return sha3sum(n[:]), n, len(n[:]), nil
	}

	ci := n.cinfo()
	if len(ci.hash) == 32 {
		if h.cdb == nil {
			return ci.hash, n, int(ci.blobSize), nil
		}

		if n.canUnload(h.cacheGen, h.cacheLimit) {
			return ci.hash, _HashNode(ec.BytesToHash256(ci.hash)), int(ci.blobSize), nil
		}

		if !ci.dirty {
			return ci.hash, n, int(ci.blobSize), nil
		}
	}

	folded, err := n.DoEncode(h)
	if err != nil {
		return nil, n, MAX_BLOB_SIZE, err
	}

	blob := h.blob
	if len(blob) > MAX_BLOB_SIZE {
		panic("trie: Node blob too large")
	}

	hh := sha3sum(blob)
	if len(blob) >= 32 {
		ci.hash = hh
	}
	ci.blobSize = uint16(len(blob))

	if h.cdb != nil && ci.dirty {
		ci.dirty = false
		if (force || len(blob) >= 32) {
			h256 := ec.BytesToHash256(hh)
			h.cdb.insert(h256, n, blob)
		}
	}
	return hh, folded, len(blob), nil
}


func (n _HashNode)    canUnload(cacheGen uint16, limit int16) bool { return false }
func (n _BlobNode)    canUnload(cacheGen uint16, limit int16) bool { return false }
func (n *_LeafNode)   canUnload(cacheGen uint16, limit int16) bool { return n.ci.canUnload(cacheGen, limit) }
func (n *_BranchNode) canUnload(cacheGen uint16, limit int16) bool { return n.ci.canUnload(cacheGen, limit) }

func (ci *_CacheInfo) canUnload(cacheGen uint16, limit int16) bool {
	return !ci.dirty && (cacheGen - ci.gen >= uint16(limit))
}

func (n _HashNode) dump(w io.Writer, level int) {
	fmt.Fprintf(w, "<%x..%x>\n", n[:8], n[24:])
}

func (n _BlobNode) dump(w io.Writer, level int) {
	fmt.Fprintf(w, "{%x}\n", n[:])
}

func path2str(path []byte) string {
	buf := make([]byte, len(path))
	for i, c := range(path) {
		if c >= 10 {
			buf[i] = 'a' + byte(c - 10)
		} else {
			buf[i] = '0' + byte(c)
		}
	}
	return string(buf)
}

func printNodeValue(w io.Writer, kind byte, path, value []byte) {
	fmt.Fprintf(w, "%c%s", kind, path2str(path))
	if len(value) > 16 {
		k := len(value)
		fmt.Fprintf(w, "~~~%x..%x", value[:8], value[k-8:])
	} else if len(value) > 0 {
		fmt.Fprintf(w, "~~~%x", value)
	}
	fmt.Fprintf(w, "\n")
}

func (n *_LeafNode) dump(w io.Writer, level int) {
	printNodeValue(w, '@', n.Path, n.Value)
}

func (n *_BranchNode) dump(w io.Writer, level int) {
	printNodeValue(w, '%', n.Path, n.Value)

	indent := strings.Repeat("  ", level + 1)
	for i, c := range n.Children {
		if c != nil {
			fmt.Fprintf(w, "%s%x", indent, i)
			c.dump(w, level + 1)
		}
	}
}


func (n _HashNode) DoEncode(h *NodeHasher) (_Node, error) {
	util.CantReachHere()
	return n, nil
}

func (n _HashNode) DoDecode(blob []byte, cacheGen uint16, hash []byte) error {
	util.CantReachHere()
	return nil
}

func (n _BlobNode) DoEncode(h *NodeHasher) (_Node, error) {
	h.blob = h.blob[:0]
	h.blob = append(h.blob, n[:]...)
	return n, nil
}

func (n _BlobNode) DoDecode(blob []byte, cacheGen uint16, hash []byte) error {
	util.CantReachHere()
	return nil
}

func path2compact(path []byte, buf []byte) []byte {
	n := len(path)
	if n > 255 {
		panic("trie: path is toooooo long")
	}

	size := 1 + (n + 1) / 2
	if cap(buf) < size {
		buf = make([]byte, size)
	}
	buf = buf[:0]

	buf = append(buf, byte(n))
	for i := 0; i < n - 1; i += 2 {
		b := (path[i] << 4) | path[i+1]
		buf = append(buf, b)
	}

	if n % 2 == 1 {
		b := path[n - 1] << 4
		buf = append(buf, b)
	}

	return buf
}

func compact2path(compact []byte) ([]byte, int) {
	if len(compact) < 1 {
		return nil, 0
	}
	n := int(compact[0])

	clen := 1 + (int(n) + 1) / 2
	if clen > len(compact) {
		return nil, -len(compact)
	}

	var path []byte
	if n > 0 {
		path = make([]byte, n)
		compact = compact[1:]
		for i := 0; i < int(n - 1); i += 2 {
			b := compact[i/2]
			path[i] = b >> 4
			path[i+1] = b & 0x0f
		}

		if n % 2 == 1 {
			b := compact[n/2]
			if b & 0x0f != 0 {
				return nil, -clen
			}
			path[n-1] = b >> 4
		}
	}

	return path, clen
}


func (n *_LeafNode) DoEncode(h *NodeHasher) (_Node, error) {
	w := xstr.NewBytesWriter(&h.blob)
	s := &obj.Serializer{W:w}

	s.WriteByte(NODE_Leaf)

	var buf [33]byte
	compact := path2compact(n.Path, buf[:])
	s.Write(compact)
	s.Write(n.Value)
	return n, s.Err
}

func (n *_LeafNode) DoDecode(blob []byte, cacheGen uint16, hash []byte) error {
	util.Assert(len(hash) == 0 || len(hash) == 32)
	if len(blob) < 3 || blob[0] != NODE_Leaf {
		return fmt.Errorf("trie: invalid encoded blob for _LeafNode")
	}

	var k int
	n.Path, k = compact2path(blob[1:])
	if k <= 0 || 1 + k >= len(blob) {
		return fmt.Errorf("trie: invalid encoded blob for _LeafNode")
	}

	value := blob[1+k:]
	n.Value = make([]byte, len(value))
	copy(n.Value, value)
	n.ci.gen = cacheGen
	n.ci.hash = hash
	n.ci.blobSize = uint16(len(blob))
	if n.ci.blobSize < 32 {
		n.ci.hash = nil
	}
	return nil
}

func (n *_BranchNode) DoEncode(h *NodeHasher) (_Node, error) {
	folded := *n
AGAIN:
	for i, child := range n.Children {
		if child != nil {
			switch child.(type) {
			case _HashNode, _BlobNode:
				continue AGAIN
			}

			hh, _, bsize, err := h.DoHash(child, false)
			if err != nil {
				return n, err
			}

			if bsize < 32 {
				folded.Children[i] = _BlobNode(util.CloneBytes(h.blob))
			} else if len(hh) == 32 {
				folded.Children[i] = _HashNode(ec.BytesToHash256(hh))
			} else {
				util.CantReachHere()
			}
		}
	}

	w := xstr.NewBytesWriter(&h.blob)
	s := &obj.Serializer{W:w}

	s.WriteByte(NODE_Branch)

	var buf [32]byte
	compact := path2compact(n.Path, buf[:])
	s.Write(compact)

	for i, child := range folded.Children {
		if child == nil {
			continue
		}

		switch child := child.(type) {
		case _HashNode:
			s.WriteByte(byte(i*2))
			s.Write(child[:])

		case _BlobNode:
			s.WriteByte(byte(i*2+1))
			s.WriteByte(byte(len(child)))
			s.Write(child[:])
		}
	}

	if len(n.Value) > 0 {
		s.WriteByte(byte(32))
		s.Write(n.Value)
	}

	return &folded, s.Err
}

func (n *_BranchNode) DoDecode(blob []byte, cacheGen uint16, hash []byte) error {
	util.Assert(len(hash) == 0 || len(hash) == 32)
	if len(blob) < 8 || blob[0] != NODE_Branch {
		return fmt.Errorf("trie: invalid encoded blob for _BranchNode")
	}

	var k int
	n.Path, k = compact2path(blob[1:])
	if k <= 0 || 1 + k >= len(blob) {
		return fmt.Errorf("trie: invalid encoded blob for _BranchNode")
	}

	br := bytes.NewBuffer(blob[1+k:])
	d := &obj.Deserializer{R:br}

	hasValue := false
	last_i := -1
	for d.Err == nil && br.Len() > 0 {
		var err error
		var buf [32]byte
		var child _Node

		i := int(d.ReadByte())
		if i >= 32 { // the value
			hasValue = true
			break
		}

		if i % 2 == 0 {
			d.Read(buf[:])
			child = _HashNode(buf)
		} else { // embeded node
			k := d.ReadByte()
			if k < 2 || k >= 32 {
				return fmt.Errorf("trie: invalid size (%d) of embeded leaf node for _BranchNode", k) 
			}

			d.Read(buf[:k])
			child, err = DecodeNode(buf[:k], cacheGen, nil)
			if err != nil {
				return err
			}
		}

		if i <= last_i {
			return fmt.Errorf("trie: children index must be greater than the last one")
		}
		last_i = i

		n.Children[i/2] = child
	}

	if hasValue {
		value := br.Bytes()
		if len(value) == 0 {
			return fmt.Errorf("trie: invalid undecoded blob for _BranchNode")
		}
		n.Value = make([]byte, len(value))
		copy(n.Value, value)
	} else if br.Len() > 0 {
		return fmt.Errorf("trie: %d bytes undecoded blob left for _BranchNode", br.Len())
	}

	n.ci.gen = cacheGen
	n.ci.hash = hash
	n.ci.blobSize = uint16(len(blob))
	if n.ci.blobSize < 32 {
		n.ci.hash = nil
	}
	return d.Err
}

func (n *_BranchNode) AddChildValue(path []byte, value []byte, ci _CacheInfo) {
	if len(path) == 0{
		n.Value = value
	} else {
		idx := path[0]
		n.Children[idx] = &_LeafNode{path[1:], value, ci, }
	}
}

func (n *_BranchNode) AddChildBranch(path []byte, child *_BranchNode, ci _CacheInfo) {
	if len(path) == 0 {
		panic("trie: path to _BranchNode must contain at least one byte")
	}

	idx := path[0]
	n.Children[idx] = child

	child.Path = path[1:]
	child.ci = ci
}

func EncodeNode(n _Node) []byte {
	h := newNodeHasher(nil, 0, 0)
	n.DoEncode(h)
	return h.blob
}

func DecodeNode(blob []byte, cacheGen uint16, hash []byte) (_Node, error) {
	if len(blob) == 0 {
		return nil, io.ErrUnexpectedEOF
	}

	var n _Node
	switch blob[0] {
	case NODE_Leaf:
		n = &_LeafNode{}

	case NODE_Branch:
		n = &_BranchNode{}

	default:
		return nil, fmt.Errorf("trie: Decode unknown node type %d", blob[0])
	}

	err := n.DoDecode(blob, cacheGen, hash)
	if err != nil {
		return nil, err
	}

	return n, nil
}

func MustDecodeNode(blob []byte, cacheGen uint16, hash []byte) (_Node) {
	n, err := DecodeNode(blob, cacheGen, hash)
	util.AssertNoError(err)
	return n
}

