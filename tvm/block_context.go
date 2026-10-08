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
	// BlockLT is the legacy signed block logical time (c7[4]). When zero, it is
	// derived per transaction; negative values remain signed for compatibility.
	BlockLT int64
	// BlockLTUint64 is the full-width block logical time. When non-zero, it
	// overrides BlockLT; zero leaves BlockLT (including a negative value) in
	// effect. Use it for protocol values above MaxInt64.
	BlockLTUint64 uint64
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
	// ConfigAddress is the actual configuration contract from the masterchain
	// state's ConfigParams.config_addr. Parameter 0 may be absent or name a
	// proposed replacement that was not installed. Nil retains parameter-0
	// inference for callers that only have the config dictionary.
	ConfigAddress *[32]byte
}

// BlockContext is the per-block execution context: the prepared config plus
// block-scoped inputs, with the c7 unpacked config tuple built once. It is
// immutable after construction and safe to share between concurrently
// executing account lanes.
type BlockContext struct {
	cfg           *PreparedBlockchainConfig
	now           uint32
	blockLT       int64
	blockLTU64    uint64
	randSeed      []byte
	prevBlocks    tuple.Tuple
	libraries     []*cell.Cell
	configAddr    preparedAddr256
	hasConfigAddr bool
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
		blockLTU64: opts.BlockLTUint64,
		randSeed:   randSeed,
		prevBlocks: opts.PrevBlocks,
		libraries:  append([]*cell.Cell(nil), opts.Libraries...),
	}
	if opts.ConfigAddress != nil {
		out.configAddr = preparedAddr256(*opts.ConfigAddress)
		out.hasConfigAddr = true
	} else if c.configAddr != nil {
		out.configAddr = *c.configAddr
		out.hasConfigAddr = true
	}
	out.unpackedConfig = buildUnpackedConfig(c, now, opts.GlobalID)
	return out, nil
}

func (b *BlockContext) isSpecialAccount(addr *address.Address) bool {
	if !transactionIsMasterchain(addr) {
		return false
	}
	data := addr.Data()
	if len(data) != 32 {
		return false
	}
	key := preparedAddr256(data)
	_, fundamental := b.cfg.fundamentalAccounts[key]
	return fundamental || b.hasConfigAddr && key == b.configAddr
}

// Config returns the prepared per-epoch config this context was built from.
func (b *BlockContext) Config() *PreparedBlockchainConfig {
	return b.cfg
}

// BindAccountStorageStat validates an account storage-stat proof against the
// hash committed by account and binds it to subsequent transaction execution.
// It mirrors block::Account::init_account_storage_stat in the reference node.
func (b *BlockContext) BindAccountStorageStat(account *PreparedAccount, root *cell.Cell) error {
	return transactionBindAccountStorageStat(&account.runtime, root, b.cfg)
}

// Now returns the resolved block unix time.
func (b *BlockContext) Now() uint32 {
	return b.now
}

// BlockLT returns the legacy signed block logical time option. Use
// BlockLTUint64 when the full-width option may have been supplied.
func (b *BlockContext) BlockLT() int64 {
	return b.blockLT
}

// BlockLTUint64 returns the configured full-width override, or zero when none
// was supplied.
func (b *BlockContext) BlockLTUint64() uint64 {
	return b.blockLTU64
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

	// The unpacked config is always a 7-element tuple, even when every slot
	// is empty.
	return tuple.NewTupleOwned(values)
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
