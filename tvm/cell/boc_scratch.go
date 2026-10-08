package cell

import (
	"fmt"
	"io"
	"sync"
)

// BOCScratch owns the serializer's temporary cell list, hash index and reorder
// table. Its zero value is ready for use. Reusing one scratch removes those
// allocations while every returned BoC remains independently owned by the
// caller.
//
// A scratch is not safe for concurrent use. Use one per serialization that can
// overlap another and do not copy one after its first use; the package-level
// serialization functions do this through an internal pool.
type BOCScratch struct {
	noCopy     noCopy
	serializer bocSerializer
	index      bocHashIndex
}

// noCopy makes go vet reject copies of state that owns reusable backing arrays.
// See sync.noCopy in the standard library.
type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

// Serialize writes a canonical BoC into a newly owned byte slice while reusing
// the temporary traversal storage retained by s.
func (s *BOCScratch) Serialize(roots []*Cell, opts BOCSerializeOptions) ([]byte, error) {
	bag, err := s.prepare(roots, opts.CellsCountHint)
	if err != nil {
		s.clearCells()
		return nil, err
	}
	if bag == nil {
		return nil, nil
	}
	defer s.clearCells()

	boc := bag.serialize(opts.mode())
	if boc == nil {
		return nil, fmt.Errorf("failed to serialize boc")
	}
	return boc, nil
}

func (s *BOCScratch) append(dst []byte, roots []*Cell, opts BOCSerializeOptions) ([]byte, error) {
	bag, err := s.prepare(roots, opts.CellsCountHint)
	if err != nil {
		s.clearCells()
		return nil, err
	}
	if bag == nil {
		return dst, nil
	}
	defer s.clearCells()

	return bag.appendTo(dst, opts.mode())
}

func (s *BOCScratch) write(w io.Writer, roots []*Cell, opts BOCSerializeOptions) error {
	bag, err := s.prepare(roots, opts.CellsCountHint)
	if err != nil {
		s.clearCells()
		return err
	}
	if bag == nil {
		return nil
	}
	defer s.clearCells()

	return bag.writeTo(w, opts.mode())
}

func (s *BOCScratch) fileHash(root *Cell, opts BOCSerializeOptions) []byte {
	bag, err := s.prepare([]*Cell{root}, opts.CellsCountHint)
	if err != nil || bag == nil {
		s.clearCells()
		return nil
	}
	defer s.clearCells()

	fileHash, ok := bag.computeFileHash(opts.mode())
	if !ok {
		return nil
	}
	return fileHash
}

func (s *BOCScratch) prepare(roots []*Cell, cellsCountHint int) (*bocSerializer, error) {
	if len(roots) == 0 {
		return nil, nil
	}

	bag := &s.serializer
	s.clearCells()
	bag.cellCount = 0
	bag.intRefs = 0
	bag.intHashes = 0
	bag.topHashes = 0
	bag.maxDepth = maxDepth
	bag.dataBytes = 0

	if cap(bag.roots) < len(roots) {
		bag.roots = make([]bocRoot, len(roots))
	} else {
		bag.roots = bag.roots[:len(roots)]
	}
	if cellsCountHint > cap(bag.cellList) {
		bag.cellList = make([]bocSerializeItem, 0, cellsCountHint)
	} else {
		bag.cellList = bag.cellList[:0]
	}
	s.index.reset(cellsCountHint)
	bag.cellIndex = &s.index

	for i, root := range roots {
		bag.roots[i] = bocRoot{cell: root, idx: bocInvalidCellIndex}
	}
	if err := bag.importCells(); err != nil {
		return nil, err
	}
	return bag, nil
}

// clearCells is the ownership boundary between calls. The scratch may keep
// large scalar buffers, but it must not keep an old block DAG alive while it is
// idle in the pool.
func (s *BOCScratch) clearCells() {
	bag := &s.serializer
	clear(bag.cellList)
	clear(bag.roots)
	bag.cellList = bag.cellList[:0]
	bag.roots = bag.roots[:0]
	bag.cellIndex = nil
}

// Avoid retaining the exceptional giant-BoC working set in a process-wide
// pool. Mainnet-sized block and candidate serializations stay well below this
// cap and are the repeated workload the pool is for.
const (
	bocScratchMaxRetainedCells = 1 << 16
	bocScratchMaxRetainedRoots = 1 << 10
)

var bocScratchPool sync.Pool

func acquireBOCScratch() *BOCScratch {
	scratch, _ := bocScratchPool.Get().(*BOCScratch)
	if scratch == nil {
		return new(BOCScratch)
	}
	return scratch
}

func releaseBOCScratch(scratch *BOCScratch) {
	scratch.clearCells()
	if cap(scratch.serializer.cellList) > bocScratchMaxRetainedCells ||
		cap(scratch.serializer.roots) > bocScratchMaxRetainedRoots {
		return
	}
	bocScratchPool.Put(scratch)
}
