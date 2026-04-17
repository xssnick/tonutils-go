package cell

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
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

type BOCOptions struct {
	WithCRC32C    bool
	WithIndex     bool
	WithCacheBits bool
	WithTopHash   bool
	WithIntHashes bool
}

func (o BOCOptions) mode() int {
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

type cellHashKey = Hash

func makeCellHashKey(cell *Cell) cellHashKey {
	var key cellHashKey
	copy(key[:], cell.getHash(_DataCellMaxLevel))
	return key
}

func newBOCSerializer(roots []*Cell) (*bocSerializer, error) {
	if len(roots) == 0 {
		return nil, nil
	}

	bag := &bocSerializer{
		cells:    map[cellHashKey]int{},
		roots:    make([]bocRoot, len(roots)),
		maxDepth: maxDepth,
	}
	for i, root := range roots {
		bag.roots[i] = bocRoot{cell: root, idx: -1}
	}

	if err := bag.importCells(); err != nil {
		return nil, err
	}

	return bag, nil
}

func ToBOCWithOptions(roots []*Cell, opts BOCOptions) []byte {
	bag, err := newBOCSerializer(roots)
	if err != nil || bag == nil {
		return nil
	}

	return bag.serialize(opts.mode())
}

func (c *Cell) ToBOCWithOptions(opts BOCOptions) []byte {
	return ToBOCWithOptions([]*Cell{c.rawCell()}, opts)
}

func ComputeFileHash(root *Cell) []byte {
	if root == nil {
		return nil
	}

	opts := BOCOptions{
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

	boc := bag.serialize(opts.mode())
	if boc == nil {
		return nil
	}

	hash := sha256.Sum256(boc)
	return hash[:]
}

type bocRoot struct {
	cell *Cell
	idx  int
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
	refIdx      [4]int
	refNum      int
	wt          byte
	hcnt        byte
	newIdx      int
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

	cells    map[cellHashKey]int
	cellList []bocSerializeItem
	roots    []bocRoot

	rvIdx       int
	cellListTmp []bocSerializeItem
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

func (s *bocSerializer) importCell(cell *Cell, depth int) (int, error) {
	if depth > s.maxDepth {
		return 0, fmt.Errorf("cell depth too large")
	}
	if cell == nil {
		return 0, fmt.Errorf("cell is nil")
	}
	cell = cell.rawCell()

	hash := makeCellHashKey(cell)
	if pos, ok := s.cells[hash]; ok {
		s.cellList[pos].shouldCache = true
		return pos, nil
	}

	visibleRefs := cell.visibleRefs()
	refs := [4]int{-1, -1, -1, -1}
	sumChildWeight := 1
	for i, ref := range visibleRefs {
		refIdx, err := s.importCell(ref, depth+1)
		if err != nil {
			return 0, err
		}
		refs[i] = refIdx
		sumChildWeight += int(s.cellList[refIdx].wt)
		s.intRefs++
	}

	wt := sumChildWeight
	if wt > 0xFF {
		wt = 0xFF
	}

	s.cells[hash] = s.cellCount
	s.cellList = append(s.cellList, bocSerializeItem{
		cell:   cell,
		refIdx: refs,
		refNum: len(visibleRefs),
		wt:     byte(wt),
		hcnt:   byte(cell.getLevelMask().getHashesCount()),
		newIdx: -1,
	})
	s.dataBytes += uint64(cell.serializedBOCSize(false))

	idx := s.cellCount
	s.cellCount++
	return idx, nil
}

func (s *bocSerializer) reorderCells() {
	s.intHashes = 0
	for i := s.cellCount - 1; i >= 0; i-- {
		item := &s.cellList[i]
		refsCnt, remaining, sum, mask := item.refNum, item.refNum, bocMaxCellWeight-1, 0

		for j := 0; j < refsCnt; j++ {
			child := &s.cellList[item.refIdx[j]]
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

			child := &s.cellList[item.refIdx[j]]
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
		for j := 0; j < item.refNum; j++ {
			sum += int(s.cellList[item.refIdx[j]].wt)
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
		item := &s.cellList[s.roots[i].idx]
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

	s.rvIdx = 0
	s.cellListTmp = make([]bocSerializeItem, 0, s.cellCount)

	for _, root := range s.roots {
		s.revisit(root.idx, 0)
		s.revisit(root.idx, 1)
	}
	for i := range s.roots {
		s.roots[i].idx = s.revisit(s.roots[i].idx, 2)
	}

	s.cellList = s.cellListTmp
	s.cellListTmp = nil
}

func (s *bocSerializer) revisit(cellIdx, force int) int {
	item := &s.cellList[cellIdx]

	if item.newIdx >= 0 {
		return item.newIdx
	}

	if force == 0 {
		if item.newIdx != -1 {
			return item.newIdx
		}

		for j := item.refNum - 1; j >= 0; j-- {
			childIdx := item.refIdx[j]
			childForce := 0
			if s.cellList[childIdx].isSpecial() {
				childForce = 1
			}
			s.revisit(childIdx, childForce)
		}

		item.newIdx = -2
		return item.newIdx
	}

	if force > 1 {
		idx := s.rvIdx
		s.rvIdx++
		item.newIdx = idx
		s.cellListTmp = append(s.cellListTmp, *item)
		return idx
	}

	if item.newIdx == -3 {
		return item.newIdx
	}

	if item.isSpecial() {
		s.revisit(cellIdx, 0)
	}

	for j := item.refNum - 1; j >= 0; j-- {
		s.revisit(item.refIdx[j], 1)
	}
	for j := item.refNum - 1; j >= 0; j-- {
		item.refIdx[j] = s.revisit(item.refIdx[j], 2)
	}

	item.newIdx = -3
	return item.newIdx
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

	dataSize := s.dataBytes + uint64(s.intRefs*refByteSize) + uint64(hashBytes*(hashSize+depthSize))
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
	info, ok := s.estimateSerializedSize(mode)
	if !ok {
		return nil
	}

	data := make([]byte, info.totalSize)
	dynBuffer := make([]byte, 8)
	pos := 0

	storeUint := func(value uint64, sz int) {
		copy(data[pos:pos+sz], dynamicIntBytes(value, uint(sz), dynBuffer))
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
		storeUint(uint64(s.cellCount-1-root.idx), info.refByteSize)
	}

	if info.hasIndex {
		var offs uint64
		for n := s.cellCount; n > 0; n-- {
			i := n - 1
			item := &s.cellList[i]
			offs += uint64(item.cell.serializedBOCSize(s.shouldSerializeHashes(item, mode)) + item.refNum*info.refByteSize)

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

		for j := 0; j < item.refNum; j++ {
			storeUint(uint64(s.cellCount-1-item.refIdx[j]), info.refByteSize)
		}
	}

	if info.hasCRC32C {
		crc := crc32.Checksum(data[:pos], castTable)
		binary.LittleEndian.PutUint32(data[pos:], crc)
	}

	return data
}

func (s *bocSerializer) shouldSerializeHashes(item *bocSerializeItem, mode int) bool {
	withHashes := mode&bocModeWithIntHashes != 0 && item.wt == 0
	if item.isRootCell && mode&bocModeWithTopHash != 0 {
		withHashes = true
	}
	return withHashes
}

func (c *Cell) serializedBOCSize(withHashes bool) int {
	size := 2 + c.serializedBOCBodySize()
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

	return offset + c.serializeBOCBodyTo(dst[offset:])
}

func (c *Cell) serializedBOCBodySize() int {
	return int((uint(c.bitsSz) + 7) / 8)
}

func (c *Cell) serializeBOCBodyTo(dst []byte) int {
	bodySize := c.serializedBOCBodySize()
	if bodySize == 0 {
		return 0
	}

	copy(dst[:bodySize], c.data[:bodySize])
	if tailBits := c.bitsSz % 8; tailBits != 0 {
		dst[bodySize-1] |= 1 << (7 - tailBits)
	}
	return bodySize
}
