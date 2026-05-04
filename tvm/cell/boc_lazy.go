package cell

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
)

type bocPayloadCellInfo struct {
	bodyOffset int
	refsOffset int

	dsc1 byte
	dsc2 byte
}

type lazyBOCLoader struct {
	payload    []byte
	cells      []bocPayloadCellInfo
	index      bocCellIndex
	refSzBytes int

	mu    sync.Mutex
	cache []*Cell
}

func parseLazyBOC(rootsIndex []uint32, cellsNum, refSzBytes, dataLen int, r *BOCNoCopyReader, index bocCellIndex, options BOCParseOptions) ([]*Cell, error) {
	if !options.TrustedHashes {
		return nil, errors.New("lazy boc parsing requires trusted hashes")
	}

	payload, err := r.readBytes(dataLen)
	if err != nil {
		return nil, fmt.Errorf("failed to read payload, want %d: %w", dataLen, err)
	}

	loader := &lazyBOCLoader{
		payload:    payload,
		cells:      make([]bocPayloadCellInfo, cellsNum),
		index:      index,
		refSzBytes: refSzBytes,
		cache:      make([]*Cell, cellsNum),
	}
	if err = loader.init(rootsIndex); err != nil {
		return nil, err
	}

	roots := make([]*Cell, len(rootsIndex))
	for i, idx := range rootsIndex {
		if !loader.cells[idx].withHashes() {
			return nil, fmt.Errorf("lazy boc root cell #%d does not store hashes", idx)
		}

		root, err := loader.loadDataCell(int(idx))
		if err != nil {
			return nil, fmt.Errorf("failed to load root cell #%d: %w", idx, err)
		}
		roots[i] = root
	}

	return roots, nil
}

func (l *lazyBOCLoader) init(rootsIndex []uint32) error {
	cacheRefs := []uint8(nil)
	if l.index.hasCacheBits {
		cacheRefs = make([]uint8, len(l.cells))
		for _, idx := range rootsIndex {
			if cacheRefs[idx] < 2 {
				cacheRefs[idx]++
			}
		}
	}

	offset := 0
	indexEnabled := l.index.enabled()
	for i := range l.cells {
		end := len(l.payload)
		if indexEnabled {
			end = l.index.cellEnd(i)
			if end < offset || end > len(l.payload) {
				return errors.New("invalid cell index")
			}
		}

		info, nextOffset, err := parseBOCPayloadCellInfo(l.payload, offset, end, l.refSzBytes, indexEnabled)
		if err != nil {
			return fmt.Errorf("invalid cell #%d: %w", i, err)
		}
		l.cells[i] = info

		for ref := 0; ref < info.refsCount(); ref++ {
			refIdx := info.refIndex(l.payload, ref, l.refSzBytes)
			if refIdx == i {
				return errors.New("recursive reference of cells")
			}
			if refIdx < i {
				return errors.New("reference to index which is behind parent cell")
			}
			if refIdx < 0 || refIdx >= len(l.cells) {
				return errors.New("invalid index, out of scope")
			}
			if cacheRefs != nil && cacheRefs[refIdx] < 2 {
				cacheRefs[refIdx]++
			}
		}

		offset = nextOffset
	}

	if offset != len(l.payload) {
		if indexEnabled {
			return errors.New("invalid cell index")
		}
		return errors.New("failed to parse cells payload, corrupted data")
	}

	if cacheRefs != nil {
		for i := range l.cells {
			if shouldCache := cacheRefs[i] > 1; shouldCache != l.index.cacheBit(i) {
				return fmt.Errorf("invalid cache flag for cell #%d", i)
			}
		}
	}

	return nil
}

func parseBOCPayloadCellInfo(payload []byte, begin, end, refSzBytes int, indexed bool) (bocPayloadCellInfo, int, error) {
	if end-begin < 2 {
		if indexed {
			return bocPayloadCellInfo{}, 0, errors.New("invalid cell index")
		}
		return bocPayloadCellInfo{}, 0, errors.New("failed to parse cell header, corrupted data")
	}

	dsc1 := payload[begin]
	dsc2 := payload[begin+1]
	refsNum := int(dsc1 & 0b111)
	if refsNum > 4 {
		return bocPayloadCellInfo{}, 0, errors.New("too many refs in cell")
	}

	info := bocPayloadCellInfo{
		dsc1: dsc1,
		dsc2: dsc2,
	}

	offset := begin + 2
	if info.withHashes() {
		hashesNum := info.levelMask().getHashesCount()
		hashesSize := hashesNum * hashSize
		depthsSize := hashesNum * depthSize
		if end-offset < hashesSize+depthsSize {
			return bocPayloadCellInfo{}, 0, errors.New("failed to parse cell hashes, corrupted data")
		}

		offset += hashesSize + depthsSize
	}

	bodyBytes := cellBodyBytesSize(dsc2)
	if end-offset < bodyBytes {
		return bocPayloadCellInfo{}, 0, errors.New("failed to parse cell payload, corrupted data")
	}
	if dsc2%2 != 0 {
		if _, err := cellBodyBitsSize(dsc2, payload[offset:offset+bodyBytes]); err != nil {
			return bocPayloadCellInfo{}, 0, err
		}
	}

	info.bodyOffset = offset
	offset += bodyBytes
	if end-offset < refsNum*refSzBytes {
		return bocPayloadCellInfo{}, 0, errors.New("failed to parse cell refs, corrupted data")
	}

	info.refsOffset = offset
	offset += refsNum * refSzBytes
	if indexed && offset != end {
		return bocPayloadCellInfo{}, 0, errors.New("invalid indexed cell boundary")
	}

	return info, offset, nil
}

func (l *lazyBOCLoader) loadAnyCell(idx int) (*Cell, error) {
	if idx < 0 || idx >= len(l.cells) {
		return nil, errors.New("invalid index, out of scope")
	}
	if l.cells[idx].withHashes() {
		return l.createLazyCell(idx)
	}
	return l.loadDataCell(idx)
}

func (l *lazyBOCLoader) loadDataCell(idx int) (*Cell, error) {
	if idx < 0 || idx >= len(l.cells) {
		return nil, errors.New("invalid index, out of scope")
	}

	l.mu.Lock()
	cached := l.cache[idx]
	l.mu.Unlock()
	if cached != nil {
		return cached, nil
	}

	cell, err := l.deserializeDataCell(idx)
	if err != nil {
		return nil, err
	}

	if l.index.hasCacheBits && !l.index.cacheBit(idx) {
		return cell, nil
	}

	l.mu.Lock()
	if cached = l.cache[idx]; cached != nil {
		l.mu.Unlock()
		return cached, nil
	}
	l.cache[idx] = cell
	l.mu.Unlock()

	return cell, nil
}

func (l *lazyBOCLoader) deserializeDataCell(idx int) (*Cell, error) {
	info := l.cells[idx]
	body := info.body(l.payload)
	bitsSz, err := cellBodyBitsSize(info.dsc2, body)
	if err != nil {
		return nil, err
	}

	c := &Cell{
		data:   body,
		bitsSz: bitsSz,
	}
	c.setSpecial(info.dsc1&0b1000 != 0)
	levelMask := info.levelMask()
	refCnt := info.refsCount()
	c.setLevelMask(levelMask)
	c.setRefsCount(refCnt)

	for ref := 0; ref < refCnt; ref++ {
		refIdx := info.refIndex(l.payload, ref, l.refSzBytes)
		loaded, err := l.loadAnyCell(refIdx)
		if err != nil {
			return nil, fmt.Errorf("failed to load ref %d: %w", ref, err)
		}
		c.refs[ref] = loaded
	}

	if info.withHashes() {
		var depths [4]uint16
		info.fillDepths(l.payload, depths[:])
		if err = setTrustedHashesDepths(c, levelMask, info.hashes(l.payload), depths[:levelMask.getHashesCount()]); err != nil {
			return nil, err
		}
	} else if err = c.calculateHashes(); err != nil {
		return nil, err
	}

	if c.IsSpecial() {
		if err = validateBoundaryCell(c); err != nil {
			return nil, err
		}
	}

	return c, nil
}

func (l *lazyBOCLoader) createLazyCell(idx int) (*Cell, error) {
	info := l.cells[idx]
	if !info.withHashes() {
		return nil, errors.New("lazy boc cell does not store hashes")
	}

	levelMask := info.levelMask()
	hashesCount := levelMask.getHashesCount()
	var depths [4]uint16
	info.fillDepths(l.payload, depths[:])

	return createLazyPrunedRef(LazyRef{
		LevelMask: levelMask,
		Hashes:    info.hashes(l.payload),
		Depths:    depths[:hashesCount],
	}, func(Hash) (*Cell, error) {
		return l.loadDataCell(idx)
	})
}

func (info bocPayloadCellInfo) body(payload []byte) []byte {
	return payload[info.bodyOffset:info.refsOffset:info.refsOffset]
}

func (info bocPayloadCellInfo) refIndex(payload []byte, ref, refSzBytes int) int {
	offset := info.refsOffset + ref*refSzBytes
	return dynIntFromPayloadSize(refSzBytes, payload[offset:offset+refSzBytes])
}

func (info bocPayloadCellInfo) hashes(payload []byte) []byte {
	hashesOffset, hashesSize, _ := info.hashesDepthsRange()
	return payload[hashesOffset : hashesOffset+hashesSize]
}

func (info bocPayloadCellInfo) depths(payload []byte) []byte {
	hashesOffset, hashesSize, depthsSize := info.hashesDepthsRange()
	depthsOffset := hashesOffset + hashesSize
	return payload[depthsOffset : depthsOffset+depthsSize]
}

func (info bocPayloadCellInfo) hashesDepthsRange() (int, int, int) {
	hashesCount := info.levelMask().getHashesCount()
	hashesSize := hashesCount * hashSize
	depthsSize := hashesCount * depthSize
	return info.bodyOffset - hashesSize - depthsSize, hashesSize, depthsSize
}

func (info bocPayloadCellInfo) levelMask() LevelMask {
	return LevelMask{info.dsc1 >> 5}
}

func (info bocPayloadCellInfo) refsCount() int {
	return int(info.dsc1 & 0b111)
}

func (info bocPayloadCellInfo) withHashes() bool {
	return info.dsc1&0b10000 != 0
}

func (info bocPayloadCellInfo) fillDepths(payload []byte, dst []uint16) {
	depths := info.depths(payload)
	for i := 0; i < info.levelMask().getHashesCount(); i++ {
		dst[i] = binary.BigEndian.Uint16(depths[i*depthSize : (i+1)*depthSize])
	}
}

func dynIntFromPayloadSize(size int, data []byte) int {
	switch size {
	case 1:
		return int(data[0])
	case 2:
		return int(binary.BigEndian.Uint16(data))
	case 3:
		return int(data[0])<<16 | int(data[1])<<8 | int(data[2])
	case 4:
		return int(binary.BigEndian.Uint32(data))
	default:
		return dynInt(data[:size])
	}
}
