package cell_test

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

var proofLazyBenchmarkSink *cell.Cell

type proofBenchmarkPager struct {
	bodies map[cell.Hash]*cell.Cell
	loads  int
}

func proofBenchmarkLazyRef(c *cell.Cell) cell.LazyRef {
	ref := cell.LazyRef{LevelMask: c.LevelMask()}
	for level := 0; level <= c.Level(); level++ {
		if ref.LevelMask.IsSignificant(level) {
			ref.Hashes = append(ref.Hashes, c.Hash(level)...)
			ref.Depths = append(ref.Depths, c.Depth(level))
		}
	}
	return ref
}

func (p *proofBenchmarkPager) page(b *testing.B, c *cell.Cell) *cell.Cell {
	b.Helper()
	hash := c.HashKey()
	if cached := p.bodies[hash]; cached != nil {
		return cached
	}
	refs := make([]cell.LazyRef, c.RefsNum())
	for i := range refs {
		ref := c.MustPeekRef(i)
		p.page(b, ref)
		refs[i] = proofBenchmarkLazyRef(ref)
	}
	data := make([]byte, c.SerializedBOCBodySize())
	c.SerializeBOCBodyTo(data)
	d1 := byte(c.RefsNum()) | c.LevelMask().Mask<<5
	if c.IsSpecial() {
		d1 |= 8
	}
	d2 := byte(c.BitsSize() / 8 * 2)
	if c.BitsSize()%8 != 0 {
		d2++
	}
	meta := proofBenchmarkLazyRef(c)
	loaded, err := cell.CreateWithLazyRefsUnsafe(uint16(d1)<<8|uint16(d2), data, meta.Hashes, meta.Depths, refs,
		func(hash cell.Hash) (*cell.Cell, error) {
			p.loads++
			return p.bodies[hash], nil
		})
	if err != nil {
		b.Fatal(err)
	}
	p.bodies[hash] = loaded
	return loaded
}

func BenchmarkFastProofCombineLazyDAG(b *testing.B) {
	left := cell.BeginCell().MustStoreUInt(1, 8).MustStoreRef(cell.BeginCell().MustStoreUInt(11, 8).EndCell()).EndCell()
	right := cell.BeginCell().MustStoreUInt(2, 8).MustStoreRef(cell.BeginCell().MustStoreUInt(22, 8).EndCell()).EndCell()
	root := cell.BeginCell().MustStoreRef(left).MustStoreRef(right).EndCell()
	makeProof := func(index int) *cell.Cell {
		skeleton := cell.CreateProofSkeleton()
		skeleton.ProofRef(index).SetRecursive()
		proof, err := root.CreateProof(skeleton)
		if err != nil {
			b.Fatal(err)
		}
		body, err := cell.UnwrapProof(proof, root.Hash())
		if err != nil {
			b.Fatal(err)
		}
		for range 16 {
			body = cell.BeginCell().MustStoreRef(body).MustStoreRef(body).EndCell()
		}
		return body
	}
	left, right = makeProof(0), makeProof(1)
	for _, name := range []string{"resident", "lazy"} {
		b.Run(name, func(b *testing.B) {
			pager := proofBenchmarkPager{bodies: make(map[cell.Hash]*cell.Cell)}
			a, z := left, right
			if name == "lazy" {
				a, z = pager.page(b, left), pager.page(b, right)
			}
			b.ReportAllocs()
			for b.Loop() {
				result, err := cell.CombineMerkleProofFastRaw(a, z)
				if err != nil {
					b.Fatal(err)
				}
				proofLazyBenchmarkSink = result
			}
			b.ReportMetric(float64(pager.loads)/float64(b.N), "loads/op")
		})
	}
}

func BenchmarkRecordRecursiveLazyDAG(b *testing.B) {
	root := cell.BeginCell().MustStoreUInt(1, 8).EndCell()
	for range 16 {
		root = cell.BeginCell().MustStoreRef(root).MustStoreRef(root).EndCell()
	}
	pager := proofBenchmarkPager{bodies: make(map[cell.Hash]*cell.Cell)}
	root = pager.page(b, root)
	b.ReportAllocs()
	for b.Loop() {
		reads := cell.NewReadSet(root)
		if err := reads.RecordRecursive(root); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(pager.loads)/float64(b.N), "loads/op")
}
