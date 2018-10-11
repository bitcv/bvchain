package pex

import "time"

// addresses under which the address manager will claim to need more addresses.
const needAddressThreshold = 1000

// interval used to dump the address cache to disk for future use.
const dumpAddressInterval = time.Minute * 2

// max addresses in each old address bucket.
const oldBucketSize = 64

// buckets we split old addresses over.
const oldBucketCount = 64

// max addresses in each new address bucket.
const newBucketSize = 64

// buckets that we spread new addresses over.
const newBucketCount = 256

// old buckets over which an address group will be spread.
const oldBucketsPerGroup = 4

// new buckets over which a source address group will be spread.
const newBucketsPerGroup = 32

// buckets a frequently seen new address may end up in.
const maxNewBucketsPerAddress = 4

// days before which we assume an address has vanished
// if we have not seen it announced in that long.
const numMissingDays = 7

// tries without a single success before we assume an address is bad.
const numRetries = 3

// max failures we will accept without a success before considering an address bad.
const maxFailures = 10 // ?

// days since the last success before we will consider evicting an address.
const minBadDays = 7

// % of total addresses known returned by GetSelection.
const _GET_SELECTION_PERCENT = 23

// min addresses that must be returned by GetSelection. Useful for bootstrapping.
const _MIN_GET_SELECTION = 32

// max addresses returned by GetSelection
const _MAX_GET_SELECTION = 250

