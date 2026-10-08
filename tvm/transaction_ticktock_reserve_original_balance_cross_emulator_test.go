//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
)

// Below global version 9, tick/tock transactions use the account balance
// minus fees already collected as the RAWRESERVE mode&4 base, just like
// ordinary transactions. This witness keeps all fees at zero so a missing
// base turns the otherwise valid reserve action into result code 34.
func TestTVMCrossEmulatorTickTockReserveOriginalBalanceVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	configAddrCell, err := tlb.ToCell(&tlb.ConfigParamAddress{Address: tickTockTestAddr.Data()})
	if err != nil {
		t.Fatalf("failed to build special account config: %v", err)
	}
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	code := makeTickTockReserveOnlyCode(t, 0, 4, newData)

	for _, version := range []uint32{0, 4, 8, 9} {
		version := version
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
			configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
				int32(tlb.ConfigParamConfigAddress): configAddrCell,
			})

			for _, isTock := range []bool{false, true} {
				isTock := isTock
				name := "tick"
				if isTock {
					name = "tock"
				}
				t.Run(name, func(t *testing.T) {
					shard, err := buildTickTockShardAccountForTest(t, tickTockTestAddr, code, origData, tickTockTestBalance)
					if err != nil {
						t.Fatalf("failed to build tick/tock shard: %v", err)
					}

					goRes, err := testEmulateTickTockTransaction(NewTVM(), shard, isTock, testTxParams{
						Now:        now,
						RandSeed:   append([]byte(nil), tonopsTestSeed...),
						ConfigRoot: configRoot,
					})
					if err != nil {
						t.Fatalf("go tick/tock emulation failed: %v", err)
					}
					refRes, err := runReferenceTickTockWithConfigRoot(code, origData, tickTockTestAddr, isTock, now, tickTockTestBalance, tonopsTestSeed, configRoot)
					if err != nil {
						t.Fatalf("reference tick/tock emulation failed: %v", err)
					}

					assertTickTockActionSucceeded(t, "go", goRes.TransactionCell)
					assertTickTockActionSucceeded(t, "reference", refRes.txCell)
					if goRes.Accepted != refRes.accepted || goRes.GasUsed != refRes.gasUsed {
						t.Fatalf("execution result mismatch: go accepted/gas=%t/%d reference=%t/%d", goRes.Accepted, goRes.GasUsed, refRes.accepted, refRes.gasUsed)
					}
					if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
						t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
					}
					if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
						t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
					}
				})
			}
		})
	}
}

func makeTickTockReserveOnlyCode(t *testing.T, amount uint64, mode uint8, newData *cell.Cell) *cell.Cell {
	t.Helper()

	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHINT(new(big.Int).SetUint64(amount)).Serialize(),
		stackop.PUSHINT(new(big.Int).SetUint64(uint64(mode))).Serialize(),
		funcsop.RAWRESERVE().Serialize(),
		stackop.PUSHREF(newData).Serialize(),
		execop.POPCTR(4).Serialize(),
	)
}

func assertTickTockActionSucceeded(t *testing.T, side string, txCell *cell.Cell) {
	t.Helper()

	var tx tlb.Transaction
	if err := tlb.LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
		t.Fatalf("failed to decode %s transaction: %v", side, err)
	}
	desc, ok := tx.Description.(tlb.TransactionDescriptionTickTock)
	if !ok || desc.ActionPhase == nil {
		t.Fatalf("%s transaction description = %T with action phase %v", side, tx.Description, desc.ActionPhase)
	}
	if !desc.ActionPhase.Success || !desc.ActionPhase.Valid || desc.ActionPhase.ResultCode != 0 {
		t.Fatalf("%s action phase: success=%t valid=%t result=%d, want true/true/0", side, desc.ActionPhase.Success, desc.ActionPhase.Valid, desc.ActionPhase.ResultCode)
	}
}
