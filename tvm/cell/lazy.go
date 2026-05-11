package cell

import (
	"encoding/binary"
	"errors"
)

var ErrLazyLoaderNotSet = errors.New("lazy pruned ref loader is not set")
var ErrLazyRefNotFound = errors.New("lazy pruned ref not found")

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
// hashes and lazy pruned refs. The parent cell is not marked lazy; only its
// pruned references are lazy boundaries.
// descriptors must be encoded as d1<<8 | d2, and data must contain the serialized
// cell body, including the top-up bit for non-byte-aligned cells.
// loader is captured by the lazy references; pass nil to create unresolved
// placeholders which return ErrLazyLoaderNotSet when loaded.
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
	levelMask := LevelMask{Mask: dsc1 >> 5}
	bitsSz, err := cellBodyBitsSize(dsc2, data)
	if err != nil {
		return nil, err
	}

	c := &Cell{
		data:   data,
		bitsSz: bitsSz,
	}
	c.setSpecial(dsc1&0b1000 != 0)
	c.setLevelMask(levelMask)
	c.setRefsCount(refCnt)
	if err = setTrustedHashesDepths(c, levelMask, hashes, depths); err != nil {
		return nil, err
	}

	for i := range refs {
		c.refs[i], err = createLazyPrunedRef(refs[i], loader)
		if err != nil {
			return nil, err
		}
	}
	return c, nil
}

func cellBodyBytesSize(dsc2 byte) int {
	return int(dsc2/2 + dsc2%2)
}

func cellBodyBitsSize(dsc2 byte, data []byte) (uint16, error) {
	payloadSize := cellBodyBytesSize(dsc2)
	if dsc2%2 == 0 {
		return uint16(payloadSize * 8), nil
	}

	last := data[payloadSize-1]
	for i := 0; i < 7; i++ {
		if (last>>i)&1 == 1 {
			return uint16((payloadSize-1)*8 + 7 - i), nil
		}
	}

	return 0, errors.New("invalid cell payload")
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

	prunedMask := actualMask
	prunedLevel := actualLevel
	if prunedLevel == 0 {
		prunedMask = LevelMask{Mask: oneLevelMask(1)}
		prunedLevel = 1
	}
	storedMask := prunedMask.Apply(prunedLevel - 1)
	storedCount := storedMask.getHashesCount()
	data := make([]byte, 2+storedCount*(hashSize+depthSize))
	data[0] = byte(PrunedCellType)
	data[1] = prunedMask.Mask

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
	c.setLevelMask(prunedMask)
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
	meta := c.meta
	if meta == nil || meta.lazyLoader == nil {
		return nil, ErrLazyLoaderNotSet
	}

	loaded, err := meta.lazyLoader(c.HashKey())
	if err != nil {
		return nil, err
	}
	if loaded == nil {
		return nil, ErrLazyRefNotFound
	}

	if meta.viewLevel == 0 {
		return loaded, nil
	}
	return loaded.Virtualize(meta.viewLevel - 1), nil
}

func (c *Cell) cellLazyLoader() LazyCellLoader {
	if c.meta == nil {
		return nil
	}
	return c.meta.lazyLoader
}
