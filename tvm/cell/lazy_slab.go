package cell

import (
	"encoding/binary"
	"errors"
)

// The slab layout behind CreateWithLazyRefsUnsafe.
//
// The per-object construction this replaced built a cell with R lazy
// references out of 2+3R heap allocations: the body clone the caller made, the
// Cell itself, and per reference a placeholder Cell, its pruned payload and a
// cellMeta for the loader pointer. Measured on the storage decode path that
// was 8 allocations and ~300 ns for the two-reference cell a state tree
// averages, and the allocation count is what the cost was made of: decode is
// malloc-bound, not parse-bound.
//
// Every one of those objects lives exactly as long as the root cell — a
// placeholder is reachable only through its parent, and resolution never
// installs the loaded child back into the tree — so they can share one
// allocation. The slab types below do that: the root, the placeholders, their
// metas, their pruned payloads and the body copy are fields of a single
// struct, and the constructor returns a pointer to its root field. Interior
// pointers keep the whole slab alive, which is exactly the lifetime the parts
// had anyway.
//
// Two deliberate limits keep the slab from bloating the bytes axis (GC cycle
// frequency tracks allocated bytes, so a fatter-but-single allocation is not
// automatically a win):
//   - the per-reference pruned buffer is sized for a LEVEL-0 child (36 bytes),
//     which is what state trees consist of; a higher-level child falls back to
//     its own exact-size allocation while its Cell and meta stay in the slab;
//   - a cell with no references does not use a slab at all: the classic two
//     allocations carry an exact-size body, and a fixed 128-byte slab body
//     would double the bytes of every leaf for one saved allocation.
//
// data is copied in every variant, so the caller may reuse its buffer
// immediately; the pre-slab constructor took ownership instead, and the two
// storage decoders used to pay a bytes.Clone for that.
const (
	lazySlabBodyCap = 128
	// A level-0 placeholder stores one hash and one depth after the two-byte
	// pruned header.
	lazySlabPrunedCap = 2 + hashSize + depthSize
)

type lazySlab2 struct {
	root   Cell
	refs   [2]Cell
	metas  [2]cellMeta
	pruned [2][lazySlabPrunedCap]byte
	body   [lazySlabBodyCap]byte
}

type lazySlab4 struct {
	root   Cell
	refs   [4]Cell
	metas  [4]cellMeta
	pruned [4][lazySlabPrunedCap]byte
	body   [lazySlabBodyCap]byte
}

// The BOC variants carry no body buffer: a cell materialized out of a lazy
// BOC keeps its data as a slice into the deserializer's shared payload, so the
// slab only holds the Cell, the placeholder Cells, their metas and their
// pruned payloads. Splitting the types rather than reusing lazySlab2/4 keeps
// the storage path's 128-byte body out of every BOC materialization — the
// bytes axis is what decides GC cycle frequency, and the BOC path pays zero
// body bytes today.
type bocLazySlab1 struct {
	root   Cell
	refs   [1]Cell
	metas  [1]bocLazyCellMeta
	pruned [1][lazySlabPrunedCap]byte
}

type bocLazySlab2 struct {
	root   Cell
	refs   [2]Cell
	metas  [2]bocLazyCellMeta
	pruned [2][lazySlabPrunedCap]byte
}

type bocLazySlab3 struct {
	root   Cell
	refs   [3]Cell
	metas  [3]bocLazyCellMeta
	pruned [3][lazySlabPrunedCap]byte
}

type bocLazySlab4 struct {
	root   Cell
	refs   [4]Cell
	metas  [4]bocLazyCellMeta
	pruned [4][lazySlabPrunedCap]byte
}

// bocLazyCellMeta extends only BoC placeholder metadata with its resolver.
// cellMeta must remain the first field: the cellLazyBOC tag permits recovering
// this allocation from the prefix pointer without enlarging storage-cell metas.
// Direct loader/index state replaces one escaping closure per lazy reference.
type bocLazyCellMeta struct {
	cellMeta
	loader *lazyBOCLoader
	index  uint32
}

type bocLazyRef struct {
	cell   Cell
	meta   bocLazyCellMeta
	pruned [lazySlabPrunedCap]byte
}

// buildLazySlab lays one cell and its placeholders into slab-provided memory.
func buildLazySlab(root *Cell, body []byte, refCells []Cell, metas []cellMeta, pruned [][lazySlabPrunedCap]byte,
	descriptors uint16, data, hashes []byte, depths []uint16, refs []LazyRef, loader LazyCellLoader) (*Cell, error) {
	if err := initSlabRoot(root, body, descriptors, data, hashes, depths); err != nil {
		return nil, err
	}
	for i := range refs {
		if err := initSlabRef(&refCells[i], &metas[i], pruned[i][:], refs[i], loader); err != nil {
			return nil, err
		}
		root.refs[i] = &refCells[i]
	}
	return root, nil
}

func initSlabRoot(c *Cell, body []byte, descriptors uint16, data, hashes []byte, depths []uint16) error {
	dsc1 := byte(descriptors >> 8)
	dsc2 := byte(descriptors)
	bitsSz, err := cellBodyBitsSize(dsc2, data)
	if err != nil {
		return err
	}

	n := copy(body, data)
	levelMask := LevelMask{Mask: dsc1 >> 5}
	c.data = body[:n]
	c.bitsSz = bitsSz
	c.setSpecial(dsc1&0b1000 != 0)
	c.setLevelMask(levelMask)
	c.setRefsCount(int(dsc1 & 0b111))
	c.resolveType()
	// Level 0 stores its one hash inline; higher root levels go through
	// ensureMeta and allocate. State-tree roots decoded from records are level
	// 0, so the slab does not reserve meta space for the rare case.
	return setTrustedHashesDepths(c, levelMask, hashes, depths)
}

// initSlabRef is createLazyPrunedRef building into slab-provided memory. The
// pruned buffer fits a level-0 child; a higher-level child takes its own
// exact-size payload allocation while the Cell and meta stay in the slab.
func initSlabRef(dst *Cell, meta *cellMeta, buf []byte, ref LazyRef, loader LazyCellLoader) error {
	actualMask := ref.LevelMask
	if actualMask.Mask > 0b111 {
		return errors.New("invalid lazy ref level mask")
	}
	actualLevel := actualMask.GetLevel()
	hashesCount := actualMask.getHashesCount()
	if len(ref.Hashes) != hashesCount*hashSize {
		return errors.New("invalid lazy ref hashes size")
	}
	if len(ref.Depths) != hashesCount {
		return errors.New("invalid lazy ref depths count")
	}

	storedMask := actualMask
	if actualLevel > 0 {
		storedMask = actualMask.Apply(actualLevel - 1)
	}
	storedCount := storedMask.getHashesCount()
	need := 2 + storedCount*(hashSize+depthSize)
	data := buf[:0]
	if need <= len(buf) {
		data = buf[:need]
	} else {
		data = make([]byte, need)
	}
	data[0] = byte(PrunedCellType)
	data[1] = actualMask.Mask

	for i := 0; i < storedCount; i++ {
		copy(data[2+i*hashSize:], ref.Hashes[i*hashSize:(i+1)*hashSize])
	}
	depthOff := 2 + storedCount*hashSize
	for i := 0; i < storedCount; i++ {
		binary.BigEndian.PutUint16(data[depthOff+i*depthSize:], ref.Depths[i])
	}

	dst.data = data
	dst.bitsSz = uint16(len(data) * 8)
	dst.setSpecial(true)
	dst.setLazy(true)
	dst.setLevelMask(actualMask)
	dst.setHashAt(0, ref.Hashes[(hashesCount-1)*hashSize:hashesCount*hashSize])
	dst.setDepthAt(0, ref.Depths[hashesCount-1])
	if loader != nil {
		meta.lazyLoader = loader
		dst.meta = meta
	}
	return nil
}
