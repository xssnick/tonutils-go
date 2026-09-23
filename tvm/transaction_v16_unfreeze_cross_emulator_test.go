//go:build cgo && tvm_cross_emulator

package tvm

import (
	"fmt"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
)

func TestTVMCrossEmulatorTransactionV16FrozenPrefixBoundaries(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	// TON 5af24f7479756278d9b50923feb9da676e642f84 transaction.cpp:1873-1904
	// exempts frozen accounts from the prefix check starting at version 16.
	// Stored-state hashes and deployment prefix limits still apply. Prefix 31
	// is valid in StateInit's five-bit field, despite being invalid for anycast.
	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	code := codeFromBuilders(t, funcsop.ACCEPT().Serialize(),
		stackop.PUSHREF(newData).Serialize(), execop.POPCTR(4).Serialize())
	for _, version := range []uint32{15, 16} {
		configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
		for _, depth := range []uint64{9, 31} {
			stateInit := &tlb.StateInit{
				Depth: &depth,
				Code:  code,
				Data:  cell.BeginCell().EndCell(),
			}
			stateCell, err := tlb.ToCell(stateInit)
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name         string
				status       tlb.AccountStatus
				external     bool
				hashMismatch bool
			}{
				{name: "frozen_internal", status: tlb.AccountStatusFrozen},
				{name: "frozen_external", status: tlb.AccountStatusFrozen, external: true},
				{name: "frozen_hash_mismatch", status: tlb.AccountStatusFrozen, hashMismatch: true},
				{name: "uninit", status: tlb.AccountStatusUninit},
				{name: "nonexist", status: tlb.AccountStatusNonExist},
			} {
				// A rejected external message has no transaction to compare.
				if tc.external && version < 16 {
					continue
				}
				t.Run(fmt.Sprintf("v%d/depth=%d/%s", version, depth, tc.name), func(t *testing.T) {
					addrData := append([]byte(nil), stateCell.Hash()...)
					if tc.status == tlb.AccountStatusFrozen {
						// Unfreezing must compare against the saved state hash, even
						// when the account ID differs outside the fixed prefix.
						addrData[len(addrData)-1] ^= 1
					}
					addr := address.NewAddress(0, 0, addrData)
					stateHash := append([]byte(nil), stateCell.Hash()...)
					if tc.hashMismatch {
						stateHash[0] ^= 1
					}
					shard := buildTransactionTestNoneShardAccount(t)
					if tc.status != tlb.AccountStatusNonExist {
						shard = buildTransactionTestStoredShardAccount(t, addr, tc.status, walletSendTestBalance,
							tlb.StorageInfo{LastPaid: now}, nil, stateHash)
					}
					var msgCell *cell.Cell
					if tc.external {
						msgCell = mustTransactionMsgCell(t, &tlb.ExternalMessage{
							DstAddr: addr, StateInit: stateInit, Body: cell.BeginCell().EndCell(),
						})
					} else {
						msgCell = mustTransactionMsgCell(t, &tlb.InternalMessage{
							IHRDisabled: true, SrcAddr: internalEmulationSrcAddr, DstAddr: addr,
							Amount: tlb.FromNanoTONU(1_000_000_000), StateInit: stateInit, Body: cell.BeginCell().EndCell(),
						})
					}
					result, err := testEmulateTransaction(NewTVM(), shard, msgCell, testTxParams{
						Address: addr, Now: now, BlockLT: transactionTestLogicalTime,
						LogicalTime: transactionTestLogicalTime, RandSeed: tonopsTestSeed, ConfigRoot: configRoot,
					})
					if err != nil {
						t.Fatal(err)
					}
					reference, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msgCell, now,
						uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
					if err != nil {
						t.Fatal(err)
					}
					wantActive := version >= 16 && tc.status == tlb.AccountStatusFrozen && !tc.hashMismatch
					if wantActive {
						phase, ok := mustTransactionComputePhase(t, result.TransactionCell).Phase.(tlb.ComputePhaseVM)
						if !ok || !phase.Success {
							t.Fatalf("unfreeze compute phase=%+v, want successful VM execution", phase)
						}
						reloaded, err := PrepareAccount(result.NextAccount.ShardAccount(), addr)
						if err != nil {
							t.Fatal(err)
						}
						state := reloaded.State()
						if state.Status != tlb.AccountStatusActive || state.StateInit.Depth == nil || *state.StateInit.Depth != depth ||
							state.StateInit.Code.HashKey() != code.HashKey() || state.StateInit.Data.HashKey() != newData.HashKey() {
							t.Fatalf("unfrozen account lost its prefix or VM state: %+v", state)
						}
					} else {
						assertOrdinaryTransactionComputeSkipped(t, "go", result.TransactionCell, tlb.ComputeSkipReasonBadState)
						assertOrdinaryTransactionComputeSkipped(t, "reference", reference.txCell, tlb.ComputeSkipReasonBadState)
					}
					if result.TransactionCell.HashKey() != reference.txCell.HashKey() {
						t.Fatalf("transaction hash mismatch: go=%x reference=%x", result.TransactionCell.Hash(), reference.txCell.Hash())
					}
					if result.NextAccount.ShardAccountCell().HashKey() != reference.shardCell.HashKey() {
						t.Fatalf("account hash mismatch: go=%x reference=%x", result.NextAccount.ShardAccountCell().Hash(), reference.shardCell.Hash())
					}
				})
			}
		}
	}
}
