package cell

import "slices"

const augBulkInsertionSortThreshold = 32

// sortAugBulkItems orders a bulk augmented-dictionary write by its whole key.
// The incoming slices all start at bit zero and carry keySz bits.
//
// Large batches are distributed by their first byte before sorting within each
// bucket. Hash-like dictionary keys spread evenly across those buckets, so the
// comparison-sort work is done on much smaller slices. Recording source
// indexes in input order keeps the sort stable, including for duplicate keys
// that the caller rejects after sorting. Applying that order in place keeps
// the scratch proportional to an index per item instead of a second copy of
// the comparatively large item structs.
func sortAugBulkItems(items []augBulkItem, keySz uint) {
	if len(items) < dictBulkRadixThreshold || keySz < 8 {
		slices.SortStableFunc(items, func(a, b augBulkItem) int {
			return compareKeySlices(&a.key, &b.key)
		})
		return
	}

	var bounds [257]int
	for i := range items {
		bounds[int(items[i].key.cell.data[0])+1]++
	}
	for i := 1; i < len(bounds); i++ {
		bounds[i] += bounds[i-1]
	}

	scratch, order := acquireBulkSortOrder(len(items))
	cursor := bounds
	for i := range items {
		bucket := int(items[i].key.cell.data[0])
		order[cursor[bucket]] = i
		cursor[bucket]++
	}
	applyBulkOrder(items, order)
	releaseBulkSortOrder(scratch)

	for bucket := 0; bucket < 256; bucket++ {
		start, end := bounds[bucket], bounds[bucket+1]
		if end-start < 2 {
			continue
		}
		sortAugBulkItemBucket(items[start:end])
	}
}

// sortAugBulkDeleteKeys is the DeleteMany counterpart of sortAugBulkItems.
// It is kept concrete so the hot counting and scattering loops do not call a
// generic key accessor for every item.
func sortAugBulkDeleteKeys(items []Slice, keySz uint) {
	if len(items) < dictBulkRadixThreshold || keySz < 8 {
		slices.SortStableFunc(items, func(a, b Slice) int {
			return compareKeySlices(&a, &b)
		})
		return
	}

	var bounds [257]int
	for i := range items {
		bounds[int(items[i].cell.data[0])+1]++
	}
	for i := 1; i < len(bounds); i++ {
		bounds[i] += bounds[i-1]
	}

	scratch, order := acquireBulkSortOrder(len(items))
	cursor := bounds
	for i := range items {
		bucket := int(items[i].cell.data[0])
		order[cursor[bucket]] = i
		cursor[bucket]++
	}
	applyBulkOrder(items, order)
	releaseBulkSortOrder(scratch)

	for bucket := 0; bucket < 256; bucket++ {
		start, end := bounds[bucket], bounds[bucket+1]
		if end-start < 2 {
			continue
		}
		sortAugBulkDeleteBucket(items[start:end])
	}
}

func sortAugBulkItemBucket(items []augBulkItem) {
	if len(items) >= augBulkInsertionSortThreshold {
		slices.SortStableFunc(items, func(a, b augBulkItem) int {
			return compareKeySlices(&a.key, &b.key)
		})
		return
	}

	for i := 1; i < len(items); i++ {
		item := items[i]
		j := i
		for j > 0 && compareKeySlices(&item.key, &items[j-1].key) < 0 {
			items[j] = items[j-1]
			j--
		}
		items[j] = item
	}
}

func sortAugBulkDeleteBucket(items []Slice) {
	if len(items) >= augBulkInsertionSortThreshold {
		slices.SortStableFunc(items, func(a, b Slice) int {
			return compareKeySlices(&a, &b)
		})
		return
	}

	for i := 1; i < len(items); i++ {
		item := items[i]
		j := i
		for j > 0 && compareKeySlices(&item, &items[j-1]) < 0 {
			items[j] = items[j-1]
			j--
		}
		items[j] = item
	}
}
