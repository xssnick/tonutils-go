package cell

import "testing"

var traceReadSetFusionSink *Cell

func BenchmarkCellWithTraceRetag(b *testing.B) {
	leaf := BeginCell().MustStoreUInt(7, 8).EndCell()
	base := BeginCell().MustStoreRef(leaf).EndCell()
	pruned, err := createPrunedBranchFromCell(base, 2)
	if err != nil {
		b.Fatal(err)
	}
	multilevel := BeginCell().MustStoreRef(pruned).EndCell()
	ref := LazyRef{LevelMask: leaf.LevelMask(), Hashes: leaf.Hash(), Depths: []uint16{leaf.Depth()}}
	lazy, err := createLazyPrunedRef(ref, func(Hash) (*Cell, error) { return leaf, nil })
	if err != nil {
		b.Fatal(err)
	}
	parsed, err := FromBOCWithOptions(base.ToBOC(), BOCParseOptions{Lazy: true})
	if err != nil {
		b.Fatal(err)
	}
	first := NewTrace(TraceHooks{OnLoad: func(*Cell) {}})
	second := NewTrace(TraceHooks{OnLoad: func(*Cell) {}})
	for _, tc := range []struct {
		name string
		cell *Cell
	}{
		{"ordinary", leaf},
		{"multilevel", multilevel},
		{"virtual", multilevel.Virtualize(0)},
		{"lazy", lazy},
		{"boc_lazy", parsed.MustPeekRef(0)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			traced := tc.cell.WithTrace(first)
			for _, operation := range []string{"attach", "replace", "remove"} {
				b.Run(operation, func(b *testing.B) {
					source, trace := tc.cell, first
					if operation == "replace" {
						source, trace = traced, second
					} else if operation == "remove" {
						source, trace = traced, nil
					}
					b.ReportAllocs()
					for b.Loop() {
						traceReadSetFusionSink = source.WithTrace(trace)
					}
				})
			}
		})
	}
}

func BenchmarkReadSetProofDAG(b *testing.B) {
	shared := BeginCell().MustStoreRef(BeginCell().MustStoreUInt(0xAA, 8).EndCell()).EndCell()
	var next uint64
	var build func(int) *Cell
	build = func(depth int) *Cell {
		next++
		builder := BeginCell().MustStoreUInt(next, 32)
		if depth == 0 {
			return builder.MustStoreRef(shared).EndCell()
		}
		first := build(depth - 1)
		return builder.MustStoreRef(first).MustStoreRef(build(depth - 1)).
			MustStoreRef(build(depth - 1)).MustStoreRef(first).EndCell()
	}
	resident := build(6)
	for _, storage := range []string{"resident", "lazy"} {
		b.Run(storage, func(b *testing.B) {
			root := resident
			if storage == "lazy" {
				var err error
				root, err = FromBOCWithOptions(resident.ToBOC(), BOCParseOptions{Lazy: true, DisableLazyCache: true})
				if err != nil {
					b.Fatal(err)
				}
			}
			for _, selection := range []string{"dense", "sparse"} {
				b.Run(selection, func(b *testing.B) {
					reads := NewReadSet(root)
					seen := make(map[Hash]bool)
					var record func(*Cell, int)
					record = func(c *Cell, depth int) {
						if seen[c.HashKey()] || selection == "sparse" && depth == 3 {
							return
						}
						seen[c.HashKey()] = true
						loaded, err := c.Prewarm()
						if err != nil {
							b.Fatal(err)
						}
						reads.Record(loaded)
						for i := 0; i < int(loaded.RefsNum()); i++ {
							record(loaded.MustPeekRef(i), depth+1)
						}
					}
					record(root, 0)
					b.ReportAllocs()
					for b.Loop() {
						proof, err := reads.Proof()
						if err != nil {
							b.Fatal(err)
						}
						traceReadSetFusionSink = proof
					}
				})
			}
		})
	}
}
