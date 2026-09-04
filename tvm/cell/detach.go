package cell

import (
	"errors"
	"fmt"
)

// ErrCannotCloneDetachedCell reports a cell graph whose runtime view cannot be
// reproduced by an independently owned eager graph.
var ErrCannotCloneDetachedCell = errors.New("cell graph cannot be cloned detached")

// detachedCloneMinCells is the sizing CloneDetached starts from when the
// caller has no better estimate of the reachable graph.
const detachedCloneMinCells = 1024

// CloneDetached copies the eager graph reachable from c into independently
// owned cell and payload arenas. It preserves the finalized hashes, depths,
// flags, types and reference sharing without recalculating hashes or parsing a
// BOC. Lazy, virtualized and traced cells are rejected because copying their
// runtime loaders or views would retain external ownership and change the
// semantics of the clone.
func (c *Cell) CloneDetached() (*Cell, error) {
	return c.CloneDetachedSized(0)
}

// CloneDetachedSized is CloneDetached for a caller that knows, or bounds from
// above, how many cells the graph reachable from c has: the visited index and
// the source list are sized to it once instead of growing by doubling from a
// thousand entries, which on a 18k-cell block was a third of the bytes the
// clone allocated. The output is the same graph for every value of the hint,
// and a zero or negative hint keeps the default sizing. The hint must come
// from something already bounded — a parsed BoC's declared cell count is,
// because the parser has admitted exactly that many cells.
func (c *Cell) CloneDetachedSized(cellsHint int) (*Cell, error) {
	if cellsHint < detachedCloneMinCells {
		cellsHint = detachedCloneMinCells
	}
	index := make(map[*Cell]int, cellsHint)
	index[c] = 0
	sources := make([]*Cell, 1, cellsHint)
	sources[0] = c
	payloadSize := 0
	extraMetaCount := 0
	extraHashSlots := 0

	for sourceIndex := 0; sourceIndex < len(sources); sourceIndex++ {
		source := sources[sourceIndex]
		if err := validateDetachedCloneSource(source); err != nil {
			return nil, fmt.Errorf("%w: cell %d: %v", ErrCannotCloneDetachedCell, sourceIndex, err)
		}

		payloadSize += len(source.data)
		if source.meta != nil && source.meta.extraHashes != nil {
			extraMetaCount++
			extraHashSlots += source.extraHashSlots()
		}

		for i := 0; i < source.refsCount(); i++ {
			ref := source.refs[i]
			if _, exists := index[ref]; exists {
				continue
			}

			index[ref] = len(sources)
			sources = append(sources, ref)
		}
	}

	cells := make([]Cell, len(sources))
	payload := make([]byte, payloadSize)
	metas := make([]cellMeta, extraMetaCount)
	// The extra hashes are packed into one slab of exactly the slots the cells
	// use, on the layout prewireParsedExtraHashes describes: a level-1 cell
	// carries one hash above level 0, not three.
	var hashes []Hash
	if extraMetaCount > 0 {
		hashes = make([]Hash, extraHashSlots+extraHashWindowSpare)
	}
	payloadOffset := 0
	metaOffset := 0
	hashOffset := 0

	for i, source := range sources {
		dst := &cells[i]
		*dst = *source
		dst.refs = [4]*Cell{}
		dst.meta = nil
		if len(source.data) > 0 {
			data := payload[payloadOffset : payloadOffset+len(source.data)]
			copy(data, source.data)
			dst.data = data[:len(data):len(data)]
			payloadOffset += len(data)
		} else {
			dst.data = nil
		}

		if source.meta != nil && source.meta.extraHashes != nil {
			meta := &metas[metaOffset]
			slots := source.extraHashSlots()
			window := extraHashWindow(hashes, hashOffset)
			copy(window[:slots], source.meta.extraHashes[:slots])
			meta.extraHashes = window
			meta.extraDepths = source.meta.extraDepths
			dst.meta = meta
			metaOffset++
			hashOffset += slots
		}
	}

	for i, source := range sources {
		refCount := source.refsCount()
		for ref := 0; ref < refCount; ref++ {
			cells[i].refs[ref] = &cells[index[source.refs[ref]]]
		}
	}

	return &cells[0], nil
}

func validateDetachedCloneSource(c *Cell) error {
	if c.IsLazy() {
		return errors.New("lazy cell")
	}
	if c.IsVirtualized() {
		return errors.New("virtualized cell")
	}
	if c.meta == nil {
		return nil
	}
	if c.meta.trace != nil {
		return errors.New("traced cell")
	}
	if c.meta.viewOf != nil || c.meta.viewLevel != 0 {
		return errors.New("cell view")
	}
	if c.meta.lazyLoader != nil || c.meta.skipLazyRefValidation {
		return errors.New("loader-backed cell")
	}
	if c.meta.extraHashes == nil && c.meta.extraDepths != [3]uint16{} {
		return errors.New("depth metadata without hashes")
	}

	return nil
}
