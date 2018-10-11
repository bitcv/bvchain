package db


type KvDb interface {
	Put(key []byte, value []byte) error
	Get(key []byte) ([]byte, error)
	Has(key []byte) (bool, error)
	Remove(key []byte) error
	Close()
	NewBatch() KvBatch
}

type KvBatch interface {
	Put(key []byte, value []byte) error
	Write() error	// Write to the backing db
	Reset()		// Reset resets the batch for reuse
	DataSize() int	// amount of data in the batch
}

