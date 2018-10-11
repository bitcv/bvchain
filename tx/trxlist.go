package tx

import (
//	"bvchain/util"
//	"bvchain/util/log"

	"container/heap"
	"math"
	"math/big"
	"sort"
)

// _NonceHeap is a heap.Interface implementation over 64bit unsigned integers for
// retrieving sorted transactions from the possibly gapped future queue.
type _NonceHeap []uint64

func (h _NonceHeap) Len() int           { return len(h) }
func (h _NonceHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h _NonceHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *_NonceHeap) Push(x interface{}) {
	*h = append(*h, x.(uint64))
}

func (h *_NonceHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0:n-1]
	return x
}

// _TrxSortedMap is a nonce->transaction hash map with a heap based index to allow
// iterating over the contents in a nonce-incrementing way.
type _TrxSortedMap struct {
	items map[uint64]*Transaction // Hash map storing the transaction data
	index _NonceHeap              // Heap of nonces of all the stored transactions (non-strict mode)
	_cache []*Transaction         // Cache of the transactions already sorted
}

// newTrxSortedMap creates a new nonce-sorted transaction map.
func newTrxSortedMap() *_TrxSortedMap {
	return &_TrxSortedMap{
		items: make(map[uint64]*Transaction),
	}
}

// Len returns the length of the transaction map.
func (m *_TrxSortedMap) Len() int {
	return len(m.items)
}

// Get retrieves the current transactions associated with the given nonce.
func (m *_TrxSortedMap) Get(nonce uint64) *Transaction {
	return m.items[nonce]
}

// Put inserts a new transaction into the map, also updating the map's nonce
// index. If a transaction already exists with the same nonce, it's overwritten.
func (m *_TrxSortedMap) Put(trx *Transaction) {
	nonce := trx.Nonce
	if m.items[nonce] == nil {
		heap.Push(&m.index, nonce)
	}
	m.items[nonce] = trx
	m._cache = nil
}

// RemoveLowerNonce removes all transactions from the map with a nonce lower than the
// provided threshold. Every removed transaction is returned for any post-removal
// maintenance.
func (m *_TrxSortedMap) RemoveLowerNonce(threshold uint64) []*Transaction {
	var removed []*Transaction

	// Pop off heap items until the threshold is reached
	for m.index.Len() > 0 && m.index[0] < threshold {
		nonce := heap.Pop(&m.index).(uint64)
		removed = append(removed, m.items[nonce])
		delete(m.items, nonce)
	}
	// If we had a cached order, shift the front
	if m._cache != nil {
		m._cache = m._cache[len(removed):]
	}
	return removed
}

// Filter iterates over the list of transactions and removes all of them for which
// the specified function evaluates to true.
func (m *_TrxSortedMap) Filter(filter func(*Transaction) bool) []*Transaction {
	var removed []*Transaction

	// Collect all the transactions to filter out
	for nonce, trx := range m.items {
		if filter(trx) {
			removed = append(removed, trx)
			delete(m.items, nonce)
		}
	}

	// If transactions were removed, the heap and _cache are ruined
	if len(removed) > 0 {
		m.index = make([]uint64, 0, len(m.items))
		for nonce := range m.items {
			m.index = append(m.index, nonce)
		}
		heap.Init(&m.index)

		m._cache = nil
	}
	return removed
}

// Cap places a hard limit on the number of items, returning all transactions
// exceeding that limit.
func (m *_TrxSortedMap) Cap(num int) []*Transaction {
	if len(m.items) <= num {
		return nil
	}

	var removed []*Transaction

	sort.Sort(m.index)
	for n := len(m.items); n > num; n-- {
		removed = append(removed, m.items[m.index[n-1]])
		delete(m.items, m.index[n-1])
	}
	m.index = m.index[:num]
	heap.Init(&m.index)

	if m._cache != nil {
		m._cache = m._cache[:len(m._cache)-len(removed)]
	}
	return removed
}

// Remove deletes a transaction from the maintained map, returning whether the
// transaction was found.
func (m *_TrxSortedMap) Remove(nonce uint64) bool {
	if _, ok := m.items[nonce]; !ok {
		return false
	}

	// Otherwise delete the transaction and fix the heap index
	for i := 0; i < m.index.Len(); i++ {
		if m.index[i] == nonce {
			heap.Remove(&m.index, i)
			break
		}
	}
	delete(m.items, nonce)
	m._cache = nil

	return true
}

// Ready retrieves a sequentially increasing list of transactions starting at the
// provided nonce that is ready for processing. The returned transactions will be
// removed from the list.
//
// Note, all transactions with nonces lower than start will also be returned to
// prevent getting into and invalid state. This is not something that should ever
// happen but better to be self correcting than failing!
func (m *_TrxSortedMap) Ready(start uint64) []*Transaction {
	// Short circuit if no transactions are available
	if m.index.Len() == 0 || m.index[0] > start {
		return nil
	}

	// XXX: start not used below? why?
	// Otherwise start accumulating incremental transactions
	var ready []*Transaction
	for next := m.index[0]; m.index.Len() > 0 && m.index[0] == next; next++ {
		ready = append(ready, m.items[next])
		delete(m.items, next)
		heap.Pop(&m.index)
	}
	m._cache = nil

	return ready
}

type _TrxsByNonce []*Transaction

func (s _TrxsByNonce) Len() int           { return len(s) }
func (s _TrxsByNonce) Less(i, j int) bool { return s[i].Nonce < s[j].Nonce }
func (s _TrxsByNonce) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }


// Flatten creates a nonce-sorted slice of transactions based on the loosely
// sorted internal representation. The result of the sorting is cached in case
// it's requested again before any modifications are made to the contents.
func (m *_TrxSortedMap) Flatten() []*Transaction {
	// If the sorting was not cached yet, create and _cache it
	if m._cache == nil {
		m._cache = make([]*Transaction, 0, len(m.items))
		for _, trx := range m.items {
			m._cache = append(m._cache, trx)
		}
		sort.Sort(_TrxsByNonce(m._cache))
	}
	// Copy the _cache to prevent accidental modifications
	trxs := make([]*Transaction, len(m._cache))
	copy(trxs, m._cache)
	return trxs
}

// _TrxList is a "list" of transactions belonging to an account, sorted by account
// nonce. The same type can be used both for storing contiguous transactions for
// the executable/pending queue; and for storing gapped transactions for the non-
// executable/future queue, with minor behavioral changes.
type _TrxList struct {
	strict bool         // Whether nonces are strictly continuous or not
	trxs *_TrxSortedMap // Heap indexed sorted hash map of the transactions

	costcap *big.Int // Price of the highest costing transaction (reset only if exceeds balance)
	gascap  uint64   // Gas limit of the highest spending transaction (reset only if exceeds block limit)
}

// newTxList create a new transaction list for maintaining nonce-indexable fast,
// gapped, sortable transaction lists.
func newTxList(strict bool) *_TrxList {
	return &_TrxList{
		strict: strict,
		trxs: newTrxSortedMap(),
		costcap: new(big.Int),
	}
}

// Overlaps returns whether the transaction specified has the same nonce as one
// already contained within the list.
func (l *_TrxList) Overlaps(trx *Transaction) bool {
	return l.trxs.Get(trx.Nonce) != nil
}

// Add tries to insert a new transaction into the list, returning whether the
// transaction was accepted, and if yes, any previous transaction it replaced.
//
// If the new transaction is accepted into the list, the lists' cost and gas
// thresholds are also potentially updated.
func (l *_TrxList) Add(trx *Transaction, priceBump uint64) (bool, *Transaction) {
	// If there's an older better transaction, abort
	old := l.trxs.Get(trx.Nonce)
	if old != nil {
		threshold := new(big.Int).Div(new(big.Int).Mul(old.GasPrice(), big.NewInt(100+int64(priceBump))), big.NewInt(100))
		// Have to ensure that the new gas price is higher than the old gas
		// price as well as checking the percentage threshold to ensure that
		// this is accurate for low (Wei-level) gas price replacements
		if old.GasPrice().Cmp(trx.GasPrice()) >= 0 || threshold.Cmp(trx.GasPrice()) > 0 {
			return false, nil
		}
	}
	// Otherwise overwrite the old transaction with the current one
	l.trxs.Put(trx)
	if cost := trx.Cost(); l.costcap.Cmp(cost) < 0 {
		l.costcap = cost
	}
	if gas := trx.Gas(); l.gascap < gas {
		l.gascap = gas
	}
	return true, old
}

// RemoveLowerNonce removes all transactions from the list with a nonce lower than the
// provided threshold. Every removed transaction is returned for any post-removal
// maintenance.
func (l *_TrxList) RemoveLowerNonce(threshold uint64) []*Transaction {
	return l.trxs.RemoveLowerNonce(threshold)
}

// Filter removes all transactions from the list with a cost or gas limit higher
// than the provided thresholds. Every removed transaction is returned for any
// post-removal maintenance. Strict-mode invalidated transactions are also
// returned.
//
// This method uses the cached costcap and gascap to quickly decide if there's even
// a point in calculating all the costs or if the balance covers all. If the threshold
// is lower than the costgas cap, the caps will be reset to a new high after removing
// the newly invalidated transactions.
func (l *_TrxList) Filter(costLimit *big.Int, gasLimit uint64) ([]*Transaction, []*Transaction) {
	// If all transactions are below the threshold, short circuit
	if l.costcap.Cmp(costLimit) <= 0 && l.gascap <= gasLimit {
		return nil, nil
	}
	l.costcap = new(big.Int).Set(costLimit) // Lower the caps to the thresholds
	l.gascap = gasLimit

	// Filter out all the transactions above the account's funds
	removed := l.trxs.Filter(func(trx *Transaction) bool { return trx.Cost().Cmp(costLimit) > 0 || trx.Gas() > gasLimit })

	// If the list was strict, filter anything above the lowest nonce
	var invalids []*Transaction

	if l.strict && len(removed) > 0 {
		lowest := uint64(math.MaxUint64)
		for _, trx := range removed {
			if nonce := trx.Nonce; lowest > nonce {
				lowest = nonce
			}
		}
		invalids = l.trxs.Filter(func(trx *Transaction) bool { return trx.Nonce > lowest })
	}
	return removed, invalids
}

// Cap places a hard limit on the number of items, returning all transactions
// exceeding that limit.
func (l *_TrxList) Cap(threshold int) []*Transaction {
	return l.trxs.Cap(threshold)
}

// Remove deletes a transaction from the maintained list, returning whether the
// transaction was found, and also returning any transaction invalidated due to
// the deletion (strict mode only).
func (l *_TrxList) Remove(trx *Transaction) (bool, []*Transaction) {
	nonce := trx.Nonce
	if removed := l.trxs.Remove(nonce); !removed {
		return false, nil
	}
	// In strict mode, filter out non-executable transactions
	if l.strict {
		return true, l.trxs.Filter(func(trx *Transaction) bool { return trx.Nonce > nonce })
	}
	return true, nil
}

// Ready retrieves a sequentially increasing list of transactions starting at the
// provided nonce that is ready for processing. The returned transactions will be
// removed from the list.
//
// Note, all transactions with nonces lower than start will also be returned to
// prevent getting into and invalid state. This is not something that should ever
// happen but better to be self correcting than failing!
func (l *_TrxList) Ready(start uint64) []*Transaction {
	return l.trxs.Ready(start)
}

// Len returns the length of the transaction list.
func (l *_TrxList) Len() int {
	return l.trxs.Len()
}

// Empty returns whether the list of transactions is empty or not.
func (l *_TrxList) Empty() bool {
	return l.Len() == 0
}

// Flatten creates a nonce-sorted slice of transactions based on the loosely
// sorted internal representation. The result of the sorting is cached in case
// it's requested again before any modifications are made to the contents.
func (l *_TrxList) Flatten() []*Transaction {
	return l.trxs.Flatten()
}

