package tvm

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

// TestBoundStorageStatSurvivesDipBelowThreshold pins the commit cadence a
// full-collated producer has to keep.
//
// The reference carries an account's storage-stat dictionary across the whole
// transaction chain with no size test — it gates only the dict hash on
// acc_state_cells_for_storage_dict — so an account whose state dips under the
// threshold and then grows again keeps walking the same dictionary. Gating the
// carry on size instead makes the dipped transaction rebuild the stat from the
// account state and touch no dictionary cell at all. The block is the same
// either way, but the Merkle proof shipped beside it is not: it would contain
// none of the dictionary, and a reference validator continuing its own walk
// there hits pruned branches and rejects the candidate.
//
// The test therefore asserts the reads, which is what the proof is cut from,
// and separately that the committed dictionary is unchanged.
func TestBoundStorageStatSurvivesDipBelowThreshold(t *testing.T) {
	cfg := transactionTestConfigWithGlobalVersion(t, uint32(vmcore.MaxSupportedGlobalVersion))
	now := uint32(tonopsTestTime.Unix())
	addr := address.MustParseRawAddr("0:2b63f898590aa9e300fd0e696bc834b9ebe3ab75ec16dbc2a90955d960cc6a85")
	threshold := transactionGetSizeLimits(cfg).accStateCellsForStorageDict

	code := cell.BeginCell().MustStoreUInt(0xC0DE, 32).EndCell()
	chain := func(depth int) *cell.Cell {
		tail := cell.BeginCell().MustStoreUInt(0xFFFF, 64).EndCell()
		for i := depth - 1; i >= 0; i-- {
			tail = cell.BeginCell().MustStoreUInt(uint64(0x1000+i), 64).MustStoreRef(tail).EndCell()
		}
		return tail
	}
	// The dipped state the account sits in, and the state its next transaction
	// grows back into.
	dippedData := cell.BeginCell().MustStoreUInt(1, 8).MustStoreRef(chain(3)).EndCell()
	grownData := cell.BeginCell().MustStoreUInt(2, 8).MustStoreRef(chain(30)).EndCell()

	prepareDipped := func(t *testing.T) (*PreparedAccount, *cell.Cell) {
		t.Helper()
		// A dipped account records no storage_dict_hash: it is below the
		// threshold, which is exactly why only the carried binding can keep the
		// dictionary alive.
		shard := buildTransactionTestStoredShardAccount(t, addr, tlb.AccountStatusActive, 1_000_000_000, tlb.StorageInfo{
			StorageUsed:  tlb.StorageUsed{CellsUsed: big.NewInt(6), BitsUsed: big.NewInt(400)},
			StorageExtra: tlb.StorageExtraNone{},
			LastPaid:     now,
		}, &tlb.StateInit{Code: code, Data: dippedData}, nil)
		prepared, err := PrepareAccount(shard, addr)
		if err != nil {
			t.Fatalf("prepare dipped account: %v", err)
		}
		storageForStat, err := transactionOldAccountStorageForStat(&prepared.runtime, true)
		if err != nil {
			t.Fatalf("derive dipped storage for stat: %v", err)
		}
		return prepared, storageForStat
	}

	_, storageForStat := prepareDipped(t)
	usage, dippedDict, err := transactionComputeAccountStorageStat(storageForStat, 0)
	if err != nil {
		t.Fatalf("compute dipped storage stat: %v", err)
	}
	if dippedDict == nil {
		t.Fatal("dipped state produced no dictionary, the fixture proves nothing")
	}
	if usage.cells >= threshold {
		t.Fatalf("dipped state has %d cells, not below the storage-dict threshold %d", usage.cells, threshold)
	}

	build := func(t *testing.T, bound *cell.Cell) *builtTransactionAccount {
		t.Helper()
		prepared, boundTo := prepareDipped(t)
		// Bind the dictionary the way this executor's own previous transaction
		// leaves it: for exactly this storage cell, with no dict hash to lean on.
		prepared.runtime.accountStorageStat = bound
		prepared.runtime.statBoundTo = boundTo.HashKey()
		built, err := buildTransactionAccountCell(
			&prepared.runtime, tlb.AccountStatusActive, big.NewInt(1_000_000_000),
			nil, 2, now, nil, code, grownData, nil, nil, false, cfg, bound, nil,
		)
		if err != nil {
			t.Fatalf("build grown account: %v", err)
		}
		return built
	}

	read := cell.NewReadSet(dippedDict)
	built := build(t, read.Root())

	if _, recorded := read.Contains(dippedDict.HashKey()); !recorded {
		t.Fatal("the transaction after the dip never opened the bound storage dictionary, " +
			"so the proof it ships carries none of it")
	}

	// The committed dictionary is the same function of the resulting state, so
	// keeping the carry alive must not move it.
	grownStorage := built.storageCellForStat
	if grownStorage == nil {
		grownStorage = built.storageCell
	}
	_, truth, err := transactionComputeAccountStorageStat(grownStorage, 0)
	if err != nil {
		t.Fatalf("compute grown storage stat: %v", err)
	}
	if built.storageStat == nil {
		t.Fatal("the grown transaction produced no storage dictionary")
	}
	if !bytes.Equal(built.storageStat.Hash(), truth.Hash()) {
		t.Fatalf("carried dictionary %x differs from the direct walk %x",
			built.storageStat.Hash(), truth.Hash())
	}
	if !built.storageStatBound {
		t.Fatal("the carried dictionary lost its binding")
	}
}

// TestStorageStatEntryCapacityClampsTheHint pins the bound on the entry-map
// hint. The hint is the account's own declared cell count, which the walk that
// consumes it has not verified — it is recomputing exactly that number — so an
// absurd declaration must cost a bounded amount of memory and nothing else.
func TestStorageStatEntryCapacityClampsTheHint(t *testing.T) {
	cases := []struct {
		hint uint64
		want int
	}{
		{hint: 0, want: 8},
		{hint: 26, want: 34},
		{hint: 4000, want: 4008},
		{hint: storageStatEntryHintCap, want: storageStatEntryHintCap + 8},
		{hint: storageStatEntryHintCap + 1, want: storageStatEntryHintCap + 8},
		{hint: 1 << 40, want: storageStatEntryHintCap + 8},
		{hint: ^uint64(0), want: storageStatEntryHintCap + 8},
	}
	for _, tc := range cases {
		if got := storageStatEntryCapacity(tc.hint); got != tc.want {
			t.Fatalf("capacity for hint %d = %d, want %d", tc.hint, got, tc.want)
		}
	}

	// An account declaring an impossible size still computes its stat, and
	// computes the same one a truthful declaration produces.
	code := cell.BeginCell().MustStoreUInt(0xC0DE, 32).EndCell()
	data := cell.BeginCell().MustStoreUInt(7, 8).
		MustStoreRef(cell.BeginCell().MustStoreUInt(8, 64).EndCell()).EndCell()
	si, err := tlb.ToCell(&tlb.StateInit{Code: code, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	storage := cell.BeginCell().MustStoreUInt(0, 64).MustStoreBigCoins(big.NewInt(1e9)).
		MustStoreDict(nil).MustStoreBoolBit(true).MustStoreBuilder(si.ToBuilder()).EndCell()

	honest, honestDict, err := transactionComputeAccountStorageStat(storage, 4)
	if err != nil {
		t.Fatalf("honest hint: %v", err)
	}
	absurd, absurdDict, err := transactionComputeAccountStorageStat(storage, ^uint64(0))
	if err != nil {
		t.Fatalf("absurd hint: %v", err)
	}
	if honest != absurd {
		t.Fatalf("usage under an absurd hint = %+v, want %+v", absurd, honest)
	}
	if (honestDict == nil) != (absurdDict == nil) {
		t.Fatal("an absurd hint changed whether a dictionary was produced")
	}
	if honestDict != nil && !bytes.Equal(honestDict.Hash(), absurdDict.Hash()) {
		t.Fatal("an absurd hint changed the computed dictionary")
	}
}
