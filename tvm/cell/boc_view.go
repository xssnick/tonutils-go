package cell

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"sync"
)

const bocViewReadWindowSize = 4 << 20

// BOCViewOptions configures a random-access BoC view.
type BOCViewOptions struct {
	// TrustedHashes trusts serialized hashes/depths when a cell stores them in
	// the BoC payload. Cells without serialized hashes are still hashed normally.
	TrustedHashes bool
	// RequireIndex rejects BoCs without an index table.
	RequireIndex bool
	// ValidateCRC validates the CRC32C trailer when the BoC carries one.
	ValidateCRC bool
	// BuildIndex scans non-indexed BoCs and builds an in-memory index.
	BuildIndex bool
}

// BOCCellRefs is the compact list of referenced BoC cell indexes.
type BOCCellRefs struct {
	Items [4]uint32
	Count uint8
}

// BOCCellMeta is the storage-visible metadata of a BoC cell.
type BOCCellMeta struct {
	Hash      Hash
	LevelMask LevelMask
	Hashes    [4]Hash
	Depths    [4]uint16
	Count     uint8
	Stored    bool
}

// BOCCellView is a decoded BoC cell record.
//
// Body points into the scratch buffer returned by BOCView.ReadCell. Callers
// should consume it before reusing that buffer.
type BOCCellView struct {
	Index uint32
	D1    byte
	D2    byte
	Bits  uint16
	Body  []byte
	Refs  BOCCellRefs
	Meta  BOCCellMeta
}

// BOCView exposes random-access cells from a BoC without loading the full BoC
// payload or materializing a Cell tree.
type BOCView struct {
	r    io.ReaderAt
	size int64

	cells uint32
	roots []uint32

	payloadOffset uint64
	payloadSize   uint64
	crcOffset     uint64

	refSize    uint8
	offsetSize uint8
	flags      bocFlags

	offsets32 []uint32
	offsets64 []uint64
	cacheBits []byte

	trustedHashes bool
	meta          *bocViewMetaStore

	mu          sync.Mutex
	windowStart uint64
	window      []byte
}

type bocViewMetaStore struct {
	offsets    []uint32
	hashes     []Hash
	depths     []uint16
	levelMasks []byte
	stored     []byte
}

type bocViewHeader struct {
	flags        bocFlags
	refSize      uint8
	offsetSize   uint8
	cells        uint32
	roots        []uint32
	payloadSize  uint64
	indexOffset  uint64
	payloadStart uint64
	crcOffset    uint64
}

// OpenBOCView opens a random-access BoC view over r.
func OpenBOCView(r io.ReaderAt, size int64, opts BOCViewOptions) (*BOCView, error) {
	if r == nil {
		return nil, errors.New("invalid boc")
	}
	if size <= 0 {
		return nil, errors.New("invalid boc size")
	}

	header, err := readBOCViewHeader(r, size)
	if err != nil {
		return nil, err
	}
	if opts.RequireIndex && !header.flags.hasIndex {
		return nil, errors.New("boc index is required")
	}

	view := &BOCView{
		r:             r,
		size:          size,
		cells:         header.cells,
		roots:         header.roots,
		payloadOffset: header.payloadStart,
		payloadSize:   header.payloadSize,
		crcOffset:     header.crcOffset,
		refSize:       header.refSize,
		offsetSize:    header.offsetSize,
		flags:         header.flags,
		trustedHashes: opts.TrustedHashes,
	}

	if header.flags.hasIndex {
		if err = view.loadIndex(header.indexOffset); err != nil {
			return nil, err
		}
	} else if opts.BuildIndex {
		if err = view.buildIndex(); err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("boc index is missing")
	}

	if opts.ValidateCRC && header.flags.HasCrc32c {
		if err = view.validateCRC(); err != nil {
			return nil, err
		}
	}

	if err = view.buildMeta(); err != nil {
		return nil, err
	}
	return view, nil
}

func (v *BOCView) Cells() uint32 {
	if v == nil {
		return 0
	}
	return v.cells
}

func (v *BOCView) Roots() []uint32 {
	if v == nil || len(v.roots) == 0 {
		return nil
	}
	roots := make([]uint32, len(v.roots))
	copy(roots, v.roots)
	return roots
}

// HashAtLevel returns the cell hash visible at the requested level.
func (m BOCCellMeta) HashAtLevel(level int) Hash {
	if m.Count == 0 {
		return Hash{}
	}
	hashIndex := m.LevelMask.Apply(level).getHashIndex()
	if hashIndex >= int(m.Count) {
		hashIndex = int(m.Count) - 1
	}
	return m.Hashes[hashIndex]
}

// DepthAtLevel returns the cell depth visible at the requested level.
func (m BOCCellMeta) DepthAtLevel(level int) uint16 {
	if m.Count == 0 {
		return 0
	}
	hashIndex := m.LevelMask.Apply(level).getHashIndex()
	if hashIndex >= int(m.Count) {
		hashIndex = int(m.Count) - 1
	}
	return m.Depths[hashIndex]
}

// CellMeta returns precomputed metadata for a BoC cell without reading its
// serialized body again.
func (v *BOCView) CellMeta(idx uint32) (BOCCellMeta, error) {
	var meta BOCCellMeta
	if v == nil {
		return meta, errors.New("boc view is nil")
	}
	if idx >= v.cells {
		return meta, errors.New("invalid cell index")
	}
	return v.cellMeta(idx), nil
}

// Cell loads one BoC cell using an internal scratch allocation.
func (v *BOCView) Cell(idx uint32) (BOCCellView, error) {
	cell, _, err := v.ReadCell(idx, nil)
	return cell, err
}

// ReadCell loads one BoC cell. The returned scratch slice owns cell.Body and
// can be reused by the caller on the next ReadCell call.
func (v *BOCView) ReadCell(idx uint32, scratch []byte) (BOCCellView, []byte, error) {
	var out BOCCellView
	if v == nil {
		return out, scratch, errors.New("boc view is nil")
	}
	if idx >= v.cells {
		return out, scratch, errors.New("invalid cell index")
	}

	start := v.cellStart(idx)
	end := v.cellEnd(idx)
	if end < start || end > v.payloadSize {
		return out, scratch, errors.New("invalid cell index")
	}
	cellSize := end - start
	if cellSize > maxSerializedBOCCellBytes {
		return out, scratch, fmt.Errorf("cell #%d is too large: %d", idx, cellSize)
	}

	if cap(scratch) < int(cellSize) {
		scratch = make([]byte, int(cellSize))
	}
	scratch = scratch[:int(cellSize)]
	if err := v.readPayloadAt(scratch, start); err != nil {
		return out, scratch, err
	}

	info, next, err := parseBOCPayloadCellInfo(scratch, 0, len(scratch), int(v.refSize), true)
	if err != nil {
		return out, scratch, fmt.Errorf("invalid cell #%d: %w", idx, err)
	}
	if next != len(scratch) {
		return out, scratch, fmt.Errorf("invalid indexed cell boundary for cell #%d", idx)
	}

	out = BOCCellView{
		Index: idx,
		D1:    info.dsc1 &^ 0b00010000,
		D2:    info.dsc2,
		Bits:  info.bitsSz,
		Body:  info.body(scratch),
		Refs:  bocCellRefsFromInfo(info, scratch, int(v.refSize)),
		Meta:  v.cellMeta(idx),
	}
	return out, scratch, nil
}

func readBOCViewHeader(r io.ReaderAt, size int64) (bocViewHeader, error) {
	var header bocViewHeader
	br := bocViewReader{r: r, size: size}

	var magic [4]byte
	if err := br.readFull(magic[:]); err != nil {
		return header, errors.New("invalid boc")
	}

	legacyIndexed := false
	switch {
	case matchBOCMagic(magic[:], bocMagic):
	case matchBOCMagic(magic[:], bocIdxMagic):
		legacyIndexed = true
	case matchBOCMagic(magic[:], bocIdxCRC32CMagic):
		legacyIndexed = true
	default:
		return header, errors.New("invalid boc magic header")
	}

	flagsByte, err := br.readByte()
	if err != nil {
		return header, fmt.Errorf("failed to read boc flags: %w", err)
	}
	flags, cellNumSizeBytes := parseBOCFlags(flagsByte)
	if legacyIndexed {
		flags.hasIndex = true
		flags.HasCrc32c = matchBOCMagic(magic[:], bocIdxCRC32CMagic)
		flags.hasCacheBits = false
	}
	if cellNumSizeBytes < 1 || cellNumSizeBytes > 4 {
		return header, errors.New("invalid boc size descriptor")
	}

	dataSize, err := br.readByte()
	if err != nil {
		return header, fmt.Errorf("failed to read boc offset descriptor: %w", err)
	}
	dataSizeBytes := int(dataSize)
	if dataSizeBytes < 1 || dataSizeBytes > 8 {
		return header, errors.New("invalid boc offset descriptor")
	}

	cellsNum, err := br.readDynInt(cellNumSizeBytes)
	if err != nil {
		return header, fmt.Errorf("failed to read cells count: %w", err)
	}
	rootsNum, err := br.readDynInt(cellNumSizeBytes)
	if err != nil {
		return header, fmt.Errorf("failed to read roots count: %w", err)
	}
	if cellsNum <= 0 || rootsNum <= 0 {
		return header, errors.New("invalid boc counters")
	}
	if legacyIndexed && rootsNum != 1 {
		return header, errors.New("invalid boc counters")
	}
	if MaxBOCCells > 0 && cellsNum > MaxBOCCells {
		return header, fmt.Errorf("too many cells in boc: %d > %d", cellsNum, MaxBOCCells)
	}
	if MaxBOCRoots > 0 && rootsNum > MaxBOCRoots {
		return header, fmt.Errorf("too many roots in boc: %d > %d", rootsNum, MaxBOCRoots)
	}

	absentNum, err := br.readDynInt(cellNumSizeBytes)
	if err != nil {
		return header, fmt.Errorf("failed to read absent count: %w", err)
	}
	if absentNum < 0 || absentNum > cellsNum || rootsNum+absentNum > cellsNum {
		return header, errors.New("invalid boc counters")
	}

	dataLen, err := br.readDynInt(dataSizeBytes)
	if err != nil {
		return header, fmt.Errorf("failed to read cells data size: %w", err)
	}
	if dataLen < cellsNum*2 {
		return header, errors.New("invalid boc cells data size")
	}
	maxPayloadBytes := maxBOCPayloadBytes()
	if maxPayloadBytes > 0 && dataLen > maxPayloadBytes {
		return header, fmt.Errorf("boc cells data size is too big: %d > %d", dataLen, maxPayloadBytes)
	}
	if cellsNum > 0 && dataLen > cellsNum*maxSerializedBOCCellBytes {
		return header, fmt.Errorf("boc cells data size is too big for cells count: data len %d, cells %d", dataLen, cellsNum)
	}
	if flags.hasCacheBits && !flags.hasIndex {
		return header, fmt.Errorf("cache flag cant be set without index flag")
	}

	rootsIndex := make([]uint32, rootsNum)
	if legacyIndexed {
		rootsIndex[0] = 0
	} else {
		for i := 0; i < rootsNum; i++ {
			rootIndex, err := br.readDynInt(cellNumSizeBytes)
			if err != nil {
				return header, fmt.Errorf("failed to read root index: %w", err)
			}
			if rootIndex < 0 || rootIndex >= cellsNum {
				return header, errors.New("invalid root index")
			}
			rootsIndex[i] = uint32(rootIndex)
		}
	}

	indexOffset := uint64(br.offset)
	if flags.hasIndex {
		indexBytes := uint64(cellsNum) * uint64(dataSizeBytes)
		if uint64(size)-indexOffset < indexBytes {
			return header, io.ErrUnexpectedEOF
		}
		br.offset += int64(indexBytes)
	}

	payloadStart := uint64(br.offset)
	crcOffset := payloadStart + uint64(dataLen)
	totalSize := crcOffset
	if flags.HasCrc32c {
		totalSize += 4
	}
	if totalSize != uint64(size) {
		return header, fmt.Errorf("unexpected trailing data after boc payload: %d bytes", uint64(size)-totalSize)
	}

	return bocViewHeader{
		flags:        flags,
		refSize:      uint8(cellNumSizeBytes),
		offsetSize:   uint8(dataSizeBytes),
		cells:        uint32(cellsNum),
		roots:        rootsIndex,
		payloadSize:  uint64(dataLen),
		indexOffset:  indexOffset,
		payloadStart: payloadStart,
		crcOffset:    crcOffset,
	}, nil
}

type bocViewReader struct {
	r      io.ReaderAt
	size   int64
	offset int64
}

func (r *bocViewReader) readByte() (byte, error) {
	var buf [1]byte
	if err := r.readFull(buf[:]); err != nil {
		return 0, err
	}
	return buf[0], nil
}

func (r *bocViewReader) readDynInt(size int) (int, error) {
	var buf [8]byte
	if size < 0 || size > len(buf) {
		return 0, errors.New("invalid dynamic integer size")
	}
	if err := r.readFull(buf[:size]); err != nil {
		return 0, err
	}
	return dynInt(buf[:size]), nil
}

func (r *bocViewReader) readFull(buf []byte) error {
	if len(buf) == 0 {
		return nil
	}
	if r.offset < 0 || int64(len(buf)) > r.size-r.offset {
		return io.ErrUnexpectedEOF
	}
	if _, err := r.r.ReadAt(buf, r.offset); err != nil {
		return err
	}
	r.offset += int64(len(buf))
	return nil
}

func (v *BOCView) loadIndex(indexOffset uint64) error {
	if v.offsetSize <= 4 {
		v.offsets32 = make([]uint32, v.cells)
	} else {
		v.offsets64 = make([]uint64, v.cells)
	}

	const indexReadChunkSize = 4 << 20
	entrySize := int(v.offsetSize)
	entriesPerChunk := indexReadChunkSize / entrySize
	if entriesPerChunk == 0 {
		entriesPerChunk = 1
	}
	data := make([]byte, entriesPerChunk*entrySize)

	for first := uint32(0); first < v.cells; {
		count := uint32(entriesPerChunk)
		if left := v.cells - first; count > left {
			count = left
		}
		chunkBytes := int(count) * entrySize
		offset := indexOffset + uint64(first)*uint64(v.offsetSize)
		if err := v.readAt(data[:chunkBytes], offset); err != nil {
			return fmt.Errorf("failed to read custom index, err: %w", err)
		}

		for j := uint32(0); j < count; j++ {
			pos := int(j) * entrySize
			raw := uint64(dynInt(data[pos : pos+entrySize]))
			if v.flags.hasCacheBits {
				if raw&1 != 0 {
					v.setCacheBit(first + j)
				}
				raw >>= 1
			}
			if v.offsets32 != nil {
				if raw > uint64(^uint32(0)) {
					return errors.New("boc index offset is too large")
				}
				v.offsets32[first+j] = uint32(raw)
			} else {
				v.offsets64[first+j] = raw
			}
		}
		first += count
	}

	prev := uint64(0)
	for i := uint32(0); i < v.cells; i++ {
		end := v.cellEnd(i)
		if end < prev || end > v.payloadSize {
			return errors.New("invalid cell index")
		}
		prev = end
	}
	if prev != v.payloadSize {
		return errors.New("invalid cell index")
	}
	return nil
}

func (v *BOCView) buildIndex() error {
	if v.payloadSize > uint64(^uint32(0)) {
		v.offsets64 = make([]uint64, v.cells)
	} else {
		v.offsets32 = make([]uint32, v.cells)
	}

	offset := uint64(0)
	for i := uint32(0); i < v.cells; i++ {
		end := v.payloadSize
		info, cellSize, err := v.readCellInfoAt(offset, end)
		if err != nil {
			return fmt.Errorf("invalid cell #%d: %w", i, err)
		}
		for ref := 0; ref < info.refsCount(); ref++ {
			refIdx, err := v.readRefIndexAt(offset+uint64(info.refsOffset), ref)
			if err != nil {
				return fmt.Errorf("invalid cell #%d: %w", i, err)
			}
			if refIdx == i {
				return errors.New("recursive reference of cells")
			}
			if refIdx < i {
				return errors.New("reference to index which is behind parent cell")
			}
			if refIdx >= v.cells {
				return errors.New("invalid index, out of scope")
			}
		}
		offset += cellSize
		v.setCellEnd(i, offset)
	}
	if offset != v.payloadSize {
		return errors.New("failed to parse cells payload, corrupted data")
	}
	return nil
}

func (v *BOCView) buildMeta() error {
	hashesCount := uint64(0)
	levelMasks := make([]byte, v.cells)
	scratch := []byte(nil)
	for i := uint32(0); i < v.cells; i++ {
		cell, nextScratch, err := v.readCellNoMeta(i, scratch)
		scratch = nextScratch
		if err != nil {
			return err
		}
		if err = v.validateCellRefs(i, cell.info, cell.data); err != nil {
			return fmt.Errorf("invalid cell #%d: %w", i, err)
		}
		levelMask := cell.info.levelMask()
		levelMasks[i] = levelMask.Mask
		hashesCount += uint64(levelMask.getHashesCount())
		if hashesCount > uint64(^uint32(0)) {
			return errors.New("too many cell hashes")
		}
	}

	v.meta = &bocViewMetaStore{
		offsets:    make([]uint32, v.cells),
		hashes:     make([]Hash, hashesCount),
		depths:     make([]uint16, hashesCount),
		levelMasks: levelMasks,
	}
	if v.trustedHashes {
		v.meta.stored = make([]byte, (v.cells+7)/8)
	}

	var offset uint32
	for i := uint32(0); i < v.cells; i++ {
		v.meta.offsets[i] = offset
		offset += uint32(LevelMask{Mask: levelMasks[i]}.getHashesCount())
	}

	for idx := v.cells; idx > 0; idx-- {
		i := idx - 1
		cell, nextScratch, err := v.readCellNoMeta(i, scratch)
		scratch = nextScratch
		if err != nil {
			return err
		}
		if v.trustStoredMeta(cell.info) {
			v.setStoredMeta(i)
			v.storeTrustedCellMeta(i, cell.info, cell.data)
			if cell.info.isSpecial() {
				typ := cell.info.cellType(cell.data)
				if typ == UnknownCellType {
					return fmt.Errorf("invalid cell #%d: unknown special cell type", i)
				}
				if err = v.validateSpecialCell(cell.info, cell.data, typ); err != nil {
					return fmt.Errorf("invalid cell #%d: %w", i, err)
				}
			}
			continue
		}
		if err = v.computeCellMeta(i, cell.info, cell.data); err != nil {
			return fmt.Errorf("invalid cell #%d: %w", i, err)
		}
	}
	return nil
}

func (v *BOCView) validateCellRefs(idx uint32, info bocPayloadCellInfo, data []byte) error {
	for ref := 0; ref < info.refsCount(); ref++ {
		refIdx := uint32(info.refIndex(data, ref, int(v.refSize)))
		if refIdx == idx {
			return errors.New("recursive reference of cells")
		}
		if refIdx < idx {
			return errors.New("reference to index which is behind parent cell")
		}
		if refIdx >= v.cells {
			return errors.New("invalid index, out of scope")
		}
	}
	return nil
}

type bocViewCellData struct {
	info bocPayloadCellInfo
	data []byte
}

func (v *BOCView) readCellNoMeta(idx uint32, scratch []byte) (bocViewCellData, []byte, error) {
	var out bocViewCellData
	start := v.cellStart(idx)
	end := v.cellEnd(idx)
	if end < start || end > v.payloadSize {
		return out, scratch, errors.New("invalid cell index")
	}
	cellSize := end - start
	if cellSize > maxSerializedBOCCellBytes {
		return out, scratch, fmt.Errorf("cell #%d is too large: %d", idx, cellSize)
	}
	if cap(scratch) < int(cellSize) {
		scratch = make([]byte, int(cellSize))
	}
	scratch = scratch[:int(cellSize)]
	if err := v.readPayloadAt(scratch, start); err != nil {
		return out, scratch, err
	}

	info, next, err := parseBOCPayloadCellInfo(scratch, 0, len(scratch), int(v.refSize), true)
	if err != nil {
		return out, scratch, fmt.Errorf("invalid cell #%d: %w", idx, err)
	}
	if next != len(scratch) {
		return out, scratch, fmt.Errorf("invalid indexed cell boundary for cell #%d", idx)
	}
	out.info = info
	out.data = scratch
	return out, scratch, nil
}

func (v *BOCView) readCellInfoAt(offset, end uint64) (bocPayloadCellInfo, uint64, error) {
	if end < offset || end > v.payloadSize {
		return bocPayloadCellInfo{}, 0, errors.New("invalid cell index")
	}
	if end-offset < 2 {
		return bocPayloadCellInfo{}, 0, errors.New("failed to parse cell header, corrupted data")
	}

	var header [2]byte
	if err := v.readPayloadAt(header[:], offset); err != nil {
		return bocPayloadCellInfo{}, 0, err
	}

	info := bocPayloadCellInfo{
		dsc1: header[0],
		dsc2: header[1],
	}
	refsNum := info.refsCount()
	if refsNum > 4 {
		return bocPayloadCellInfo{}, 0, errors.New("too many refs in cell")
	}

	pos := offset + 2
	if info.withHashes() {
		hashesCount := info.levelMask().getHashesCount()
		pos += uint64(hashesCount * (hashSize + depthSize))
	}

	bodyBytes := uint64(cellBodyBytesSize(info.dsc2))
	if end-pos < bodyBytes {
		return bocPayloadCellInfo{}, 0, errors.New("failed to read cell payload, corrupted data")
	}
	body := make([]byte, bodyBytes)
	if err := v.readPayloadAt(body, pos); err != nil {
		return bocPayloadCellInfo{}, 0, err
	}
	bitsSz := uint16(bodyBytes * 8)
	if info.dsc2%2 != 0 {
		var err error
		if bitsSz, err = cellBodyBitsSize(info.dsc2, body); err != nil {
			return bocPayloadCellInfo{}, 0, err
		}
	}
	info.bodyOffset = int(pos - offset)
	info.bitsSz = bitsSz
	pos += bodyBytes

	refsSize := uint64(refsNum) * uint64(v.refSize)
	if end-pos < refsSize {
		return bocPayloadCellInfo{}, 0, errors.New("failed to read cell refs, corrupted data")
	}
	info.refsOffset = int(pos - offset)
	pos += refsSize
	return info, pos - offset, nil
}

func (v *BOCView) computeCellMeta(idx uint32, info bocPayloadCellInfo, data []byte) error {
	typ := info.cellType(data)
	if info.isSpecial() && typ == UnknownCellType {
		return fmt.Errorf("unknown special cell type")
	}

	if typ == PrunedCellType {
		if err := v.computePrunedCellMeta(idx, info, data); err != nil {
			return err
		}
	} else {
		if err := v.computeRegularCellMeta(idx, info, data, typ); err != nil {
			return err
		}
	}
	if info.isSpecial() {
		if err := v.validateSpecialCell(info, data, typ); err != nil {
			return err
		}
	}
	if info.withHashes() && !v.trustedHashes {
		if err := v.validateStoredCellMeta(idx, info, data); err != nil {
			return err
		}
	}
	return nil
}

func (v *BOCView) storeTrustedCellMeta(idx uint32, info bocPayloadCellInfo, data []byte) {
	hashesCount := info.levelMask().getHashesCount()
	for hashIndex := 0; hashIndex < hashesCount; hashIndex++ {
		var hash Hash
		copy(hash[:], info.storedHash(data, hashIndex))
		v.setComputedMeta(idx, hashIndex, hash, info.storedDepth(data, hashIndex))
	}
}

func (v *BOCView) computeRegularCellMeta(idx uint32, info bocPayloadCellInfo, data []byte, typ Type) error {
	body := info.body(data)
	levelMask := info.levelMask()
	refCnt := info.refsCount()
	level := levelMask.GetLevel()
	isMerkle := typ == MerkleProofCellType || typ == MerkleUpdateCellType

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
			hash := v.computedMetaHash(idx, hashIndex-1)
			bufPos += copy(hashBuf[bufPos:], hash[:])
		}

		childLevelIndex := levelIndex
		if isMerkle {
			childLevelIndex++
		}

		var depth uint16
		for ref := 0; ref < refCnt; ref++ {
			refIdx := info.refIndex(data, ref, int(v.refSize))
			childDepth := v.cellMetaDepth(uint32(refIdx), childLevelIndex)
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
			refIdx := info.refIndex(data, ref, int(v.refSize))
			hash := v.cellMetaHash(uint32(refIdx), childLevelIndex)
			bufPos += copy(hashBuf[bufPos:], hash[:])
		}

		sum := sha256.Sum256(hashBuf[:bufPos])
		v.setComputedMeta(idx, hashIndex, sum, depth)
		hashIndex++
	}
	return nil
}

func (v *BOCView) computePrunedCellMeta(idx uint32, info bocPayloadCellInfo, data []byte) error {
	body := info.body(data)
	levelMask := info.levelMask()
	level := levelMask.GetLevel()
	storedMask := levelMask.Apply(level - 1)
	storedCount := storedMask.getHashesCount()

	hashesOffset := 2
	depthsOffset := hashesOffset + storedCount*hashSize
	for i := 0; i < storedCount; i++ {
		var hash Hash
		copy(hash[:], body[hashesOffset+i*hashSize:hashesOffset+(i+1)*hashSize])
		depth := binary.BigEndian.Uint16(body[depthsOffset+i*depthSize : depthsOffset+(i+1)*depthSize])
		v.setComputedMeta(idx, i, hash, depth)
	}

	var hashBuf [2 + maxCellDataBytes]byte
	hashBuf[0] = info.hashDescriptor1(levelMask)
	hashBuf[1] = info.dsc2
	bufPos := 2 + copy(hashBuf[2:], body)
	hashIndex := levelMask.getHashIndex()
	sum := sha256.Sum256(hashBuf[:bufPos])
	v.setComputedMeta(idx, hashIndex, sum, 0)
	return nil
}

func (v *BOCView) validateStoredCellMeta(idx uint32, info bocPayloadCellInfo, data []byte) error {
	hashIndex := 0
	for level := 0; level <= info.levelMask().GetLevel(); level++ {
		if !info.levelMask().IsSignificant(level) {
			continue
		}

		hash := v.computedMetaHash(idx, hashIndex)
		if !bytes.Equal(info.storedHash(data, hashIndex), hash[:]) {
			return fmt.Errorf("serialized hash mismatch at level %d", level)
		}
		if info.storedDepth(data, hashIndex) != v.computedMetaDepth(idx, hashIndex) {
			return fmt.Errorf("serialized depth mismatch at level %d", level)
		}
		hashIndex++
	}
	return nil
}

func (v *BOCView) validateSpecialCell(info bocPayloadCellInfo, data []byte, typ Type) error {
	body := info.body(data)
	switch typ {
	case PrunedCellType:
		if info.refsCount() != 0 {
			return fmt.Errorf("pruned branch special cell has a cell reference")
		}
		if len(body) < 2 {
			return fmt.Errorf("not enough data for a pruned branch special cell")
		}
		if info.levelMask().Mask != body[1] {
			return fmt.Errorf("pruned branch level mask mismatch")
		}
		level := info.levelMask().GetLevel()
		if level > _DataCellMaxLevel || level == 0 {
			return fmt.Errorf("pruned branch has an invalid level")
		}
		expectedBits := (2 + info.levelMask().Apply(level-1).getHashesCount()*(hashSize+depthSize)) * 8
		if int(info.bitsSz) != expectedBits {
			return fmt.Errorf("not enough data for a pruned branch special cell")
		}
	case LibraryCellType:
		if info.refsCount() != 0 {
			return fmt.Errorf("library special cell has a cell reference")
		}
		if info.bitsSz != 8+256 {
			return fmt.Errorf("not enough data for a library special cell")
		}
	case MerkleProofCellType:
		if info.bitsSz != 8+(hashSize+depthSize)*8 {
			return fmt.Errorf("not enough data for a merkle proof special cell")
		}
		if info.refsCount() != 1 {
			return fmt.Errorf("wrong references count for a merkle proof special cell")
		}
		refIdx := uint32(info.refIndex(data, 0, int(v.refSize)))
		refHash := v.cellMetaHash(refIdx, 0)
		if !bytes.Equal(body[1:1+hashSize], refHash[:]) {
			return fmt.Errorf("hash mismatch in a merkle proof special cell")
		}
		if binary.BigEndian.Uint16(body[1+hashSize:1+hashSize+depthSize]) != v.cellMetaDepth(refIdx, 0) {
			return fmt.Errorf("depth mismatch in a merkle proof special cell")
		}
		expectedMask := v.cellLevelMask(refIdx).Mask >> 1
		if info.levelMask().Mask != expectedMask {
			return fmt.Errorf("merkle proof level mask mismatch")
		}
	case MerkleUpdateCellType:
		if info.bitsSz != 8+(hashSize+depthSize)*8*2 {
			return fmt.Errorf("not enough data for a merkle update special cell")
		}
		if info.refsCount() != 2 {
			return fmt.Errorf("wrong references count for a merkle update special cell")
		}
		leftIdx := uint32(info.refIndex(data, 0, int(v.refSize)))
		rightIdx := uint32(info.refIndex(data, 1, int(v.refSize)))
		leftHash := v.cellMetaHash(leftIdx, 0)
		rightHash := v.cellMetaHash(rightIdx, 0)
		if !bytes.Equal(body[1:1+hashSize], leftHash[:]) {
			return fmt.Errorf("first hash mismatch in a merkle update special cell")
		}
		if !bytes.Equal(body[1+hashSize:1+hashSize*2], rightHash[:]) {
			return fmt.Errorf("second hash mismatch in a merkle update special cell")
		}
		firstDepthOff := 1 + hashSize*2
		secondDepthOff := firstDepthOff + depthSize
		if binary.BigEndian.Uint16(body[firstDepthOff:firstDepthOff+depthSize]) != v.cellMetaDepth(leftIdx, 0) {
			return fmt.Errorf("first depth mismatch in a merkle update special cell")
		}
		if binary.BigEndian.Uint16(body[secondDepthOff:secondDepthOff+depthSize]) != v.cellMetaDepth(rightIdx, 0) {
			return fmt.Errorf("second depth mismatch in a merkle update special cell")
		}
		expectedMask := (v.cellLevelMask(leftIdx).Mask | v.cellLevelMask(rightIdx).Mask) >> 1
		if info.levelMask().Mask != expectedMask {
			return fmt.Errorf("merkle update level mask mismatch")
		}
	default:
		return fmt.Errorf("unknown special cell type")
	}
	return nil
}

func (v *BOCView) cellMeta(idx uint32) BOCCellMeta {
	levelMask := v.cellLevelMask(idx)
	meta := BOCCellMeta{
		LevelMask: levelMask,
		Count:     uint8(levelMask.getHashesCount()),
		Stored:    v.isStoredMeta(idx),
	}
	for hashIndex := 0; hashIndex < int(meta.Count); hashIndex++ {
		meta.Hashes[hashIndex] = v.computedMetaHash(idx, hashIndex)
		meta.Depths[hashIndex] = v.computedMetaDepth(idx, hashIndex)
	}
	meta.Hash = meta.Hashes[meta.Count-1]
	return meta
}

func (v *BOCView) cellMetaHash(idx uint32, level int) Hash {
	hashIndex := v.cellLevelMask(idx).Apply(level).getHashIndex()
	return v.computedMetaHash(idx, hashIndex)
}

func (v *BOCView) cellMetaDepth(idx uint32, level int) uint16 {
	hashIndex := v.cellLevelMask(idx).Apply(level).getHashIndex()
	return v.computedMetaDepth(idx, hashIndex)
}

func (v *BOCView) cellLevelMask(idx uint32) LevelMask {
	return LevelMask{Mask: v.meta.levelMasks[idx]}
}

func (v *BOCView) computedMetaHash(idx uint32, hashIndex int) Hash {
	return v.meta.hashes[int(v.meta.offsets[idx])+hashIndex]
}

func (v *BOCView) computedMetaDepth(idx uint32, hashIndex int) uint16 {
	return v.meta.depths[int(v.meta.offsets[idx])+hashIndex]
}

func (v *BOCView) setComputedMeta(idx uint32, hashIndex int, hash Hash, depth uint16) {
	offset := int(v.meta.offsets[idx]) + hashIndex
	v.meta.hashes[offset] = hash
	v.meta.depths[offset] = depth
}

func (v *BOCView) setStoredMeta(idx uint32) {
	if len(v.meta.stored) == 0 {
		return
	}
	v.meta.stored[idx/8] |= 1 << (idx & 7)
}

func (v *BOCView) isStoredMeta(idx uint32) bool {
	return len(v.meta.stored) != 0 && v.meta.stored[idx/8]&(1<<(idx&7)) != 0
}

func (v *BOCView) trustStoredMeta(info bocPayloadCellInfo) bool {
	return v.trustedHashes && info.withHashes()
}

func bocCellRefsFromInfo(info bocPayloadCellInfo, data []byte, refSize int) BOCCellRefs {
	var refs BOCCellRefs
	refs.Count = uint8(info.refsCount())
	for i := 0; i < int(refs.Count); i++ {
		refs.Items[i] = uint32(info.refIndex(data, i, refSize))
	}
	return refs
}

func (v *BOCView) readRefIndexAt(refsOffset uint64, ref int) (uint32, error) {
	var buf [4]byte
	offset := refsOffset + uint64(ref*int(v.refSize))
	if err := v.readPayloadAt(buf[:v.refSize], offset); err != nil {
		return 0, err
	}
	return uint32(dynIntFromPayloadSize(int(v.refSize), buf[:v.refSize])), nil
}

func (v *BOCView) cellStart(idx uint32) uint64 {
	if idx == 0 {
		return 0
	}
	return v.cellEnd(idx - 1)
}

func (v *BOCView) cellEnd(idx uint32) uint64 {
	if v.offsets32 != nil {
		return uint64(v.offsets32[idx])
	}
	return v.offsets64[idx]
}

func (v *BOCView) setCellEnd(idx uint32, offset uint64) {
	if v.offsets32 != nil {
		v.offsets32[idx] = uint32(offset)
		return
	}
	v.offsets64[idx] = offset
}

func (v *BOCView) setCacheBit(idx uint32) {
	if v.cacheBits == nil {
		v.cacheBits = make([]byte, (v.cells+7)/8)
	}
	v.cacheBits[idx/8] |= 1 << (idx & 7)
}

func (v *BOCView) readPayloadAt(dst []byte, offset uint64) error {
	return v.readAt(dst, v.payloadOffset+offset)
}

func (v *BOCView) readAt(dst []byte, offset uint64) error {
	if len(dst) == 0 {
		return nil
	}
	if offset > uint64(v.size) || uint64(len(dst)) > uint64(v.size)-offset {
		return io.ErrUnexpectedEOF
	}
	if len(dst) > bocViewReadWindowSize/2 {
		_, err := v.r.ReadAt(dst, int64(offset))
		return err
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	windowEnd := v.windowStart + uint64(len(v.window))
	if offset >= v.windowStart && offset+uint64(len(dst)) <= windowEnd {
		copy(dst, v.window[offset-v.windowStart:])
		return nil
	}

	windowStart := offset / bocViewReadWindowSize * bocViewReadWindowSize
	windowSize := bocViewReadWindowSize
	if remain := uint64(v.size) - windowStart; remain < uint64(windowSize) {
		windowSize = int(remain)
	}
	if offset+uint64(len(dst)) > windowStart+uint64(windowSize) {
		_, err := v.r.ReadAt(dst, int64(offset))
		return err
	}
	if cap(v.window) < windowSize {
		v.window = make([]byte, windowSize)
	}
	v.window = v.window[:windowSize]
	if _, err := v.r.ReadAt(v.window, int64(windowStart)); err != nil {
		return err
	}
	v.windowStart = windowStart
	copy(dst, v.window[offset-windowStart:])
	return nil
}

func (v *BOCView) validateCRC() error {
	if v.crcOffset+4 > uint64(v.size) {
		return io.ErrUnexpectedEOF
	}

	var stored [4]byte
	if err := v.readAt(stored[:], v.crcOffset); err != nil {
		return fmt.Errorf("failed to read crc32c trailer: %w", err)
	}

	crc := uint32(0)
	buf := make([]byte, bocViewReadWindowSize)
	for offset := uint64(0); offset < v.crcOffset; {
		n := len(buf)
		if remain := v.crcOffset - offset; remain < uint64(n) {
			n = int(remain)
		}
		if _, err := v.r.ReadAt(buf[:n], int64(offset)); err != nil {
			return err
		}
		crc = crc32.Update(crc, castTable, buf[:n])
		offset += uint64(n)
	}
	if binary.LittleEndian.Uint32(stored[:]) != crc {
		return errors.New("checksum not matches")
	}
	return nil
}
