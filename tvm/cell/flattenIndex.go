package cell

import (
	"sort"
)

type idxItem struct {
	index uint64
	cell  *Cell
}

func flattenIndex(cells []*Cell) ([]*idxItem, map[string]*idxItem) {
	index := map[string]*idxItem{}

	idx := uint64(0)
	for len(cells) > 0 {
		next := make([]*Cell, 0, len(cells)*4)
		for _, p := range cells {
			// move cell forward in boc, because behind reference is not allowed
			index[string(p.Hash())] = &idxItem{
				index: idx,
				cell:  p,
			}
			idx++

			next = append(next, p.refs...)
		}
		cells = next
	}

	idxSlice := make([]*idxItem, 0, len(index))
	for _, id := range index {
		idxSlice = append(idxSlice, id)
	}

	sort.Slice(idxSlice, func(i, j int) bool {
		return idxSlice[i].index < idxSlice[j].index
	})

	for i, id := range idxSlice {
		// remove gaps in indexes
		id.index = uint64(i)
	}

	return idxSlice, index
}
