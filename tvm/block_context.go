package tvm

import (
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

// BlockOptions carries the per-block execution inputs that cannot be derived
// from the blockchain config.
type BlockOptions struct {
	// Now is the block unix time. When zero, the current wall clock is used.
	Now uint32
	// BlockLT is the block logical time (c7[4]). When zero, it is derived per
	// transaction from the transaction start LT.
	BlockLT int64
	// RandSeed is the block-level random seed. Per-account seeds are derived
	// from it unless TransactionOptions.RandSeed overrides them.
	RandSeed []byte
	// PrevBlocks is the c7 previous-blocks tuple (c7[13]). An empty tuple maps
	// to a null c7 entry.
	PrevBlocks tuple.Tuple
	// GlobalID overrides config param 19 in the c7 unpacked config when
	// non-zero (useful for configs that predate the param).
	GlobalID int32
	// Libraries are block-level library collections available to every
	// transaction of the block (e.g. the masterchain libraries dict).
	Libraries []*cell.Cell
}

// BlockContext is the per-block execution context: the prepared config plus
// block-scoped inputs, with the c7 unpacked config tuple built once. It is
// immutable after construction and safe to share between concurrently
// executing account lanes.
type BlockContext struct {
	cfg        *PreparedBlockchainConfig
	now        uint32
	blockLT    int64
	randSeed   []byte
	prevBlocks tuple.Tuple
	libraries  []*cell.Cell
	// unpackedConfig is the prebuilt c7 unpacked config value: either a
	// tuple.Tuple or nil when no source params exist.
	unpackedConfig any
}

// NewBlockContext builds the immutable per-block execution context.
func (c *PreparedBlockchainConfig) NewBlockContext(opts BlockOptions) (*BlockContext, error) {
	if c == nil {
		return nil, errConfigRootRequired
	}

	now := opts.Now
	if now == 0 {
		now = uint32(time.Now().Unix())
	}

	randSeed := append([]byte(nil), opts.RandSeed...)
	if len(randSeed) == 0 {
		// the reference generates a fresh 256-bit block seed when none is set;
		// callers that need reproducible runs must pass RandSeed explicitly
		randSeed = make([]byte, 32)
		if _, err := rand.Read(randSeed); err != nil {
			return nil, fmt.Errorf("failed to generate block rand seed: %w", err)
		}
	}
	out := &BlockContext{
		cfg:        c,
		now:        now,
		blockLT:    opts.BlockLT,
		randSeed:   randSeed,
		prevBlocks: opts.PrevBlocks,
		libraries:  append([]*cell.Cell(nil), opts.Libraries...),
	}
	out.unpackedConfig = buildUnpackedConfig(c, now, opts.GlobalID)
	return out, nil
}

// Config returns the prepared per-epoch config this context was built from.
func (b *BlockContext) Config() *PreparedBlockchainConfig {
	return b.cfg
}

// Now returns the resolved block unix time.
func (b *BlockContext) Now() uint32 {
	return b.now
}

// BlockLT returns the configured block logical time, zero when derived per
// transaction.
func (b *BlockContext) BlockLT() int64 {
	return b.blockLT
}

// UnpackedConfig returns the prebuilt c7 unpacked config tuple and whether it
// is present.
func (b *BlockContext) UnpackedConfig() (tuple.Tuple, bool) {
	t, ok := b.unpackedConfig.(tuple.Tuple)
	return t, ok
}

// buildUnpackedConfig assembles the c7 unpacked config tuple (global version 6+)
// from prepared param roots without any config dictionary access.
func buildUnpackedConfig(cfg *PreparedBlockchainConfig, now uint32, globalIDOverride int32) any {
	values := make([]any, 7)
	if prices := cfg.currentStoragePricesSlice(now); prices != nil {
		values[0] = prices
	}
	for i, param := range cfg.unpackedParams {
		values[i+1] = unpackedConfigParamSlice(param)
	}
	if globalIDOverride != 0 {
		values[1] = cell.BeginCell().MustStoreUInt(uint64(uint32(globalIDOverride)), 32).ToSlice()
	}

	for _, value := range values {
		if value != nil {
			return tuple.NewTupleOwned(values)
		}
	}
	return nil
}

func unpackedConfigParamSlice(param *cell.Cell) any {
	if param == nil {
		return nil
	}
	sl, err := param.BeginParse()
	if err != nil {
		return nil
	}
	return sl
}

func (b *BlockContext) prevBlocksValue() any {
	if b.prevBlocks.Len() == 0 {
		return nil
	}
	return b.prevBlocks
}

// AccountRandSeed derives the per-account random seed for this block:
// sha256(block rand seed || rewritten account address), using the pre-v8
// layout quirk when the config global version requires it. It returns nil when
// the block has no rand seed. Lanes can compute it once per account and pass
// it through TransactionOptions.RandSeed.
func (b *BlockContext) AccountRandSeed(accountAddr *address.Address) ([]byte, error) {
	return accountRandSeedBytes(b.randSeed, accountAddr, b.cfg.version)
}

// AccountRandSeed derives the per-account random seed from a block-level seed:
// sha256(block rand seed || rewritten account address). This is the layout
// used since global version 8; for earlier versions use
// BlockContext.AccountRandSeed which resolves the version automatically.
func AccountRandSeed(blockRandSeed []byte, accountAddr *address.Address) ([]byte, error) {
	if len(blockRandSeed) == 0 {
		return nil, errors.New("block rand seed is empty")
	}
	return accountRandSeedBytes(blockRandSeed, accountAddr, 8)
}
