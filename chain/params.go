package chain

import (
	"fmt"
)

const MAX_BLOCK_SIZE = 1024*1024*16
const MAX_WITNESS_COUNT = 509
const MIN_WITNESS_COUNT = 7

// ChainParams contains consensus critical parameters
// that determine the validity of blocks.
type ChainParams struct {
	BlockInterval int
	MaintenanceInterval int
	MaintenanceSkipSlots int

	MaxWitnessCount int
	MinWitnessCount int

	MaxBlockSize int
	MaxTrxSize int

	MaxBlockTrxs int
	MaxBlockGas int64
	MaxTrxGas int64
}

// DefaultChainParams returns a default ChainParams.
func DefaultChainParams() *ChainParams {
	return &ChainParams{
		BlockInterval: 5,
		MaintenanceInterval: 3600,
		MaintenanceSkipSlots: 3,
		MaxWitnessCount: MAX_WITNESS_COUNT,
		MinWitnessCount: MIN_WITNESS_COUNT,

		MaxBlockSize: 1024*1024,
		MaxBlockTrxs: 1024,
		MaxBlockGas: -1,
		MaxTrxSize: 1024*4,
		MaxTrxGas: -1,
	}
}

// Validate validates the ChainParams to ensure all values
// are within their allowed limits, and returns an error if they are not.
func (pms *ChainParams) Validate() error {
	if pms.MaxBlockSize <= 0 {
		return fmt.Errorf("MaxBlockSize must be greater than 0. Got %d", pms.MaxBlockSize)
	}
	if pms.MaxBlockSize > MAX_BLOCK_SIZE {
		return fmt.Errorf("MaxBlockSize (%d) is too big. Must not be greater than %d",
				pms.MaxBlockSize, MAX_BLOCK_SIZE)
	}
	return nil
}

func (pms *ChainParams) Clone() *ChainParams {
	pms2 := *pms
	return &pms2
}

