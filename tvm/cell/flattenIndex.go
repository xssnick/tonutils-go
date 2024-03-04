package cell

import (
	"sort"
)

type idxItem struct {
	index     uint64
	dataIndex int
	repeats   int
	withHash  bool
	cell      *Cell
}

func flattenIndex(cells []*Cell, withTopHash, withIntHashes bool) ([]*idxItem, map[string]*idxItem) {
	index := map[string]*idxItem{}

	// TODO: withIntHashes

	idx := uint64(0)
	for len(cells) > 0 {
		next := make([]*Cell, 0, len(cells)*4)
		for _, p := range cells {
			hash := string(p.Hash())

			if v, ok := index[hash]; ok {
				v.repeats++
				continue
			}

			// move cell forward in boc, because behind reference is not allowed
			index[hash] = &idxItem{
				cell:     p,
				index:    idx,
				withHash: withTopHash,
			}
			idx++
			next = append(next, p.refs...)
		}
		withTopHash = false // only once, for roots
		cells = next
	}

	idxSlice := make([]*idxItem, 0, len(index))
	for _, id := range index {
		idxSlice = append(idxSlice, id)
	}

	for verifyOrder := true; verifyOrder; {
		verifyOrder = false

		for _, id := range idxSlice {
			for _, ref := range id.cell.refs {
				idRef := index[string(ref.Hash())]

				if idRef.index < id.index {
					// if we found that ref index is behind parent,
					// move ref index forward
					idRef.index = idx
					idx++

					// we changed index, so we need to verify order again
					verifyOrder = true
				}
			}
		}
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
