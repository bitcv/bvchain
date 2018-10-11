package tx

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"

	"github.com/go-kit/kit/log/term"

	"github.com/tendermint/tendermint/abci/example/kvstore"
	"github.com/tendermint/tendermint/libs/log"

	cfg "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/proxy"
	"github.com/tendermint/tendermint/types"
)

// trxPoolLogger is a TestingLogger which uses a different
// color for each validator ("validator" key must exist).
func trxPoolLogger() log.Logger {
	return log.TestingLoggerWithColorFn(func(keyvals ...interface{}) term.FgBgColor {
		for i := 0; i < len(keyvals)-1; i += 2 {
			if keyvals[i] == "validator" {
				return term.FgBgColor{Fg: term.Color(uint8(keyvals[i+1].(int) + 1))}
			}
		}
		return term.FgBgColor{}
	})
}

// connect N TrxPool reactors through N switches
func makeAndConnectTrxPoolReactors(config *cfg.Config, N int) []*TrxPoolReactor {
	reactors := make([]*TrxPoolReactor, N)
	logger := trxPoolLogger()
	for i := 0; i < N; i++ {
		app := kvstore.NewKVStoreApplication()
		cc := proxy.NewLocalClientCreator(app)
		tp := newTrxPoolWithApp(cc)

		reactors[i] = NewTrxPoolReactor(config.TrxPool, tp) // so we dont start the consensus states
		reactors[i].SetLogger(logger.With("validator", i))
	}

	p2p.MakeConnectedSwitches(config.P2P, N, func(i int, s *p2p.Switch) *p2p.Switch {
		s.AddReactor("MEMPOOL", reactors[i])
		return s

	}, p2p.Connect2Switches)
	return reactors
}

// wait for all trxs on all reactors
func waitForTrxs(t *testing.T, trxs types.Trxs, reactors []*TrxPoolReactor) {
	// wait for the trxs in all TrxPools
	wg := new(sync.WaitGroup)
	for i := 0; i < len(reactors); i++ {
		wg.Add(1)
		go _waitForTrxs(t, wg, trxs, i, reactors)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	timer := time.After(TIMEOUT)
	select {
	case <-timer:
		t.Fatal("Timed out waiting for trxs")
	case <-done:
	}
}

// wait for all trxs on a single TrxPool
func _waitForTrxs(t *testing.T, wg *sync.WaitGroup, trxs types.Trxs, reactorIdx int, reactors []*TrxPoolReactor) {

	tp := reactors[reactorIdx].TrxPool
	for tp.Size() != len(trxs) {
		time.Sleep(time.Millisecond * 100)
	}

	reapedTrxs := tp.Reap(len(trxs))
	for i, trx := range trxs {
		assert.Equal(t, trx, reapedTrxs[i], fmt.Sprintf("trxs at index %d on reactor %d don't match: %v vs %v", i, reactorIdx, trx, reapedTrxs[i]))
	}
	wg.Done()
}

const (
	NUM_TXS = 1000
	TIMEOUT = 120 * time.Second // ridiculously high because CircleCI is slow
)

func TestReactorBroadcastTrxMessage(t *testing.T) {
	config := cfg.TestConfig()
	const N = 4
	reactors := makeAndConnectTrxPoolReactors(config, N)
	defer func() {
		for _, r := range reactors {
			r.Stop()
		}
	}()

	// send a bunch of trxs to the first reactor's TrxPool
	// and wait for them all to be received in the others
	trxs := checkTrxs(t, reactors[0].TrxPool, NUM_TXS)
	waitForTrxs(t, trxs, reactors)
}

func TestBroadcastTrxForPeerStopsWhenPeerStops(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	config := cfg.TestConfig()
	const N = 2
	reactors := makeAndConnectTrxPoolReactors(config, N)
	defer func() {
		for _, r := range reactors {
			r.Stop()
		}
	}()

	// stop peer
	sw := reactors[1].Switch
	sw.StopPeerForError(sw.Peers().List()[0], errors.New("some reason"))

	// check that we are not leaking any go-routines
	// i.e. broadcastTrxRoutine finishes when peer is stopped
	leaktest.CheckTimeout(t, 10*time.Second)()
}

func TestBroadcastTrxForPeerStopsWhenReactorStops(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	config := cfg.TestConfig()
	const N = 2
	reactors := makeAndConnectTrxPoolReactors(config, N)

	// stop reactors
	for _, r := range reactors {
		r.Stop()
	}

	// check that we are not leaking any go-routines
	// i.e. broadcastTrxRoutine finishes when reactor is stopped
	leaktest.CheckTimeout(t, 10*time.Second)()
}
