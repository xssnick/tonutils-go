package tlb

import (
	"fmt"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

type loaderRefAllocChild struct {
	Value uint32 `tlb:"## 32"`
}

type loaderRefAllocParent struct {
	Child loaderRefAllocChild `tlb:"^"`
}

var loaderRefAllocSink loaderRefAllocParent
var hashUpdateAllocSink HashUpdate

func TestLoaderReferencedStructIntoPreservesTrace(t *testing.T) {
	loads := 0
	child := cell.BeginCell().MustStoreUInt(0xAABBCCDD, 32).EndCell()
	childTrace := cell.NewTrace(cell.TraceHooks{OnLoad: func(got *cell.Cell) {
		loads++
		if got != child {
			t.Fatal("trace loaded a cloned child")
		}
	}})
	parentTrace := cell.NewTrace(cell.TraceHooks{OnChild: func(ref int) *cell.Trace {
		if ref != 0 {
			t.Fatalf("child index = %d, want 0", ref)
		}
		return childTrace
	}})
	root := cell.BeginCell().MustStoreRef(child).EndCell().WithTrace(parentTrace)

	var decoded loaderRefAllocParent
	if err := LoadFromCell(&decoded, root.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	if decoded.Child.Value != 0xAABBCCDD || loads != 1 {
		t.Fatalf("decoded value=%x loads=%d", decoded.Child.Value, loads)
	}
	if child.Trace() != nil {
		t.Fatal("generic ref loader attached trace metadata to immutable child")
	}
}

func TestLoaderReferencedStructIntoSkipsProofBranch(t *testing.T) {
	leaf := cell.BeginCell().MustStoreUInt(1, 1).EndCell()
	child := cell.BeginCell().MustStoreUInt(0xAABBCCDD, 32).MustStoreRef(leaf).EndCell()
	pruned, err := cell.CreatePrunedBranch(child, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	root := cell.BeginCell().MustStoreRef(pruned).EndCell()
	loader := root.MustBeginParse()

	var decoded loaderRefAllocParent
	if err = LoadFromCellAsProof(&decoded, loader); err != nil {
		t.Fatal(err)
	}
	if decoded.Child.Value != 0 || loader.RefsNum() != 0 {
		t.Fatal("proof branch was parsed or not consumed")
	}
}

func BenchmarkLoaderReferencedStruct(b *testing.B) {
	child := cell.BeginCell().MustStoreUInt(0xAABBCCDD, 32).EndCell()
	root := cell.BeginCell().MustStoreRef(child).EndCell()
	base := *root.MustBeginParse()

	b.ReportAllocs()
	for b.Loop() {
		s := base
		var decoded loaderRefAllocParent
		if err := LoadFromCell(&decoded, &s); err != nil {
			b.Fatal(err)
		}
		loaderRefAllocSink = decoded
	}
}

func BenchmarkLoaderReferencedStructTraced(b *testing.B) {
	child := cell.BeginCell().MustStoreUInt(0xAABBCCDD, 32).EndCell()
	childTrace := cell.NewTrace(cell.TraceHooks{OnLoad: func(*cell.Cell) {}})
	parentTrace := cell.NewTrace(cell.TraceHooks{OnChild: func(int) *cell.Trace { return childTrace }})
	root := cell.BeginCell().MustStoreRef(child).EndCell().WithTrace(parentTrace)
	base := *root.MustBeginParse()

	b.ReportAllocs()
	for b.Loop() {
		s := base
		var decoded loaderRefAllocParent
		if err := LoadFromCell(&decoded, &s); err != nil {
			b.Fatal(err)
		}
		loaderRefAllocSink = decoded
	}
}

func BenchmarkHashUpdateFixedHashes(b *testing.B) {
	payload := make([]byte, 64)
	for i := range payload {
		payload[i] = byte(i)
	}
	root := cell.BeginCell().MustStoreUInt(0x72, 8).MustStoreSlice(payload, 512).EndCell()
	base := *root.MustBeginParse()

	b.Run("OwnedSlices", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s := base
			h, err := loadHashUpdateOwnedSlices(&s)
			if err != nil {
				b.Fatal(err)
			}
			hashUpdateAllocSink = h
		}
	})
	b.Run("SharedBacking", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s := base
			var h HashUpdate
			if err := h.LoadFromCell(&s); err != nil {
				b.Fatal(err)
			}
			hashUpdateAllocSink = h
		}
	})
}

func loadHashUpdateOwnedSlices(loader *cell.Slice) (HashUpdate, error) {
	magic, err := loader.LoadUInt(8)
	if err != nil {
		return HashUpdate{}, err
	}
	if magic != 0x72 {
		return HashUpdate{}, fmt.Errorf("invalid hash update magic %x", magic)
	}
	oldHash, err := loader.LoadSlice(256)
	if err != nil {
		return HashUpdate{}, err
	}
	newHash, err := loader.LoadSlice(256)
	if err != nil {
		return HashUpdate{}, err
	}
	return HashUpdate{OldHash: oldHash, NewHash: newHash}, nil
}
