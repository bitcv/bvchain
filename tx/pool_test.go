package tx

import (
	"bvchain/util/log"
	"bvchain/cfg"

	"crypto/md5"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"
	"github.com/stretchr/testify/require"
)

func newTrxPoolWithApp(cc proxy.ClientCreator) *TrxPool {
	config := cfg.ResetTestRoot("mempool_test")

	appConnMem, _ := cc.NewABCIClient()
	appConnMem.SetLogger(log.TestingLogger().With("module", "abci-client", "connection", "TrxPool"))
	err := appConnMem.Start()
	if err != nil {
		panic(err)
	}
	tp := NewTrxPool(config.TrxPool, appConnMem, 0)
	tp.SetLogger(log.TestingLogger())
	return tp
}

func ensureNoFire(t *testing.T, ch <-chan struct{}, timeoutMS int) {
	timer := time.NewTimer(time.Duration(timeoutMS) * time.Millisecond)
	select {
	case <-ch:
		t.Fatal("Expected not to fire")
	case <-timer.C:
	}
}

func ensureFire(t *testing.T, ch <-chan struct{}, timeoutMS int) {
	timer := time.NewTimer(time.Duration(timeoutMS) * time.Millisecond)
	select {
	case <-ch:
	case <-timer.C:
		t.Fatal("Expected to fire")
	}
}

func checkTrxs(t *testing.T, tp *TrxPool, count int) types.Trxs {
	trxs := make(types.Trxs, count)
	for i := 0; i < count; i++ {
		trxBytes := make([]byte, 20)
		trxs[i] = trxBytes
		_, err := rand.Read(trxBytes)
		if err != nil {
			t.Error(err)
		}
		if err := tp.CheckTrx(trxBytes, nil); err != nil {
			t.Fatalf("Error after CheckTrx: %v", err)
		}
	}
	return trxs
}

func TestTrxsAvailable(t *testing.T) {
	app := kvstore.NewKVStoreApplication()
	cc := proxy.NewLocalClientCreator(app)
	tp := newTrxPoolWithApp(cc)
	tp.EnableTrxsAvailable()

	timeoutMS := 500

	// with no trxs, it shouldnt fire
	ensureNoFire(t, tp.TrxsAvailable(), timeoutMS)

	// send a bunch of trxs, it should only fire once
	trxs := checkTrxs(t, tp, 100)
	ensureFire(t, tp.TrxsAvailable(), timeoutMS)
	ensureNoFire(t, tp.TrxsAvailable(), timeoutMS)

	// call update with half the trxs.
	// it should fire once now for the new height
	// since there are still trxs left
	committedTrxs, trxs := trxs[:50], trxs[50:]
	if err := tp.Update(1, committedTrxs); err != nil {
		t.Error(err)
	}
	ensureFire(t, tp.TrxsAvailable(), timeoutMS)
	ensureNoFire(t, tp.TrxsAvailable(), timeoutMS)

	// send a bunch more trxs. we already fired for this height so it shouldnt fire again
	moreTrxs := checkTrxs(t, tp, 50)
	ensureNoFire(t, tp.TrxsAvailable(), timeoutMS)

	// now call update with all the trxs. it should not fire as there are no trxs left
	committedTrxs = append(trxs, moreTrxs...)
	if err := tp.Update(2, committedTrxs); err != nil {
		t.Error(err)
	}
	ensureNoFire(t, tp.TrxsAvailable(), timeoutMS)

	// send a bunch more trxs, it should only fire once
	checkTrxs(t, tp, 100)
	ensureFire(t, tp.TrxsAvailable(), timeoutMS)
	ensureNoFire(t, tp.TrxsAvailable(), timeoutMS)
}

func TestSerialReap(t *testing.T) {
	app := counter.NewCounterApplication(true)
	app.SetOption(abci.RequestSetOption{Key: "serial", Value: "on"})
	cc := proxy.NewLocalClientCreator(app)

	tp := newTrxPoolWithApp(cc)
	appConnCon, _ := cc.NewABCIClient()
	appConnCon.SetLogger(log.TestingLogger().With("module", "abci-client", "connection", "consensus"))
	err := appConnCon.Start()
	require.Nil(t, err)

	cacheMap := make(map[string]struct{})
	deliverTrxsRange := func(start, end int) {
		// Deliver some trxs.
		for i := start; i < end; i++ {

			// This will succeed
			trxBytes := make([]byte, 8)
			binary.BigEndian.PutUint64(trxBytes, uint64(i))
			err := tp.CheckTrx(trxBytes, nil)
			_, cached := cacheMap[string(trxBytes)]
			if cached {
				require.NotNil(t, err, "expected error for cached trx")
			} else {
				require.Nil(t, err, "expected no err for uncached trx")
			}
			cacheMap[string(trxBytes)] = struct{}{}

			// Duplicates are cached and should return error
			err = tp.CheckTrx(trxBytes, nil)
			require.NotNil(t, err, "Expected error after CheckTrx on duplicated trx")
		}
	}

	reapCheck := func(exp int) {
		trxs := tp.Reap(-1)
		require.Equal(t, len(trxs), exp, cmn.Fmt("Expected to reap %v trxs but got %v", exp, len(trxs)))
	}

	updateRange := func(start, end int) {
		trxs := make([]types.Trx, 0)
		for i := start; i < end; i++ {
			trxBytes := make([]byte, 8)
			binary.BigEndian.PutUint64(trxBytes, uint64(i))
			trxs = append(trxs, trxBytes)
		}
		if err := tp.Update(0, trxs); err != nil {
			t.Error(err)
		}
	}

	commitRange := func(start, end int) {
		// Deliver some trxs.
		for i := start; i < end; i++ {
			trxBytes := make([]byte, 8)
			binary.BigEndian.PutUint64(trxBytes, uint64(i))
			res, err := appConnCon.DeliverTrxSync(trxBytes)
			if err != nil {
				t.Errorf("Client error committing trx: %v", err)
			}
			if res.IsErr() {
				t.Errorf("Error committing trx. Code:%v result:%X log:%v",
					res.Code, res.Data, res.Log)
			}
		}
		res, err := appConnCon.CommitSync()
		if err != nil {
			t.Errorf("Client error committing: %v", err)
		}
		if len(res.Data) != 8 {
			t.Errorf("Error committing. Hash:%X", res.Data)
		}
	}

	//----------------------------------------

	// Deliver some trxs.
	deliverTrxsRange(0, 100)

	// Reap the trxs.
	reapCheck(100)

	// Reap again.  We should get the same amount
	reapCheck(100)

	// Deliver 0 to 999, we should reap 900 new trxs
	// because 100 were already counted.
	deliverTrxsRange(0, 1000)

	// Reap the trxs.
	reapCheck(1000)

	// Reap again.  We should get the same amount
	reapCheck(1000)

	// Commit from the conensus AppConn
	commitRange(0, 500)
	updateRange(0, 500)

	// We should have 500 left.
	reapCheck(500)

	// Deliver 100 invalid trxs and 100 valid trxs
	deliverTrxsRange(900, 1100)

	// We should have 600 now.
	reapCheck(600)
}

func TestTrxPoolCloseWAL(t *testing.T) {
	// 1. Create the temporary directory for TrxPool and WAL testing.
	rootDir, err := ioutil.TempDir("", "TrxPool-test")
	require.Nil(t, err, "expecting successful tmpdir creation")
	defer os.RemoveAll(rootDir)

	// 2. Ensure that it doesn't contain any elements -- Sanity check
	m1, err := filepath.Glob(filepath.Join(rootDir, "*"))
	require.Nil(t, err, "successful globbing expected")
	require.Equal(t, 0, len(m1), "no matches yet")

	// 3. Create the TrxPool
	wcfg := cfg.DefaultTrxPoolConfig()
	wcfg.RootDir = rootDir
	app := kvstore.NewKVStoreApplication()
	cc := proxy.NewLocalClientCreator(app)
	appConnMem, _ := cc.NewABCIClient()
	tp := NewTrxPool(wcfg, appConnMem, 10)
	tp.InitWAL()

	// 4. Ensure that the directory contains the WAL file
	m2, err := filepath.Glob(filepath.Join(rootDir, "*"))
	require.Nil(t, err, "successful globbing expected")
	require.Equal(t, 1, len(m2), "expecting the wal match in")

	// 5. Write some contents to the WAL
	tp.CheckTrx(types.Trx([]byte("foo")), nil)
	walFilepath := tp.wal.Path
	sum1 := checksumFile(walFilepath, t)

	// 6. Sanity check to ensure that the written TX matches the expectation.
	require.Equal(t, sum1, checksumIt([]byte("foo\n")), "foo with a newline should be written")

	// 7. Invoke CloseWAL() and ensure it discards the
	// WAL thus any other write won't go through.
	require.True(t, tp.CloseWAL(), "CloseWAL should CloseWAL")
	tp.CheckTrx(types.Trx([]byte("bar")), nil)
	sum2 := checksumFile(walFilepath, t)
	require.Equal(t, sum1, sum2, "expected no change to the WAL after invoking CloseWAL() since it was discarded")

	// 8. Second CloseWAL should do nothing
	require.False(t, tp.CloseWAL(), "CloseWAL should CloseWAL")

	// 9. Sanity check to ensure that the WAL file still exists
	m3, err := filepath.Glob(filepath.Join(rootDir, "*"))
	require.Nil(t, err, "successful globbing expected")
	require.Equal(t, 1, len(m3), "expecting the wal match in")
}

func checksumIt(data []byte) string {
	h := md5.New()
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func checksumFile(p string, t *testing.T) string {
	data, err := ioutil.ReadFile(p)
	require.Nil(t, err, "expecting successful read of %q", p)
	return checksumIt(data)
}

