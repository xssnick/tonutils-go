package tvm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// storageStatBindingAccount builds a masterchain account whose state carries a
// few refs. Masterchain never stores storage_dict_hash (store_storage_dict_hash
// = version >= 11 && !is_masterchain), so this is exactly the case where the
// carried stat can only be trusted through the executor's own binding.
func storageStatBindingAccount(t *testing.T, payload uint64) (*PreparedAccount, *cell.Cell) {
	return storageStatBindingAccountSized(t, payload, 3)
}

// storageStatBindingData builds a contract data cell holding a dictionary with
// entries keys, one of which carries marker. This is the shape a real contract
// state has: changing one value rewrites only the path down to that leaf, and
// every sibling subtree is carried over by pointer. A linear chain would be the
// degenerate case where the whole state changes.
func storageStatBindingData(t *testing.T, payload uint64, entries int, marker uint64) *cell.Cell {
	t.Helper()

	dict := cell.NewDict(32)
	for i := 0; i < entries; i++ {
		value := payload + uint64(i)
		if i == entries/2 {
			value = marker
		}
		key := cell.BeginCell().MustStoreUInt(uint64(i), 32).EndCell()
		if err := dict.Set(key, cell.BeginCell().MustStoreUInt(value, 64).EndCell()); err != nil {
			t.Fatal(err)
		}
	}

	return cell.BeginCell().
		MustStoreUInt(payload, 64).
		MustStoreDict(dict).
		EndCell()
}

// storageStatBindingAccountSized builds the same account with a data dictionary
// of the requested size, so a test can put the state on either side of the
// storage-dict threshold.
func storageStatBindingAccountSized(t *testing.T, payload uint64, chain int) (*PreparedAccount, *cell.Cell) {
	t.Helper()

	addr := address.MustParseRawAddr("-1:" + "00000000000000000000000000000000000000000000000000000000000000ff")

	data := storageStatBindingData(t, payload, chain, 0xCC)
	code := cell.BeginCell().MustStoreUInt(0xDEADBEEF, 32).EndCell()

	stateInitCell, err := tlb.ToCell(&tlb.StateInit{Code: code, Data: data})
	if err != nil {
		t.Fatal(err)
	}

	// The AccountStorage cell is independent of storage_stat, so build it once
	// to learn the real usage and only then stamp it into storage_info -- the
	// carried stat is seeded from those numbers and rejects a state whose
	// recorded usage is smaller than its own root.
	storageOnly := cell.BeginCell().
		MustStoreUInt(0, 64).
		MustStoreBigCoins(big.NewInt(1_000_000_000)).
		MustStoreDict(nil).
		MustStoreBoolBit(true).
		MustStoreBuilder(stateInitCell.ToBuilder()).
		EndCell()
	usage, _, err := transactionComputeAccountStorageStat(storageOnly)
	if err != nil {
		t.Fatal(err)
	}

	storageInfo := tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: new(big.Int).SetUint64(usage.cells),
			BitsUsed:  new(big.Int).SetUint64(usage.bits),
		},
		StorageExtra: tlb.StorageExtraNone{},
	}
	storageInfoCell, err := tlb.ToCell(&storageInfo)
	if err != nil {
		t.Fatal(err)
	}

	accountCell := cell.BeginCell().
		MustStoreBoolBit(true).
		MustStoreAddr(addr).
		MustStoreBuilder(storageInfoCell.ToBuilder()).
		MustStoreBuilder(storageOnly.ToBuilder()).
		EndCell()

	acc, err := PrepareAccount(&tlb.ShardAccount{
		Account:       accountCell,
		LastTransHash: make([]byte, 32),
		LastTransLT:   0,
	}, addr)
	if err != nil {
		t.Fatal(err)
	}
	return acc, data
}

// storageStatBindingNextStorage rebuilds the account storage with a single
// changed leaf deep in the data chain, which is what an ordinary transaction
// does: almost the whole tree is carried over unchanged.
func storageStatBindingNextStorage(t *testing.T, payload uint64, chain int, newLeaf uint64) *cell.Cell {
	t.Helper()

	data := storageStatBindingData(t, payload, chain, newLeaf)
	code := cell.BeginCell().MustStoreUInt(0xDEADBEEF, 32).EndCell()

	stateInitCell, err := tlb.ToCell(&tlb.StateInit{Code: code, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	return cell.BeginCell().
		MustStoreUInt(1, 64).
		MustStoreBigCoins(big.NewInt(1_000_000_000)).
		MustStoreDict(nil).
		MustStoreBoolBit(true).
		MustStoreBuilder(stateInitCell.ToBuilder()).
		EndCell()
}

// storageStatBindingInfo runs the storage-info step for a changed state, which
// is the branch that either applies the carried stat incrementally or falls
// back to a full recompute.
func storageStatBindingInfo(t *testing.T, acc *PreparedAccount, newStorage *cell.Cell, carried *cell.Cell) (transactionUsage, *cell.Cell, bool) {
	t.Helper()

	runtime := acc.runtime
	usage, _, _, next, bound, err := transactionAccountStorageInfo(&runtime, newStorage, emptyPreparedTestConfig(), carried)
	if err != nil {
		t.Fatal(err)
	}
	return usage, next, bound
}

// The incremental path must produce byte-identical results to the full
// recompute -- that recompute is the one already checked against the C++
// emulator, so equality is what carries the parity guarantee over.
func TestStorageStatBoundHintMatchesFullRecompute(t *testing.T) {
	// Above the storage-dict threshold, where the incremental path is enabled.
	const chain = 40
	acc, _ := storageStatBindingAccountSized(t, 1, chain)
	next := storageStatBindingNextStorage(t, 1, chain, 0xDD)

	wantUsage, wantDict, wantBound := storageStatBindingInfo(t, acc, next, nil)
	if !wantBound {
		t.Fatal("a freshly computed stat must be reported as bound")
	}

	// Feed the account's own stat back in with the binding the executor sets.
	bound := acc
	statRoot := bound.runtime.storageCellForStat
	if statRoot == nil {
		statRoot = bound.runtime.storageCell
	}
	if statRoot == nil {
		t.Skip("account has no storage cell to bind to")
	}
	_, carried, err := transactionComputeAccountStorageStat(statRoot)
	if err != nil {
		t.Fatal(err)
	}
	bound.runtime.statBoundTo = statRoot.HashKey()

	gotUsage, gotDict, gotBound := storageStatBindingInfo(t, bound, next, carried)
	if !gotBound {
		t.Fatal("a bound stat must stay bound after being applied")
	}
	if gotUsage != wantUsage {
		t.Fatalf("incremental usage = %+v, want %+v (full recompute)", gotUsage, wantUsage)
	}
	if !transactionCellEqual(gotDict, wantDict) {
		t.Fatal("incremental storage stat dictionary does not match full recompute")
	}
}

// A stat that this executor did not bind to exactly this state must be ignored,
// otherwise a caller could hand back a stale or foreign dictionary and get a
// wrong storage_used -- and therefore a wrong storage fee.
func TestStorageStatUnboundHintIsIgnored(t *testing.T) {
	const chain = 40
	acc, _ := storageStatBindingAccountSized(t, 1, chain)
	other, _ := storageStatBindingAccountSized(t, 7, chain)
	next := storageStatBindingNextStorage(t, 1, chain, 0xDD)

	wantUsage, wantDict, _ := storageStatBindingInfo(t, acc, next, nil)

	// A dictionary describing a different account state, offered without a
	// binding: it must be dropped, not applied.
	foreignRoot := other.runtime.storageCell
	_, foreign, err := transactionComputeAccountStorageStat(foreignRoot)
	if err != nil {
		t.Fatal(err)
	}

	gotUsage, gotDict, _ := storageStatBindingInfo(t, acc, next, foreign)
	if gotUsage != wantUsage {
		t.Fatalf("unbound hint changed the usage: got %+v, want %+v", gotUsage, wantUsage)
	}
	if !transactionCellEqual(gotDict, wantDict) {
		t.Fatal("unbound hint changed the storage stat dictionary")
	}

	// The same dictionary with a binding that names a different state is also
	// rejected: the binding is checked against the state, not merely present.
	stale := acc
	stale.runtime.statBoundTo = foreignRoot.HashKey()
	staleUsage, staleDict, _ := storageStatBindingInfo(t, stale, next, foreign)
	if staleUsage != wantUsage {
		t.Fatalf("stale binding changed the usage: got %+v, want %+v", staleUsage, wantUsage)
	}
	if !transactionCellEqual(staleDict, wantDict) {
		t.Fatal("stale binding changed the storage stat dictionary")
	}
}

func BenchmarkStorageStatIncrementalVsRecompute(b *testing.B) {
	t := &testing.T{}
	for _, chain := range []int{3, 40, 400, 2000} {
		acc, _ := storageStatBindingAccountSized(t, 1, chain)
		next := storageStatBindingNextStorage(t, 1, chain, 0xDD)
		statRoot := acc.runtime.storageCell
		_, carried, err := transactionComputeAccountStorageStat(statRoot)
		if err != nil {
			b.Fatal(err)
		}
		bound := acc
		bound.runtime.statBoundTo = statRoot.HashKey()

		b.Run("cells="+itoa(chain)+"/recompute", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				storageStatBindingInfo(t, acc, next, nil)
			}
		})
		b.Run("cells="+itoa(chain)+"/incremental", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				storageStatBindingInfo(t, bound, next, carried)
			}
		})
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
