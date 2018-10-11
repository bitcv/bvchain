package trie

import (
	"bvchain/db"
	"bvchain/ec"

	"testing"
	"testing/quick"
	"errors"
	"bytes"
	"math/rand"
	"reflect"
	"encoding/binary"
	"os"
	"fmt"
	"io/ioutil"
	"github.com/davecgh/go-spew/spew"
)

func newEmpty() *Trie {
        tr, _ := New(ec.Hash256{}, db.NewMemDb())
        return tr
}

func ExampleTrie_dump() {
	tr := newEmpty()
	tr.Update([]byte("abc"), []byte("hello"))
	tr.Update([]byte("abcdef"), []byte("foobar"))
	tr.Update([]byte("abxyz"), []byte("world1"))
	tr.Update([]byte("abxg12"), []byte("world2"))
	tr.Update([]byte("abxvi"), []byte("world3"))
	tr.Update([]byte("abxvo"), []byte("world4"))
	tr.Update([]byte("abxva"), []byte("abcdefghijklmnopqrstuvwxyz"))
	tr.Update([]byte("abxvab"), []byte("pqrstuvwxyz"))
	tr.Update([]byte("abxvabc"), []byte("pqrstuvwxyz------------------------------------------abc"))
	tr.dump(os.Stdout)

// Output:
/*
%6162
  6%3~~~68656c6c6f
    6@46566~~~666f6f626172
  7%8
    6@73132~~~776f726c6432
    7%
      6%6
        1%~~~6162636465666768..737475767778797a
          6%2~~~707172737475767778797a
            6@3~~~7071727374757677..2d2d2d2d2d616263
        9@~~~776f726c6433
        f@~~~776f726c6434
      9@7a~~~776f726c6431
*/
}

func TestEmptyTrie(t *testing.T) {
	var tr Trie
	root := tr.RootHash()
	if root != emptyRootHash {
		t.Errorf("expected %x got %x", emptyRootHash, root)
	}
}

func TestNull(t *testing.T) {
	var tr Trie
	key := make([]byte, 32)
	value := []byte("test")
	tr.Update(key, value)
	if !bytes.Equal(tr.Get(key), value) {
		t.Fatal("wrong value")
	}
}

func TestMissingRoot(t *testing.T) {
	tr, _ := New(ec.MustHexToHash256("0beec7b5ea3f0fdbc95d0dd47f3c5bc275da8a33"), db.NewMemDb())
	if tr != nil {
		t.Error("New returned non-nil tr for invalid root")
	}
}

//func TestMissingNodeDisk(t *testing.T)    { testMissingNode(t, false) }
//func TestMissingNodeMemonly(t *testing.T) { testMissingNode(t, true) }

func testMissingNode(t *testing.T, memonly bool) {
	kvdb := db.NewMemDb()
	cdb := NewCacheDb(kvdb)

	tr, _ := NewWithCacheDb(ec.Hash256{}, cdb)
	updateString(tr, "120000", "qwerqwerqwerqwerqwerqwerqwerqwer")
	updateString(tr, "123456", "asdfasdfasdfasdfasdfasdfasdfasdf")
	root, _ := tr.Commit(nil)
	if !memonly {
		cdb.Commit(root, true)
	}

	var value []byte
	tr, _ = NewWithCacheDb(root, cdb)
	value = tr.Get([]byte("120000"))
	if value == nil {
		t.Errorf("expected value not got")
	}

	tr, _ = NewWithCacheDb(root, cdb)
	value = tr.Get([]byte("120099"))
	if value != nil {
		t.Errorf("get unexpected value")
	}

	tr, _ = NewWithCacheDb(root, cdb)
	value = tr.Get([]byte("123456"))
	if value == nil {
		t.Errorf("expected value not got")
	}

	tr, _ = NewWithCacheDb(root, cdb)
	tr.Update([]byte("120099"), []byte("zxcvzxcvzxcvzxcvzxcvzxcvzxcvzxcv"))

	tr, _ = NewWithCacheDb(root, cdb)
	tr.Remove([]byte("123456"))

	hash := ec.MustHexToHash256("e1d943cc8f061a0c0b98162830b970395ac9315654824bf21b73b891365262f9")
	if memonly {
		delete(cdb.items, hash)
	} else {
		kvdb.Remove(hash[:])
	}

	tr, _ = NewWithCacheDb(root, cdb)
	value = tr.Get([]byte("120000"))

	tr, _ = NewWithCacheDb(root, cdb)
	value = tr.Get([]byte("120099"))

	tr, _ = NewWithCacheDb(root, cdb)
	value = tr.Get([]byte("123456"))

	tr, _ = NewWithCacheDb(root, cdb)
	tr.Update([]byte("120099"), []byte("zxcv"))

	tr, _ = NewWithCacheDb(root, cdb)
	tr.Remove([]byte("123456"))
}

func TestInsert(t *testing.T) {
	tr := newEmpty()

	updateString(tr, "doe", "reindeer")
	updateString(tr, "dog", "puppy")
	updateString(tr, "dogglesworth", "cat")

	exp := ec.MustHexToHash256("96f75a1a228e281e79484417352054cc98889090a08007aa43c8268819b7ec6c")
	root := tr.RootHash()
	if root != exp {
		t.Errorf("exp %x got %x", exp[:], root[:])
	}

	tr = newEmpty()
	updateString(tr, "A", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	exp = ec.MustHexToHash256("8331e0a55a0de9e3144815588e9f4684ef64c38441641313202b3a72dab47fcc")
	root, err := tr.Commit(nil)
	if err != nil {
		t.Fatalf("commit error: %v", err)
	}
	if root != exp {
		t.Errorf("exp %x got %x", exp[:], root[:])
	}
}

func TestGet(t *testing.T) {
	tr := newEmpty()
	updateString(tr, "doe", "reindeer")
	updateString(tr, "dog", "puppy")
	updateString(tr, "dogglesworth", "cat")

	for i := 0; i < 2; i++ {
		res := getString(tr, "dog")
		if !bytes.Equal(res, []byte("puppy")) {
			t.Errorf("expected puppy got %x", res)
		}

		unknown := getString(tr, "unknown")
		if unknown != nil {
			t.Errorf("expected nil got %x", unknown)
		}

		if i == 1 {
			return
		}
		tr.Commit(nil)
	}
}

func TestRemove(t *testing.T) {
	tr := newEmpty()
	vals := []struct{ k, v string }{
		{"do", "verb"},
		{"ether", "wookiedoo"},
		{"horse", "stallion"},
		{"shaman", "horse"},
		{"doge", "coin"},
		{"ether", ""},
		{"dog", "puppy"},
		{"shaman", ""},
	}
	for _, val := range vals {
		if val.v != "" {
			updateString(tr, val.k, val.v)
		} else {
			deleteString(tr, val.k)
		}
	}

	hash := tr.RootHash()
	exp := ec.MustHexToHash256("5dc031f5a4c1fae8d107e9dc7e4d24c7d96a90f837bfefaac582472b57216444")
	if hash != exp {
		t.Errorf("expected %x got %x", exp[:], hash[:])
	}
}

func TestEmptyValues(t *testing.T) {
	tr := newEmpty()

	vals := []struct{ k, v string }{
		{"do", "verb"},
		{"ether", "wookiedoo"},
		{"horse", "stallion"},
		{"shaman", "horse"},
		{"doge", "coin"},
		{"ether", ""},
		{"dog", "puppy"},
		{"shaman", ""},
	}
	for _, val := range vals {
		updateString(tr, val.k, val.v)
	}

	hash := tr.RootHash()
	exp := ec.MustHexToHash256("5dc031f5a4c1fae8d107e9dc7e4d24c7d96a90f837bfefaac582472b57216444")
	if hash != exp {
		t.Errorf("expected %x got %x", exp[:], hash[:])
	}
}

func TestReplication(t *testing.T) {
	tr := newEmpty()
	vals := []struct{ k, v string }{
		{"do", "verb"},
		{"ether", "wookiedoo"},
		{"horse", "stallion"},
		{"shaman", "horse"},
		{"doge", "coin"},
		{"dog", "puppy"},
		{"somethingveryoddindeedthis is", "myothernodedata"},
	}
	for _, val := range vals {
		updateString(tr, val.k, val.v)
	}
	exp, err := tr.Commit(nil)
	if err != nil {
		t.Fatalf("commit error: %v", err)
	}

	// create a new tr on top of the database and check that lookups work.
	tr2, err := NewWithCacheDb(exp, tr.cdb)
	if err != nil {
		t.Fatalf("can't recreate tr at %x: %v", exp[:], err)
	}
	for _, kv := range vals {
		r := string(getString(tr2, kv.k))
		if r != kv.v {
			t.Errorf("tr2 have %q => %q, expect %q", kv.k, r, kv.v)
		}
	}
	hash, err := tr2.Commit(nil)
	if err != nil {
		t.Fatalf("commit error: %v", err)
	}
	if hash != exp {
		t.Errorf("root failure. expected %x got %x", exp[:], hash[:])
	}

	// perform some insertions on the new tr.
	vals2 := []struct{ k, v string }{
		{"do", "verb"},
		{"ether", "wookiedoo"},
		{"horse", "stallion"},
		{"shaman", "horse"},
		{"doge", "coin"},
//		{"ether", ""},
		{"dog", "puppy"},
		{"somethingveryoddindeedthis is", "myothernodedata"},
//		{"shaman", ""},
	}
	for _, val := range vals2 {
		updateString(tr2, val.k, val.v)
	}
	if hash := tr2.RootHash(); hash != exp {
		t.Errorf("root failure. expected %x got %x", exp[:], hash[:])
	}
}

func TestLargeValue(t *testing.T) {
	tr := newEmpty()
	tr.Update([]byte("key1"), []byte{99, 99, 99, 99})
	tr.Update([]byte("key2"), bytes.Repeat([]byte{1}, 32))
	tr.RootHash()
}

type _CountingDb struct {
	db.KvDb
	gets map[string]int
}

func (db *_CountingDb) Get(key []byte) ([]byte, error) {
	db.gets[string(key)]++
	return db.KvDb.Get(key)
}

// TestCacheUnload checks that decoded nodes are unloaded after a
// certain number of commit operations.
func TestCacheUnload(t *testing.T) {
	// Create test tr with two branches.
	tr := newEmpty()
	key1 := "---------------------------------"
	key2 := "---some other branch"
	updateString(tr, key1, "this is the branch of key1.")
	updateString(tr, key2, "this is the branch of key2.")

	root, _ := tr.Commit(nil)
	tr.cdb.Commit(root, true)

	// Commit the tr repeatedly and access key1.
	// The branch containing it is loaded from DB exactly two times:
	// in the 0th and 6th iteration.
	db := &_CountingDb{KvDb: tr.cdb.kvdb, gets: make(map[string]int)}
	tr, _ = New(root, db)
	tr.SetCacheLimit(5)
	for i := 0; i < 12; i++ {
		getString(tr, key1)
		tr.Commit(nil)
	}
	// Check that it got loaded two times.
	for dbkey, count := range db.gets {
		if count != 2 {
			t.Errorf("db key %x loaded %d times, want %d times", []byte(dbkey), count, 2)
		}
	}
}

// randTest performs random tr operations.
// Instances of this test are created by Generate.
type randTest []randTestStep

type randTestStep struct {
	op    int
	key   []byte // for _OP_UPDATE, _OP_REMOVE, _OP_GET
	value []byte // for _OP_UPDATE
	err   error  // for debugging
}

const (
	_OP_UPDATE = iota
	_OP_REMOVE
	_OP_GET
	_OP_COMMIT
	_OP_HASH
	_OP_RESET
	_OP_ITER_CHECK_HASH
	_OP_CHECK_CACHE
	_OP_MAX // boundary value, not an actual op
)

func (randTest) Generate(r *rand.Rand, size int) reflect.Value {
	var allKeys [][]byte
	genKey := func() []byte {
		if len(allKeys) < 2 || r.Intn(100) < 10 {
			// new key
			key := make([]byte, r.Intn(50))
			r.Read(key)
			allKeys = append(allKeys, key)
			return key
		}
		// use existing key
		return allKeys[r.Intn(len(allKeys))]
	}

	var steps randTest
	for i := 0; i < size; i++ {
		step := randTestStep{op: r.Intn(_OP_MAX)}
		switch step.op {
		case _OP_UPDATE:
			step.key = genKey()
			step.value = make([]byte, 8)
			binary.BigEndian.PutUint64(step.value, uint64(i))

		case _OP_GET, _OP_REMOVE:
			step.key = genKey()
		}
		steps = append(steps, step)
	}
	return reflect.ValueOf(steps)
}

func runRandTest(rt randTest) bool {
	cdb := NewCacheDb(db.NewMemDb())

	tr, _ := NewWithCacheDb(ec.Hash256{}, cdb)
	values := make(map[string]string) // tracks content of the tr

	for i, step := range rt {
		switch step.op {
		case _OP_UPDATE:
			tr.Update(step.key, step.value)
			values[string(step.key)] = string(step.value)

		case _OP_REMOVE:
			tr.Remove(step.key)
			delete(values, string(step.key))

		case _OP_GET:
			v := tr.Get(step.key)
			want := values[string(step.key)]
			if string(v) != want {
				panic("Can't get here")
				rt[i].err = fmt.Errorf("mismatch for key 0x%x, got 0x%x want 0x%x", step.key, v, want)
			}

		case _OP_COMMIT:
			_, rt[i].err = tr.Commit(nil)

		case _OP_HASH:
			tr.RootHash()

		case _OP_RESET:
			hash, err := tr.Commit(nil)
			if err != nil {
				rt[i].err = err
				return false
			}
			newtr, err := NewWithCacheDb(hash, cdb)
			if err != nil {
				rt[i].err = err
				return false
			}
			tr = newtr

		case _OP_ITER_CHECK_HASH:
			checktr, _ := NewWithCacheDb(ec.Hash256{}, cdb)
			it := NewIterator(tr.NodeIterator(nil))
			for it.Next() {
				checktr.Update(it.Key, it.Value)
			}
			if tr.RootHash() != checktr.RootHash() {
				rt[i].err = fmt.Errorf("hash mismatch in _OP_ITER_CHECK_HASH")
			}

		case _OP_CHECK_CACHE:
			rt[i].err = checkCacheInvariant(tr.rootNode, nil, tr.cacheGen, false, 0)
		}
		// Abort the test on error.
		if rt[i].err != nil {
			return false
		}
	}
	return true
}

func checkCacheInvariant(n, parent _Node, parentCacheGen uint16, parentDirty bool, depth int) error {
	if n == nil {
		return nil
	}

	ci := n.cinfo()
	if ci == nil {
		return nil
	}

	errorf := func(format string, args ...interface{}) error {
		msg := fmt.Sprintf(format, args...)
		msg += fmt.Sprintf("\nat depth %d node %s", depth, spew.Sdump(n))
		msg += fmt.Sprintf("parent: %s", spew.Sdump(parent))
		return errors.New(msg)
	}

	if ci.gen > parentCacheGen {
		panic("Can't reach here")
		return errorf("cache invariant violation: %d > %d\n", ci.gen, parentCacheGen)
	}

	if depth > 0 && !parentDirty && ci.dirty {
		panic("Can't reach here")
		return errorf("cache invariant violation: %d > %d\n", ci.gen, parentCacheGen)
	}

	if node, ok := n.(*_BranchNode); ok {
		for _, child := range node.Children {
			if err := checkCacheInvariant(child, node, ci.gen, ci.dirty, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func TestRandom(t *testing.T) {
	if err := quick.Check(runRandTest, nil); err != nil {
		if cerr, ok := err.(*quick.CheckError); ok {
			t.Fatalf("random test iteration %d failed: %s", cerr.Count, spew.Sdump(cerr.In))
		}
		t.Fatal(err)
	}
}


func BenchmarkGet(b *testing.B)      { benchGet(b, false) }
func BenchmarkGetDb(b *testing.B)    { benchGet(b, true) }
func BenchmarkUpdateBE(b *testing.B) { benchUpdate(b, binary.BigEndian) }
func BenchmarkUpdateLE(b *testing.B) { benchUpdate(b, binary.LittleEndian) }

const benchElemCount = 20000

func benchGet(b *testing.B, commit bool) {
	tr := new(Trie)
	if commit {
		_, tmpdb := tempDb()
		tr, _ = NewWithCacheDb(ec.Hash256{}, tmpdb)
	}
	k := make([]byte, 32)
	for i := 0; i < benchElemCount; i++ {
		binary.LittleEndian.PutUint64(k, uint64(i))
		tr.Update(k, k)
	}
	binary.LittleEndian.PutUint64(k, benchElemCount/2)
	if commit {
		tr.Commit(nil)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tr.Get(k)
	}
	b.StopTimer()

	if commit {
		ldb := tr.cdb.kvdb.(*db.LevelDb)
		ldb.Close()
		os.RemoveAll(ldb.Path())
	}
}

func benchUpdate(b *testing.B, e binary.ByteOrder) *Trie {
	tr := newEmpty()
	k := make([]byte, 32)
	for i := 0; i < b.N; i++ {
		e.PutUint64(k, uint64(i))
		tr.Update(k, k)
	}
	return tr
}

// Benchmarks the tr hashing. Since the tr caches the result of any operation,
// we cannot use b.N as the number of hashing rouns, since all rounds apart from
// the first one will be NOOP. As such, we'll use b.N as the number of account to
// insert into the tr before measuring the hashing.
func BenchmarkHash(b *testing.B) {
	// Make the random benchmark deterministic
	random := rand.New(rand.NewSource(0))

	// Create a realistic account tr to hash
	addresses := make([][20]byte, b.N)
	for i := 0; i < len(addresses); i++ {
		for j := 0; j < len(addresses[i]); j++ {
			addresses[i][j] = byte(random.Intn(256))
		}
	}
	accounts := make([][]byte, len(addresses))
	for i := 0; i < len(accounts); i++ {
/*
		var (
			nonce   = uint64(random.Int63())
			balance = new(big.Int).Rand(random, new(big.Int).Exp(common.Big2, common.Big256, nil))
			root    = emptyRoot
			code    = crypto.Keccak256(nil)
		)
		accounts[i], _ = rlp.EncodeToBytes([]interface{}{nonce, balance, root, code})
*/
	}
	// Insert the accounts into the tr and hash it
	tr := newEmpty()
	for i := 0; i < len(addresses); i++ {
		h := ec.Sum256(addresses[i][:])
		tr.Update(h[:], accounts[i])
	}
	b.ResetTimer()
	b.ReportAllocs()
	tr.RootHash()
}

func tempDb() (string, *CacheDb) {
	dir, err := ioutil.TempDir("", "tr-bench")
	if err != nil {
		panic(fmt.Sprintf("can't create temporary directory: %v", err))
	}
	kvdb, err := db.NewLevelDb(dir, 256, 0)
	if err != nil {
		panic(fmt.Sprintf("can't create temporary database: %v", err))
	}
	return dir, NewCacheDb(kvdb)
}

func getString(tr *Trie, k string) []byte {
	return tr.Get([]byte(k))
}

func updateString(tr *Trie, k, v string) {
	tr.Update([]byte(k), []byte(v))
}

func deleteString(tr *Trie, k string) {
	tr.Remove([]byte(k))
}

