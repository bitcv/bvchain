package merkle

import (
	"bvchain/ec"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

type strHasher string

func (str strHasher) Hash() []byte {
	hash := ec.Sum256([]byte(str))
	return hash[:]
}

func TestSimpleMap(t *testing.T) {
	{
		db := newSimpleMap()
		db.Set("key1", strHasher("value1"))
		assert.Equal(t, "b12816abf8d08ea0116d3949d822719cd580b4f9cbad3f1a02065496dbc1686b", fmt.Sprintf("%x", db.Hash()), "Hash didn't match")
	}
	{
		db := newSimpleMap()
		db.Set("key1", strHasher("value2"))
		assert.Equal(t, "7a1da1ec783faaacb4d734b0851e4ffd008f51bda159ed2ef8fb8684da5b280b", fmt.Sprintf("%x", db.Hash()), "Hash didn't match")
	}
	{
		db := newSimpleMap()
		db.Set("key1", strHasher("value1"))
		db.Set("key2", strHasher("value2"))
		assert.Equal(t, "581d32df3c7ed832953c7ef3ee32e879b2c1bd7a4a6e75a8a7cf674bd1c1e9a3", fmt.Sprintf("%x", db.Hash()), "Hash didn't match")
	}
	{
		db := newSimpleMap()
		db.Set("key2", strHasher("value2")) // NOTE: out of order
		db.Set("key1", strHasher("value1"))
		assert.Equal(t, "581d32df3c7ed832953c7ef3ee32e879b2c1bd7a4a6e75a8a7cf674bd1c1e9a3", fmt.Sprintf("%x", db.Hash()), "Hash didn't match")
	}
	{
		db := newSimpleMap()
		db.Set("key1", strHasher("value1"))
		db.Set("key2", strHasher("value2"))
		db.Set("key3", strHasher("value3"))
		assert.Equal(t, "2883add6b2050249f86dbc679dca1f2e889188031a06bd23ba9083b97aac4201", fmt.Sprintf("%x", db.Hash()), "Hash didn't match")
	}
	{
		db := newSimpleMap()
		db.Set("key2", strHasher("value2")) // NOTE: out of order
		db.Set("key1", strHasher("value1"))
		db.Set("key3", strHasher("value3"))
		assert.Equal(t, "2883add6b2050249f86dbc679dca1f2e889188031a06bd23ba9083b97aac4201", fmt.Sprintf("%x", db.Hash()), "Hash didn't match")
	}
}
