package state

import (
	"bvchain/ec"
	"bvchain/obj"
	"bvchain/trie"

	"fmt"
	"math/big"
	"bytes"
)

type _AccountState struct {
	address ec.Address
	account Account
	sdb *StateDb
	tr *trie.Trie

	dbErr error

	cachedStorage map[ec.Hash256]ec.Hash256
	dirtyStorage map[ec.Hash256]ec.Hash256

	dirtyCode bool
	suicided bool
	removed bool
}

type Account struct {
        Nonce uint64
	Vid int64	// WitnessId or VoterId
	Balance *big.Int
	StoreRoot []byte // merkle root of the storage trie                                                                   
	CodeHash []byte
}


func newAccountState(sdb *StateDb, address ec.Address, account *Account) *_AccountState {
	as := &_AccountState {
		address: address,
		account: *account,
		sdb: sdb,
		cachedStorage: make(map[ec.Hash256]ec.Hash256),
		dirtyStorage: make(map[ec.Hash256]ec.Hash256),
	}
	if as.account.Balance == nil {
		as.account.Balance = new(big.Int)
	}
	return as
}

func (as *_AccountState) empty() bool {
	a := &as.account
        return a.Nonce == 0 && a.Vid == 0 && a.Balance.Sign() == 0 && len(a.CodeHash) == 0
}

func (as *_AccountState) touch() {
	as.sdb.journal.Append(_TouchChange{
                addr: &as.address,
        })
}

func (as *_AccountState) Address() ec.Address {
        return as.address
}

func (as *_AccountState) Marshal() []byte {
	return as.account.Marshal()
}

func (as *_AccountState) Balance() *big.Int {
	return as.account.Balance
}

func (as *_AccountState) AddBalance(amount *big.Int) {
        if amount.Sign() == 0 {
                if as.empty() {
                        as.touch()
                }
                return
        }
        as.SetBalance(new(big.Int).Add(as.Balance(), amount))
}

func (as *_AccountState) SubBalance(amount *big.Int) {
        if amount.Sign() == 0 {
                return
        }
        as.SetBalance(new(big.Int).Sub(as.Balance(), amount))
}


func (as *_AccountState) SetBalance(amount *big.Int) {
        as.sdb.journal.Append(_BalanceChange{
                addr: &as.address,
                prev:    new(big.Int).Set(as.account.Balance),
        })
	as.setBalance(amount)
}

func (as *_AccountState) setBalance(amount *big.Int) {
	as.account.Balance = amount
}

func (as *_AccountState) SetVid(vid int64) {
	if as.account.Vid != vid {
		as.sdb.journal.Append(_VidChange{
			addr: &as.address,
			prev: as.account.Vid,
		})
		as.setVid(vid)
	}
}

func (as *_AccountState) setVid(vid int64) {
	as.account.Vid = vid
}

func (as *_AccountState) Nonce() uint64 {
	return as.account.Nonce
}

func (as *_AccountState) SetNonce(nonce uint64) {
        as.sdb.journal.Append(_NonceChange{
                addr: &as.address,
                prev: as.account.Nonce,
        })
        as.setNonce(nonce)
}

func (as *_AccountState) setNonce(nonce uint64) {
	as.account.Nonce = nonce
}

func (as *_AccountState) setError(err error) {
	as.dbErr = err
}

func (as *_AccountState) getTrie(cdb *trie.CacheDb) *trie.Trie {
	if as.tr == nil {
		var err error
		var hash ec.Hash256
		copy(hash[:], as.account.StoreRoot)
		as.tr, err = trie.NewWithCacheDb(hash, cdb)
		if err != nil {
			as.tr, _ = trie.NewWithCacheDb(ec.Hash256{}, cdb)
			as.setError(fmt.Errorf("can't create storage trie: %v", err))
		}
	}
	return as.tr
}

// GetState returns a value in account storage.
func (as *_AccountState) GetState(cdb *trie.CacheDb, key ec.Hash256) ec.Hash256 {
        value, exists := as.cachedStorage[key]
        if exists {
                return value
        }

        bz := as.getTrie(cdb).Get(key[:])

        if len(bz) > 0 {
		content := bz
                value = ec.BytesToHash256(content)
        }
        as.cachedStorage[key] = value
        return value
}

// SetState updates a value in account storage.
func (as *_AccountState) SetState(cdb *trie.CacheDb, key, value ec.Hash256) {
        as.sdb.journal.Append(_StorageChange{
                addr: &as.address,
                key: key,
                prevValue: as.GetState(cdb, key),
        })
        as.setState(key, value)
}

func (as *_AccountState) setState(key, value ec.Hash256) {
        as.cachedStorage[key] = value
        as.dirtyStorage[key] = value
}


func (a *Account) Marshal() []byte {
	var bf bytes.Buffer
	s := obj.NewSerializer(&bf)
	s.WriteUvarint(a.Nonce)
	s.WriteVarint(a.Vid)
	s.WriteBigInt(a.Balance)
	s.WriteVariableBytes(a.StoreRoot)
	s.WriteVariableBytes(a.CodeHash)
	return bf.Bytes()
}


func (a *Account) Unmarshal(bz []byte) error {
	bf := bytes.NewBuffer(bz)
	d := obj.NewDeserializer(bf)
	a.Nonce = d.ReadUvarint()
	a.Vid = d.ReadVarint()
	a.Balance = d.ReadBigInt()
	a.StoreRoot = d.ReadVariableBytes(32, nil)
	a.CodeHash = d.ReadVariableBytes(32, nil)
	if d.N < len(bz) {
		return fmt.Errorf("More data than expected when unmarshaling account data")
	}
	return d.Err
}

func UnmarshalAccount(bz []byte) (*Account, error) {
	a := &Account{}
	err := a.Unmarshal(bz)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (a *Account) equal(b *Account) bool {
	if a.Nonce != b.Nonce ||
		a.Vid != b.Vid ||
		!bytes.Equal(a.StoreRoot, b.StoreRoot) ||
		!bytes.Equal(a.CodeHash, b.CodeHash) {
		return false
	}

	if a.Balance == nil {
		return b.Balance == nil || b.Balance.Sign() == 0
	}

	if b.Balance == nil {
		return a.Balance.Sign() == 0
	}

	return a.Balance.Cmp(b.Balance) == 0
}

