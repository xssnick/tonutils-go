package cell

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
	"hash/crc32"
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

func newBOCSerializer(roots []*Cell) (*bocSerializer, error) {
	if len(roots) == 0 {
		return nil, nil
	}

	bag := &bocSerializer{
		roots:     make([]bocRoot, len(roots)),
		maxDepth:  maxDepth,
		cellIndex: &bocHashIndex{},
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
	bag, err := newBOCSerializer(roots)
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

	bag, err := newBOCSerializer([]*Cell{root})
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

	cellOrder []uint32
}

func (s *bocSerializer) importCells() error {
	for i := range s.roots {
		idx, err := s.importCell(s.roots[i].cell, 0)
		if err != nil {
			return err
		}
		s.roots[i].idx = idx
	}

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
		ref, err := refView.resolvedRef(i)
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
	s.cellOrder = make([]uint32, 0, s.cellCount)
	var nextIdx uint32

	for _, root := range s.roots {
		s.revisit(root.idx, 0, newIdx, &nextIdx)
		s.revisit(root.idx, 1, newIdx, &nextIdx)
	}
	for i := range s.roots {
		s.roots[i].idx = s.revisit(s.roots[i].idx, 2, newIdx, &nextIdx)
	}
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
		s.cellOrder = append(s.cellOrder, cellIdx)
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

func (s *bocSerializer) itemByIndex(idx int) *bocSerializeItem {
	if s.cellOrder != nil {
		return &s.cellList[int(s.cellOrder[idx])]
	}
	return &s.cellList[idx]
}

func (s *bocSerializer) serialize(mode int) []byte {
	info, ok := s.estimateSerializedSize(mode)
	if !ok {
		return nil
	}

	data := make([]byte, info.totalSize)
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
			item := s.itemByIndex(i)
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
		item := s.itemByIndex(s.cellCount - 1 - i)
		pos += item.cell.serializeBOCTo(data[pos:], s.shouldSerializeHashes(item, mode))

		for j := 0; j < int(item.refNum); j++ {
			storeUint(uint64(s.cellCount-1-int(item.refIdx[j])), info.refByteSize)
		}
	}

	if info.hasCRC32C {
		crc := crc32.Checksum(data[:pos], castTable)
		binary.LittleEndian.PutUint32(data[pos:], crc)
	}

	return data
}

type bocHashWriter struct {
	sum hash.Hash
	crc uint32

	buf       [8]byte
	crcActive bool
}

func (w *bocHashWriter) write(data []byte) {
	_, _ = w.sum.Write(data)
	if w.crcActive {
		w.crc = crc32.Update(w.crc, castTable, data)
	}
}

func (w *bocHashWriter) writeByte(value byte) {
	w.buf[0] = value
	w.write(w.buf[:1])
}

func (w *bocHashWriter) writeUint(value uint64, sz int) {
	storeUintTo(w.buf[:sz], value, sz)
	w.write(w.buf[:sz])
}

func (s *bocSerializer) computeFileHash(mode int) ([]byte, bool) {
	info, ok := s.estimateSerializedSize(mode)
	if !ok {
		return nil, false
	}

	h := sha256.New()
	w := bocHashWriter{
		sum:       h,
		crcActive: info.hasCRC32C,
	}

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
			item := s.itemByIndex(i)
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
		item := s.itemByIndex(s.cellCount - 1 - i)
		n := item.cell.serializeBOCTo(cellBuf[:], s.shouldSerializeHashes(item, mode))
		w.write(cellBuf[:n])

		for j := 0; j < int(item.refNum); j++ {
			w.writeUint(uint64(s.cellCount-1-int(item.refIdx[j])), info.refByteSize)
		}
	}

	if info.hasCRC32C {
		var crcBuf [4]byte
		binary.LittleEndian.PutUint32(crcBuf[:], w.crc)
		w.crcActive = false
		w.write(crcBuf[:])
	}

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

		for level := 0; level <= levelMask.GetLevel(); level++ {
			if !levelMask.IsSignificant(level) {
				continue
			}
			copy(dst[offset:offset+hashSize], c.getHash(level))
			offset += hashSize
		}
		for level := 0; level <= levelMask.GetLevel(); level++ {
			if !levelMask.IsSignificant(level) {
				continue
			}
			binary.BigEndian.PutUint16(dst[offset:offset+depthSize], c.getDepth(level))
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
