package cell

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
)

type bocPayloadCellInfo struct {
	bodyOffset int
	refsOffset int

	bitsSz uint16
	dsc1   byte
	dsc2   byte
}

type lazyBOCLoader struct {
	payload       []byte
	cells         []bocPayloadCellInfo
	metaOffsets   []uint32
	metaHashes    []byte
	metaDepths    []uint16
	index         bocCellIndex
	refSzBytes    int
	trustedHashes bool

	mu    sync.Mutex
	cache []*Cell
}

func parseLazyBOC(rootsIndex []uint32, cellsNum, refSzBytes, dataLen int, r *BOCNoCopyReader, index bocCellIndex, options BOCParseOptions) ([]*Cell, error) {
	payload, err := r.readBytes(dataLen)
	if err != nil {
		return nil, fmt.Errorf("failed to read payload, want %d: %w", dataLen, err)
	}
	if !options.NoCopyPayload && r.direct {
		payloadCopy := make([]byte, len(payload))
		copy(payloadCopy, payload)
		payload = payloadCopy
	}

	loader := &lazyBOCLoader{
		payload:       payload,
		cells:         make([]bocPayloadCellInfo, cellsNum),
		index:         index,
		refSzBytes:    refSzBytes,
		trustedHashes: options.TrustedHashes,
	}
	if !options.DisableLazyCache {
		loader.cache = make([]*Cell, cellsNum)
	}
	if err = loader.init(rootsIndex); err != nil {
		return nil, err
	}

	roots := make([]*Cell, len(rootsIndex))
	for i, idx := range rootsIndex {
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
			incBOCCacheRef(cacheRefs, int(idx))
		}
	}

	offset := 0
	indexEnabled := l.index.enabled()
	computedHashesCount := 0
	for i := range l.cells {
		end := len(l.payload)
		if indexEnabled {
			if cacheRefs != nil {
				var cacheBit bool
				end, cacheBit = l.index.cellEndAndCacheBit(i)
				if cacheBit {
					setBOCIndexCacheBit(cacheRefs, i)
				}
			} else {
				end = l.index.cellEnd(i)
			}
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
			if cacheRefs != nil {
				incBOCCacheRef(cacheRefs, refIdx)
			}
		}

		if l.needsComputedMeta(info) {
			if l.metaOffsets == nil {
				l.metaOffsets = make([]uint32, len(l.cells))
			}
			hashesCount := info.levelMask().getHashesCount()
			if uint64(computedHashesCount)+uint64(hashesCount) > uint64(^uint32(0)) {
				return errors.New("too many computed cell hashes")
			}
			l.metaOffsets[i] = uint32(computedHashesCount)
			computedHashesCount += hashesCount
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
			if shouldCache := bocShouldCache(cacheRefs, i); shouldCache != bocIndexCacheBit(cacheRefs, i) {
				return fmt.Errorf("invalid cache flag for cell #%d", i)
			}
		}
	}

	if computedHashesCount > 0 {
		l.metaHashes = make([]byte, computedHashesCount*hashSize)
		l.metaDepths = make([]uint16, computedHashesCount)
		for idx := len(l.cells) - 1; idx >= 0; idx-- {
			if !l.needsComputedMeta(l.cells[idx]) {
				continue
			}

			if err := l.computeCellMeta(idx); err != nil {
				return fmt.Errorf("invalid cell #%d: %w", idx, err)
			}
		}
	}

	return nil
}

func (l *lazyBOCLoader) trustStoredMeta(info bocPayloadCellInfo) bool {
	return l.trustedHashes && info.withHashes()
}

func (l *lazyBOCLoader) needsComputedMeta(info bocPayloadCellInfo) bool {
	return !l.trustStoredMeta(info)
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
	bitsSz := uint16(bodyBytes * 8)
	if dsc2%2 != 0 {
		var err error
		if bitsSz, err = cellBodyBitsSize(dsc2, payload[offset:offset+bodyBytes]); err != nil {
			return bocPayloadCellInfo{}, 0, err
		}
	}

	info.bodyOffset = offset
	info.bitsSz = bitsSz
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
	return l.createLazyCell(idx)
}

func (l *lazyBOCLoader) loadDataCell(idx int) (*Cell, error) {
	if idx < 0 || idx >= len(l.cells) {
		return nil, errors.New("invalid index, out of scope")
	}

	if l.cache != nil {
		l.mu.Lock()
		cached := l.cache[idx]
		l.mu.Unlock()
		if cached != nil {
			return cached, nil
		}
	}

	cell, err := l.deserializeDataCell(idx)
	if err != nil {
		return nil, err
	}

	if l.cache == nil || (l.index.hasCacheBits && !l.index.cacheBit(idx)) {
		return cell, nil
	}

	l.mu.Lock()
	if cached := l.cache[idx]; cached != nil {
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

	c := &Cell{
		data:   body,
		bitsSz: info.bitsSz,
	}
	c.setSpecial(info.isSpecial())
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

	if err := l.setCellHashesDepths(c, idx); err != nil {
		return nil, err
	}

	if c.IsSpecial() {
		if err := validateBoundaryCell(c); err != nil {
			return nil, err
		}
	}

	return c, nil
}

func (l *lazyBOCLoader) createLazyCell(idx int) (*Cell, error) {
	info := l.cells[idx]
	levelMask := info.levelMask()
	hashesCount := levelMask.getHashesCount()

	if l.trustStoredMeta(info) {
		var depths [4]uint16
		info.fillStoredDepths(l.payload, depths[:])

		return createLazyPrunedRef(LazyRef{
			LevelMask: levelMask,
			Hashes:    info.hashes(l.payload),
			Depths:    depths[:hashesCount],
		}, func(Hash) (*Cell, error) {
			return l.loadDataCell(idx)
		})
	}

	metaOffset := int(l.metaOffsets[idx])
	hashesOffset := metaOffset * hashSize
	return createLazyPrunedRef(LazyRef{
		LevelMask: levelMask,
		Hashes:    l.metaHashes[hashesOffset : hashesOffset+hashesCount*hashSize],
		Depths:    l.metaDepths[metaOffset : metaOffset+hashesCount],
	}, func(Hash) (*Cell, error) {
		return l.loadDataCell(idx)
	})
}

func (l *lazyBOCLoader) computeCellMeta(idx int) error {
	typ, err := l.dataCellType(idx)
	if err != nil {
		return err
	}
	if typ == PrunedCellType {
		if err = l.computePrunedCellMeta(idx); err != nil {
			return err
		}
	} else {
		if err = l.computeRegularCellMeta(idx, typ); err != nil {
			return err
		}
	}

	if l.cells[idx].isSpecial() {
		if err = l.validateBoundaryCell(idx); err != nil {
			return err
		}
	}
	if l.cells[idx].withHashes() && !l.trustedHashes {
		if err = l.validateStoredCellMeta(idx); err != nil {
			return err
		}
	}
	return nil
}

func (l *lazyBOCLoader) computeRegularCellMeta(idx int, typ Type) error {
	info := l.cells[idx]
	body := info.body(l.payload)
	levelMask := info.levelMask()
	refCnt := info.refsCount()
	level := levelMask.GetLevel()
	isMerkle := typ == MerkleProofCellType || typ == MerkleUpdateCellType

	var refIndexes [4]int
	for ref := 0; ref < refCnt; ref++ {
		refIndexes[ref] = info.refIndex(l.payload, ref, l.refSzBytes)
	}

	hashIndex := 0
	var hashBuf [2 + maxCellDataBytes + (4 * depthSize) + (4 * hashSize)]byte
	for levelIndex := 0; levelIndex <= level; levelIndex++ {
		if !levelMask.IsSignificant(levelIndex) {
			continue
		}

		hashBuf[0] = info.hashDescriptor1(levelMask.Apply(levelIndex))
		hashBuf[1] = info.dsc2
		bufPos := 2

		if hashIndex == 0 {
			bufPos += copy(hashBuf[bufPos:], body)
		} else {
			bufPos += copy(hashBuf[bufPos:], l.computedMetaHash(idx, hashIndex-1))
		}

		childLevelIndex := levelIndex
		if isMerkle {
			childLevelIndex++
		}

		var depth uint16
		for ref := 0; ref < refCnt; ref++ {
			childDepth := l.cellMetaDepth(refIndexes[ref], childLevelIndex)
			binary.BigEndian.PutUint16(hashBuf[bufPos:bufPos+depthSize], childDepth)
			bufPos += depthSize

			if childDepth > depth {
				depth = childDepth
			}
		}
		if refCnt > 0 {
			depth++
			if depth > maxDepth {
				return ErrCellDepthLimit
			}
		}

		for ref := 0; ref < refCnt; ref++ {
			bufPos += copy(hashBuf[bufPos:], l.cellMetaHash(refIndexes[ref], childLevelIndex))
		}

		sum := sha256.Sum256(hashBuf[:bufPos])
		l.setComputedMeta(idx, hashIndex, sum[:], depth)
		hashIndex++
	}
	return nil
}

func (l *lazyBOCLoader) computePrunedCellMeta(idx int) error {
	info := l.cells[idx]
	body := info.body(l.payload)
	levelMask := info.levelMask()
	level := levelMask.GetLevel()
	storedMask := levelMask.Apply(level - 1)
	storedCount := storedMask.getHashesCount()

	hashesOffset := 2
	depthsOffset := hashesOffset + storedCount*hashSize
	for i := 0; i < storedCount; i++ {
		hash := body[hashesOffset+i*hashSize : hashesOffset+(i+1)*hashSize]
		depth := binary.BigEndian.Uint16(body[depthsOffset+i*depthSize : depthsOffset+(i+1)*depthSize])
		l.setComputedMeta(idx, i, hash, depth)
	}

	var hashBuf [2 + maxCellDataBytes]byte
	hashBuf[0] = info.hashDescriptor1(levelMask)
	hashBuf[1] = info.dsc2
	bufPos := 2 + copy(hashBuf[2:], body)
	hashIndex := levelMask.getHashIndex()
	sum := sha256.Sum256(hashBuf[:bufPos])
	l.setComputedMeta(idx, hashIndex, sum[:], 0)

	return nil
}

func (l *lazyBOCLoader) validateStoredCellMeta(idx int) error {
	info := l.cells[idx]
	levelMask := info.levelMask()
	metaOffset := int(l.metaOffsets[idx])

	hashIndex := 0
	for level := 0; level <= levelMask.GetLevel(); level++ {
		if !levelMask.IsSignificant(level) {
			continue
		}

		if !bytes.Equal(info.storedHash(l.payload, hashIndex), l.computedMetaHash(idx, hashIndex)) {
			return fmt.Errorf("serialized hash mismatch at level %d", level)
		}
		if info.storedDepth(l.payload, hashIndex) != l.metaDepths[metaOffset+hashIndex] {
			return fmt.Errorf("serialized depth mismatch at level %d", level)
		}
		hashIndex++
	}

	return nil
}

func (l *lazyBOCLoader) setCellHashesDepths(c *Cell, idx int) error {
	info := l.cells[idx]
	levelMask := info.levelMask()
	hashesCount := levelMask.getHashesCount()

	if l.trustStoredMeta(info) {
		var depths [4]uint16
		info.fillStoredDepths(l.payload, depths[:])
		if c.GetType() == PrunedCellType {
			c.setHashAt(0, info.storedHash(l.payload, hashesCount-1))
			c.setDepthAt(0, depths[hashesCount-1])
			return nil
		}

		return setTrustedHashesDepths(c, levelMask, info.hashes(l.payload), depths[:hashesCount])
	}

	metaOffset := int(l.metaOffsets[idx])
	hashesOffset := metaOffset * hashSize
	hashes := l.metaHashes[hashesOffset : hashesOffset+hashesCount*hashSize]
	if c.GetType() == PrunedCellType {
		hashOffset := (hashesCount - 1) * hashSize
		c.setHashAt(0, hashes[hashOffset:hashOffset+hashSize])
		c.setDepthAt(0, l.metaDepths[metaOffset+hashesCount-1])
		return nil
	}

	return setTrustedHashesDepths(c, levelMask, hashes, l.metaDepths[metaOffset:metaOffset+hashesCount])
}

func (l *lazyBOCLoader) cellMetaHash(idx, level int) []byte {
	info := l.cells[idx]
	hashIndex := info.levelMask().Apply(level).getHashIndex()
	if l.trustStoredMeta(info) {
		return info.storedHash(l.payload, hashIndex)
	}
	return l.computedMetaHash(idx, hashIndex)
}

func (l *lazyBOCLoader) cellMetaDepth(idx, level int) uint16 {
	info := l.cells[idx]
	hashIndex := info.levelMask().Apply(level).getHashIndex()
	if l.trustStoredMeta(info) {
		return info.storedDepth(l.payload, hashIndex)
	}
	return l.metaDepths[int(l.metaOffsets[idx])+hashIndex]
}

func (l *lazyBOCLoader) computedMetaHash(idx, hashIndex int) []byte {
	offset := (int(l.metaOffsets[idx]) + hashIndex) * hashSize
	return l.metaHashes[offset : offset+hashSize]
}

func (l *lazyBOCLoader) setComputedMeta(idx, hashIndex int, hash []byte, depth uint16) {
	offset := int(l.metaOffsets[idx]) + hashIndex
	copy(l.metaHashes[offset*hashSize:(offset+1)*hashSize], hash)
	l.metaDepths[offset] = depth
}

func (l *lazyBOCLoader) dataCellType(idx int) (Type, error) {
	info := l.cells[idx]
	typ := info.cellType(l.payload)
	if info.isSpecial() && typ == UnknownCellType {
		return typ, fmt.Errorf("unknown special cell type")
	}
	return typ, nil
}

func (l *lazyBOCLoader) validateBoundaryCell(idx int) error {
	c := l.newPayloadCell(idx)
	refCnt := c.refsCount()
	for ref := 0; ref < refCnt; ref++ {
		refIdx := l.cells[idx].refIndex(l.payload, ref, l.refSzBytes)
		loaded, err := l.createLazyCell(refIdx)
		if err != nil {
			return fmt.Errorf("failed to index ref %d: %w", ref, err)
		}
		c.refs[ref] = loaded
	}

	return validateBoundaryCell(c)
}

func (l *lazyBOCLoader) newPayloadCell(idx int) *Cell {
	info := l.cells[idx]
	c := &Cell{
		data:   info.body(l.payload),
		bitsSz: info.bitsSz,
	}
	c.setSpecial(info.isSpecial())
	c.setLevelMask(info.levelMask())
	c.setRefsCount(info.refsCount())
	return c
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

func (info bocPayloadCellInfo) storedHash(payload []byte, hashIndex int) []byte {
	offset, _, _ := info.hashesDepthsRange()
	offset += hashIndex * hashSize
	return payload[offset : offset+hashSize]
}

func (info bocPayloadCellInfo) storedDepth(payload []byte, hashIndex int) uint16 {
	hashesOffset, hashesSize, _ := info.hashesDepthsRange()
	offset := hashesOffset + hashesSize + hashIndex*depthSize
	return binary.BigEndian.Uint16(payload[offset : offset+depthSize])
}

func (info bocPayloadCellInfo) fillStoredDepths(payload []byte, dst []uint16) {
	depths := info.depths(payload)
	for i := 0; i < info.levelMask().getHashesCount(); i++ {
		dst[i] = binary.BigEndian.Uint16(depths[i*depthSize : (i+1)*depthSize])
	}
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

func (info bocPayloadCellInfo) isSpecial() bool {
	return info.dsc1&0b1000 != 0
}

func (info bocPayloadCellInfo) withHashes() bool {
	return info.dsc1&0b10000 != 0
}

func (info bocPayloadCellInfo) hashDescriptor1(levelMask LevelMask) byte {
	return (info.dsc1 & 0b00001111) | (levelMask.Mask << 5)
}

func (info bocPayloadCellInfo) cellType(payload []byte) Type {
	if !info.isSpecial() {
		return OrdinaryCellType
	}
	if info.bitsSz < 8 {
		return UnknownCellType
	}

	body := info.body(payload)
	switch Type(body[0]) {
	case PrunedCellType:
		if info.bitsSz >= 288 {
			msk := LevelMask{body[1]}
			lvl := msk.GetLevel()
			if lvl > 0 && lvl <= 3 && info.bitsSz >= uint16(16+(256+16)*(msk.Apply(lvl-1).getHashIndex()+1)) {
				return PrunedCellType
			}
		}
	case MerkleProofCellType:
		if info.refsCount() == 1 && info.bitsSz == 280 {
			return MerkleProofCellType
		}
	case MerkleUpdateCellType:
		if info.refsCount() == 2 && info.bitsSz == 552 {
			return MerkleUpdateCellType
		}
	case LibraryCellType:
		if info.bitsSz == 8+256 {
			return LibraryCellType
		}
	}
	return UnknownCellType
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
