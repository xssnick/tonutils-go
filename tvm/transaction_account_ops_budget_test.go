package tvm

import (
	"math/big"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// The reference validates a candidate account with
// block::tlb::t_ShardAccount.validate_csr(10000, new_value)
// (validator/impl/validate-query.cpp:3085), where one operation buys entry into
// one cell (crypto/tl/tlblib.cpp:128-147). The ^Account reference spends the
// first, the extra-currency hashmap spends one per node, and a hashmap with L
// entries has exactly 2L-1 nodes, so the reference accepts up to 5000 extra
// currencies and refuses 5001.
func TestPrepareAccountMatchesValidateCsrExtraCurrencyBudget(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries int
		reject  bool
	}{
		{name: "at the budget", entries: 5000},
		{name: "one entry over the budget", entries: 5001, reject: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			extra := cell.NewDict(32)
			for i := 0; i < tc.entries; i++ {
				value := cell.BeginCell().MustStoreUInt(1, 5).MustStoreUInt(1, 8).EndCell()
				if err := extra.SetIntKey(big.NewInt(int64(i)), value); err != nil {
					t.Fatal(err)
				}
			}
			shard := transactionAccountStructureShard(t, tonopsTestAddr, tlb.StorageUsed{
				CellsUsed: big.NewInt(0),
				BitsUsed:  big.NewInt(0),
			}, extra, tlb.AccountStatusUninit, nil)

			_, err := PrepareAccount(shard, tonopsTestAddr)
			if !tc.reject {
				if err != nil {
					t.Fatalf("account with %d extra currencies was rejected: %v", tc.entries, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("account with %d extra currencies was accepted", tc.entries)
			}
			if !strings.Contains(err.Error(), "validation budget") {
				t.Fatalf("account with %d extra currencies was rejected for the wrong reason: %v", tc.entries, err)
			}
		})
	}
}

// A hashmap is a tree only by its keys: nothing stops both branches of a fork
// from pointing at the same cell, so a well-formed extra-currency dictionary of
// 16 distinct cells expands to 65535 node visits. This is the shape the
// reference's operation budget exists for.
func TestPrepareAccountRejectsSharedSubtreeExtraCurrencyDictionary(t *testing.T) {
	shard := transactionAccountStructureShard(t, tonopsTestAddr, tlb.StorageUsed{
		CellsUsed: big.NewInt(0),
		BitsUsed:  big.NewInt(0),
	}, transactionSharedSubtreeExtraCurrencies(14).AsDict(32), tlb.AccountStatusUninit, nil)

	_, err := PrepareAccount(shard, tonopsTestAddr)
	if err == nil {
		t.Fatal("extra currency dictionary with shared subtrees was accepted")
	}
	if !strings.Contains(err.Error(), "validation budget") {
		t.Fatalf("extra currency dictionary with shared subtrees was rejected for the wrong reason: %v", err)
	}
}

// The budget must not reach past what the reference walks. A large but ordinary
// account keeps validating, and StateInit code/data stay opaque ^Cell -- the
// code tree here would cost over two million operations if it were charged,
// while RefAnything charges nothing (crypto/tl/tlblib.hpp:1026-1034).
func TestPrepareAccountBudgetSparesOrdinaryLargeAccounts(t *testing.T) {
	extra := cell.NewDict(32)
	for i := 0; i < 512; i++ {
		value := cell.BeginCell().MustStoreUInt(4, 5).MustStoreUInt(0x11223344, 32).EndCell()
		if err := extra.SetIntKey(big.NewInt(int64(i)*7919), value); err != nil {
			t.Fatal(err)
		}
	}

	code := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	for i := 0; i < 20; i++ {
		code = cell.BeginCell().MustStoreRef(code).MustStoreRef(code).EndCell()
	}
	data := cell.BeginCell().MustStoreUInt(0xBB, 8).MustStoreRef(code).EndCell()

	shard := transactionAccountStructureShard(t, tonopsTestAddr, tlb.StorageUsed{
		CellsUsed: big.NewInt(1024),
		BitsUsed:  big.NewInt(65536),
	}, extra, tlb.AccountStatusActive, &tlb.StateInit{Code: code, Data: data})

	if _, err := PrepareAccount(shard, tonopsTestAddr); err != nil {
		t.Fatalf("large but ordinary account was rejected: %v", err)
	}
}

// transactionSharedSubtreeExtraCurrencies builds a valid HashmapE 32
// (VarUInteger 32) whose every fork points both branches at one cell: levels+2
// distinct cells describing 2^levels entries.
func transactionSharedSubtreeExtraCurrencies(levels int) *cell.Cell {
	node := cell.BeginCell().
		MustStoreUInt(0, 2). // hml_short with an empty label: a leaf at zero remaining key bits
		MustStoreUInt(1, 5). // VarUInteger 32: one byte
		MustStoreUInt(1, 8).
		EndCell()
	for i := 0; i < levels; i++ {
		node = cell.BeginCell().
			MustStoreUInt(0, 2). // hml_short with an empty label: a fork
			MustStoreRef(node).
			MustStoreRef(node).
			EndCell()
	}
	return cell.BeginCell().
		MustStoreUInt(3, 2).                 // hml_same
		MustStoreUInt(0, 1).                 // of zero bits
		MustStoreUInt(uint64(31-levels), 6). // spending every key bit the forks below do not
		MustStoreRef(node).
		MustStoreRef(node).
		EndCell()
}
