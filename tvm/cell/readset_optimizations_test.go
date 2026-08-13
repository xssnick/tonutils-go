package cell

import (
	"bytes"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// A shard slot carries four bytes of the hash as a fingerprint, so two distinct
// cells can land on the same slot with the same fingerprint. The full compare on
// a candidate hit is what keeps that from handing out the wrong cell.
func TestReadSetShardVerifiesFingerprintCollisions(t *testing.T) {
	cellA := BeginCell().MustStoreUInt(0xA, 4).EndCell()
	cellB := BeginCell().MustStoreUInt(0xB, 4).EndCell()
	cellA.hash0 = Hash{0x11, 0x22, 0x33, 0x44, 0xA1}
	cellB.hash0 = Hash{0x11, 0x22, 0x33, 0x44, 0xB2}
	duplicateA := cellA.copy()

	rs := NewReadSet(cellA)
	rs.Record(cellA)
	rs.Record(cellB)
	rs.Record(duplicateA)
	for i := 0; i < 128; i++ {
		rs.Record(duplicateA)
	}

	for _, want := range []*Cell{cellA, cellB} {
		got, ok := rs.Contains(want.HashKey())
		if !ok {
			t.Fatalf("recorded cell %x was not found", want.Hash())
		}
		if got.HashKey() != want.HashKey() {
			t.Fatalf("recorded cell mismatch: got=%x want=%x", got.Hash(), want.Hash())
		}
	}
	if got, ok := rs.Contains(Hash{0x11, 0x22, 0x33, 0x44, 0xC3}); ok {
		t.Fatalf("fingerprint collision returned unrelated cell %x", got.Hash())
	}
	if rs.Size() != 2 {
		t.Fatalf("duplicate hashes inflated the record: size=%d want=2", rs.Size())
	}
}

func TestReadSetConcurrentRecordingIsExactlyOnce(t *testing.T) {
	rs := NewReadSet(BeginCell().EndCell())
	cell := BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	var callbacks atomic.Int32
	rs.SetRecordCallback(func(*Cell) {
		callbacks.Add(1)
	})

	const goroutines = 64
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				rs.Record(cell)
			}
		}()
	}
	wg.Wait()

	if got := callbacks.Load(); got != 1 {
		t.Fatalf("record callback count mismatch: got=%d want=1", got)
	}
	if got, ok := rs.Contains(cell.HashKey()); !ok || got != cell {
		t.Fatal("concurrently recorded cell was not in the record")
	}
	if rs.Size() != 1 {
		t.Fatalf("record size mismatch: got=%d want=1", rs.Size())
	}
}

func TestCreateMerkleUpdateReadReuseKeepsBoundary(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xE, 4).EndCell()
	reused := BeginCell().MustStoreUInt(0xC0DE, 16).MustStoreRef(leaf).EndCell()
	from := BeginCell().MustStoreUInt(0xA1, 8).MustStoreRef(reused).EndCell()

	rs := NewReadSet(from)
	loaded, err := rs.Root().MustBeginParse().LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	reusedRef := loaded.BaseCell()
	to := BeginCell().MustStoreUInt(0xB2, 8).MustStoreRef(reusedRef).EndCell()

	update, err := rs.CreateMerkleUpdate(to)
	if err != nil {
		t.Fatal(err)
	}
	prunedReused, err := createPrunedBranchFromCell(reused, 1)
	if err != nil {
		t.Fatal(err)
	}
	expectedFrom := BeginCell().MustStoreUInt(0xA1, 8).MustStoreRef(prunedReused).EndCell()
	expectedTo := BeginCell().MustStoreUInt(0xB2, 8).MustStoreRef(prunedReused).EndCell()
	expected, err := CreateMerkleUpdate(expectedFrom, expectedTo)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(update.ToBOCWithOptions(BOCSerializeOptions{}), expected.ToBOCWithOptions(BOCSerializeOptions{})) {
		t.Fatal("a subtree that was read and handed back did not become a boundary")
	}
}

// A destination the walk cannot open fails the whole update, and the record must
// come out of it exactly as it went in: the caller may retry, and a proof taken
// afterwards is still bounded by the reads the block was admitted against.
func TestCreateMerkleUpdateDestinationErrorLeavesRecordIntact(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xE, 4).EndCell()
	reused := BeginCell().MustStoreUInt(0xC0DE, 16).MustStoreRef(leaf).EndCell()
	from := BeginCell().MustStoreUInt(0xA1, 8).MustStoreRef(reused).EndCell()

	rs := NewReadSet(from)
	reusedRef, err := rs.Root().MustBeginParse().PeekRefCellAt(0)
	if err != nil {
		t.Fatal(err)
	}
	recorded := rs.Size()

	loadErr := errors.New("forced lazy load failure")
	failing := mustCreateLazyPrunedRef(t, lazyRefFromCell(leaf), func(Hash) (*Cell, error) {
		return nil, loadErr
	})
	broken := BeginCell().MustStoreRef(reusedRef).MustStoreRef(failing).EndCell()
	if _, err = rs.CreateMerkleUpdate(broken); !errors.Is(err, loadErr) {
		t.Fatalf("unexpected update error: got=%v want=%v", err, loadErr)
	}
	if rs.Size() != recorded {
		t.Fatalf("failed update changed the record: got=%d want=%d", rs.Size(), recorded)
	}

	to := BeginCell().MustStoreUInt(0xB2, 8).MustStoreRef(reusedRef).EndCell()
	update, err := rs.CreateMerkleUpdate(to)
	if err != nil {
		t.Fatalf("update after a failed one: %v", err)
	}
	checkMerkleUpdate(t, from.WithoutTrace(), to, update)
}
