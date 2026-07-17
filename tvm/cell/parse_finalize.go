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
func prewireParsedExtraHashes(cells []Cell) {
	extra := 0
	for i := range cells {
		c := &cells[i]
		if c.flags&cellFlagLevelMaskMask == 0 {
			continue
		}
		if c.resolveType() != PrunedCellType {
			extra++
		}
	}
	if extra == 0 {
		return
	}

	metas := make([]cellMeta, extra)
	hashes := make([][3]Hash, extra)
	k := 0
	for i := range cells {
		c := &cells[i]
		if c.flags&cellFlagLevelMaskMask == 0 || c.GetType() == PrunedCellType {
			continue
		}
		metas[k].extraHashes = &hashes[k]
		c.meta = &metas[k]
		k++
	}
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
