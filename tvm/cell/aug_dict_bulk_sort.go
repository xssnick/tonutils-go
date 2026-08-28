package cell

import (
	"slices"
	"sync"
)

// Keep the repeated working set, but do not pin an exceptional one-off batch
// in the process-wide pool. The order contains only indexes, so releasing it
// cannot retain key or value cells.
const augBulkSortMaxRetainedItems = 1 << 16

const augBulkInsertionSortThreshold = 32

type augBulkSortScratch struct {
	order []int
}

var augBulkSortPool sync.Pool

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

	scratch, order := acquireAugBulkSortOrder(len(items))
	cursor := bounds
	for i := range items {
		bucket := int(items[i].key.cell.data[0])
		order[cursor[bucket]] = i
		cursor[bucket]++
	}
	applyAugBulkOrder(items, order)
	releaseAugBulkSortOrder(scratch)

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

	scratch, order := acquireAugBulkSortOrder(len(items))
	cursor := bounds
	for i := range items {
		bucket := int(items[i].cell.data[0])
		order[cursor[bucket]] = i
		cursor[bucket]++
	}
	applyAugBulkOrder(items, order)
	releaseAugBulkSortOrder(scratch)

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

// applyAugBulkOrder applies a destination-to-source permutation in place. The
// permutation itself doubles as the visited set: a negative entry marks a
// destination already fixed by its cycle.
func applyAugBulkOrder[T any](items []T, order []int) {
	for start := range order {
		next := order[start]
		if next < 0 {
			continue
		}
		if next == start {
			order[start] = -1
			continue
		}

		saved := items[start]
		current := start
		for next != start {
			items[current] = items[next]
			order[current] = -1
			current = next
			next = order[current]
		}
		items[current] = saved
		order[current] = -1
	}
}

func acquireAugBulkSortOrder(items int) (*augBulkSortScratch, []int) {
	scratch, _ := augBulkSortPool.Get().(*augBulkSortScratch)
	if scratch == nil {
		scratch = new(augBulkSortScratch)
	}
	if cap(scratch.order) < items {
		scratch.order = make([]int, items)
	} else {
		scratch.order = scratch.order[:items]
	}
	return scratch, scratch.order
}

func releaseAugBulkSortOrder(scratch *augBulkSortScratch) {
	scratch.order = scratch.order[:0]
	if cap(scratch.order) <= augBulkSortMaxRetainedItems {
		augBulkSortPool.Put(scratch)
	}
}
