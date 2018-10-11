package tx

import (
	"bvchain/util"
	"bvchain/util/log"
	"bvchain/cfg"

	"container/list"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"errors"
)


var ErrTrxPoolIsFull = errors.New("TrxPool is full")
var ErrTxInCache = errors.New("Tx already exists in cache")

// TrxId is the hex encoded hash of the bytes as a Trx.
func TrxId(trx []byte) string {
	return fmt.Sprintf("%X", Trx(trx).Hash())
}

// TrxPool is an ordered in-memory pool for transactions before they are proposed.
type TrxPool struct {
	config *cfg.TrxPoolConfig

	mtx sync.Mutex
	cache *_TrxCache
	trxs                 *list.List    // concurrent linked-list of good trxs
	counter              int64         // simple incrementing counter
	height               int64         // the last block Update()'d to
	rechecking           int32         // for re-checking filtered trxs on Update()
	recheckCursor        *list.Element // next expected response
	recheckEnd           *list.Element // re-checking stops here
	notifiedTrxsAvailable bool
	trxsAvailable        chan struct{} // fires once for each height, when the TrxPool is not empty

	logger log.Logger
}

// TrxPoolOption sets an optional parameter on the TrxPool.
type TrxPoolOption func(*TrxPool)

// NewTrxPool returns a new TrxPool with the given configuration and connection to an application.
func NewTrxPool(config *cfg.TrxPoolConfig, height int64, options ...TrxPoolOption) *TrxPool {
	tp := &TrxPool{
		config:        config,
		cache: newTrxCache(config.Size),
		trxs:          list.New(),
		counter:       0,
		height:        height,
		rechecking:    0,
		recheckCursor: nil,
		recheckEnd:    nil,
		logger:        log.NewNopLogger(),
	}
	for _, option := range options {
		option(tp)
	}
	return tp
}

// EnableTrxsAvailable initializes the TrxsAvailable channel,
// ensuring it will trigger once every height when transactions are available.
// NOTE: not thread safe - should only be called once, on startup
func (tp *TrxPool) EnableTrxsAvailable() {
	tp.trxsAvailable = make(chan struct{}, 1)
}

// SetLogger sets the Logger.
func (tp *TrxPool) SetLogger(l log.Logger) {
	tp.logger = l
}

// Size returns the number of transactions in the TrxPool.
func (tp *TrxPool) Size() int {
	return tp.trxs.Len()
}

// Flush removes all transactions from the TrxPool
func (tp *TrxPool) Flush() {
	tp.mtx.Lock()
	defer tp.mtx.Unlock()

	tp.trxs.Init()
}

// TrxsFront returns the first transaction in the ordered list for peer
// goroutines to call .NextWait() on.
func (tp *TrxPool) TrxsFront() *list.Element {
	return tp.trxs.Front()
}


// CheckAndPush executes a new transaction to determine its validity
// and whether it should be added to the TrxPool.
func (tp *TrxPool) CheckAndPush(trx Trx) error {
	tp.mtx.Lock()
	defer tp.mtx.Unlock()

	if tp.Size() >= tp.config.Size {
		return ErrTrxPoolIsFull
	}

	if !tp.cache.Push(trx) {
		return ErrTxInCache
	}

	tp.counter++
	myTrx := &_MyTrx{
		counter: tp.counter,
		height: tp.height,
		trx: trx,
	}
	tp.trxs.PushBack(myTrx)
	// TODO

	return nil
}

// TrxsAvailable returns a channel which fires once for every height,
// and only when transactions are available in the TrxPool.
// NOTE: the returned channel may be nil if EnableTrxsAvailable was not called.
func (tp *TrxPool) TrxsAvailable() <-chan struct{} {
	return tp.trxsAvailable
}

func (tp *TrxPool) notifyTrxsAvailable() {
	if tp.Size() == 0 {
		panic("notified trxs available but TrxPool is empty!")
	}
	if tp.trxsAvailable != nil && !tp.notifiedTrxsAvailable {
		// channel cap is 1, so this will send once
		tp.notifiedTrxsAvailable = true
		select {
		case tp.trxsAvailable <- struct{}{}:
		default:
		}
	}
}

// Reap returns a list of transactions currently in the TrxPool.
// If maxTrxs is -1, there is no cap on the number of returned transactions.
func (tp *TrxPool) Reap(maxTrxs int) Trxs {
	tp.mtx.Lock()
	defer tp.mtx.Unlock()

	for atomic.LoadInt32(&tp.rechecking) > 0 {
		// TODO: Something better?
		time.Sleep(time.Millisecond * 10)
	}

	trxs := tp.collectTrxs(maxTrxs)
	return trxs
}

// maxTrxs: -1 means uncapped, 0 means none
func (tp *TrxPool) collectTrxs(maxTrxs int) Trxs {
	if maxTrxs == 0 {
		return []Trx{}
	} else if maxTrxs < 0 {
		maxTrxs = tp.trxs.Len()
	}
	trxs := make([]Trx, 0, util.MinInt(tp.trxs.Len(), maxTrxs))
	for e := tp.trxs.Front(); e != nil && len(trxs) < maxTrxs; e = e.Next() {
		mTrx := e.Value.(*_MyTrx)
		trxs = append(trxs, mTrx.trx)
	}
	return trxs
}

// Update informs the TrxPool that the given trxs were committed and can be discarded.
// NOTE: this should be called *after* block is committed by consensus.
// NOTE: unsafe; Lock/Unlock must be managed by caller
func (tp *TrxPool) Update(height int64, trxs Trxs) error {
	// First, create a lookup map of trxns in new trxs.
	trxsMap := make(map[string]struct{})
	for _, trx := range trxs {
		trxsMap[string(trx)] = struct{}{}
	}

	// Set height
	tp.height = height
	tp.notifiedTrxsAvailable = false

	// Remove transactions that are already in trxs.
	goodTrxs := tp.filterTrxs(trxsMap)
	// Recheck TrxPool trxs if any trxs were committed in the block
	// NOTE/XXX: in some apps a trx could be invalidated due to EndBlock,
	//	so we really still do need to recheck, but this is for debugging
	if tp.config.Recheck && (tp.config.RecheckEmpty || len(goodTrxs) > 0) {
		tp.logger.Info("Recheck trxs", "numtrxs", len(goodTrxs), "height", height)
		tp.recheckTrxs(goodTrxs)
		// At this point, tp.trxs are being rechecked.
		// tp.recheckCursor re-scans tp.trxs and possibly removes some trxs.
		// Before tp.Reap(), we should wait for tp.recheckCursor to be nil.
	}
	return nil
}

func (tp *TrxPool) filterTrxs(blockTrxsMap map[string]struct{}) []Trx {
	goodTrxs := make([]Trx, 0, tp.trxs.Len())
	for e := tp.trxs.Front(); e != nil; e = e.Next() {
		mTrx := e.Value.(*_MyTrx)
		if _, ok := blockTrxsMap[string(mTrx.trx)]; ok {
			tp.trxs.Remove(e)
			continue
		}

		goodTrxs = append(goodTrxs, mTrx.trx)
	}
	return goodTrxs
}

// NOTE: pass in goodTrxs because tp.trxs can mutate concurrently.
func (tp *TrxPool) recheckTrxs(goodTrxs []Trx) {
	if len(goodTrxs) == 0 {
		return
	}
	atomic.StoreInt32(&tp.rechecking, 1)
	tp.recheckCursor = tp.trxs.Front()
	tp.recheckEnd = tp.trxs.Back()
	// TODO
}


// _MyTrx is a transaction that successfully ran
type _MyTrx struct {
	counter int64    // a simple incrementing counter
	height int64     // height that this trx had been validated in
	trx Trx
}

// Height returns the height for this transaction
func (mTrx *_MyTrx) Height() int64 {
	return atomic.LoadInt64(&mTrx.height)
}


type _TrxCache struct {
	mtx  sync.Mutex
	size int
	dict map[string]struct{}
	list *list.List // to remove oldest trx when cache gets too big
}

// newTrxCache returns a new _TrxCache.
func newTrxCache(cacheSize int) *_TrxCache {
	return &_TrxCache{
		size: cacheSize,
		dict: make(map[string]struct{}, cacheSize),
		list: list.New(),
	}
}

// Reset resets the cache to an empty state.
func (cache *_TrxCache) Reset() {
	cache.mtx.Lock()
	cache.dict = make(map[string]struct{}, cache.size)
	cache.list.Init()
	cache.mtx.Unlock()
}

// Push adds the given trx to the cache and returns true. It returns false if trx
// is already in the cache.
func (cache *_TrxCache) Push(trx Trx) bool {
	cache.mtx.Lock()
	defer cache.mtx.Unlock()

	if _, exists := cache.dict[string(trx)]; exists {
		return false
	}

	if cache.list.Len() >= cache.size {
		popped := cache.list.Front()
		poppedTx := popped.Value.(Trx)
		// NOTE: the trx may have already been removed from the map
		// but deleting a non-existent element is fine
		delete(cache.dict, string(poppedTx))
		cache.list.Remove(popped)
	}
	cache.dict[string(trx)] = struct{}{}
	cache.list.PushBack(trx)
	return true
}

// Remove removes the given trx from the cache.
func (cache *_TrxCache) Remove(trx Trx) {
	cache.mtx.Lock()
	delete(cache.dict, string(trx))
	cache.mtx.Unlock()
}

