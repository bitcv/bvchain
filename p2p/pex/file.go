package pex

import (
	"bvchain/util"
	"bvchain/util/log"

	"encoding/json"
	"os"
)

/* Loading & Saving */

type _AddrBookJson struct {
	Key string
	Addrs []*_KnownAddress
}

func (a *_AddrBook) saveToFile(filePath string) {
	a.Logger.Info("Saving AddrBook to file", "size", a.Size())

	a.mtx.Lock()
	defer a.mtx.Unlock()

	addrs := []*_KnownAddress{}
	for _, ka := range a.addrLookup {
		addrs = append(addrs, ka)
	}

	ajson := &_AddrBookJson{
		Key:   a.key,
		Addrs: addrs,
	}

	jsonBytes, err := json.MarshalIndent(ajson, "", "\t")
	if err != nil {
		a.Logger.Error("Failed to save AddrBook to file", "err", err)
		return
	}
	err = util.WriteFileAtomic(filePath, jsonBytes, 0644)
	if err != nil {
		a.Logger.Error("Failed to save AddrBook to file", "file", filePath, "err", err)
	}
}

// Returns false if file does not exist.
// cmn.Panics if file is corrupt.
func (a *_AddrBook) loadFromFile(filePath string) bool {
	// If doesn't exist, do nothing.
	_, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return false
	}

	// Load _AddrBookJson{}
	r, err := os.Open(filePath)
	if err != nil {
		log.Error("Error opening file %s: %v", filePath, err)
		return false
	}
	defer r.Close() // nolint: errcheck
	ajson := &_AddrBookJson{}
	dec := json.NewDecoder(r)
	err = dec.Decode(ajson)
	if err != nil {
		log.Error("Error reading file %s: %v", filePath, err)
		return false
	}

	// Restore all the fields...
	// Restore the key
	a.key = ajson.Key
	// Restore .bucketsNew & .bucketsOld
	for _, ka := range ajson.Addrs {
		for _, bucketIndex := range ka.Buckets {
			bucket := a.getBucket(ka.BucketType, bucketIndex)
			bucket[ka.Addr.String()] = ka
		}
		a.addrLookup[ka.NodeId()] = ka
		if ka.BucketType == _NEW_BUCKET {
			a.nNew++
		} else {
			a.nOld++
		}
	}
	return true
}

