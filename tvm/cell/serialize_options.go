package cell

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
)

const (
	bocModeWithIndex = 1 << iota
	bocModeWithCRC32C
	bocModeWithTopHash
	bocModeWithIntHashes
	bocModeWithCacheBits
)

const bocMaxCellWeight = 64

const (
	bocInvalidCellIndex uint32 = ^uint32(0)
	bocVisitScanned            = bocInvalidCellIndex - 1
	bocVisitLinked             = bocInvalidCellIndex - 2
)

// BOCSerializeOptions configures BoC serialization.
type BOCSerializeOptions struct {
	WithCRC32C    bool
	WithIndex     bool
	WithCacheBits bool
	WithTopHash   bool
	WithIntHashes bool
	// CellsCountHint pre-sizes serializer dedup structures for the expected
	// number of unique cells, avoiding rehash and regrow work on large BoCs.
	// Zero or a negative value uses default sizing.
	CellsCountHint int
}

func (o BOCSerializeOptions) mode() int {
	mode := 0
	if o.WithIndex {
		mode |= bocModeWithIndex
	}
	if o.WithCRC32C {
		mode |= bocModeWithCRC32C
	}
	if o.WithTopHash {
		mode |= bocModeWithTopHash
	}
	if o.WithIntHashes {
		mode |= bocModeWithIntHashes
	}
	if o.WithCacheBits {
		mode |= bocModeWithCacheBits
	}
	return mode
}

func newBOCSerializer(roots []*Cell, cellsCountHint int) (*bocSerializer, error) {
	if len(roots) == 0 {
		return nil, nil
	}

	bag := &bocSerializer{
		roots:     make([]bocRoot, len(roots)),
		maxDepth:  maxDepth,
		cellIndex: newBOCHashIndex(cellsCountHint),
	}
	if cellsCountHint > 0 {
		bag.cellList = make([]bocSerializeItem, 0, cellsCountHint)
	}
	for i, root := range roots {
		bag.roots[i] = bocRoot{cell: root, idx: bocInvalidCellIndex}
	}

	if err := bag.importCells(); err != nil {
		return nil, err
	}

	return bag, nil
}

func ToBOCWithOptions(roots []*Cell, opts BOCSerializeOptions) []byte {
	boc, err := ToBOCWithOptionsErr(roots, opts)
	if err != nil {
		return nil
	}
	return boc
}

func ToBOCWithOptionsErr(roots []*Cell, opts BOCSerializeOptions) ([]byte, error) {
	bag, err := newBOCSerializer(roots, opts.CellsCountHint)
	if err != nil {
		return nil, err
	}
	if bag == nil {
		return nil, nil
	}

	boc := bag.serialize(opts.mode())
	if boc == nil {
		return nil, fmt.Errorf("failed to serialize boc")
	}
	return boc, nil
}

// AppendBOCWithOptions serializes roots into BoC and appends the result to dst.
func AppendBOCWithOptions(dst []byte, roots []*Cell, opts BOCSerializeOptions) ([]byte, error) {
	bag, err := newBOCSerializer(roots, opts.CellsCountHint)
	if err != nil {
		return nil, err
	}
	if bag == nil {
		return dst, nil
	}

	return bag.appendTo(dst, opts.mode())
}

// WriteBOCWithOptions serializes roots into BoC and writes the result to w.
// It avoids allocating the final BoC byte slice, but still performs the normal
// prepass needed to order cells, resolve lazy refs, deduplicate cells, and build
// optional indexes.
func WriteBOCWithOptions(w io.Writer, roots []*Cell, opts BOCSerializeOptions) error {
	if w == nil {
		return fmt.Errorf("writer is nil")
	}

	bag, err := newBOCSerializer(roots, opts.CellsCountHint)
	if err != nil {
		return err
	}
	if bag == nil {
		return nil
	}

	return bag.writeTo(w, opts.mode())
}

func (c *Cell) ToBOCWithOptions(opts BOCSerializeOptions) []byte {
	boc, err := c.ToBOCWithOptionsErr(opts)
	if err != nil {
		return nil
	}
	return boc
}

func (c *Cell) ToBOCWithOptionsErr(opts BOCSerializeOptions) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("cell is nil")
	}
	return ToBOCWithOptionsErr([]*Cell{c.rawCell()}, opts)
}

// AppendBOCWithOptions serializes c into BoC and appends the result to dst.
func (c *Cell) AppendBOCWithOptions(dst []byte, opts BOCSerializeOptions) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("cell is nil")
	}
	return AppendBOCWithOptions(dst, []*Cell{c.rawCell()}, opts)
}

func ComputeFileHash(root *Cell) []byte {
	if root == nil {
		return nil
	}

	opts := BOCSerializeOptions{
		WithCRC32C:    true,
		WithIndex:     true,
		WithCacheBits: true,
		WithTopHash:   true,
		WithIntHashes: true,
	}

	bag, err := newBOCSerializer([]*Cell{root}, opts.CellsCountHint)
	if err != nil || bag == nil {
		return nil
	}

	fileHash, ok := bag.computeFileHash(opts.mode())
	if !ok {
		return nil
	}

	return fileHash
}

type bocRoot struct {
	cell *Cell
	idx  uint32
}

type bocSerializeInfo struct {
	refByteSize    int
	offsetByteSize int
	dataSize       uint64
	rootsOffset    int
	indexOffset    int
	dataOffset     int
	totalSize      uint64
	hasIndex       bool
	hasCRC32C      bool
	hasCacheBits   bool
}

type bocSerializeItem struct {
	cell        *Cell
	refIdx      [4]uint32
	refNum      uint8
	wt          byte
	hcnt        byte
	shouldCache bool
	isRootCell  bool
}

func (i bocSerializeItem) isSpecial() bool {
	return i.wt == 0
}

type bocSerializer struct {
	cellCount int
	intRefs   int
	intHashes int
	topHashes int
	maxDepth  int

	dataBytes uint64

	cellIndex *bocHashIndex

	cellList []bocSerializeItem
	roots    []bocRoot
}

func (s *bocSerializer) importCells() error {
	for i := range s.roots {
		idx, err := s.importCell(s.roots[i].cell, 0)
		if err != nil {
			return err
		}
		s.roots[i].idx = idx
	}

	s.cellIndex = nil
	s.reorderCells()
	if s.cellCount == 0 {
		return fmt.Errorf("no cells to serialize")
	}
	return nil
}

func (s *bocSerializer) importCell(cell *Cell, depth int) (uint32, error) {
	if depth > s.maxDepth {
		return 0, fmt.Errorf("cell depth too large")
	}
	if cell == nil {
		return 0, fmt.Errorf("cell is nil")
	}
	cell = cell.rawCell()
	if cell.IsLazy() {
		if pos, found := s.findImportedCell(cell); found {
			return pos, nil
		}

		loaded, err := loadLazyPrunedRef(cell)
		if err != nil {
			return 0, err
		}
		cell = loaded.rawCell()
	}

	hash := cell.getHash(_DataCellMaxLevel)
	refCnt := cell.refsCount()
	hashFingerprint := bocHashFingerprint(hash)
	pos, found := s.cellIndex.get(hashFingerprint, hash, cell, s.cellList)
	if found {
		s.cellList[int(pos)].shouldCache = true
		return pos, nil
	}

	refView := newCellRefView(cell)
	refs := [4]uint32{
		bocInvalidCellIndex,
		bocInvalidCellIndex,
		bocInvalidCellIndex,
		bocInvalidCellIndex,
	}
	sumChildWeight := 1
	for i := 0; i < refCnt; i++ {
		boundaryRef, err := refView.boundaryRef(i)
		if err != nil {
			return 0, fmt.Errorf("failed to load ref %d: %w", i, err)
		}
		if boundaryRef != nil && boundaryRef.IsLazy() {
			refIdx, found := s.findImportedCell(boundaryRef)
			if found {
				refs[i] = refIdx
				sumChildWeight += int(s.cellList[int(refIdx)].wt)
				s.intRefs++
				continue
			}
		}

		ref, err := boundaryRef.load()
		if err != nil {
			return 0, fmt.Errorf("failed to load ref %d: %w", i, err)
		}

		refIdx, err := s.importCell(ref, depth+1)
		if err != nil {
			return 0, err
		}
		refs[i] = refIdx
		sumChildWeight += int(s.cellList[int(refIdx)].wt)
		s.intRefs++
	}

	wt := sumChildWeight
	if wt > 0xFF {
		wt = 0xFF
	}

	idx := s.cellCount
	if uint64(idx) >= uint64(bocVisitLinked) {
		return 0, fmt.Errorf("too many cells to serialize")
	}

	s.cellList = append(s.cellList, bocSerializeItem{
		cell:   cell,
		refIdx: refs,
		refNum: uint8(refCnt),
		wt:     byte(wt),
		hcnt:   byte(cell.getLevelMask().getHashesCount()),
	})
	s.cellIndex.set(hashFingerprint, uint32(idx))
	s.dataBytes += uint64(cell.serializedBOCSize(false))

	s.cellCount++
	return uint32(idx), nil
}

func (s *bocSerializer) findImportedCell(cell *Cell) (uint32, bool) {
	hash := cell.getHash(_DataCellMaxLevel)
	hashFingerprint := bocHashFingerprint(hash)
	pos, found := s.cellIndex.get(hashFingerprint, hash, cell, s.cellList)
	if found {
		s.cellList[int(pos)].shouldCache = true
	}
	return pos, found
}

func (s *bocSerializer) reorderCells() {
	s.intHashes = 0
	for i := s.cellCount - 1; i >= 0; i-- {
		item := &s.cellList[i]
		refsCnt := int(item.refNum)
		remaining, sum, mask := refsCnt, bocMaxCellWeight-1, 0

		for j := 0; j < refsCnt; j++ {
			child := &s.cellList[int(item.refIdx[j])]
			limit := (bocMaxCellWeight - 1 + j) / refsCnt
			if int(child.wt) <= limit {
				sum -= int(child.wt)
				remaining--
				mask |= 1 << j
			}
		}

		if remaining == 0 {
			continue
		}

		for j := 0; j < refsCnt; j++ {
			if mask&(1<<j) != 0 {
				continue
			}

			child := &s.cellList[int(item.refIdx[j])]
			limit := sum / remaining
			sum++
			if int(child.wt) > limit {
				child.wt = byte(limit)
			}
		}
	}

	for i := 0; i < s.cellCount; i++ {
		item := &s.cellList[i]
		sum := 1
		for j := 0; j < int(item.refNum); j++ {
			sum += int(s.cellList[int(item.refIdx[j])].wt)
		}

		if sum <= int(item.wt) {
			item.wt = byte(sum)
			continue
		}

		item.wt = 0
		s.intHashes += int(item.hcnt)
	}

	s.topHashes = 0
	for i := range s.roots {
		item := &s.cellList[int(s.roots[i].idx)]
		if !item.isRootCell {
			item.isRootCell = true
			if item.wt != 0 {
				s.topHashes += int(item.hcnt)
			}
		}
	}

	if s.cellCount == 0 {
		return
	}

	newIdx := make([]uint32, s.cellCount)
	for i := range newIdx {
		newIdx[i] = bocInvalidCellIndex
	}
	var nextIdx uint32

	for _, root := range s.roots {
		s.revisit(root.idx, 0, newIdx, &nextIdx)
		s.revisit(root.idx, 1, newIdx, &nextIdx)
	}
	for i := range s.roots {
		s.roots[i].idx = s.revisit(s.roots[i].idx, 2, newIdx, &nextIdx)
	}

	s.reorderInPlace(newIdx)
}

func (s *bocSerializer) revisit(cellIdx uint32, force int, newIdx []uint32, nextIdx *uint32) uint32 {
	pos := int(cellIdx)
	item := &s.cellList[pos]

	if newIdx[pos] < bocVisitLinked {
		return newIdx[pos]
	}

	if force == 0 {
		if newIdx[pos] != bocInvalidCellIndex {
			return newIdx[pos]
		}

		for j := int(item.refNum) - 1; j >= 0; j-- {
			childIdx := item.refIdx[j]
			childForce := 0
			if s.cellList[int(childIdx)].isSpecial() {
				childForce = 1
			}
			s.revisit(childIdx, childForce, newIdx, nextIdx)
		}

		newIdx[pos] = bocVisitScanned
		return newIdx[pos]
	}

	if force > 1 {
		idx := *nextIdx
		*nextIdx = idx + 1
		newIdx[pos] = idx
		return idx
	}

	if newIdx[pos] == bocVisitLinked {
		return newIdx[pos]
	}

	if item.isSpecial() {
		s.revisit(cellIdx, 0, newIdx, nextIdx)
	}

	for j := int(item.refNum) - 1; j >= 0; j-- {
		s.revisit(item.refIdx[j], 1, newIdx, nextIdx)
	}
	for j := int(item.refNum) - 1; j >= 0; j-- {
		item.refIdx[j] = s.revisit(item.refIdx[j], 2, newIdx, nextIdx)
	}

	newIdx[pos] = bocVisitLinked
	return newIdx[pos]
}

func (s *bocSerializer) reorderInPlace(indexes []uint32) {
	for i := 0; i < s.cellCount; i++ {
		for indexes[i] != uint32(i) {
			dst := int(indexes[i])
			s.cellList[i], s.cellList[dst] = s.cellList[dst], s.cellList[i]
			indexes[i], indexes[dst] = indexes[dst], indexes[i]
		}
	}
}

func (s *bocSerializer) computeSizes(mode int) (uint64, int, int) {
	if len(s.roots) == 0 || s.dataBytes == 0 {
		return 0, 0, 0
	}

	refByteSize := 0
	for uint64(s.cellCount) >= (uint64(1) << uint(refByteSize*8)) {
		refByteSize++
	}

	hashBytes := 0
	if mode&bocModeWithTopHash != 0 {
		hashBytes += s.topHashes
	}
	if mode&bocModeWithIntHashes != 0 {
		hashBytes += s.intHashes
	}

	dataSize := s.dataBytes + uint64(s.intRefs)*uint64(refByteSize) + uint64(hashBytes)*uint64(hashSize+depthSize)
	maxOffset := dataSize
	if mode&bocModeWithCacheBits != 0 {
		maxOffset *= 2
	}

	offsetByteSize := 0
	for maxOffset >= (uint64(1) << uint(offsetByteSize*8)) {
		offsetByteSize++
	}

	if refByteSize < 1 || refByteSize > 4 || offsetByteSize < 1 || offsetByteSize > 8 {
		return 0, 0, 0
	}

	return dataSize, refByteSize, offsetByteSize
}

func (s *bocSerializer) estimateSerializedSize(mode int) (bocSerializeInfo, bool) {
	if mode&bocModeWithCacheBits != 0 && mode&bocModeWithIndex == 0 {
		return bocSerializeInfo{}, false
	}

	dataSize, refByteSize, offsetByteSize := s.computeSizes(mode)
	if dataSize == 0 {
		return bocSerializeInfo{}, false
	}

	info := bocSerializeInfo{
		refByteSize:    refByteSize,
		offsetByteSize: offsetByteSize,
		dataSize:       dataSize,
		hasIndex:       mode&bocModeWithIndex != 0,
		hasCRC32C:      mode&bocModeWithCRC32C != 0,
		hasCacheBits:   mode&bocModeWithCacheBits != 0,
	}

	info.rootsOffset = 4 + 1 + 1 + 3*info.refByteSize + info.offsetByteSize
	info.indexOffset = info.rootsOffset + len(s.roots)*info.refByteSize
	info.dataOffset = info.indexOffset
	if info.hasIndex {
		info.dataOffset += s.cellCount * info.offsetByteSize
	}

	info.totalSize = uint64(info.dataOffset) + info.dataSize
	if info.hasCRC32C {
		info.totalSize += 4
	}

	return info, true
}

func (s *bocSerializer) serialize(mode int) []byte {
	data, err := s.appendTo(nil, mode)
	if err != nil {
		return nil
	}
	return data
}

func (s *bocSerializer) appendTo(dst []byte, mode int) ([]byte, error) {
	info, ok := s.estimateSerializedSize(mode)
	if !ok {
		return nil, fmt.Errorf("failed to serialize boc")
	}

	maxInt := int(^uint(0) >> 1)
	if info.totalSize > uint64(maxInt-len(dst)) {
		return nil, fmt.Errorf("boc is too big")
	}

	start := len(dst)
	end := start + int(info.totalSize)
	if cap(dst) < end {
		next := make([]byte, end)
		copy(next, dst)
		dst = next
	} else {
		dst = dst[:end]
	}

	data := dst[start:end]
	pos := 0

	storeUint := func(value uint64, sz int) {
		storeUintTo(data[pos:pos+sz], value, sz)
		pos += sz
	}

	copy(data[pos:], bocMagic)
	pos += len(bocMagic)

	flags := byte(info.refByteSize)
	if info.hasIndex {
		flags |= 1 << 7
	}
	if info.hasCRC32C {
		flags |= 1 << 6
	}
	if info.hasCacheBits {
		flags |= 1 << 5
	}
	data[pos] = flags
	pos++

	data[pos] = byte(info.offsetByteSize)
	pos++

	storeUint(uint64(s.cellCount), info.refByteSize)
	storeUint(uint64(len(s.roots)), info.refByteSize)
	storeUint(0, info.refByteSize)
	storeUint(info.dataSize, info.offsetByteSize)

	for _, root := range s.roots {
		storeUint(uint64(s.cellCount-1-int(root.idx)), info.refByteSize)
	}

	if info.hasIndex {
		var offs uint64
		for n := s.cellCount; n > 0; n-- {
			i := n - 1
			item := &s.cellList[i]
			offs += uint64(item.cell.serializedBOCSize(s.shouldSerializeHashes(item, mode)) + int(item.refNum)*info.refByteSize)

			offset := offs
			if info.hasCacheBits {
				offset = offs * 2
				if item.shouldCache {
					offset++
				}
			}
			storeUint(offset, info.offsetByteSize)
		}
	}

	for i := 0; i < s.cellCount; i++ {
		item := &s.cellList[s.cellCount-1-i]
		pos += item.cell.serializeBOCTo(data[pos:], s.shouldSerializeHashes(item, mode))

		for j := 0; j < int(item.refNum); j++ {
			storeUint(uint64(s.cellCount-1-int(item.refIdx[j])), info.refByteSize)
		}
	}

	if info.hasCRC32C {
		crc := crc32.Checksum(data[:pos], castTable)
		binary.LittleEndian.PutUint32(data[pos:], crc)
	}

	return dst, nil
}

const bocStreamBufferSize = 64 << 10

// Hashing benefits from batching the many small serializer writes, while a
// smaller cap keeps ComputeFileHash from allocating a near-BoC-sized buffer.
const bocHashBufferSize = 4 << 10

func bocAdaptiveBufferSize(totalSize uint64, limit int) int {
	if totalSize > 0 && totalSize < uint64(limit) {
		return int(totalSize)
	}
	return limit
}

type bocStreamWriter struct {
	dst io.Writer
	crc uint32

	buf []byte
	pos int

	crcActive bool
	err       error
}

func newBOCStreamWriter(dst io.Writer, crcActive bool, bufferSize int) bocStreamWriter {
	if bufferSize <= 0 {
		bufferSize = bocStreamBufferSize
	}
	return bocStreamWriter{
		dst:       dst,
		buf:       make([]byte, bufferSize),
		crcActive: crcActive,
	}
}

func (w *bocStreamWriter) write(data []byte) {
	if w.err != nil || len(data) == 0 {
		return
	}
	if len(data) >= len(w.buf) {
		w.flush()
		if w.err != nil {
			return
		}
		w.writeFull(data)
		return
	}
	if len(data) > len(w.buf)-w.pos {
		w.flush()
		if w.err != nil {
			return
		}
	}

	copy(w.buf[w.pos:], data)
	w.pos += len(data)
}

func (w *bocStreamWriter) writeByte(value byte) {
	if w.err != nil {
		return
	}
	if w.pos == len(w.buf) {
		w.flush()
		if w.err != nil {
			return
		}
	}

	w.buf[w.pos] = value
	w.pos++
}

func (w *bocStreamWriter) writeUint(value uint64, sz int) {
	if w.err != nil {
		return
	}
	if len(w.buf)-w.pos < sz {
		w.flush()
		if w.err != nil {
			return
		}
	}

	storeUintTo(w.buf[w.pos:w.pos+sz], value, sz)
	w.pos += sz
}

func (w *bocStreamWriter) finish() error {
	if w.err != nil {
		return w.err
	}
	w.flush()
	return w.err
}

func (w *bocStreamWriter) currentCRC() uint32 {
	if !w.crcActive || w.pos == 0 {
		return w.crc
	}
	return crc32.Update(w.crc, castTable, w.buf[:w.pos])
}

func (w *bocStreamWriter) flush() {
	if w.err != nil || w.pos == 0 {
		return
	}

	data := w.buf[:w.pos]
	if w.crcActive {
		w.crc = crc32.Update(w.crc, castTable, data)
	}

	n, err := w.dst.Write(data)
	if err == nil && n != w.pos {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.err = err
		return
	}
	w.pos = 0
}

func (w *bocStreamWriter) writeFull(data []byte) {
	if w.crcActive {
		w.crc = crc32.Update(w.crc, castTable, data)
	}

	n, err := w.dst.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.err = err
	}
}

func (s *bocSerializer) writeTo(dst io.Writer, mode int) error {
	info, ok := s.estimateSerializedSize(mode)
	if !ok {
		return fmt.Errorf("failed to serialize boc")
	}

	w := newBOCStreamWriter(dst, info.hasCRC32C, bocAdaptiveBufferSize(info.totalSize, bocStreamBufferSize))

	w.write(bocMagic)

	flags := byte(info.refByteSize)
	if info.hasIndex {
		flags |= 1 << 7
	}
	if info.hasCRC32C {
		flags |= 1 << 6
	}
	if info.hasCacheBits {
		flags |= 1 << 5
	}
	w.writeByte(flags)
	w.writeByte(byte(info.offsetByteSize))

	w.writeUint(uint64(s.cellCount), info.refByteSize)
	w.writeUint(uint64(len(s.roots)), info.refByteSize)
	w.writeUint(0, info.refByteSize)
	w.writeUint(info.dataSize, info.offsetByteSize)

	for _, root := range s.roots {
		w.writeUint(uint64(s.cellCount-1-int(root.idx)), info.refByteSize)
	}

	if info.hasIndex {
		var offs uint64
		for n := s.cellCount; n > 0; n-- {
			i := n - 1
			item := &s.cellList[i]
			offs += uint64(item.cell.serializedBOCSize(s.shouldSerializeHashes(item, mode)) + int(item.refNum)*info.refByteSize)

			offset := offs
			if info.hasCacheBits {
				offset = offs * 2
				if item.shouldCache {
					offset++
				}
			}
			w.writeUint(offset, info.offsetByteSize)
		}
	}

	var cellBuf [2 + maxCellDataBytes + 4*(hashSize+depthSize)]byte
	for i := 0; i < s.cellCount; i++ {
		item := &s.cellList[s.cellCount-1-i]
		n := item.cell.serializeBOCTo(cellBuf[:], s.shouldSerializeHashes(item, mode))
		w.write(cellBuf[:n])

		for j := 0; j < int(item.refNum); j++ {
			w.writeUint(uint64(s.cellCount-1-int(item.refIdx[j])), info.refByteSize)
		}
	}

	if info.hasCRC32C {
		var crcBuf [4]byte
		binary.LittleEndian.PutUint32(crcBuf[:], w.currentCRC())
		w.crcActive = false
		w.write(crcBuf[:])
	}

	return w.finish()
}

type bocHashWriter struct {
	sum hash.Hash
	crc uint32

	buf       []byte
	pos       int
	crcActive bool
}

func newBOCHashWriter(sum hash.Hash, crcActive bool, bufferSize int) bocHashWriter {
	if bufferSize <= 0 {
		bufferSize = bocHashBufferSize
	}
	return bocHashWriter{
		sum:       sum,
		buf:       make([]byte, bufferSize),
		crcActive: crcActive,
	}
}

func (w *bocHashWriter) write(data []byte) {
	if len(data) == 0 {
		return
	}
	if len(data) >= len(w.buf) {
		w.flush()
		w.writeFull(data)
		return
	}
	if len(data) > len(w.buf)-w.pos {
		w.flush()
	}

	copy(w.buf[w.pos:], data)
	w.pos += len(data)
}

func (w *bocHashWriter) writeFull(data []byte) {
	_, _ = w.sum.Write(data)
	if w.crcActive {
		w.crc = crc32.Update(w.crc, castTable, data)
	}
}

func (w *bocHashWriter) writeByte(value byte) {
	if w.pos == len(w.buf) {
		w.flush()
	}

	w.buf[w.pos] = value
	w.pos++
}

func (w *bocHashWriter) writeUint(value uint64, sz int) {
	if len(w.buf)-w.pos < sz {
		w.flush()
	}

	storeUintTo(w.buf[w.pos:w.pos+sz], value, sz)
	w.pos += sz
}

func (w *bocHashWriter) currentCRC() uint32 {
	if !w.crcActive || w.pos == 0 {
		return w.crc
	}
	return crc32.Update(w.crc, castTable, w.buf[:w.pos])
}

func (w *bocHashWriter) flush() {
	if w.pos == 0 {
		return
	}

	w.writeFull(w.buf[:w.pos])
	w.pos = 0
}

func (s *bocSerializer) computeFileHash(mode int) ([]byte, bool) {
	info, ok := s.estimateSerializedSize(mode)
	if !ok {
		return nil, false
	}

	h := sha256.New()
	w := newBOCHashWriter(h, info.hasCRC32C, bocAdaptiveBufferSize(info.totalSize, bocHashBufferSize))

	w.write(bocMagic)

	flags := byte(info.refByteSize)
	if info.hasIndex {
		flags |= 1 << 7
	}
	if info.hasCRC32C {
		flags |= 1 << 6
	}
	if info.hasCacheBits {
		flags |= 1 << 5
	}
	w.writeByte(flags)
	w.writeByte(byte(info.offsetByteSize))

	w.writeUint(uint64(s.cellCount), info.refByteSize)
	w.writeUint(uint64(len(s.roots)), info.refByteSize)
	w.writeUint(0, info.refByteSize)
	w.writeUint(info.dataSize, info.offsetByteSize)

	for _, root := range s.roots {
		w.writeUint(uint64(s.cellCount-1-int(root.idx)), info.refByteSize)
	}

	if info.hasIndex {
		var offs uint64
		for n := s.cellCount; n > 0; n-- {
			i := n - 1
			item := &s.cellList[i]
			offs += uint64(item.cell.serializedBOCSize(s.shouldSerializeHashes(item, mode)) + int(item.refNum)*info.refByteSize)

			offset := offs
			if info.hasCacheBits {
				offset = offs * 2
				if item.shouldCache {
					offset++
				}
			}
			w.writeUint(offset, info.offsetByteSize)
		}
	}

	var cellBuf [2 + maxCellDataBytes + 4*(hashSize+depthSize)]byte
	for i := 0; i < s.cellCount; i++ {
		item := &s.cellList[s.cellCount-1-i]
		n := item.cell.serializeBOCTo(cellBuf[:], s.shouldSerializeHashes(item, mode))
		w.write(cellBuf[:n])

		for j := 0; j < int(item.refNum); j++ {
			w.writeUint(uint64(s.cellCount-1-int(item.refIdx[j])), info.refByteSize)
		}
	}

	if info.hasCRC32C {
		var crcBuf [4]byte
		binary.LittleEndian.PutUint32(crcBuf[:], w.currentCRC())
		w.crcActive = false
		w.write(crcBuf[:])
	}
	w.flush()

	var out [sha256.Size]byte
	return h.Sum(out[:0]), true
}

func (s *bocSerializer) shouldSerializeHashes(item *bocSerializeItem, mode int) bool {
	withHashes := mode&bocModeWithIntHashes != 0 && item.wt == 0
	if item.isRootCell && mode&bocModeWithTopHash != 0 {
		withHashes = true
	}
	return withHashes
}

func (c *Cell) serializedBOCSize(withHashes bool) int {
	size := 2 + c.SerializedBOCBodySize()
	if withHashes {
		size += c.getLevelMask().getHashesCount() * (hashSize + depthSize)
	}
	return size
}

func (c *Cell) serializeBOCTo(dst []byte, withHashes bool) int {
	levelMask := c.getLevelMask()
	dst[0], dst[1] = c.descriptors(levelMask)

	offset := 2
	if withHashes {
		dst[0] |= 16
		level := levelMask.GetLevel()

		for i := 0; i <= level; i++ {
			if !levelMask.IsSignificant(i) {
				continue
			}
			copy(dst[offset:offset+hashSize], c.getHash(i))
			offset += hashSize
		}
		for i := 0; i <= level; i++ {
			if !levelMask.IsSignificant(i) {
				continue
			}
			binary.BigEndian.PutUint16(dst[offset:offset+depthSize], c.getDepth(i))
			offset += depthSize
		}
	}

	return offset + c.SerializeBOCBodyTo(dst[offset:])
}

func (c *Cell) SerializedBOCBodySize() int {
	return int((uint(c.bitsSz) + 7) / 8)
}

func (c *Cell) SerializeBOCBodyTo(dst []byte) int {
	bodySize := c.SerializedBOCBodySize()
	if bodySize == 0 {
		return 0
	}

	copy(dst[:bodySize], c.data[:bodySize])
	if tailBits := c.bitsSz % 8; tailBits != 0 {
		dst[bodySize-1] |= 1 << (7 - tailBits)
	}
	return bodySize
}
