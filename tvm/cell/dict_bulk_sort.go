package cell

import (
	"slices"
	"sync"
)

// dictBulkRadixThreshold is where distributing by the first key byte starts to
// pay for its counting pass and its scratch buffer. Below it the batch is
// small enough that a plain stable sort wins.
const dictBulkRadixThreshold = 256

// Keep the repeated working set, but do not pin an exceptional one-off batch
// in the process-wide pool. The order contains only indexes, so releasing it
// cannot retain key or value cells.
const bulkSortMaxRetainedItems = 1 << 16

type bulkSortScratch struct {
	order []int
}

var bulkSortPool sync.Pool

// sortDictBulkItems orders a bulk batch by key, stably: entries with equal
// keys keep their input order, which is what lets the callers resolve a
// duplicate key in favour of the last item.
//
// Above a threshold the batch is first distributed into 256 buckets by its
// leading key byte and only then sorted inside each bucket. Bulk dictionary
// keys are cell hashes in every hot caller — account storage stats, the
// storage-stat commit — so they are uniformly distributed and the buckets come
// out even, turning one comparison sort of the whole batch into 256 sorts of a
// few hundredths of it. Measured on 4096 hashes it is an order of magnitude
// faster than sorting the batch whole, and the gap widens with the batch,
// because the distribution pass is linear where the comparison sort is not.
//
// A key shorter than one byte cannot be bucketed by its first byte at all --
// the byte holds bits that are not part of the key -- so it takes the plain
// path.
func sortDictBulkItems(items []DictBulkKV, keySz uint) {
	if len(items) < dictBulkRadixThreshold || keySz < 8 {
		slices.SortStableFunc(items, func(a, b DictBulkKV) int {
			return compareDictBulkKeys(a.Key, b.Key, keySz)
		})
		return
	}

	var bounds [257]int
	for i := range items {
		bounds[int(items[i].Key[0])+1]++
	}
	for i := 1; i < len(bounds); i++ {
		bounds[i] += bounds[i-1]
	}

	// Scattering in input order keeps equal keys in input order, so the whole
	// pass stays stable.
	scratch, order := acquireBulkSortOrder(len(items))
	cursor := bounds
	for i := range items {
		bucket := int(items[i].Key[0])
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
		slices.SortStableFunc(items[start:end], func(a, b DictBulkKV) int {
			return compareDictBulkKeys(a.Key, b.Key, keySz)
		})
	}
}

// applyBulkOrder applies a destination-to-source permutation in place. The
// permutation itself doubles as the visited set: a negative entry marks a
// destination already fixed by its cycle.
func applyBulkOrder[T any](items []T, order []int) {
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

func acquireBulkSortOrder(items int) (*bulkSortScratch, []int) {
	scratch, _ := bulkSortPool.Get().(*bulkSortScratch)
	if scratch == nil {
		scratch = new(bulkSortScratch)
	}
	if cap(scratch.order) < items {
		scratch.order = make([]int, items)
	} else {
		scratch.order = scratch.order[:items]
	}
	return scratch, scratch.order
}

func releaseBulkSortOrder(scratch *bulkSortScratch) {
	scratch.order = scratch.order[:0]
	if cap(scratch.order) <= bulkSortMaxRetainedItems {
		bulkSortPool.Put(scratch)
	}
}
