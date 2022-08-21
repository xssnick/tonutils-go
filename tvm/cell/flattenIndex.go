package cell

import "log"

func flattenIndex(src []*Cell) []*Cell {
	pending := src
	allCells := map[string]*Cell{}
	notPermCells := map[string]struct{}{}
	sorted := []string{}

	for len(pending) > 0 {
		cells := append([]*Cell{}, pending...)
		pending = []*Cell{}

		for _, cell := range cells {
			hash := string(cell.Hash())
			if _, ok := allCells[hash]; ok {
				continue
			}

			notPermCells[hash] = struct{}{}
			allCells[hash] = cell

			pending = append(pending, cell.refs...)
		}
	}

	tempMark := map[string]bool{}
	var visit func(hash string)
	visit = func(hash string) {
		if _, ok := notPermCells[hash]; !ok {
			return
		}

		if tempMark[hash] {
			log.Println("Unknown branch, hash exists")
			return
		}

		tempMark[hash] = true

		for _, c := range allCells[hash].refs {
			visit(string(c.Hash()))
		}

		sorted = append([]string{hash}, sorted...)
		delete(tempMark, hash)
		delete(notPermCells, hash)
	}

	for len(notPermCells) > 0 {
		for k := range notPermCells {
			visit(k)
			break
		}
	}

	indexes := map[string]int{}
	for i := 0; i < len(sorted); i++ {
		indexes[sorted[i]] = i
	}

	result := []*Cell{}
	for _, ent := range sorted {
		rrr := allCells[ent]
		rrr.index = indexes[string(rrr.Hash())]

		for _, ref := range rrr.refs {
			ref.index = indexes[string(ref.Hash())]
		}
		result = append(result, rrr)
	}

	return result
}
