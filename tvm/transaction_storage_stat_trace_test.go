package tvm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

func TestTransactionAccountStorageStatTracesChangedPredecessorSubtrees(t *testing.T) {
	removedDeep := cell.BeginCell().MustStoreUInt(0x11, 8).EndCell()
	removed := cell.BeginCell().MustStoreRef(removedDeep).EndCell()
	addedDeep := cell.BeginCell().MustStoreUInt(0x22, 8).EndCell()
	added := cell.BeginCell().MustStoreRef(addedDeep).EndCell()
	unrelated := cell.BeginCell().MustStoreRef(
		cell.BeginCell().MustStoreUInt(0x33, 8).EndCell(),
	).EndCell()
	predecessor := cell.BeginCell().
		MustStoreRef(removed).
		MustStoreRef(added).
		MustStoreRef(unrelated).
		EndCell()

	read := cell.NewReadSet(predecessor)
	predecessorSlice := read.Root().MustBeginParse()
	tracedRemoved, err := predecessorSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	tracedAdded, err := predecessorSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = predecessorSlice.LoadRefCell(); err != nil {
		t.Fatal(err)
	}
	// Account decoding has already loaded the changed storage roots, while the
	// descendants are first needed by AccountStorageStat::replace_roots.
	if _, err = tracedRemoved.BeginParse(); err != nil {
		t.Fatal(err)
	}
	if _, err = tracedAdded.BeginParse(); err != nil {
		t.Fatal(err)
	}

	rawOldStorage := cell.BeginCell().MustStoreRef(removed).EndCell()
	oldUsage, oldDict, err := transactionComputeAccountStorageStat(rawOldStorage)
	if err != nil {
		t.Fatal(err)
	}
	oldStorage := cell.BeginCell().MustStoreRef(tracedRemoved).EndCell()
	stat, err := transactionInitAccountStorageStat(oldDict, oldStorage, tlb.StorageUsed{
		CellsUsed: new(big.Int).SetUint64(oldUsage.cells),
		BitsUsed:  new(big.Int).SetUint64(oldUsage.bits),
	}, transactionAccountStorageStatRootHash(oldDict), false)
	if err != nil {
		t.Fatal(err)
	}
	newStorage := cell.BeginCell().MustStoreRef(tracedAdded).EndCell()
	if _, _, err = stat.replaceStorage(newStorage); err != nil {
		t.Fatal(err)
	}

	proof, err := read.Proof()
	if err != nil {
		t.Fatal(err)
	}
	proven, err := cell.UnwrapProof(proof, predecessor.Hash())
	if err != nil {
		t.Fatal(err)
	}
	provenSlice := proven.MustBeginParse()
	for i, label := range [...]string{"removed", "added"} {
		root, err := provenSlice.LoadRefCell()
		if err != nil {
			t.Fatalf("load %s root: %v", label, err)
		}
		child, err := root.MustBeginParse().LoadRefCell()
		if err != nil {
			t.Fatalf("load %s child: %v", label, err)
		}
		if child.GetType() == cell.PrunedCellType {
			t.Fatalf("%s storage-stat child %d is pruned", label, i)
		}
		if _, err = child.BeginParse(); err != nil {
			t.Fatalf("parse %s child: %v", label, err)
		}
	}
	provenUnrelated, err := provenSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	if provenUnrelated.GetType() != cell.PrunedCellType {
		t.Fatal("unrelated predecessor subtree is materialized")
	}
}

func TestTransactionAccountStorageStatProofIncludesUnchangedOldRoot(t *testing.T) {
	// Keep unchanged and changed hashes in opposite top-level dictionary
	// branches so a changed-root lookup cannot expose the unchanged leaf by
	// accident.
	var unchanged, otherUnchanged, oldData, newData *cell.Cell
	for i := uint64(0); unchanged == nil || otherUnchanged == nil || oldData == nil || newData == nil; i++ {
		candidate := cell.BeginCell().MustStoreUInt(i, 16).EndCell()
		if candidate.Hash()[0]&0x80 == 0 {
			if unchanged == nil {
				unchanged = candidate
			} else {
				otherUnchanged = candidate
			}
			continue
		}
		if oldData == nil {
			oldData = candidate
		} else {
			newData = candidate
		}
	}
	oldStorage := cell.BeginCell().
		MustStoreRef(unchanged).
		MustStoreRef(otherUnchanged).
		MustStoreRef(oldData).
		EndCell()
	newStorage := cell.BeginCell().
		MustStoreRef(unchanged).
		MustStoreRef(otherUnchanged).
		MustStoreRef(newData).
		EndCell()

	oldUsage, oldDict, err := transactionComputeAccountStorageStat(oldStorage)
	if err != nil {
		t.Fatal(err)
	}
	proofBuilder := cell.NewMerkleProofBuilder(oldDict)
	stat, err := transactionInitAccountStorageStat(
		proofBuilder.Root(),
		oldStorage,
		tlb.StorageUsed{
			CellsUsed: new(big.Int).SetUint64(oldUsage.cells),
			BitsUsed:  new(big.Int).SetUint64(oldUsage.bits),
		},
		transactionAccountStorageStatRootHash(oldDict),
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = stat.addHint(vmcore.LoadedCells{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = stat.replaceStorage(newStorage); err != nil {
		t.Fatal(err)
	}

	proof, err := proofBuilder.CreateProof()
	if err != nil {
		t.Fatal(err)
	}
	proven, err := cell.UnwrapProofVirtualized(proof, oldDict.Hash(0))
	if err != nil {
		t.Fatal(err)
	}
	value, err := proven.AsDict(256).LoadValueByBytesKey(unchanged.Hash())
	if err != nil {
		t.Fatalf("lookup unchanged old storage root: %v", err)
	}
	if value.BitsLeft() != 34 || value.RefsNum() != 0 {
		t.Fatal("unchanged old storage root has malformed storage-stat entry")
	}
}

func TestTransactionAccountStorageStatHintSkipsLoadedSharedChildLookup(t *testing.T) {
	var shared [4]*cell.Cell
	for i := range shared {
		shared[i] = transactionStorageStatTestTree(uint64(i+1), 4)
	}
	oldBuilder := cell.BeginCell().MustStoreUInt(0, 1)
	newBuilder := cell.BeginCell().MustStoreUInt(1, 1)
	for _, child := range shared {
		oldBuilder.MustStoreRef(child)
		newBuilder.MustStoreRef(child)
	}
	oldRoot := oldBuilder.EndCell()
	newRoot := newBuilder.EndCell()
	oldStorage := cell.BeginCell().MustStoreRef(oldRoot).EndCell()
	newStorage := cell.BeginCell().MustStoreRef(newRoot).EndCell()

	oldUsage, oldDict, err := transactionComputeAccountStorageStat(oldStorage)
	if err != nil {
		t.Fatal(err)
	}
	storageUsed := tlb.StorageUsed{
		CellsUsed: new(big.Int).SetUint64(oldUsage.cells),
		BitsUsed:  new(big.Int).SetUint64(oldUsage.bits),
	}
	loaded := transactionStorageStatLoadedCells(t, oldRoot.HashKey())

	proofBuilder := cell.NewMerkleProofBuilder(oldDict)
	provingStat, err := transactionInitAccountStorageStat(
		proofBuilder.Root(),
		oldStorage,
		storageUsed,
		transactionAccountStorageStatRootHash(oldDict),
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = provingStat.addHint(loaded); err != nil {
		t.Fatal(err)
	}
	if _, err = provingStat.replaceRoots([4]*cell.Cell{newRoot}, 1); err != nil {
		t.Fatal(err)
	}

	proof, err := proofBuilder.CreateProof()
	if err != nil {
		t.Fatal(err)
	}
	sparseDict, err := cell.UnwrapProofVirtualized(proof, oldDict.Hash(0))
	if err != nil {
		t.Fatal(err)
	}
	withoutHint, err := transactionInitAccountStorageStat(
		sparseDict,
		oldStorage,
		storageUsed,
		transactionAccountStorageStatRootHash(oldDict),
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = withoutHint.replaceRoots([4]*cell.Cell{newRoot}, 1); err == nil {
		t.Fatal("root replacement without the VM hint unexpectedly accepted the sparse proof")
	}

	withHint, err := transactionInitAccountStorageStat(
		sparseDict,
		oldStorage,
		storageUsed,
		transactionAccountStorageStatRootHash(oldDict),
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = withHint.addHint(loaded); err != nil {
		t.Fatal(err)
	}
	_, err = withHint.replaceRoots([4]*cell.Cell{newRoot}, 1)
	if err != nil {
		t.Fatalf("replace roots with VM hint: %v", err)
	}
	usage := transactionUsage{cells: withHint.totalCells + 1, bits: withHint.totalBits + uint64(newStorage.BitsSize())}
	wantUsage, _, err := transactionComputeAccountStorageStat(newStorage)
	if err != nil {
		t.Fatal(err)
	}
	if usage != wantUsage {
		t.Fatalf("hinted usage=%+v, want %+v", usage, wantUsage)
	}
}

func transactionStorageStatTestTree(seed uint64, depth int) *cell.Cell {
	builder := cell.BeginCell().MustStoreUInt(seed, 32)
	if depth > 0 {
		builder.MustStoreRef(transactionStorageStatTestTree(seed*2, depth-1))
		builder.MustStoreRef(transactionStorageStatTestTree(seed*2+1, depth-1))
	}
	return builder.EndCell()
}

func transactionStorageStatLoadedCells(t *testing.T, keys ...cell.Hash) vmcore.LoadedCells {
	t.Helper()

	state := &vmcore.State{Gas: vmcore.NewGas()}
	state.Cells.Init(state)
	for _, key := range keys {
		if err := state.Cells.RegisterCellLoadKey(key); err != nil {
			t.Fatal(err)
		}
	}
	return state.Cells.LoadedCells()
}
