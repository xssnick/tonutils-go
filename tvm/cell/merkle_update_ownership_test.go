package cell

import (
	"bytes"
	"fmt"
	"runtime"
	"testing"
)

type ownedUpdateApply struct {
	name  string
	apply func(*Cell, *Cell) (*Cell, error)
}

func ownedUpdateAppliers() []ownedUpdateApply {
	return []ownedUpdateApply{
		{name: "classic", apply: ApplyMerkleUpdate},
		{name: "prepared", apply: func(from, update *Cell) (*Cell, error) {
			p, err := PrepareMerkleUpdate(update)
			if err != nil {
				return nil, err
			}
			return p.ApplyTo(from)
		}},
		{name: "planned", apply: func(from, update *Cell) (*Cell, error) {
			p, err := PrepareMerkleUpdatePlanned(update)
			if err != nil {
				return nil, err
			}
			return p.ApplyTo(from)
		}},
	}
}

func ownershipCells(roots ...*Cell) map[*Cell]struct{} {
	seen := make(map[*Cell]struct{})
	stack := append([]*Cell(nil), roots...)
	for len(stack) > 0 {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		for i := 0; i < c.refsCount(); i++ {
			stack = append(stack, c.refs[i])
		}
	}
	return seen
}

func assertUpdateOwnership(t *testing.T, result, parent *Cell, roots ...*Cell) {
	t.Helper()
	sources := ownershipCells(roots...)
	payloads := make(map[*byte]struct{}, len(sources))
	for c := range sources {
		if len(c.data) != 0 {
			payloads[&c.data[0]] = struct{}{}
		}
	}
	for c := range ownershipCells(result) {
		if _, aliases := sources[c]; aliases {
			t.Fatal("applied state retains a cell from the temporary update arena")
		}
		if len(c.data) != 0 {
			if _, aliases := payloads[&c.data[0]]; aliases {
				t.Fatal("applied state retains the temporary update payload")
			}
		}
	}
	if countSharedCells(result, parent) == 0 {
		t.Fatal("applying an update copied the unchanged parent subtrees")
	}
}

func TestMerkleUpdateOwnsParsedDestination(t *testing.T) {
	for _, applier := range ownedUpdateAppliers() {
		for _, noCopy := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/no_copy=%v", applier.name, noCopy), func(t *testing.T) {
				fixture := newPrunedUpdateFixture(t, 8, 12, 20260917)
				update, err := fixture.read.CreateMerkleUpdate(fixture.to)
				if err != nil {
					t.Fatal(err)
				}
				combined := ToBOCWithOptions([]*Cell{update, prunedUpdateTree(9, 31337)}, BOCSerializeOptions{})
				roots, err := FromBOCMultiRootWithOptions(combined, BOCParseOptions{NoCopyPayload: noCopy})
				if err != nil {
					t.Fatal(err)
				}
				parent := fixture.read.Source()
				result, err := applier.apply(parent, roots[0])
				if err != nil {
					t.Fatal(err)
				}
				if result.HashKey() != fixture.to.HashKey() || !bytes.Equal(result.ToBOC(), fixture.to.ToBOC()) {
					t.Fatal("owned application changed state hash or canonical BOC")
				}
				assertUpdateOwnership(t, result, parent, roots...)
			})
		}
	}
}

func TestMerkleUpdateOwnedDestinationSharesRepeatedLeaf(t *testing.T) {
	for _, applier := range ownedUpdateAppliers() {
		t.Run(applier.name, func(t *testing.T) {
			parent := BeginCell().MustStoreUInt(1, 8).EndCell()
			leaf := BeginCell().MustStoreUInt(2, 8).EndCell()
			destination := BeginCell().MustStoreRef(leaf).MustStoreRef(leaf).EndCell()
			update, err := CreateMerkleUpdate(parent, destination)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := FromBOC(update.ToBOC())
			if err != nil {
				t.Fatal(err)
			}
			result, err := applier.apply(parent, parsed)
			if err != nil {
				t.Fatal(err)
			}
			if result.refs[0] != result.refs[1] {
				t.Fatal("ownership cloning duplicated a shared destination leaf")
			}
			if _, aliases := ownershipCells(parsed)[result.refs[0]]; aliases {
				t.Fatal("shared leaf still belongs to the parsed update")
			}
		})
	}
}

func TestMerkleUpdateOwnershipKeepsBoundaryRehash(t *testing.T) {
	for _, applier := range ownedUpdateAppliers() {
		t.Run(applier.name, func(t *testing.T) {
			parent := BeginCell().MustStoreUInt(1, 8).
				MustStoreRef(BeginCell().MustStoreUInt(3, 8).EndCell()).EndCell()
			boundary, err := CreatePrunedBranch(parent, 1, 0)
			if err != nil {
				t.Fatal(err)
			}
			if boundary.GetType() != PrunedCellType || boundary.Level() != 1 {
				t.Fatal("fixture did not create a level-1 pruned boundary")
			}
			destination := BeginCell().MustStoreUInt(2, 8).MustStoreRef(boundary).EndCell()
			// Exercise the low-level API with inconsistent trusted metadata.
			// Boundary substitution used to recompute this node. An ownership
			// optimization must not turn that into trusting its stale hash.
			destination.hash0[0] ^= 0xff
			update := mustMerkleUpdateCell(t, parent, destination)
			want, err := benchmarkApplyMerkleUpdateBaseline(parent, update)
			if err != nil {
				t.Fatal(err)
			}
			// The legacy baseline returns virtualized pruned boundaries. Its
			// hash is the reference result, but its BOC is not a full state.
			wantState := BeginCell().MustStoreUInt(2, 8).MustStoreRef(parent).EndCell()
			got, err := applier.apply(parent, update)
			if err != nil {
				t.Fatal(err)
			}
			if got.HashKey() != want.HashKey() || !bytes.Equal(got.ToBOC(), wantState.ToBOC()) {
				t.Fatal("ownership optimization skipped a boundary-driven rehash")
			}
		})
	}
}

func ownershipBaggage(depth int, path uint64) *Cell {
	if depth == 0 {
		var data [120]byte
		for i := range data {
			data[i] = byte(path >> uint(i%8*8))
		}
		return BeginCell().MustStoreSlice(data[:], 960).EndCell()
	}
	return BeginCell().MustStoreRef(ownershipBaggage(depth-1, path*2)).
		MustStoreRef(ownershipBaggage(depth-1, path*2+1)).EndCell()
}

// Keep only the latest state; each update carries unrelated collated data in
// the same parse arena. The destination preserves a different new leaf from
// every generation, just as a sparse account workload does.
func ownershipSequence(tb testing.TB, apply func(*Cell, *Cell) (*Cell, error), blocks int) *Cell {
	tb.Helper()
	root := prunedUpdateTree(8, 0)
	baggage := ownershipBaggage(13, 1)
	for i := 0; i < blocks; i++ {
		read := NewReadSet(root)
		destination := rebuildTouchedPaths(tb, read.Root(), 8, map[uint64]struct{}{uint64(i): {}}, 0)
		update, err := read.CreateMerkleUpdate(destination)
		if err != nil {
			tb.Fatal(err)
		}
		combined := ToBOCWithOptions([]*Cell{update, baggage}, BOCSerializeOptions{})
		roots, err := FromBOCMultiRootWithOptions(combined, BOCParseOptions{NoCopyPayload: true})
		if err != nil {
			tb.Fatal(err)
		}
		root, err = apply(root, roots[0])
		if err != nil {
			tb.Fatal(err)
		}
		if root.HashKey() != destination.HashKey() {
			tb.Fatal("sequence changed the destination hash")
		}
	}
	return root
}

func TestMerkleUpdateLongSequenceRetainedHeap(t *testing.T) {
	for _, applier := range ownedUpdateAppliers() {
		t.Run(applier.name, func(t *testing.T) {
			runtime.GC()
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			root := ownershipSequence(t, applier.apply, 32)
			runtime.GC()
			runtime.GC()
			runtime.ReadMemStats(&after)
			retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
			t.Logf("32 updates: retained heap delta %.3f MiB", float64(retained)/(1<<20))
			runtime.KeepAlive(root)
			// A final state has fewer than 1,000 cells. Leave several MiB of
			// headroom for runtime/test caches; pinning 32 parse arenas costs
			// around 80 MiB and cannot fit inside this tolerance.
			if retained > 8<<20 {
				t.Fatalf("latest state retains obsolete update arenas: %d bytes", retained)
			}
		})
	}
}

func BenchmarkMerkleUpdateOwnedParsed(b *testing.B) {
	fixture := newPrunedUpdateFixture(b, 14, 345, 20260917)
	update, err := fixture.read.CreateMerkleUpdate(fixture.to)
	if err != nil {
		b.Fatal(err)
	}
	parsed, err := FromBOC(update.ToBOC())
	if err != nil {
		b.Fatal(err)
	}
	for _, applier := range ownedUpdateAppliers() {
		b.Run(applier.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				result, err := applier.apply(fixture.read.Source(), parsed)
				if err != nil {
					b.Fatal(err)
				}
				if result.HashKey() != fixture.to.HashKey() {
					b.Fatal("state hash changed")
				}
			}
		})
	}
}
