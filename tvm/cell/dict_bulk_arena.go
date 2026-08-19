package cell

// dictBuildArena carves the nodes of a bulk dictionary build out of two
// contiguous slabs — one for the cells, one for their data — instead of
// letting every node take its own size-classed allocation.
//
// A dictionary over n entries has n leaves and n-1 forks, so a build of a
// large account storage stat finalizes thousands of cells whose whole purpose
// is to be handed on as one tree. Allocating them together costs the collector
// two objects instead of a few thousand, and keeps the tree contiguous for the
// hashing pass that immediately walks it.
//
// The tree outlives the build, so the slabs are never recycled: a node that is
// still referenced pins the whole slab. That is the right trade here because a
// dictionary is retained or dropped whole, but it is why the arena is confined
// to the bulk build and is not a general allocator.
type dictBuildArena struct {
	cells []Cell
	data  []byte
}

// newDictBuildArena sizes the slabs for nodes cells holding dataBytes of cell
// data in total. Both bounds are estimates: running past either falls back to
// ordinary allocation, so a wrong estimate costs speed and never correctness.
func newDictBuildArena(nodes, dataBytes int) *dictBuildArena {
	if nodes <= 0 {
		return nil
	}
	return &dictBuildArena{
		cells: make([]Cell, 0, nodes),
		data:  make([]byte, 0, dataBytes),
	}
}

// take returns a zeroed cell and a data window of size bytes, or nil when
// either slab is exhausted.
func (a *dictBuildArena) take(size int) (*Cell, []byte) {
	if a == nil || len(a.cells) == cap(a.cells) || len(a.data)+size > cap(a.data) {
		return nil, nil
	}

	a.cells = a.cells[:len(a.cells)+1]
	cell := &a.cells[len(a.cells)-1]

	start := len(a.data)
	a.data = a.data[:start+size]
	return cell, a.data[start : start+size : start+size]
}

// dictBulkArenaDataBytes estimates the cell data a bulk build over items will
// produce. Every key contributes its bits to the labels along its own path
// exactly once, so the key material is bounded by len(items)*keySz; the rest
// is the values and a generous allowance for per-node label encoding.
func dictBulkArenaDataBytes(items []DictBulkKV, keySz uint) int {
	bits := uint64(len(items)) * uint64(keySz)
	for i := range items {
		if items[i].Value != nil {
			bits += uint64(items[i].Value.BitsUsed())
		}
	}
	// two nodes per entry, each allowed a full long-form label header
	bits += uint64(2*len(items)) * 16
	return int(bits/8) + 128
}

// storeDictNodeArena is storeDictNodeTraced writing its node into the arena.
// With no arena, or once the arena is spent, it falls back to the ordinary
// allocating path.
func storeDictNodeArena(arena *dictBuildArena, label *Slice, payload *Builder, keyLen uint, trace *Trace) (*Cell, error) {
	b := BeginCell().SetTrace(trace)
	if err := storeDictLabel(b, label, keyLen); err != nil {
		return nil, err
	}
	if err := b.StoreBuilderUncheckedDepth(payload); err != nil {
		return nil, err
	}

	cell, buf := arena.take(b.usedBytes())
	if cell == nil {
		return b.EndCellSpecial(false)
	}
	// Same order as EndCellSpecial: a trace that refuses the creation must
	// refuse it before the cell exists.
	if err := b.trace.NotifyCreate(); err != nil {
		return nil, err
	}
	if err := finalizeCellInto(cell, buf, b, false); err != nil {
		return nil, err
	}
	return cell, nil
}

func (d *Dictionary) storeLeafArena(arena *dictBuildArena, keyPfx *Slice, value *Builder, keyOffset uint) (*Cell, error) {
	if value == nil {
		return nil, nil
	}
	return storeDictNodeArena(arena, keyPfx, value, keyOffset, d.trace)
}

func (d *Dictionary) storeForkArena(arena *dictBuildArena, label *Slice, left, right *Cell, keyOffset uint) (*Cell, error) {
	b := BeginCell().SetTrace(d.trace)
	if err := b.StoreRefUncheckedDepth(left); err != nil {
		return nil, err
	}
	if err := b.StoreRefUncheckedDepth(right); err != nil {
		return nil, err
	}
	return storeDictNodeArena(arena, label, b, keyOffset, d.trace)
}
