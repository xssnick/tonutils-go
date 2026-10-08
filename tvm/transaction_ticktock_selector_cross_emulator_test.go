//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTVMCrossEmulatorTickTockExplicitSelectorIgnoresStoredFlags(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	configRoot := mustReferenceTransactionConfigRoot(t)
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	tickData := cell.BeginCell().MustStoreUInt(0x1111, 16).EndCell()
	tockData := cell.BeginCell().MustStoreUInt(0x2222, 16).EndCell()
	code := makeTickTockStateOnlyCode(t, tickData, tockData)
	storage := tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now,
	}

	activeShard := func(flags *tlb.TickTock) *tlb.ShardAccount {
		return buildTransactionTestStoredShardAccount(
			t,
			tickTockTestAddr,
			tlb.AccountStatusActive,
			tickTockTestBalance,
			storage,
			&tlb.StateInit{TickTock: flags, Code: code, Data: origData},
			nil,
		)
	}

	tests := []struct {
		name         string
		shard        *tlb.ShardAccount
		isTock       bool
		wantAccepted bool
	}{
		{name: "tick_without_special", shard: activeShard(nil), wantAccepted: true},
		{name: "tick_when_only_tock_is_stored", shard: activeShard(&tlb.TickTock{Tock: true}), wantAccepted: true},
		{name: "tock_when_only_tick_is_stored", shard: activeShard(&tlb.TickTock{Tick: true}), isTock: true, wantAccepted: true},
		{
			name: "frozen_tick_skips_compute",
			shard: buildTransactionTestFrozenShardAccount(
				t,
				tickTockTestAddr,
				bytes.Repeat([]byte{0x77}, 32),
				tickTockTestBalance,
				storage,
			),
		},
		{
			name:  "uninit_tick_skips_compute",
			shard: buildTransactionTestUninitShardAccount(t, tickTockTestAddr, tickTockTestBalance, storage),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			refRes, err := runReferenceTickTockShardAccountWithConfigRoot(test.shard, test.isTock, now, tonopsTestSeed, configRoot)
			if err != nil {
				t.Fatalf("reference tick/tock emulation failed: %v", err)
			}
			if refRes.accepted != test.wantAccepted {
				t.Fatalf("reference accepted=%t, want %t", refRes.accepted, test.wantAccepted)
			}

			goRes, err := testEmulateTickTockTransaction(NewTVM(), test.shard, test.isTock, testTxParams{
				Now:        now,
				RandSeed:   append([]byte(nil), tonopsTestSeed...),
				ConfigRoot: configRoot,
			})
			if err != nil {
				t.Fatalf("go tick/tock emulation failed: %v", err)
			}
			if goRes.Accepted != refRes.accepted || goRes.GasUsed != refRes.gasUsed {
				t.Fatalf("execution mismatch: go accepted/gas=%t/%d reference=%t/%d", goRes.Accepted, goRes.GasUsed, refRes.accepted, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}
		})
	}

	t.Run("account_none_is_rejected_by_host_boundary", func(t *testing.T) {
		shard := buildTransactionTestNoneShardAccount(t)
		if _, err := runReferenceTickTockShardAccountWithConfigRoot(shard, false, now, tonopsTestSeed, configRoot); err == nil {
			t.Fatal("reference accepted account_none")
		}
		if _, err := testEmulateTickTockTransaction(NewTVM(), shard, false, testTxParams{
			Address:    tickTockTestAddr,
			Now:        now,
			RandSeed:   append([]byte(nil), tonopsTestSeed...),
			ConfigRoot: configRoot,
		}); err == nil {
			t.Fatal("Go accepted account_none")
		}
	})
}
