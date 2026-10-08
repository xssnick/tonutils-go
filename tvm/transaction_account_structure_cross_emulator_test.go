//go:build cgo && tvm_cross_emulator

package tvm

import (
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTransactionExistingAccountStructureRejectionParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	badExtra := cell.NewDict(32)
	if err := badExtra.SetIntKey(big.NewInt(1), cell.BeginCell().MustStoreUInt(0, 5).EndCell()); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		shard *tlb.ShardAccount
	}{
		{
			name: "addr_var",
			shard: transactionAccountStructureShard(t, address.NewAddressVar(0, 0, 256, make([]byte, 32)), tlb.StorageUsed{
				CellsUsed: big.NewInt(0),
				BitsUsed:  big.NewInt(0),
			}, nil, tlb.AccountStatusUninit, nil),
		},
		{
			name: "padded storage usage",
			shard: &tlb.ShardAccount{
				Account:       transactionAccountWithPaddedVarUInteger("storage used"),
				LastTransHash: make([]byte, 32),
			},
		},
		{
			name: "invalid extra currency leaf",
			shard: transactionAccountStructureShard(t, tonopsTestAddr, tlb.StorageUsed{
				CellsUsed: big.NewInt(0),
				BitsUsed:  big.NewInt(0),
			}, badExtra, tlb.AccountStatusUninit, nil),
		},
	}
	msg, err := tlb.ToCell(&tlb.ExternalMessage{DstAddr: tonopsTestAddr})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := PrepareAccount(tc.shard, tonopsTestAddr); err == nil {
				t.Fatal("Go accepted malformed existing account")
			}
			if _, err := runReferenceOrdinaryTransaction(tc.shard, msg, uint32(tonopsTestTime.Unix()), uint64(transactionTestLogicalTime), tonopsTestSeed); err == nil {
				t.Fatal("reference accepted malformed existing account")
			}
		})
	}
}

func TestTransactionExistingAccountOpaqueLibraryAcceptanceParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	opaque := cell.BeginCell().
		MustStoreUInt(0, 2).
		MustStoreRef(cell.BeginCell().EndCell()).
		EndCell().AsDict(256)
	shard := transactionAccountStructureShard(t, tonopsTestAddr, tlb.StorageUsed{
		CellsUsed: big.NewInt(0),
		BitsUsed:  big.NewInt(0),
	}, nil, tlb.AccountStatusActive, &tlb.StateInit{Lib: opaque})
	if _, err := PrepareAccount(shard, tonopsTestAddr); err != nil {
		t.Fatalf("Go rejected opaque existing-account library: %v", err)
	}

	msg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runReferenceOrdinaryTransaction(shard, msg, uint32(tonopsTestTime.Unix()), uint64(transactionTestLogicalTime), tonopsTestSeed); err != nil {
		t.Fatalf("reference rejected opaque existing-account library: %v", err)
	}
}
