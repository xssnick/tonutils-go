package tvm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

// testTxParams mirrors the legacy flat transaction-emulation config shape for
// tests: block-level fields feed BlockContext, per-transaction fields feed
// TransactionOptions.
type testTxParams struct {
	Address *address.Address
	Now     uint32
	BlockLT int64
	// LogicalTime is the per-transaction minimal LT.
	LogicalTime int64
	// RandSeed is the block-level seed; the per-account c7 seed is derived
	// from it and the account address.
	RandSeed []byte
	// AccountRandSeed is the raw per-account c7 seed (overrides derivation).
	AccountRandSeed []byte
	// ConfigRoot is prepared on the fly; set Config to reuse a prepared one.
	ConfigRoot          *cell.Cell
	Config              *PreparedConfig
	PrevBlocks          tuple.Tuple
	GlobalID            int32
	Libraries           []*cell.Cell
	Gas                 vm.Gas
	ChksigAlwaysSucceed bool
	BuildProof          bool
	AccountStorageStat  *cell.Cell
	TraceHook           vm.TraceHook
}

func (p testTxParams) blockContext() (*BlockContext, error) {
	cfg := p.Config
	if cfg == nil {
		var err error
		cfg, err = PrepareConfig(p.ConfigRoot)
		if err != nil {
			return nil, err
		}
	}
	return cfg.NewBlockContext(BlockOptions{
		Now:        p.Now,
		BlockLT:    p.BlockLT,
		RandSeed:   p.RandSeed,
		PrevBlocks: p.PrevBlocks,
		GlobalID:   p.GlobalID,
		Libraries:  p.Libraries,
	})
}

func (p testTxParams) txOptions() TransactionOptions {
	return TransactionOptions{
		LogicalTime:         p.LogicalTime,
		RandSeed:            p.AccountRandSeed,
		Gas:                 p.Gas,
		AccountStorageStat:  p.AccountStorageStat,
		BuildProof:          p.BuildProof,
		ChksigAlwaysSucceed: p.ChksigAlwaysSucceed,
		TraceHook:           p.TraceHook,
	}
}

func testEmulateTransaction(machine *TVM, shard *tlb.ShardAccount, msgCell *cell.Cell, p testTxParams) (*TransactionExecutionResult, error) {
	block, err := p.blockContext()
	if err != nil {
		return nil, err
	}
	acc, err := PrepareAccount(shard, p.Address)
	if err != nil {
		return nil, err
	}
	msg, err := PrepareMessage(msgCell)
	if err != nil {
		return nil, err
	}
	return machine.EmulateTransaction(block, acc, msg, p.txOptions())
}

func testEmulateTickTockTransaction(machine *TVM, shard *tlb.ShardAccount, isTock bool, p testTxParams) (*TransactionExecutionResult, error) {
	block, err := p.blockContext()
	if err != nil {
		return nil, err
	}
	acc, err := PrepareAccount(shard, p.Address)
	if err != nil {
		return nil, err
	}
	return machine.EmulateTickTockTransaction(block, acc, isTock, p.txOptions())
}

func testCheckExternalMessageAccepted(machine *TVM, shard *tlb.ShardAccount, msgCell *cell.Cell, p testTxParams) (bool, error) {
	block, err := p.blockContext()
	if err != nil {
		return false, err
	}
	acc, err := PrepareAccount(shard, p.Address)
	if err != nil {
		return false, err
	}
	msg, err := PrepareMessage(msgCell)
	if err != nil {
		return false, err
	}
	return machine.CheckExternalMessageAccepted(block, acc, msg, p.txOptions())
}

// testResultShardAccount returns the post-transaction shard account.
func testResultShardAccount(res *TransactionExecutionResult) *tlb.ShardAccount {
	if res == nil || res.NextAccount == nil {
		return nil
	}
	return res.NextAccount.ShardAccount()
}

// testResultAccountState returns the parsed post-transaction account state.
func testResultAccountState(res *TransactionExecutionResult) *tlb.AccountState {
	if res == nil || res.NextAccount == nil {
		return nil
	}
	return res.NextAccount.State()
}

// emptyPreparedTestConfig mirrors the legacy zero-value internal transaction
// config used by unit tests: default size limits, no prices, global version 0.
func emptyPreparedTestConfig() *PreparedConfig {
	return &PreparedConfig{sizeLimits: transactionDefaultSizeLimits()}
}

func testCheckExternalAccepted(machine *TVM, shard *tlb.ShardAccount, account *tlb.AccountState, msgCell *cell.Cell, msg *tlb.ExternalMessage, p testTxParams) (bool, error) {
	block, err := p.blockContext()
	if err != nil {
		return false, err
	}
	acc, err := PrepareParsedAccount(shard, account, msg.DstAddr)
	if err != nil {
		return false, err
	}
	pm, err := PrepareParsedMessage(msgCell, &tlb.Message{MsgType: tlb.MsgTypeExternalIn, Msg: msg})
	if err != nil {
		return false, err
	}
	return machine.CheckExternalMessageAccepted(block, acc, pm, p.txOptions())
}

// testSyntheticConfig wraps an arbitrary cell as the c7 config root without
// deriving anything from it (legacy raw-root behavior for c7 assertions).
func testSyntheticConfig(root *cell.Cell) *PreparedConfig {
	return &PreparedConfig{root: root, sizeLimits: transactionDefaultSizeLimits()}
}

func mustGlobalVersionCell(t testing.TB, version uint32) *cell.Cell {
	t.Helper()

	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: version})
	if err != nil {
		t.Fatalf("failed to build global version cell: %v", err)
	}
	return versionCell
}

func transactionEmulationSeedForTest(blockSeed []byte, addr *address.Address, globalVersion uint32) (*big.Int, error) {
	seed, err := accountRandSeedBytes(blockSeed, addr, globalVersion)
	if err != nil {
		return nil, err
	}
	if len(seed) == 0 {
		return big.NewInt(0), nil
	}
	return new(big.Int).SetBytes(seed), nil
}

// mustPrepareConfigOrNil prepares the config when a root is present; nil roots
// map to a nil config (the entry points then fail with errConfigRootRequired).
func mustPrepareConfigOrNil(root *cell.Cell) *PreparedConfig {
	if root == nil {
		return nil
	}
	return MustPrepareConfig(root)
}

// testResultTransaction parses the built transaction of a result, failing the
// test on malformed cells.
func testResultTransaction(t testing.TB, res *TransactionExecutionResult) *tlb.Transaction {
	t.Helper()
	tx, err := res.ParseTransaction()
	if err != nil {
		t.Fatalf("failed to parse result transaction: %v", err)
	}
	return tx
}
