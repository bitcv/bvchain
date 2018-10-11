package pex

import (
	"bvchain/p2p"

	"time"
)

// _KnownAddress tracks information about a known network address
// that is used to determine how viable an address is.
type _KnownAddress struct {
	Addr        *p2p.NetAddress
	Src         *p2p.NetAddress
	Attempts    int32
	LastAttempt time.Time
	LastSuccess time.Time
	BucketType  byte
	Buckets     []int
}

func newKnownAddress(addr *p2p.NetAddress, src *p2p.NetAddress) *_KnownAddress {
	return &_KnownAddress{
		Addr:        addr,
		Src:         src,
		Attempts:    0,
		LastAttempt: time.Now(),
		BucketType:  _NEW_BUCKET,
		Buckets:     nil,
	}
}

func (ka *_KnownAddress) NodeId() p2p.NodeId {
	return ka.Addr.NodeId
}

func (ka *_KnownAddress) clone() *_KnownAddress {
	return &_KnownAddress{
		Addr:        ka.Addr,
		Src:         ka.Src,
		Attempts:    ka.Attempts,
		LastAttempt: ka.LastAttempt,
		LastSuccess: ka.LastSuccess,
		BucketType:  ka.BucketType,
		Buckets:     ka.Buckets,
	}
}

func (ka *_KnownAddress) isOld() bool {
	return ka.BucketType == _OLD_BUCKET
}

func (ka *_KnownAddress) isNew() bool {
	return ka.BucketType == _NEW_BUCKET
}

func (ka *_KnownAddress) markAttempt() {
	now := time.Now()
	ka.LastAttempt = now
	ka.Attempts++
}

func (ka *_KnownAddress) markGood() {
	now := time.Now()
	ka.LastAttempt = now
	ka.Attempts = 0
	ka.LastSuccess = now
}

func (ka *_KnownAddress) addBucketRef(bucketIdx int) int {
	for _, bucket := range ka.Buckets {
		if bucket == bucketIdx {
			// TODO refactor to return error?
			// log.Warn(Fmt("Bucket already exists in ka.Buckets: %v", ka))
			return -1
		}
	}
	ka.Buckets = append(ka.Buckets, bucketIdx)
	return len(ka.Buckets)
}

func (ka *_KnownAddress) removeBucketRef(bucketIdx int) int {
	buckets := []int{}
	for _, bucket := range ka.Buckets {
		if bucket != bucketIdx {
			buckets = append(buckets, bucket)
		}
	}
	if len(buckets) != len(ka.Buckets)-1 {
		// TODO refactor to return error?
		// log.Warn(Fmt("bucketIdx not found in ka.Buckets: %v", ka))
		return -1
	}
	ka.Buckets = buckets
	return len(ka.Buckets)
}

/*
   An address is bad if the address in question is a New address, has not been tried in the last
   minute, and meets one of the following criteria:

   1) It claims to be from the future
   2) It hasn't been seen in over a week
   3) It has failed at least three times and never succeeded
   4) It has failed ten times in the last week

   All addresses that meet these criteria are assumed to be worthless and not
   worth keeping hold of.

*/
func (ka *_KnownAddress) isBad() bool {
	// Is Old --> good
	if ka.BucketType == _OLD_BUCKET {
		return false
	}

	// Has been attempted in the last minute --> good
	if ka.LastAttempt.After(time.Now().Add(-1 * time.Minute)) {
		return false
	}

	// TODO: From the future?

	// Too old?
	// TODO: should be a timestamp of last seen, not just last attempt
	if ka.LastAttempt.Before(time.Now().Add(-1 * numMissingDays * time.Hour * 24)) {
		return true
	}

	// Never succeeded?
	if ka.LastSuccess.IsZero() && ka.Attempts >= numRetries {
		return true
	}

	// Hasn't succeeded in too long?
	if ka.LastSuccess.Before(time.Now().Add(-1*minBadDays*time.Hour*24)) &&
		ka.Attempts >= maxFailures {
		return true
	}

	return false
}
