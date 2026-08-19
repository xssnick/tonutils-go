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

// TestStorageStatRecomputesWhenBoundDictProofFallsShort pins the fallback for
// a storage-stat dict bound from another producer's Merkle proof: such a proof
// is pruned along the producer's own update walk, and a delete replayed here
// in a different order can need a node — most often the delete-merge sibling —
// the producer never loaded. The update must then recompute the stat from the
// state directly (the absent-proof path) instead of failing the transaction,
// and must report that it did so.
//
// The test sweeps every single-node pruning of the true dict: for each
// non-root node it binds a proof with exactly that node pruned and requires
// the build to succeed with the identical account cell and dict as the
// fully-materialized control — flagged as recomputed when the pruning was
// load-bearing. Before the fallback the load-bearing arms failed with the
// classified special-cells error; before the delete-merge guard some of them
// silently committed a corrupted dict, which this test also catches through
// the control comparison.
func TestStorageStatRecomputesWhenBoundDictProofFallsShort(t *testing.T) {
	cfg := transactionTestConfigWithGlobalVersion(t, uint32(vmcore.MaxSupportedGlobalVersion))
	now := uint32(tonopsTestTime.Unix())
	addr := address.MustParseRawAddr("0:2b63f898590aa9e300fd0e696bc834b9ebe3ab75ec16dbc2a90955d960cc6a85")

	code := cell.BeginCell().MustStoreUInt(0xC0DE, 32).
		MustStoreRef(cell.BeginCell().MustStoreUInt(0xC0DE1, 64).EndCell()).
		EndCell()
	// A chain of distinct cells keeps the state above the storage-dict
	// threshold (26) with every cell unique, so cutting the tail turns into a
	// batch of dictionary deletes with real merge work.
	chain := func(depth, tag int) *cell.Cell {
		tail := cell.BeginCell().MustStoreUInt(uint64(tag), 64).EndCell()
		for i := depth - 1; i >= 0; i-- {
			tail = cell.BeginCell().MustStoreUInt(uint64(tag+i), 64).MustStoreRef(tail).EndCell()
		}
		return tail
	}
	oldData := cell.BeginCell().MustStoreUInt(1, 8).MustStoreRef(chain(30, 0x1000)).EndCell()
	newData := cell.BeginCell().MustStoreUInt(2, 8).MustStoreRef(chain(24, 0x1000)).EndCell()

	prepare := func(storageExtra any, cellsUsed, bitsUsed uint64) *PreparedAccount {
		t.Helper()
		shard := buildTransactionTestStoredShardAccount(t, addr, tlb.AccountStatusActive, 1_000_000_000, tlb.StorageInfo{
			StorageUsed: tlb.StorageUsed{
				CellsUsed: new(big.Int).SetUint64(cellsUsed),
				BitsUsed:  new(big.Int).SetUint64(bitsUsed),
			},
			StorageExtra: storageExtra,
			LastPaid:     now,
		}, &tlb.StateInit{Code: code, Data: oldData}, nil)
		prepared, err := PrepareAccount(shard, addr)
		if err != nil {
			t.Fatalf("prepare account: %v", err)
		}
		return prepared
	}

	// Derive the true storage dict and usage of the old state, so the account
	// commits honest numbers and the incremental and direct arms agree bit for
	// bit.
	probe := prepare(tlb.StorageExtraNone{}, 30, 1000)
	oldStorageForStat, err := transactionOldAccountStorageForStat(&probe.runtime, true)
	if err != nil {
		t.Fatalf("derive old storage for stat: %v", err)
	}
	oldUsage, trueDict, err := transactionComputeAccountStorageStat(oldStorageForStat, 0)
	if err != nil {
		t.Fatalf("compute true storage stat: %v", err)
	}
	if trueDict == nil {
		t.Fatal("old state is below the storage-dict threshold, fixture proves nothing")
	}
	if threshold := transactionGetSizeLimits(cfg).accStateCellsForStorageDict; oldUsage.cells < threshold {
		t.Fatalf("old state has %d cells, below the storage-dict threshold %d", oldUsage.cells, threshold)
	}

	runBuild := func(bound *cell.Cell) (*builtTransactionAccount, error) {
		prepared := prepare(tlb.StorageExtraInfo{DictHash: trueDict.Hash()}, oldUsage.cells, oldUsage.bits)
		return buildTransactionAccountCell(
			&prepared.runtime, tlb.AccountStatusActive, big.NewInt(1_000_000_000),
			nil, 2, now, nil, code, newData, nil, nil, false, cfg, bound, nil,
		)
	}

	control, err := runBuild(trueDict)
	if err != nil {
		t.Fatalf("control build with the full dict: %v", err)
	}
	if control.storageStatRecomputed {
		t.Fatal("control build reported a recompute with the full dict bound")
	}
	if control.storageStat == nil {
		t.Fatal("control build produced no storage dict")
	}

	// The direct walk of the new state and the incremental update of the old
	// dict must be the same function.
	direct, err := runBuild(nil)
	if err != nil {
		t.Fatalf("direct build without a bound dict: %v", err)
	}
	if !bytes.Equal(direct.storageStat.Hash(), control.storageStat.Hash()) ||
		!bytes.Equal(direct.cell.Hash(), control.cell.Hash()) {
		t.Fatal("direct walk and incremental update disagree, the control proves nothing")
	}

	var nodes []cell.Hash
	seen := map[cell.Hash]struct{}{}
	var collect func(c *cell.Cell)
	collect = func(c *cell.Cell) {
		key := c.HashKey()
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		nodes = append(nodes, key)
		for i := 0; i < int(c.RefsNum()); i++ {
			collect(c.MustPeekRef(i))
		}
	}
	collect(trueDict)
	if len(nodes) < 3 {
		t.Fatalf("true dict has only %d cells, the sweep proves nothing", len(nodes))
	}

	recomputed := 0
	for _, excluded := range nodes[1:] {
		proof, err := trueDict.CreateHashUsageProof(func(h cell.Hash) bool { return h != excluded })
		if err != nil {
			t.Fatalf("prune %x: create proof: %v", excluded, err)
		}
		virtual, err := cell.UnwrapProofVirtualized(proof, trueDict.Hash())
		if err != nil {
			t.Fatalf("prune %x: virtualize: %v", excluded, err)
		}

		built, err := runBuild(virtual)
		if err != nil {
			t.Fatalf("prune %x: build failed instead of recomputing the stat: %v", excluded, err)
		}
		if !bytes.Equal(built.storageStat.Hash(), control.storageStat.Hash()) {
			t.Fatalf("prune %x: storage dict %x differs from control %x",
				excluded, built.storageStat.Hash(), control.storageStat.Hash())
		}
		if !bytes.Equal(built.cell.Hash(), control.cell.Hash()) {
			t.Fatalf("prune %x: account cell diverged from control", excluded)
		}
		if built.storageStatRecomputed {
			recomputed++
		}
	}
	if recomputed == 0 {
		t.Fatal("no pruning was load-bearing, the sweep never exercised the fallback")
	}
	t.Logf("sweep: %d prunings, %d recomputes", len(nodes)-1, recomputed)
}
