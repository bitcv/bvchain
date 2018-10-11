// Modified for BvChain
// Modified for Tendermint
// Originally Copyright (c) 2013-2014 Conformal Systems LLC.
// https://github.com/conformal/btcd/blob/master/LICENSE

package pex

import (
	"bvchain/ec"
	"bvchain/p2p"
	"bvchain/util"

	"fmt"
	"math/rand"
	"encoding/binary"
	"math"
	"net"
	"sync"
	"time"
)

const (
	_NEW_BUCKET = 0x01
	_OLD_BUCKET = 0x02
)

// AddrBook is an address book used for tracking peers
// so we can gossip about them to others and select
// peers to dial.
// TODO: break this up?
type AddrBook interface {
	util.Service

	AddOurAddress(*p2p.NetAddress)

	IsOurAddress(*p2p.NetAddress) bool

	// Add and remove an address
	AddAddress(addr *p2p.NetAddress, src *p2p.NetAddress) error
	RemoveAddress(*p2p.NetAddress)

	// Check if the address is in the book
	HasAddress(*p2p.NetAddress) bool

	// Do we need more peers?
	NeedMoreAddrs() bool

	// Pick an address to dial
	PickAddress(biasNewAddrsPercent int) *p2p.NetAddress

	// Mark address
	MarkGood(*p2p.NetAddress)
	MarkAttempt(*p2p.NetAddress)
	MarkBad(*p2p.NetAddress)

	IsGood(*p2p.NetAddress) bool

	GetSelection() []*p2p.NetAddress
	GetSelectionWithBias(biasNewAddrsPercent int) []*p2p.NetAddress

	CountOfKnownAddress() int
	IterateKnownAddresses(func (*_KnownAddress))

	Save()
}

var _ AddrBook = (*_AddrBook)(nil)

// _AddrBook - concurrency safe peer address manager.
// Implements AddrBook.
type _AddrBook struct {
	util.BaseService

	// immutable after creation
	filePath          string
	routabilityStrict bool
	key               string // random prefix for bucket placement

	// accessed concurrently
	mtx        sync.Mutex
	rng       *rand.Rand
	ourAddrs   map[string]struct{}
	privateIds map[p2p.NodeId]struct{}
	addrLookup map[p2p.NodeId]*_KnownAddress // new & old
	bucketsOld []map[string]*_KnownAddress
	bucketsNew []map[string]*_KnownAddress
	nOld       int
	nNew       int

	wg sync.WaitGroup
}

// NewAddrBook creates a new address book.
// Use Start to begin processing asynchronous address updates.
func NewAddrBook(filePath string, routabilityStrict bool) *_AddrBook {
	am := &_AddrBook{
		rng:              util.NewRand(),
		ourAddrs:          make(map[string]struct{}),
		addrLookup:        make(map[p2p.NodeId]*_KnownAddress),
		filePath:          filePath,
		routabilityStrict: routabilityStrict,
	}
	am.BaseService.Init(nil, "AddrBook", am)
	am.init()
	return am
}

// Initialize the buckets.
// When modifying this, don't forget to update loadFromFile()
func (a *_AddrBook) init() {
	a.key = util.GenerateRandomId(24) // 24 * 5 = 120 bits
	// New addr buckets
	a.bucketsNew = make([]map[string]*_KnownAddress, newBucketCount)
	for i := range a.bucketsNew {
		a.bucketsNew[i] = make(map[string]*_KnownAddress)
	}
	// Old addr buckets
	a.bucketsOld = make([]map[string]*_KnownAddress, oldBucketCount)
	for i := range a.bucketsOld {
		a.bucketsOld[i] = make(map[string]*_KnownAddress)
	}
}

// OnStart implements Service.
func (a *_AddrBook) OnStart() error {
	if err := a.BaseService.OnStart(); err != nil {
		return err
	}
	a.loadFromFile(a.filePath)

	// wg.Add to ensure that any invocation of .Wait()
	// later on will wait for saveRoutine to terminate.
	a.wg.Add(1)
	go a.saveRoutine()

	return nil
}

// OnStop implements Service.
func (a *_AddrBook) OnStop() {
	a.BaseService.OnStop()
}

func (a *_AddrBook) Wait() {
	a.wg.Wait()
}

func (a *_AddrBook) FilePath() string {
	return a.filePath
}

//-------------------------------------------------------

// AddOurAddress one of our addresses.
func (a *_AddrBook) AddOurAddress(addr *p2p.NetAddress) {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	a.Logger.Info("Add our address to book", "addr", addr)
	a.ourAddrs[addr.String()] = struct{}{}
}

// IsOurAddress returns true if it is our address.
func (a *_AddrBook) IsOurAddress(addr *p2p.NetAddress) bool {
	a.mtx.Lock()
	_, ok := a.ourAddrs[addr.String()]
	a.mtx.Unlock()
	return ok
}

// AddAddress implements AddrBook
// Add address to a "new" bucket. If it's already in one, only add it probabilistically.
// Returns error if the addr is non-routable. Does not add self.
// NOTE: addr must not be nil
func (a *_AddrBook) AddAddress(addr *p2p.NetAddress, src *p2p.NetAddress) error {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	return a.addAddress(addr, src)
}

// RemoveAddress implements AddrBook - removes the address from the book.
func (a *_AddrBook) RemoveAddress(addr *p2p.NetAddress) {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	ka := a.addrLookup[addr.NodeId]
	if ka == nil {
		return
	}
	a.Logger.Info("Remove address from book", "addr", ka.Addr, "NodeId", ka.NodeId())
	a.removeFromAllBuckets(ka)
}

// IsGood returns true if peer was ever marked as good and haven't
// done anything wrong since then.
func (a *_AddrBook) IsGood(addr *p2p.NetAddress) bool {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	return a.addrLookup[addr.NodeId].isOld()
}

// HasAddress returns true if the address is in the book.
func (a *_AddrBook) HasAddress(addr *p2p.NetAddress) bool {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	ka := a.addrLookup[addr.NodeId]
	return ka != nil
}

// NeedMoreAddrs implements AddrBook - returns true if there are not have enough addresses in the book.
func (a *_AddrBook) NeedMoreAddrs() bool {
	return a.Size() < needAddressThreshold
}

// PickAddress implements AddrBook. It picks an address to connect to.
// The address is picked randomly from an old or new bucket according
// to the biasNewAddrsPercent argument, which must be between [0, 100] (or else is truncated to that range)
// and determines how biased we are to pick an address from a new bucket.
// PickAddress returns nil if the AddrBook is empty or if we try to pick
// from an empty bucket.
func (a *_AddrBook) PickAddress(biasNewAddrsPercent int) *p2p.NetAddress {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	bookSize := a.size()
	if bookSize <= 0 {
		if bookSize < 0 {
			a.Logger.Error("Addrbook size less than 0", "nNew", a.nNew, "nOld", a.nOld)
		}
		return nil
	}
	if biasNewAddrsPercent > 100 {
		biasNewAddrsPercent = 100
	}
	if biasNewAddrsPercent < 0 {
		biasNewAddrsPercent = 0
	}

	// Bias between new and old addresses.
	oldCorrelation := math.Sqrt(float64(a.nOld)) * (100.0 - float64(biasNewAddrsPercent))
	newCorrelation := math.Sqrt(float64(a.nNew)) * float64(biasNewAddrsPercent)

	// pick a random peer from a random bucket
	var bucket map[string]*_KnownAddress
	pickFromOldBucket := (newCorrelation+oldCorrelation)*a.rng.Float64() < oldCorrelation
	if (pickFromOldBucket && a.nOld == 0) ||
		(!pickFromOldBucket && a.nNew == 0) {
		return nil
	}
	// loop until we pick a random non-empty bucket
	for len(bucket) == 0 {
		if pickFromOldBucket {
			bucket = a.bucketsOld[a.rng.Intn(len(a.bucketsOld))]
		} else {
			bucket = a.bucketsNew[a.rng.Intn(len(a.bucketsNew))]
		}
	}
	// pick a random index and loop over the map to return that index
	randIndex := a.rng.Intn(len(bucket))
	for _, ka := range bucket {
		if randIndex == 0 {
			return ka.Addr
		}
		randIndex--
	}
	return nil
}

// MarkGood implements AddrBook - it marks the peer as good and
// moves it into an "old" bucket.
func (a *_AddrBook) MarkGood(addr *p2p.NetAddress) {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	ka := a.addrLookup[addr.NodeId]
	if ka == nil {
		return
	}
	ka.markGood()
	if ka.isNew() {
		a.moveToOld(ka)
	}
}

// MarkAttempt implements AddrBook - it marks that an attempt was made to connect to the address.
func (a *_AddrBook) MarkAttempt(addr *p2p.NetAddress) {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	ka := a.addrLookup[addr.NodeId]
	if ka == nil {
		return
	}
	ka.markAttempt()
}

// MarkBad implements AddrBook. Currently it just ejects the address.
// TODO: black list for some amount of time
func (a *_AddrBook) MarkBad(addr *p2p.NetAddress) {
	a.RemoveAddress(addr)
}

// GetSelection implements AddrBook.
// It randomly selects some addresses (old & new). Suitable for peer-exchange protocols.
// Must never return a nil address.
func (a *_AddrBook) GetSelection() []*p2p.NetAddress {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	bookSize := a.size()
	if bookSize <= 0 {
		if bookSize < 0 {
			a.Logger.Error("Addrbook size less than 0", "nNew", a.nNew, "nOld", a.nOld)
		}
		return nil
	}

	numAddresses := util.MaxInt(
		util.MinInt(_MIN_GET_SELECTION, bookSize),
		bookSize*_GET_SELECTION_PERCENT/100)
	numAddresses = util.MinInt(_MAX_GET_SELECTION, numAddresses)

	// XXX: instead of making a list of all addresses, shuffling, and slicing a random chunk,
	// could we just select a random numAddresses of indexes?
	allAddr := make([]*p2p.NetAddress, bookSize)
	i := 0
	for _, ka := range a.addrLookup {
		allAddr[i] = ka.Addr
		i++
	}

	// Fisher-Yates shuffle the array. We only need to do the first
	// `numAddresses' since we are throwing the rest.
	for i := 0; i < numAddresses; i++ {
		// pick a number between current index and the end
		j := a.rng.Intn(len(allAddr)-i) + i
		allAddr[i], allAddr[j] = allAddr[j], allAddr[i]
	}

	// slice off the limit we are willing to share.
	return allAddr[:numAddresses]
}

// GetSelectionWithBias implements AddrBook.
// It randomly selects some addresses (old & new). Suitable for peer-exchange protocols.
// Must never return a nil address.
//
// Each address is picked randomly from an old or new bucket according to the
// biasNewAddrsPercent argument, which must be between [0, 100] (or else is truncated to
// that range) and determines how biased we are to pick an address from a new
// bucket.
func (a *_AddrBook) GetSelectionWithBias(biasNewAddrsPercent int) []*p2p.NetAddress {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	bookSize := a.size()
	if bookSize <= 0 {
		if bookSize < 0 {
			a.Logger.Error("Addrbook size less than 0", "nNew", a.nNew, "nOld", a.nOld)
		}
		return nil
	}

	if biasNewAddrsPercent > 100 {
		biasNewAddrsPercent = 100
	}
	if biasNewAddrsPercent < 0 {
		biasNewAddrsPercent = 0
	}

	numAddresses := util.MaxInt(
		util.MinInt(_MIN_GET_SELECTION, bookSize),
		bookSize*_GET_SELECTION_PERCENT/100)
	numAddresses = util.MinInt(_MAX_GET_SELECTION, numAddresses)

	selection := make([]*p2p.NetAddress, numAddresses)

	oldBucketToAddrsMap := make(map[int]map[string]struct{})
	var oldIndex int
	newBucketToAddrsMap := make(map[int]map[string]struct{})
	var newIndex int

	selectionIndex := 0
ADDRS_LOOP:
	for selectionIndex < numAddresses {
		pickFromOldBucket := int((float64(selectionIndex)/float64(numAddresses))*100) >= biasNewAddrsPercent
		pickFromOldBucket = (pickFromOldBucket && a.nOld > 0) || a.nNew == 0
		bucket := make(map[string]*_KnownAddress)

		// loop until we pick a random non-empty bucket
		for len(bucket) == 0 {
			if pickFromOldBucket {
				oldIndex = a.rng.Intn(len(a.bucketsOld))
				bucket = a.bucketsOld[oldIndex]
			} else {
				newIndex = a.rng.Intn(len(a.bucketsNew))
				bucket = a.bucketsNew[newIndex]
			}
		}

		// pick a random index
		randIndex := a.rng.Intn(len(bucket))

		// loop over the map to return that index
		var selectedAddr *p2p.NetAddress
		for _, ka := range bucket {
			if randIndex == 0 {
				selectedAddr = ka.Addr
				break
			}
			randIndex--
		}

		// if we have selected the address before, restart the loop
		// otherwise, record it and continue
		if pickFromOldBucket {
			if addrsMap, ok := oldBucketToAddrsMap[oldIndex]; ok {
				if _, ok = addrsMap[selectedAddr.String()]; ok {
					continue ADDRS_LOOP
				}
			} else {
				oldBucketToAddrsMap[oldIndex] = make(map[string]struct{})
			}
			oldBucketToAddrsMap[oldIndex][selectedAddr.String()] = struct{}{}
		} else {
			if addrsMap, ok := newBucketToAddrsMap[newIndex]; ok {
				if _, ok = addrsMap[selectedAddr.String()]; ok {
					continue ADDRS_LOOP
				}
			} else {
				newBucketToAddrsMap[newIndex] = make(map[string]struct{})
			}
			newBucketToAddrsMap[newIndex][selectedAddr.String()] = struct{}{}
		}

		selection[selectionIndex] = selectedAddr
		selectionIndex++
	}

	return selection
}

func (a *_AddrBook) CountOfKnownAddress() int {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	return len(a.addrLookup)
}

// IterateKnownAddresses iterates the new and old addresses.
func (a *_AddrBook) IterateKnownAddresses(cb func(*_KnownAddress)) {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	for _, addr := range a.addrLookup {
		cb(addr)
	}
}


// Size returns the number of addresses in the book.
func (a *_AddrBook) Size() int {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	return a.size()
}

func (a *_AddrBook) size() int {
	return a.nNew + a.nOld
}


// Save persists the address book to disk.
func (a *_AddrBook) Save() {
	a.saveToFile(a.filePath) // thread safe
}

func (a *_AddrBook) saveRoutine() {
	defer a.wg.Done()

	tiker := time.NewTicker(dumpAddressInterval)
out:
	for {
		select {
		case <-tiker.C:
			a.saveToFile(a.filePath)
		case <-a.C4Quit():
			break out
		}
	}
	tiker.Stop()
	a.saveToFile(a.filePath)
	a.Logger.Info("Address handler done")
}

//----------------------------------------------------------

func (a *_AddrBook) getBucket(bucketType byte, bucketIdx int) map[string]*_KnownAddress {
	switch bucketType {
	case _NEW_BUCKET:
		return a.bucketsNew[bucketIdx]
	case _OLD_BUCKET:
		return a.bucketsOld[bucketIdx]
	default:
		panic("Should not happen")
	}
	return nil
}

// Adds ka to new bucket. Returns false if it couldn't do it cuz buckets full.
// NOTE: currently it always returns true.
func (a *_AddrBook) addToNewBucket(ka *_KnownAddress, bucketIdx int) {
	// Sanity check
	if ka.isOld() {
		a.Logger.Error("Failed Sanity Check! Cant add old address to new bucket", "ka", ka, "bucket", bucketIdx)
		return
	}

	addrStr := ka.Addr.String()
	bucket := a.getBucket(_NEW_BUCKET, bucketIdx)

	// Already exists?
	if _, ok := bucket[addrStr]; ok {
		return
	}

	// Enforce max addresses.
	if len(bucket) > newBucketSize {
		a.Logger.Info("new bucket is full, expiring new")
		a.expireNew(bucketIdx)
	}

	// Add to bucket.
	bucket[addrStr] = ka
	// increment nNew if the peer doesnt already exist in a bucket
	if ka.addBucketRef(bucketIdx) == 1 {
		a.nNew++
	}

	// Add it to addrLookup
	a.addrLookup[ka.NodeId()] = ka
}

// Adds ka to old bucket. Returns false if it couldn't do it cuz buckets full.
func (a *_AddrBook) addToOldBucket(ka *_KnownAddress, bucketIdx int) bool {
	// Sanity check
	if ka.isNew() {
		a.Logger.Error(fmt.Sprintf("Cannot add new address to old bucket: %v", ka))
		return false
	}
	if len(ka.Buckets) != 0 {
		a.Logger.Error(fmt.Sprintf("Cannot add already old address to another old bucket: %v", ka))
		return false
	}

	addrStr := ka.Addr.String()
	bucket := a.getBucket(_OLD_BUCKET, bucketIdx)

	// Already exists?
	if _, ok := bucket[addrStr]; ok {
		return true
	}

	// Enforce max addresses.
	if len(bucket) > oldBucketSize {
		return false
	}

	// Add to bucket.
	bucket[addrStr] = ka
	if ka.addBucketRef(bucketIdx) == 1 {
		a.nOld++
	}

	// Ensure in addrLookup
	a.addrLookup[ka.NodeId()] = ka

	return true
}

func (a *_AddrBook) removeFromBucket(ka *_KnownAddress, bucketType byte, bucketIdx int) {
	if ka.BucketType != bucketType {
		a.Logger.Error(fmt.Sprintf("Bucket type mismatch: %v", ka))
		return
	}
	bucket := a.getBucket(bucketType, bucketIdx)
	delete(bucket, ka.Addr.String())
	if ka.removeBucketRef(bucketIdx) == 0 {
		if bucketType == _NEW_BUCKET {
			a.nNew--
		} else {
			a.nOld--
		}
		delete(a.addrLookup, ka.NodeId())
	}
}

func (a *_AddrBook) removeFromAllBuckets(ka *_KnownAddress) {
	for _, bucketIdx := range ka.Buckets {
		bucket := a.getBucket(ka.BucketType, bucketIdx)
		delete(bucket, ka.Addr.String())
	}
	ka.Buckets = nil
	if ka.BucketType == _NEW_BUCKET {
		a.nNew--
	} else {
		a.nOld--
	}
	delete(a.addrLookup, ka.NodeId())
}

//----------------------------------------------------------

func (a *_AddrBook) pickOldest(bucketType byte, bucketIdx int) *_KnownAddress {
	bucket := a.getBucket(bucketType, bucketIdx)
	var oldest *_KnownAddress
	for _, ka := range bucket {
		if oldest == nil || ka.LastAttempt.Before(oldest.LastAttempt) {
			oldest = ka
		}
	}
	return oldest
}

// adds the address to a "new" bucket. if its already in one,
// it only adds it probabilistically
func (a *_AddrBook) addAddress(addr, src *p2p.NetAddress) error {
	if addr == nil || src == nil {
		return ErrAddrBookNilAddr{addr, src}
	}

	if a.routabilityStrict && !addr.Routable() {
		return ErrAddrBookNonRoutable{addr}
	}
	// TODO: we should track ourAddrs by NodeId and by IP:PORT and refuse both.
	if _, ok := a.ourAddrs[addr.String()]; ok {
		return ErrAddrBookSelf{addr}
	}

	ka := a.addrLookup[addr.NodeId]
	if ka != nil {
		// If its already old and the addr is the same, ignore it.
		if ka.isOld() && ka.Addr.Equal(addr) {
			return nil
		}
		// Already in max new buckets.
		if len(ka.Buckets) == maxNewBucketsPerAddress {
			return nil
		}
		// The more entries we have, the less likely we are to add more.
		factor := int32(2 * len(ka.Buckets))
		if a.rng.Int31n(factor) != 0 {
			return nil
		}
	} else {
		ka = newKnownAddress(addr, src)
	}

	bucket := a.calcNewBucket(addr, src)
	a.addToNewBucket(ka, bucket)
	return nil
}

// Make space in the new buckets by expiring the really bad entries.
// If no bad entries are available we remove the oldest.
func (a *_AddrBook) expireNew(bucketIdx int) {
	for addrStr, ka := range a.bucketsNew[bucketIdx] {
		// If an entry is bad, throw it away
		if ka.isBad() {
			a.Logger.Info(fmt.Sprintf("expiring bad address %v", addrStr))
			a.removeFromBucket(ka, _NEW_BUCKET, bucketIdx)
			return
		}
	}

	// If we haven't thrown out a bad entry, throw out the oldest entry
	oldest := a.pickOldest(_NEW_BUCKET, bucketIdx)
	a.removeFromBucket(oldest, _NEW_BUCKET, bucketIdx)
}

// Promotes an address from new to old. If the destination bucket is full,
// demote the oldest one to a "new" bucket.
// TODO: Demote more probabilistically?
func (a *_AddrBook) moveToOld(ka *_KnownAddress) {
	// Sanity check
	if ka.isOld() {
		a.Logger.Error(fmt.Sprintf("Cannot promote address that is already old %v", ka))
		return
	}
	if len(ka.Buckets) == 0 {
		a.Logger.Error(fmt.Sprintf("Cannot promote address that isn't in any new buckets %v", ka))
		return
	}

	// Remove from all (new) buckets.
	a.removeFromAllBuckets(ka)
	// It's officially old now.
	ka.BucketType = _OLD_BUCKET

	// Try to add it to its oldBucket destination.
	oldBucketIdx := a.calcOldBucket(ka.Addr)
	added := a.addToOldBucket(ka, oldBucketIdx)
	if !added {
		// No room; move the oldest to a new bucket
		oldest := a.pickOldest(_OLD_BUCKET, oldBucketIdx)
		a.removeFromBucket(oldest, _OLD_BUCKET, oldBucketIdx)
		newBucketIdx := a.calcNewBucket(oldest.Addr, oldest.Src)
		a.addToNewBucket(oldest, newBucketIdx)

		// Finally, add our ka to old bucket again.
		added = a.addToOldBucket(ka, oldBucketIdx)
		if !added {
			a.Logger.Error(fmt.Sprintf("Could not re-add ka %v to oldBucketIdx %v", ka, oldBucketIdx))
		}
	}
}

//---------------------------------------------------------------------
// calculate bucket placements

// sum256(key + sourcegroup +
//                int64(sum256(key + group + sourcegroup))%bucket_per_group) % num_new_buckets
func (a *_AddrBook) calcNewBucket(addr, src *p2p.NetAddress) int {
	data1 := []byte{}
	data1 = append(data1, []byte(a.key)...)
	data1 = append(data1, []byte(a.groupKey(addr))...)
	data1 = append(data1, []byte(a.groupKey(src))...)
	hash1 := sum256(data1)
	hash64 := binary.BigEndian.Uint64(hash1)
	hash64 %= newBucketsPerGroup
	var hashbuf [8]byte
	binary.BigEndian.PutUint64(hashbuf[:], hash64)
	data2 := []byte{}
	data2 = append(data2, []byte(a.key)...)
	data2 = append(data2, a.groupKey(src)...)
	data2 = append(data2, hashbuf[:]...)

	hash2 := sum256(data2)
	return int(binary.BigEndian.Uint64(hash2) % newBucketCount)
}

// sum256(key + group +
//                int64(sum256(key + addr))%buckets_per_group) % num_old_buckets
func (a *_AddrBook) calcOldBucket(addr *p2p.NetAddress) int {
	data1 := []byte{}
	data1 = append(data1, []byte(a.key)...)
	data1 = append(data1, []byte(addr.String())...)
	hash1 := sum256(data1)
	hash64 := binary.BigEndian.Uint64(hash1)
	hash64 %= oldBucketsPerGroup
	var hashbuf [8]byte
	binary.BigEndian.PutUint64(hashbuf[:], hash64)
	data2 := []byte{}
	data2 = append(data2, []byte(a.key)...)
	data2 = append(data2, a.groupKey(addr)...)
	data2 = append(data2, hashbuf[:]...)

	hash2 := sum256(data2)
	return int(binary.BigEndian.Uint64(hash2) % oldBucketCount)
}

// Return a string representing the network group of this address.
// This is the /16 for IPv4, the /32 (/36 for he.net) for IPv6, the string
// "local" for a local address and the string "unroutable" for an unroutable
// address.
func (a *_AddrBook) groupKey(na *p2p.NetAddress) string {
	if a.routabilityStrict && na.Local() {
		return "local"
	}
	if a.routabilityStrict && !na.Routable() {
		return "unroutable"
	}

	if ipv4 := na.Ip.To4(); ipv4 != nil {
		return (&net.IPNet{IP: na.Ip, Mask: net.CIDRMask(16, 32)}).String()
	}
	if na.RFC6145() || na.RFC6052() {
		// last four bytes are the ip address
		ip := net.IP(na.Ip[12:16])
		return (&net.IPNet{IP: ip, Mask: net.CIDRMask(16, 32)}).String()
	}

	if na.RFC3964() {
		ip := net.IP(na.Ip[2:7])
		return (&net.IPNet{IP: ip, Mask: net.CIDRMask(16, 32)}).String()

	}
	if na.RFC4380() {
		// teredo tunnels have the last 4 bytes as the v4 address XOR
		// 0xff.
		ip := net.IP(make([]byte, 4))
		for i, byte := range na.Ip[12:16] {
			ip[i] = byte ^ 0xff
		}
		return (&net.IPNet{IP: ip, Mask: net.CIDRMask(16, 32)}).String()
	}

	// OK, so now we know ourselves to be a IPv6 address.
	// bitcoind uses /32 for everything, except for Hurricane Electric's
	// (he.net) IP range, which it uses /36 for.
	bits := 32
	heNet := &net.IPNet{IP: net.ParseIP("2001:470::"),
		Mask: net.CIDRMask(32, 128)}
	if heNet.Contains(na.Ip) {
		bits = 36
	}

	return (&net.IPNet{IP: na.Ip, Mask: net.CIDRMask(bits, 128)}).String()
}

func sum256(b []byte) []byte {
	hash := ec.Sum256(b)
	return hash[:]
}
