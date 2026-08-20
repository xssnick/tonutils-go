package cell

import (
	"errors"
	"fmt"
)

// ErrCannotCloneDetachedCell reports a cell graph whose runtime view cannot be
// reproduced by an independently owned eager graph.
var ErrCannotCloneDetachedCell = errors.New("cell graph cannot be cloned detached")

type detachedCellMeta struct {
	meta   cellMeta
	hashes [3]Hash
}

// CloneDetached copies the eager graph reachable from c into independently
// owned cell and payload arenas. It preserves the finalized hashes, depths,
// flags, types and reference sharing without recalculating hashes or parsing a
// BOC. Lazy, virtualized and traced cells are rejected because copying their
// runtime loaders or views would retain external ownership and change the
// semantics of the clone.
func (c *Cell) CloneDetached() (*Cell, error) {
	index := make(map[*Cell]int, 1024)
	index[c] = 0
	sources := make([]*Cell, 1, 1024)
	sources[0] = c
	payloadSize := 0
	extraMetaCount := 0

	for sourceIndex := 0; sourceIndex < len(sources); sourceIndex++ {
		source := sources[sourceIndex]
		if err := validateDetachedCloneSource(source); err != nil {
			return nil, fmt.Errorf("%w: cell %d: %v", ErrCannotCloneDetachedCell, sourceIndex, err)
		}

		payloadSize += len(source.data)
		if source.meta != nil && source.meta.extraHashes != nil {
			extraMetaCount++
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
	metas := make([]detachedCellMeta, extraMetaCount)
	payloadOffset := 0
	metaOffset := 0

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
			meta.hashes = *source.meta.extraHashes
			meta.meta.extraHashes = &meta.hashes
			meta.meta.extraDepths = source.meta.extraDepths
			dst.meta = &meta.meta
			metaOffset++
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
