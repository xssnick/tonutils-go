package cell

import (
	"sort"
)

func flattenIndex(src []*Cell) []*Cell {
	pending := src
	allCells := map[string]*Cell{}

	idx := 0
	var cells []*Cell
	for len(pending) > 0 {
		var next []*Cell
		for _, p := range pending {
			hash := string(p.Hash())
			if ps, ok := allCells[hash]; ok {
				// move cell forward in boc, because behind reference is not allowed
				ps.index, p.index = idx, idx
				idx++

				// we also need to move refs
				next = append(next, p.refs...)
				continue
			}

			p.index = idx
			idx++

			allCells[hash] = p
			cells = append(cells, p)

			next = append(next, p.refs...)
		}
		pending = next
	}

	sort.Slice(cells, func(i, j int) bool {
		return cells[i].index < cells[j].index
	})

	for i, cell := range cells {
		// remove possible gaps
		cell.index = i
	}
	return cells
}
