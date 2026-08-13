//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTVMCrossEmulatorTransactionMasterchainPublicLibraryRequiresMatchingKey(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	library := cell.BeginCell().MustStoreUInt(0xC0DE, 16).EndCell()
	wrongKey := append([]byte(nil), library.Hash()...)
	wrongKey[0] ^= 0x80
	libraries := cell.NewDict(256)
	if err := libraries.Set(
		cell.BeginCell().MustStoreSlice(wrongKey, 256).EndCell(),
		cell.BeginCell().MustStoreBoolBit(true).MustStoreRef(library).EndCell(),
	); err != nil {
		t.Fatalf("failed to build library dictionary: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	stateInit := &tlb.StateInit{
		Code: makeTransactionInternalSuccessCode(t, newData),
		Data: cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell(),
		Lib:  libraries,
	}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatalf("failed to serialize state init: %v", err)
	}
	accountAddr := address.NewAddress(0, 0xFF, stateCell.Hash())
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     accountAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		StateInit:   stateInit,
		Body:        cell.BeginCell().EndCell(),
	})
	shard := buildTransactionTestNoneShardAccount(t)
	baseConfigRoot := mustReferenceTransactionConfigRoot(t)

	for _, version := range []uint32{9, 14} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
			goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     accountAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}
			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(
				shard,
				msg,
				now,
				uint64(transactionTestLogicalTime),
				tonopsTestSeed,
				configRoot,
			)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			for _, result := range []struct {
				side   string
				txCell *cell.Cell
			}{
				{side: "reference", txCell: refRes.txCell},
				{side: "go", txCell: goRes.TransactionCell},
			} {
				var tx tlb.Transaction
				if err = tlb.LoadFromCell(&tx, result.txCell.MustBeginParse()); err != nil {
					t.Fatalf("failed to parse %s transaction: %v", result.side, err)
				}
				desc, ok := tx.Description.(tlb.TransactionDescriptionOrdinary)
				if !ok {
					t.Fatalf("%s transaction description = %T", result.side, tx.Description)
				}
				phase, ok := desc.ComputePhase.Phase.(tlb.ComputePhaseVM)
				if !ok || !phase.Success {
					t.Fatalf("%s compute phase = %+v, want successful VM execution", result.side, desc.ComputePhase)
				}
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}
		})
	}
}
