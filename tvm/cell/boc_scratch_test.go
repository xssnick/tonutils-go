package cell

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"sync"
	"testing"
)

func TestBOCScratchSerializeByteParityAllModesMultiRoot(t *testing.T) {
	roots := bocScratchTestRoots()
	reused := new(BOCScratch)

	for mode := 0; mode < 1<<5; mode++ {
		t.Run(fmt.Sprintf("mode_%02d", mode), func(t *testing.T) {
			opts := bocScratchTestOptions(mode, 64)

			want, wantErr := new(BOCScratch).Serialize(roots, opts)
			got, gotErr := reused.Serialize(roots, opts)
			if (gotErr != nil) != (wantErr != nil) {
				t.Fatalf("reused scratch error = %v, fresh scratch error = %v", gotErr, wantErr)
			}
			if gotErr == nil && !bytes.Equal(got, want) {
				t.Fatal("reused scratch output differs from fresh scratch output")
			}

			pooled, pooledErr := ToBOCWithOptionsErr(roots, opts)
			if (pooledErr != nil) != (wantErr != nil) {
				t.Fatalf("package API error = %v, fresh scratch error = %v", pooledErr, wantErr)
			}
			if pooledErr == nil && !bytes.Equal(pooled, want) {
				t.Fatal("package API output differs from fresh scratch output")
			}
		})
	}
}

func TestBOCScratchReuseLargeSmallLarge(t *testing.T) {
	large := []*Cell{bocScratchTestChain(512)}
	small := []*Cell{BeginCell().MustStoreUInt(7, 3).EndCell()}
	opts := bocScratchTestOptions(31, 512)

	wantLarge, err := new(BOCScratch).Serialize(large, opts)
	if err != nil {
		t.Fatalf("serialize fresh large graph: %v", err)
	}
	wantSmall, err := new(BOCScratch).Serialize(small, opts)
	if err != nil {
		t.Fatalf("serialize fresh small graph: %v", err)
	}

	scratch := new(BOCScratch)
	firstLarge, err := scratch.Serialize(large, opts)
	if err != nil {
		t.Fatalf("serialize first large graph: %v", err)
	}
	if !bytes.Equal(firstLarge, wantLarge) {
		t.Fatal("first large serialization differs from fresh output")
	}

	gotSmall, err := scratch.Serialize(small, opts)
	if err != nil {
		t.Fatalf("serialize small graph after large graph: %v", err)
	}
	if !bytes.Equal(gotSmall, wantSmall) {
		t.Fatal("small serialization after large graph differs from fresh output")
	}

	secondLarge, err := scratch.Serialize(large, opts)
	if err != nil {
		t.Fatalf("serialize second large graph: %v", err)
	}
	if !bytes.Equal(secondLarge, wantLarge) {
		t.Fatal("second large serialization differs from fresh output")
	}
}

func TestBOCScratchSerializeOutputsAreIndependentlyOwned(t *testing.T) {
	scratch := new(BOCScratch)
	opts := bocScratchTestOptions(31, 128)

	first, err := scratch.Serialize([]*Cell{bocScratchTestChain(128)}, opts)
	if err != nil {
		t.Fatalf("serialize first graph: %v", err)
	}
	firstBefore := bytes.Clone(first)

	second, err := scratch.Serialize([]*Cell{bocScratchTestChain(96)}, opts)
	if err != nil {
		t.Fatalf("serialize second graph: %v", err)
	}
	secondBefore := bytes.Clone(second)

	second[0] ^= 0xff
	if !bytes.Equal(first, firstBefore) {
		t.Fatal("mutating the second output changed the first output")
	}
	second[0] ^= 0xff

	first[0] ^= 0xff
	if !bytes.Equal(second, secondBefore) {
		t.Fatal("mutating the first output changed the second output")
	}
}

func TestBOCScratchClearsRetainedCellsAfterSuccessAndError(t *testing.T) {
	scratch := new(BOCScratch)
	root := bocScratchTestChain(128)
	opts := bocScratchTestOptions(31, 128)

	if _, err := scratch.Serialize([]*Cell{root}, opts); err != nil {
		t.Fatalf("serialize graph: %v", err)
	}
	assertBOCScratchHasNoRetainedCells(t, scratch)

	if _, err := scratch.Serialize([]*Cell{root, nil}, opts); err == nil {
		t.Fatal("expected nil root after a valid root to fail")
	}
	assertBOCScratchHasNoRetainedCells(t, scratch)
}

func TestBOCPackageSerializationAPIsConcurrent(t *testing.T) {
	const (
		workers    = 16
		iterations = 16
	)

	roots := bocScratchTestRoots()
	root := roots[0]
	opts := bocScratchTestOptions(31, 64)

	wantMulti, err := new(BOCScratch).Serialize(roots, opts)
	if err != nil {
		t.Fatalf("serialize multi-root reference: %v", err)
	}
	wantSingle, err := new(BOCScratch).Serialize([]*Cell{root}, opts)
	if err != nil {
		t.Fatalf("serialize single-root reference: %v", err)
	}
	wantFileBOC, err := new(BOCScratch).Serialize([]*Cell{root}, BOCSerializeOptions{
		WithIndex:     true,
		WithCRC32C:    true,
		WithIntHashes: true,
		WithCacheBits: true,
	})
	if err != nil {
		t.Fatalf("serialize file-hash reference: %v", err)
	}
	wantFileHash := sha256.Sum256(wantFileBOC)

	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			for iteration := 0; iteration < iterations; iteration++ {
				var err error
				switch (worker + iteration) % 6 {
				case 0:
					var got []byte
					got, err = ToBOCWithOptionsErr(roots, opts)
					if err == nil && !bytes.Equal(got, wantMulti) {
						err = fmt.Errorf("ToBOCWithOptionsErr output differs")
					}
				case 1:
					prefix := []byte{0xaa, byte(worker), byte(iteration)}
					var got []byte
					got, err = AppendBOCWithOptions(prefix, roots, opts)
					if err == nil && (!bytes.Equal(got[:len(prefix)], prefix) || !bytes.Equal(got[len(prefix):], wantMulti)) {
						err = fmt.Errorf("AppendBOCWithOptions output differs")
					}
				case 2:
					var got bytes.Buffer
					err = WriteBOCWithOptions(&got, roots, opts)
					if err == nil && !bytes.Equal(got.Bytes(), wantMulti) {
						err = fmt.Errorf("WriteBOCWithOptions output differs")
					}
				case 3:
					var got []byte
					got, err = root.ToBOCWithOptionsErr(opts)
					if err == nil && !bytes.Equal(got, wantSingle) {
						err = fmt.Errorf("Cell.ToBOCWithOptionsErr output differs")
					}
				case 4:
					prefix := []byte{0xbb, byte(worker), byte(iteration)}
					var got []byte
					got, err = root.AppendBOCWithOptions(prefix, opts)
					if err == nil && (!bytes.Equal(got[:len(prefix)], prefix) || !bytes.Equal(got[len(prefix):], wantSingle)) {
						err = fmt.Errorf("Cell.AppendBOCWithOptions output differs")
					}
				case 5:
					if got := ComputeFileHash(root); !bytes.Equal(got, wantFileHash[:]) {
						err = fmt.Errorf("ComputeFileHash output differs")
					}
				}

				if err != nil {
					errs <- fmt.Errorf("worker %d iteration %d: %w", worker, iteration, err)
					return
				}
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func BenchmarkBOCScratchReuse(b *testing.B) {
	roots := []*Cell{bocScratchTestChain(512)}
	opts := bocScratchTestOptions(31, 512)

	want, err := new(BOCScratch).Serialize(roots, opts)
	if err != nil {
		b.Fatalf("serialize benchmark graph: %v", err)
	}

	b.Run("fresh", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(want)))

		var output []byte
		for b.Loop() {
			var scratch BOCScratch
			output, err = scratch.Serialize(roots, opts)
			if err != nil {
				b.Fatal(err)
			}
		}
		_ = output
	})

	b.Run("reused", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(want)))

		scratch := new(BOCScratch)
		var output []byte
		for b.Loop() {
			output, err = scratch.Serialize(roots, opts)
			if err != nil {
				b.Fatal(err)
			}
		}
		_ = output
	})
}

func assertBOCScratchHasNoRetainedCells(t *testing.T, scratch *BOCScratch) {
	t.Helper()

	bag := &scratch.serializer
	if len(bag.cellList) != 0 {
		t.Fatalf("scratch retained %d active cell items", len(bag.cellList))
	}
	for i, item := range bag.cellList[:cap(bag.cellList)] {
		if item.cell != nil {
			t.Fatalf("scratch retained cell pointer in item %d", i)
		}
	}

	if len(bag.roots) != 0 {
		t.Fatalf("scratch retained %d active roots", len(bag.roots))
	}
	for i, root := range bag.roots[:cap(bag.roots)] {
		if root.cell != nil {
			t.Fatalf("scratch retained root pointer in item %d", i)
		}
	}

	if bag.cellIndex != nil {
		t.Fatal("scratch retained its active cell index")
	}
}

func bocScratchTestOptions(mode, cellsCountHint int) BOCSerializeOptions {
	return BOCSerializeOptions{
		WithIndex:      mode&bocModeWithIndex != 0,
		WithCRC32C:     mode&bocModeWithCRC32C != 0,
		WithTopHash:    mode&bocModeWithTopHash != 0,
		WithIntHashes:  mode&bocModeWithIntHashes != 0,
		WithCacheBits:  mode&bocModeWithCacheBits != 0,
		CellsCountHint: cellsCountHint,
	}
}

func bocScratchTestRoots() []*Cell {
	shared := bocScratchTestChain(48)
	left := BeginCell().MustStoreUInt(0x11, 8).MustStoreRef(shared).EndCell()
	right := BeginCell().MustStoreUInt(0x22, 8).MustStoreRef(shared).EndCell()
	independent := BeginCell().MustStoreUInt(0x33, 8).EndCell()

	return []*Cell{left, right, shared, independent}
}

func bocScratchTestChain(cells int) *Cell {
	root := BeginCell().MustStoreUInt(0, 32).EndCell()
	for i := 1; i < cells; i++ {
		root = BeginCell().MustStoreUInt(uint64(i), 32).MustStoreRef(root).EndCell()
	}
	return root
}
