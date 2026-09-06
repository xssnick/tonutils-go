package cell

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"unsafe"
)

var ErrLazyLoaderNotSet = errors.New("lazy pruned ref loader is not set")
var ErrLazyRefNotFound = errors.New("lazy pruned ref not found")
var ErrLazyRefMismatch = errors.New("loaded lazy ref does not match placeholder")

// LazyCellLoader resolves lazy pruned references by the hash of the represented cell.
type LazyCellLoader func(hash Hash) (*Cell, error)

// LazyRef describes a lazy reference placeholder.
// Hashes and depths must be ordered by significant levels, starting from level 0.
type LazyRef struct {
	LevelMask LevelMask
	Hashes    []byte
	Depths    []uint16
}

// CreateWithLazyRefsUnsafe creates a regular cell with trusted precomputed
// hashes and depths, and lazy pruned placeholders for every reference. The
// input is trusted: nothing is recomputed or verified beyond structural sizes.
//
// data is COPIED — the caller may reuse its buffer immediately. The cell, its
// placeholders, their metas and the body copy come out of ONE allocation (two
// for a cell without references, whose exact-size body beats a fixed slab
// buffer): see the slab layout in lazy_slab.go. Measured against the previous
// per-object construction on the storage decode path: 8 -> 1 allocations and
// 300 -> 211 ns for the two-reference cell a state tree averages, and a
// resident decoded cell costs one live object instead of eight, which is what
// the decoded cell cache's GC mark cost is made of.
func CreateWithLazyRefsUnsafe(descriptors uint16, data, hashes []byte, depths []uint16, refs []LazyRef, loader LazyCellLoader) (*Cell, error) {
	dsc1 := byte(descriptors >> 8)
	dsc2 := byte(descriptors)
	refCnt := int(dsc1 & 0b111)
	if len(refs) != refCnt {
		return nil, errors.New("unexpected refs count")
	}

	dataBytes := cellBodyBytesSize(dsc2)
	if len(data) < dataBytes {
		return nil, errors.New("not enough cell data")
	}
	data = data[:dataBytes]

	switch {
	case refCnt == 0:
		body := make([]byte, dataBytes)
		copy(body, data)
		c := &Cell{}
		if err := initSlabRoot(c, body, descriptors, data, hashes, depths); err != nil {
			return nil, err
		}
		return c, nil
	case refCnt <= 2:
		s := &lazySlab2{}
		return buildLazySlab(&s.root, s.body[:], s.refs[:refCnt], s.metas[:refCnt], s.pruned[:refCnt],
			descriptors, data, hashes, depths, refs, loader)
	default:
		s := &lazySlab4{}
		return buildLazySlab(&s.root, s.body[:], s.refs[:refCnt], s.metas[:refCnt], s.pruned[:refCnt],
			descriptors, data, hashes, depths, refs, loader)
	}
}

func cellBodyBytesSize(dsc2 byte) int {
	return int(dsc2/2 + dsc2%2)
}

func cellBodyBitsSize(dsc2 byte, data []byte) (uint16, error) {
	payloadSize := cellBodyBytesSize(dsc2)
	if dsc2%2 == 0 {
		return uint16(payloadSize * 8), nil
	}

	bitsSz, ok := cellBodyBitsSizeFromLast(payloadSize, data[payloadSize-1])
	if !ok {
		return 0, errors.New("invalid cell payload")
	}
	return bitsSz, nil
}

func cellBodyBitsSizeFromLast(payloadSize int, last byte) (uint16, bool) {
	terminatorBit := bits.TrailingZeros8(last)
	if terminatorBit >= 7 {
		return 0, false
	}
	return uint16((payloadSize-1)*8 + 7 - terminatorBit), true
}

func createLazyPrunedRef(ref LazyRef, loader LazyCellLoader) (*Cell, error) {
	actualMask := ref.LevelMask
	if actualMask.Mask > 0b111 {
		return nil, errors.New("invalid lazy ref level mask")
	}
	actualLevel := actualMask.GetLevel()
	hashesCount := actualMask.getHashesCount()
	if len(ref.Hashes) != hashesCount*hashSize {
		return nil, errors.New("invalid lazy ref hashes size")
	}
	if len(ref.Depths) != hashesCount {
		return nil, errors.New("invalid lazy ref depths count")
	}

	storedMask := actualMask
	if actualLevel > 0 {
		storedMask = actualMask.Apply(actualLevel - 1)
	}
	storedCount := storedMask.getHashesCount()
	data := make([]byte, 2+storedCount*(hashSize+depthSize))
	data[0] = byte(PrunedCellType)
	data[1] = actualMask.Mask

	for i := 0; i < storedCount; i++ {
		copy(data[2+i*hashSize:], ref.Hashes[i*hashSize:(i+1)*hashSize])
	}
	depthOff := 2 + storedCount*hashSize
	for i := 0; i < storedCount; i++ {
		binary.BigEndian.PutUint16(data[depthOff+i*depthSize:], ref.Depths[i])
	}

	c := &Cell{
		data:   data,
		bitsSz: uint16(len(data) * 8),
	}
	c.setSpecial(true)
	c.setLazy(true)
	c.setLevelMask(actualMask)
	c.setHashAt(0, ref.Hashes[(hashesCount-1)*hashSize:hashesCount*hashSize])
	c.setDepthAt(0, ref.Depths[hashesCount-1])
	if loader != nil {
		c.ensureMeta().lazyLoader = loader
	}
	return c, nil
}

func setTrustedHashesDepths(c *Cell, levelMask LevelMask, hashes []byte, depths []uint16) error {
	hashesCount := levelMask.getHashesCount()
	if len(hashes) != hashesCount*hashSize {
		return errors.New("invalid cell hashes size")
	}
	if len(depths) != hashesCount {
		return errors.New("invalid cell depths count")
	}

	for i := 0; i < hashesCount; i++ {
		c.setHashAt(i, hashes[i*hashSize:(i+1)*hashSize])
		c.setDepthAt(i, depths[i])
	}
	return nil
}

func loadLazyPrunedRef(c *Cell) (*Cell, error) {
	return loadLazyPrunedRefWithTrace(c, c.Trace())
}

func loadLazyPrunedRefWithTrace(c *Cell, trace *Trace) (*Cell, error) {
	loaded, err := loadLazyPrunedRefData(c)
	if err != nil {
		return nil, err
	}
	return resolveLoadedLazyRefWithTrace(c, loaded, trace)
}

// loadLazyPrunedRefData reads the represented cell before attaching the
// caller's virtual level or trace, so operation-local caches can share raw data.
func loadLazyPrunedRefData(c *Cell) (*Cell, error) {
	raw := c.rawCell()
	meta := raw.meta
	if meta == nil {
		return nil, ErrLazyLoaderNotSet
	}
	if meta.lazyFlags&cellLazyBOC != 0 {
		// Only bocLazyCellMeta allocations carry this tag; ordinary metadata
		// and virtual wrappers never pass through this prefix conversion.
		boc := (*bocLazyCellMeta)(unsafe.Pointer(meta))
		return boc.loader.loadDataCell(int(boc.index))
	}
	if meta.lazyLoader == nil {
		return nil, ErrLazyLoaderNotSet
	}
	return meta.lazyLoader(raw.HashKey())
}

// loadLazyPrunedRefCached shares raw cell bodies within one traversal while
// validating each boundary and attaching its own virtual level and trace.
func loadLazyPrunedRefCached(c *Cell, cache *cellLoadCache) (*Cell, error) {
	if !c.hasLazyLoader() {
		return nil, ErrLazyLoaderNotSet
	}

	hash := c.rawCell().HashKey()
	data := cache.lookup(hash)
	missing := data == nil
	if data == nil {
		var err error
		data, err = loadLazyPrunedRefData(c)
		if err != nil {
			return nil, err
		}
	}
	// Equal hashes must not hide malformed depths or a different view.
	loaded, err := resolveLoadedLazyRefWithTrace(c, data, c.Trace())
	if err != nil {
		return nil, err
	}
	if missing {
		cache.store(hash, data.rawCell())
	}
	return loaded, nil
}

// resolveLoadedLazyRefWithTrace binds an already loaded cell to a lazy
// placeholder. It is the cache-hit half of loadLazyPrunedRefWithTrace: the
// represented hash, depth and virtual level are checked exactly as on a loader
// result, while the caller avoids asking storage for a cell it already owns.
func resolveLoadedLazyRefWithTrace(c, loaded *Cell, trace *Trace) (*Cell, error) {
	meta := c.meta
	if meta == nil {
		return nil, ErrLazyLoaderNotSet
	}
	if loaded == nil {
		return nil, ErrLazyRefNotFound
	}
	raw := c.rawCell()
	loaded = loaded.rawCell()
	if meta.lazyFlags&cellLazySkipValidation == 0 {
		if err := validateLoadedLazyRef(raw, loaded); err != nil {
			return nil, err
		}
	}

	var out *Cell
	if meta.viewLevel == 0 {
		out = loaded
	} else {
		out = loaded.Virtualize(meta.viewLevel - 1)
	}
	if trace != nil {
		out = out.WithTrace(trace)
	}
	return out, nil
}

func validateLoadedLazyRef(placeholder, loaded *Cell) error {
	placeholderMask := placeholder.getLevelMask()
	if placeholderMask != loaded.getLevelMask() {
		return fmt.Errorf("%w: level mask mismatch", ErrLazyRefMismatch)
	}

	for level := 0; level <= placeholderMask.GetLevel(); level++ {
		if !placeholderMask.IsSignificant(level) {
			continue
		}
		if !bytes.Equal(placeholder.getHash(level), loaded.getHash(level)) {
			return fmt.Errorf("%w: hash mismatch at level %d", ErrLazyRefMismatch, level)
		}
		if placeholder.getDepth(level) != loaded.getDepth(level) {
			return fmt.Errorf("%w: depth mismatch at level %d", ErrLazyRefMismatch, level)
		}
	}
	return nil
}

// PrewarmRecursive returns a new cell tree with lazy references loaded up to depth.
// depth limits how many reference edges are traversed; 0 means no limit.
// References beyond the depth boundary are kept as boundary refs, so lazy tips
// stay lazy while all cells above them are materialized.
func (c *Cell) PrewarmRecursive(depth int) (*Cell, error) {
	if depth < 0 {
		return nil, ErrNegative
	}

	var loaded cellLoadCache
	return c.prewarmRecursive(depth, depth == 0, map[prewarmRecursiveKey]*Cell{}, &loaded)
}

type prewarmRecursiveKey struct {
	hash      Hash
	depth     int
	unlimited bool
}

func (c *Cell) prewarmRecursive(depth int, unlimited bool, cache map[prewarmRecursiveKey]*Cell, loadedCells *cellLoadCache) (*Cell, error) {
	if c == nil {
		return nil, nil
	}

	loaded := c
	var err error
	if c.IsLazy() {
		loaded, err = loadLazyPrunedRefCached(c, loadedCells)
		if err != nil {
			return nil, err
		}
	}

	hash := loaded.HashKey()
	key := prewarmRecursiveKey{hash: hash, depth: depth, unlimited: unlimited}
	if unlimited {
		key.depth = 0
	}
	if cached := cache[key]; cached != nil {
		return cached, nil
	}

	refView := newCellRefView(loaded)
	refCnt := loaded.refsCount()
	var refsBuf [4]*Cell
	refs := refsBuf[:refCnt]
	if !unlimited && depth == 0 {
		for i := range refs {
			refs[i], err = refView.boundaryRef(i)
			if err != nil {
				return nil, err
			}
		}
		out, err := materializeLoadedCellWithRefs(loaded, refs)
		if err != nil {
			return nil, err
		}
		cache[key] = out
		return out, nil
	}

	nextDepth := depth
	if !unlimited {
		nextDepth--
	}
	for i := range refs {
		ref, err := refView.boundaryRef(i)
		if err != nil {
			return nil, err
		}
		refs[i], err = ref.prewarmRecursive(nextDepth, unlimited, cache, loadedCells)
		if err != nil {
			return nil, err
		}
	}
	out, err := materializeLoadedCellWithRefs(loaded, refs)
	if err != nil {
		return nil, err
	}
	cache[key] = out
	return out, nil
}

func materializeLoadedCellWithRefs(c *Cell, refs []*Cell) (*Cell, error) {
	out := c.copy()
	out.setRefs(refs)
	out.setLazy(false)
	out.clearVirtualization()
	if out.meta != nil {
		out.meta.lazyLoader = nil
		out.meta.lazyFlags = 0
		out.clearMetaIfEmpty()
	}
	if err := out.refreshLevelMaskForRefs(); err != nil {
		return nil, err
	}
	if err := out.calculateHashes(); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Cell) cellLazyLoader() LazyCellLoader {
	if c.meta == nil {
		return nil
	}
	return c.meta.lazyLoader
}

func (c *Cell) hasLazyLoader() bool {
	meta := c.rawCell().meta
	return meta != nil && (meta.lazyLoader != nil || meta.lazyFlags&cellLazyBOC != 0)
}
