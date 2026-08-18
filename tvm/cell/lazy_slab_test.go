package cell

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"unsafe"
)

// slabCase builds identical constructor inputs for the classic and slab paths:
// a level-0 ordinary (or special) parent with byte-aligned payload and refCnt
// distinct leaf children, plus a loader that serves those children.
type slabCase struct {
	descriptors uint16
	data        []byte
	hashes      []byte
	depths      []uint16
	refs        []LazyRef
	children    []*Cell
	loader      LazyCellLoader
}

func buildSlabCase(t testing.TB, refCnt int, special bool) *slabCase {
	payload := []byte{0xAB, 0xCD, byte(refCnt), 0x01}
	builder := BeginCell().MustStoreSlice(payload, uint(len(payload))*8)

	children := make([]*Cell, refCnt)
	byHash := make(map[Hash]*Cell, refCnt)
	refs := make([]LazyRef, refCnt)
	for i := range children {
		child := BeginCell().MustStoreUInt(uint64(0xF0+i), 8).EndCell()
		children[i] = child
		byHash[child.HashKey()] = child
		refs[i] = LazyRef{
			LevelMask: LevelMask{},
			Hashes:    child.Hash(),
			Depths:    []uint16{0},
		}
		builder.MustStoreRef(child)
	}
	parent := builder.EndCell()

	d1 := byte(refCnt)
	if special {
		d1 |= 0b1000
	}
	d2 := byte(2 * len(payload)) // byte-aligned body

	return &slabCase{
		descriptors: uint16(d1)<<8 | uint16(d2),
		data:        append([]byte(nil), payload...),
		hashes:      parent.Hash(),
		depths:      []uint16{1},
		refs:        refs,
		children:    children,
		loader: func(h Hash) (*Cell, error) {
			child, ok := byHash[h]
			if !ok {
				return nil, errors.New("unknown child")
			}
			return child, nil
		},
	}
}

func requireSamePlaceholder(t *testing.T, i int, reference, got *Cell) {
	t.Helper()
	if got == nil {
		t.Fatalf("ref %d missing", i)
	}
	if !bytes.Equal(reference.data, got.data) || reference.bitsSz != got.bitsSz || reference.flags != got.flags {
		t.Fatalf("ref %d placeholder differs from createLazyPrunedRef's", i)
	}
	if reference.hash0 != got.hash0 || reference.depth0 != got.depth0 {
		t.Fatalf("ref %d hash/depth differs", i)
	}
	if (reference.meta == nil) != (got.meta == nil) {
		t.Fatalf("ref %d meta presence differs", i)
	}
	if reference.meta != nil && (reference.meta.lazyLoader == nil) != (got.meta.lazyLoader == nil) {
		t.Fatalf("ref %d loader presence differs", i)
	}
}

func TestSlabCreateMatchesPlaceholderConstructor(t *testing.T) {
	for refCnt := 0; refCnt <= 4; refCnt++ {
		for _, special := range []bool{false, true} {
			t.Run(fmt.Sprintf("refs%d_special%v", refCnt, special), func(t *testing.T) {
				tc := buildSlabCase(t, refCnt, special)

				slab, err := CreateWithLazyRefsUnsafe(
					tc.descriptors, tc.data, tc.hashes, tc.depths, tc.refs, tc.loader,
				)
				if err != nil {
					t.Fatal(err)
				}

				// The constructor copies: mutating the caller's buffer must not
				// reach the cell. This is the contract the slab change added.
				tc.data[0] ^= 0xFF
				if slab.data[0] == tc.data[0] {
					t.Fatalf("cell aliases the caller's buffer")
				}
				tc.data[0] ^= 0xFF

				// Placeholders must stay byte-for-byte what createLazyPrunedRef
				// builds — it still constructs them for the trusted BoC loader,
				// so the two paths drifting apart would split placeholder
				// behaviour by origin.
				for i := 0; i < refCnt; i++ {
					reference, err := createLazyPrunedRef(tc.refs[i], tc.loader)
					if err != nil {
						t.Fatal(err)
					}
					requireSamePlaceholder(t, i, reference, slab.refs[i])
				}

				// Behavioural half: every reference resolves through the loader
				// to the child it stands for.
				for i := 0; i < refCnt; i++ {
					sSlice, err := slab.BeginParse()
					if err != nil {
						t.Fatal(err)
					}
					for skip := 0; skip < i; skip++ {
						if _, err = sSlice.LoadRefCell(); err != nil {
							t.Fatal(err)
						}
					}
					sChild, err := sSlice.LoadRefCell()
					if err != nil {
						t.Fatal(err)
					}
					if sChild.HashKey() != tc.children[i].HashKey() {
						t.Fatalf("ref %d resolved to a different child", i)
					}
				}
			})
		}
	}
}

// A child above level 0 does not fit the slab's pruned buffer and must fall
// back to its own payload allocation while staying behaviourally identical.
func TestSlabCreateHighLevelRefFallsBack(t *testing.T) {
	tc := buildSlabCase(t, 1, false)
	tc.refs[0] = LazyRef{
		LevelMask: LevelMask{Mask: 0b1},
		Hashes:    append(append([]byte(nil), tc.children[0].Hash()...), tc.children[0].Hash()...),
		Depths:    []uint16{0, 0},
	}

	slab, err := CreateWithLazyRefsUnsafe(tc.descriptors, tc.data, tc.hashes, tc.depths, tc.refs, tc.loader)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := createLazyPrunedRef(tc.refs[0], tc.loader)
	if err != nil {
		t.Fatal(err)
	}
	requireSamePlaceholder(t, 0, reference, slab.refs[0])
}

func TestSlabSizes(t *testing.T) {
	t.Logf("Cell=%dB cellMeta=%dB lazySlab2=%dB lazySlab4=%dB",
		unsafe.Sizeof(Cell{}), unsafe.Sizeof(cellMeta{}),
		unsafe.Sizeof(lazySlab2{}), unsafe.Sizeof(lazySlab4{}))
}

// The whole point of the slab, pinned: one allocation with references, two
// without (exact-size body + cell), two when a high-level child overflows the
// pruned buffer. A regression to per-object construction fails here before any
// profile would show it.
func TestSlabCreateAllocations(t *testing.T) {
	for _, tc := range []struct {
		name string
		refs int
		want float64
	}{
		{"refs0", 0, 2},
		{"refs2", 2, 1},
		{"refs4", 4, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := buildSlabCase(t, tc.refs, false)
			got := testing.AllocsPerRun(200, func() {
				if _, err := CreateWithLazyRefsUnsafe(c.descriptors, c.data, c.hashes, c.depths, c.refs, c.loader); err != nil {
					t.Fatal(err)
				}
			})
			if got != tc.want {
				t.Fatalf("allocations per construction = %v, want %v", got, tc.want)
			}
		})
	}
}

func benchSlabCreate(b *testing.B, refCnt int) {
	tc := buildSlabCase(b, refCnt, false)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := CreateWithLazyRefsUnsafe(tc.descriptors, tc.data, tc.hashes, tc.depths, tc.refs, tc.loader); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLazyCreate2Refs(b *testing.B) { benchSlabCreate(b, 2) }
func BenchmarkLazyCreate4Refs(b *testing.B) { benchSlabCreate(b, 4) }
