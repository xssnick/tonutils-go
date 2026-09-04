package cell

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

// ParallelBOCMinCells sets the minimum number of cells in a parsed BoC before
// hash finalization is spread across CPU cores. Set it to 0 or a negative
// value to always finalize sequentially.
var ParallelBOCMinCells = 16384

const (
	bocFinalizeWaveMinParallel = 1024
	bocFinalizeChunkSize       = 1024
)

func finalizeParsedCells(cells []Cell, rootsIndex []uint32, stored []storedHashesDepths, options BOCParseOptions) ([]*Cell, []Cell, error) {
	prewireParsedExtraHashes(cells)

	var err error
	if threshold := ParallelBOCMinCells; threshold > 0 && len(cells) >= threshold && runtime.GOMAXPROCS(0) > 1 {
		err = finalizeCellsParallel(cells, stored, options)
	} else {
		err = finalizeCellsSequential(cells, stored, options)
	}
	if err != nil {
		return nil, nil, err
	}

	roots := make([]*Cell, len(rootsIndex))
	for i, idx := range rootsIndex {
		roots[i] = &cells[idx]
	}
	return roots, cells, nil
}

// prewireParsedExtraHashes pre-allocates extra hash storage for every cell
// that will need it in two batch allocations instead of two small allocations
// per cell during hash finalization. Pruned cells keep higher-level hashes in
// their payload and never use the extra storage.
//
// The storage is packed. A cell with level mask m computes popcount(m)+1
// hashes and keeps the ones above level 0 in extraHashes, so it uses exactly
// popcount(m) of the three slots — one for the level-1 cells that make up the
// entire spine of a Merkle proof. Each cell's extraHashes therefore points at
// a window of one shared slab that is precisely that long, rather than at its
// own [3]Hash: the windows overlap in the type of the pointer but never in the
// bytes a cell touches. That holds because every reader and writer of the
// slots derives its index from the cell's own level mask, which the parser set
// from the descriptor byte and finalization never changes: calculateHashes and
// applyTrustedStoredHashesDepths write slots 0..popcount(m)-1 and getHash reads
// the same range (applyTrustedStoredHashesDepths writes only hash0 on a pruned
// cell, and a pruned cell gets no window here). The two spare trailing slots
// keep the *[3]Hash conversion of the last window inside the slab. Copies of a
// cell's metadata — cloneCellMeta, CloneDetached, the merkle-update tables —
// read a window whole, and what they read past the cell's own slots is bytes
// they never look at again by the same index rule.
//
// Measured on a received mainnet candidate (23k cells, 15k of them above
// level 0), the slab is 0.5 MB instead of 1.4 MB.
func prewireParsedExtraHashes(cells []Cell) {
	extra, slots := 0, 0
	for i := range cells {
		c := &cells[i]
		if c.flags&cellFlagLevelMaskMask == 0 {
			continue
		}
		if c.resolveType() != PrunedCellType {
			extra++
			slots += c.getLevelMask().getHashIndex()
		}
	}
	if extra == 0 {
		return
	}

	metas := make([]cellMeta, extra)
	hashes := make([]Hash, slots+extraHashWindowSpare)
	k, off := 0, 0
	for i := range cells {
		c := &cells[i]
		if c.flags&cellFlagLevelMaskMask == 0 || c.GetType() == PrunedCellType {
			continue
		}
		metas[k].extraHashes = extraHashWindow(hashes, off)
		c.meta = &metas[k]
		off += c.getLevelMask().getHashIndex()
		k++
	}
}

// extraHashWindowSpare is how many slots a packed extra-hash slab carries past
// its last window, so that the window can be viewed as a *[3]Hash whatever its
// real length. A level mask is at most three bits, so a window is at most
// three slots and needs at most two of slack.
const extraHashWindowSpare = 2

// extraHashWindow is the *[3]Hash view of the packed window starting at off.
// The slab must extend at least three slots past off; see extraHashWindowSpare.
func extraHashWindow(slab []Hash, off int) *[3]Hash {
	return (*[3]Hash)(slab[off : off+3])
}

// extraHashSlots is how many of a cell's extraHashes slots it uses: one per
// significant level above zero. It is the window length of the packed layout
// and the number of slots a copy of the metadata has to carry over.
func (c *Cell) extraHashSlots() int {
	return c.getLevelMask().getHashIndex()
}

func finalizeParsedCell(c *Cell, storedMeta *storedHashesDepths, options BOCParseOptions) error {
	if options.TrustedHashes && storedMeta != nil {
		if err := applyTrustedStoredHashesDepths(c, *storedMeta); err != nil {
			return err
		}
	} else {
		if err := c.calculateHashes(); err != nil {
			return err
		}
		if storedMeta != nil {
			if err := validateStoredHashesDepths(c, *storedMeta); err != nil {
				return err
			}
		}
	}
	return validateLoadedCell(c)
}

func finalizeCellsSequential(cells []Cell, stored []storedHashesDepths, options BOCParseOptions) error {
	storedIdx := len(stored) - 1
	for idx := len(cells) - 1; idx >= 0; idx-- {
		var storedMeta *storedHashesDepths
		if storedIdx >= 0 && stored[storedIdx].cellIndex == idx {
			storedMeta = &stored[storedIdx]
			storedIdx--
		}
		if err := finalizeParsedCell(&cells[idx], storedMeta, options); err != nil {
			return fmt.Errorf("invalid cell #%d: %w", idx, err)
		}
	}
	return nil
}

// finalizeCellsParallel finalizes cells wave by wave: cells of the same
// subtree height never depend on each other's hashes, so each wave is hashed
// concurrently while children are always finalized a wave earlier.
func finalizeCellsParallel(cells []Cell, stored []storedHashesDepths, options BOCParseOptions) error {
	// Compute structural heights using depth0 as scratch space; finalization
	// overwrites depth0 of every cell with the real depth later. Refs always
	// point to higher indexes, so the reverse pass sees children first.
	maxHeight := uint16(0)
	for idx := len(cells) - 1; idx >= 0; idx-- {
		c := &cells[idx]
		var height uint16
		refCnt := c.refsCount()
		for r := 0; r < refCnt; r++ {
			if h := c.refs[r].depth0 + 1; h > height {
				height = h
			}
		}
		// cell depth is at least the structural height at every level, so
		// deeper chains cannot pass depth validation anyway
		if height > maxDepth {
			return fmt.Errorf("invalid cell #%d: %w", idx, ErrCellDepthLimit)
		}
		c.depth0 = height
		if height > maxHeight {
			maxHeight = height
		}
	}

	return runCellWavesParallel(len(cells), int(maxHeight), runtime.GOMAXPROCS(0),
		func(i int) uint16 { return cells[i].depth0 },
		func(idx int) error {
			if err := finalizeParsedCell(&cells[idx], storedMetaFor(stored, idx), options); err != nil {
				return fmt.Errorf("invalid cell #%d: %w", idx, err)
			}
			return nil
		})
}

// runCellWavesParallel runs fn for every index in [0, n) wave by wave in
// increasing height order: indexes of the same height run concurrently while
// all lower waves are guaranteed to be finished. heightAt is fully consumed
// before the first fn call.
func runCellWavesParallel(n, maxHeight, workers int, heightAt func(i int) uint16, fn func(idx int) error) error {
	// counting sort of cell indexes into height waves
	waveEnds := make([]uint32, maxHeight+2)
	for i := 0; i < n; i++ {
		waveEnds[int(heightAt(i))+1]++
	}
	for h := 1; h < len(waveEnds); h++ {
		waveEnds[h] += waveEnds[h-1]
	}
	order := make([]uint32, n)
	fill := append([]uint32(nil), waveEnds[:len(waveEnds)-1]...)
	for i := 0; i < n; i++ {
		h := int(heightAt(i))
		order[fill[h]] = uint32(i)
		fill[h]++
	}

	var failed atomic.Bool
	var errOnce sync.Once
	var firstErr error

	for h := 0; h <= maxHeight; h++ {
		wave := order[waveEnds[h]:waveEnds[h+1]]
		if len(wave) < bocFinalizeWaveMinParallel {
			for _, ci := range wave {
				if err := fn(int(ci)); err != nil {
					return err
				}
			}
			continue
		}

		chunks := (len(wave) + bocFinalizeChunkSize - 1) / bocFinalizeChunkSize
		waveWorkers := workers
		if waveWorkers > chunks {
			waveWorkers = chunks
		}

		var cursor atomic.Int64
		var wg sync.WaitGroup
		wg.Add(waveWorkers)
		for w := 0; w < waveWorkers; w++ {
			go func() {
				defer wg.Done()
				for !failed.Load() {
					ck := int(cursor.Add(1) - 1)
					if ck >= chunks {
						return
					}
					lo := ck * bocFinalizeChunkSize
					hi := lo + bocFinalizeChunkSize
					if hi > len(wave) {
						hi = len(wave)
					}
					for _, ci := range wave[lo:hi] {
						if err := fn(int(ci)); err != nil {
							errOnce.Do(func() {
								firstErr = err
							})
							failed.Store(true)
							return
						}
					}
				}
			}()
		}
		wg.Wait()
		if failed.Load() {
			return firstErr
		}
	}
	return nil
}

// storedMetaFor finds serialized hash metadata for a cell index; stored is
// sorted by cellIndex ascending.
func storedMetaFor(stored []storedHashesDepths, idx int) *storedHashesDepths {
	if len(stored) == 0 {
		return nil
	}

	lo, hi := 0, len(stored)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if stored[mid].cellIndex < idx {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(stored) && stored[lo].cellIndex == idx {
		return &stored[lo]
	}
	return nil
}
