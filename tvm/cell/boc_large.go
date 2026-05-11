package cell

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	largeBOCStreamBufferSize = 4 << 20
)

const (
	largeBOCFlagShouldCache = 1 << iota
	largeBOCFlagRoot
)

func largeBOCCellCapacityHint(cellsCountHint uint64) (int, error) {
	if cellsCountHint == 0 {
		return 0, nil
	}
	if cellsCountHint >= uint64(bocVisitLinked) {
		return 0, fmt.Errorf("too many cells to serialize")
	}
	if cellsCountHint > uint64(int(^uint(0)>>1)) {
		return 0, fmt.Errorf("cells count hint is too large")
	}
	return int(cellsCountHint), nil
}

// LargeBOCMetaRecord is a compact storage-shaped cell metadata record for
// large BoC import. D1 is the ordinary BoC descriptor byte without the
// "with hashes" bit. BitsSz is the exact payload bit length, without the BoC
// terminator bit. Refs must contain top hashes in descriptor order.
//
// Depths[0] must always be filled. Hashes/Depths for higher significant levels
// are required only when D1 carries a non-zero level mask.
type LargeBOCMetaRecord struct {
	D1     byte
	BitsSz uint16
	Refs   [4]Hash
	Hashes [4]Hash
	Depths [4]uint16
}

// LargeBOCPayloadRecord is the second-pass large BoC payload record. Data must
// contain raw cell data bytes without the BoC terminator bit.
type LargeBOCPayloadRecord struct {
	Data []byte
}

// LargeBOCLoader provides two-phase large BoC loading. LoadMeta is used during
// graph import and should avoid loading payload bytes when the backing storage
// can do that cheaply. LoadPayload is used during final serialization order and
// must provide only raw cell payload bytes.
type LargeBOCLoader interface {
	// LoadMeta must append exactly one LargeBOCMetaRecord per input hash to
	// dst, in the same order as hashes. Returned records must stay valid while
	// the following batch may already be loading.
	LoadMeta(hashes []Hash, dst []LargeBOCMetaRecord) ([]LargeBOCMetaRecord, error)

	// LoadPayload must append exactly one LargeBOCPayloadRecord per input hash
	// to dst, in the same order as hashes. The returned records may borrow Data
	// from the loader; Data must stay valid while the following batch may
	// already be loading.
	LoadPayload(hashes []Hash, dst []LargeBOCPayloadRecord) ([]LargeBOCPayloadRecord, error)
}

// ToLargeBOC serializes roots into BoC and writes the result to w. It follows
// the same large-BOC shape as the C++ serializer: cells are imported by hash in
// batches, only compact metadata is retained, and payload records are loaded
// again in final serialization order. loadBatchSize controls the maximum number
// of cell records loaded by one loader call.
func ToLargeBOC(w io.Writer, rootHashes []Hash, opts BOCSerializeOptions, loader LargeBOCLoader, cellsCountHint uint64, loadBatchSize int) error {
	if w == nil {
		return fmt.Errorf("writer is nil")
	}
	if len(rootHashes) == 0 {
		return nil
	}
	if loader == nil {
		return ErrLazyLoaderNotSet
	}
	if loadBatchSize <= 0 {
		return fmt.Errorf("large boc load batch size must be positive")
	}

	capacity, err := largeBOCCellCapacityHint(cellsCountHint)
	if err != nil {
		return err
	}

	s := &largeBOCSerializer{
		maxDepth:      maxDepth,
		cellIndex:     newLargeBOCHashIndex(capacity),
		cellList:      make([]largeBOCItem, 0, capacity),
		roots:         make([]uint32, len(rootHashes)),
		batchLoad:     loader,
		loadBatchSize: loadBatchSize,
	}

	if err := s.importCells(rootHashes); err != nil {
		return err
	}
	return s.writeTo(w, opts.mode())
}

type largeBOCItem struct {
	hash Hash

	refIdx [4]uint32
	idx    uint32

	extraIdx uint32
	bitsSz   uint16
	depth0   uint16
	d1       byte
	wt       byte
	flags    byte
}

func (i *largeBOCItem) isSpecial() bool {
	return i.wt == 0
}

func (i *largeBOCItem) shouldCache() bool {
	return i.flags&largeBOCFlagShouldCache != 0
}

func (i *largeBOCItem) setShouldCache() {
	i.flags |= largeBOCFlagShouldCache
}

func (i *largeBOCItem) isRootCell() bool {
	return i.flags&largeBOCFlagRoot != 0
}

func (i *largeBOCItem) setRootCell() {
	i.flags |= largeBOCFlagRoot
}

func (i *largeBOCItem) refsNum() int {
	return int(i.d1 & 7)
}

func (i *largeBOCItem) levelMask() LevelMask {
	return LevelMask{Mask: i.d1 >> 5}
}

type largeBOCSerializer struct {
	cellCount int
	intRefs   int
	intHashes int
	topHashes int
	maxDepth  int

	dataBytes uint64

	cellIndex *largeBOCHashIndex
	cellList  []largeBOCItem
	extra     []largeBOCExtraMeta
	roots     []uint32

	batchLoad     LargeBOCLoader
	loadBatchSize int

	loadHashes   []Hash
	loadMeta     []LargeBOCMetaRecord
	loadPayloads []LargeBOCPayloadRecord
	nextFrontier []uint32
}

type largeBOCExtraMeta struct {
	hashes [4]Hash
	depths [4]uint16
}

type largeBOCMetaBatch struct {
	start   int
	end     int
	hashes  []Hash
	records []LargeBOCMetaRecord
	err     error
}

type largeBOCPayloadBatch struct {
	start   int
	end     int
	hashes  []Hash
	records []LargeBOCPayloadRecord
	err     error
}

func largeBOCEmptyRefs() [4]uint32 {
	return [4]uint32{
		bocInvalidCellIndex,
		bocInvalidCellIndex,
		bocInvalidCellIndex,
		bocInvalidCellIndex,
	}
}

func (s *largeBOCSerializer) importCells(rootHashes []Hash) error {
	frontier := make([]uint32, 0, len(rootHashes))
	for i, hash := range rootHashes {
		idx, fresh, err := s.addHash(hash)
		if err != nil {
			return err
		}

		s.roots[i] = idx
		if fresh {
			frontier = append(frontier, idx)
		}
	}

	for depth := 0; len(frontier) > 0; depth++ {
		if depth > s.maxDepth {
			return fmt.Errorf("cell depth too large")
		}

		next, err := s.importFrontier(frontier, depth)
		if err != nil {
			return err
		}
		frontier = next
	}

	s.loadHashes = nil
	s.loadMeta = nil
	s.loadPayloads = nil
	s.nextFrontier = nil
	s.cellIndex = nil

	s.computeInitialWeights()
	s.reorderCells()
	if s.cellCount == 0 {
		return fmt.Errorf("no cells to serialize")
	}
	return nil
}

func (s *largeBOCSerializer) importFrontier(frontier []uint32, depth int) ([]uint32, error) {
	next := s.nextFrontier[:0]
	if len(frontier) == 0 {
		s.nextFrontier = frontier[:0]
		return next, nil
	}

	current := s.prepareMetaBatch(frontier, 0, s.loadHashes[:0])
	current = s.loadPreparedMetaBatch(current, s.loadMeta[:0])
	var spareHashes []Hash
	var spareRecords []LargeBOCMetaRecord

	for {
		if current.err != nil {
			return nil, current.err
		}
		if len(current.records) != len(current.hashes) {
			return nil, ErrLazyRefNotFound
		}

		nextStart := current.end
		var nextBatch chan largeBOCMetaBatch
		if nextStart < len(frontier) {
			prefetch := s.prepareMetaBatch(frontier, nextStart, spareHashes)
			records := spareRecords
			nextBatch = make(chan largeBOCMetaBatch, 1)
			go func() {
				nextBatch <- s.loadPreparedMetaBatch(prefetch, records)
			}()
		}

		if err := s.importMetaBatch(frontier, depth, current, &next); err != nil {
			if nextBatch != nil {
				<-nextBatch
			}
			return nil, err
		}

		if nextBatch == nil {
			s.loadHashes = current.hashes[:0]
			s.loadMeta = current.records[:0]
			break
		}

		oldHashes := current.hashes[:0]
		oldRecords := current.records[:0]
		current = <-nextBatch
		spareHashes = oldHashes
		spareRecords = oldRecords
	}

	s.nextFrontier = frontier[:0]
	return next, nil
}

func (s *largeBOCSerializer) prepareMetaBatch(frontier []uint32, start int, hashes []Hash) largeBOCMetaBatch {
	end := start + s.loadBatchSize
	if end > len(frontier) {
		end = len(frontier)
	}

	hashes = hashes[:0]
	for _, idx := range frontier[start:end] {
		hashes = append(hashes, s.cellList[idx].hash)
	}

	return largeBOCMetaBatch{
		start:  start,
		end:    end,
		hashes: hashes,
	}
}

func (s *largeBOCSerializer) loadPreparedMetaBatch(batch largeBOCMetaBatch, records []LargeBOCMetaRecord) largeBOCMetaBatch {
	loaded, err := s.batchLoad.LoadMeta(batch.hashes, records[:0])
	batch.records = loaded
	batch.err = err
	return batch
}

func (s *largeBOCSerializer) importMetaBatch(frontier []uint32, depth int, batch largeBOCMetaBatch, next *[]uint32) error {
	for i := range batch.records {
		idx := frontier[batch.start+i]
		if err := s.importRecord(idx, &batch.records[i], batch.hashes[i], depth, next); err != nil {
			return err
		}
	}
	return nil
}

func (s *largeBOCSerializer) importRecord(idx uint32, record *LargeBOCMetaRecord, expected Hash, depth int, next *[]uint32) error {
	if err := validateCellMetaRecord(record, expected); err != nil {
		return err
	}

	refCnt := int(largeBOCRecordRefsNum(record))
	refs := largeBOCEmptyRefs()
	for i := 0; i < refCnt; i++ {
		if depth+1 > s.maxDepth {
			return fmt.Errorf("cell depth too large")
		}

		refIdx, fresh, err := s.addHash(record.Refs[i])
		if err != nil {
			return err
		}

		refs[i] = refIdx
		s.intRefs++
		if fresh {
			*next = append(*next, refIdx)
		}
	}

	item := &s.cellList[idx]
	item.refIdx = refs
	item.d1 = record.D1
	item.bitsSz = record.BitsSz
	item.depth0 = record.Depths[0]
	item.extraIdx = ^uint32(0)
	if largeBOCRecordLevelMask(record).getHashesCount() > 1 {
		item.extraIdx = uint32(len(s.extra))
		s.extra = append(s.extra, largeBOCExtraMeta{
			hashes: record.Hashes,
			depths: record.Depths,
		})
	}
	s.dataBytes += uint64(largeBOCItemDataSize(item))
	return nil
}

func (s *largeBOCSerializer) addHash(hash Hash) (uint32, bool, error) {
	if pos, found := s.findImportedHash(hash); found {
		return pos, false, nil
	}

	idx := s.cellCount
	if uint64(idx) >= uint64(bocVisitLinked) {
		return 0, false, fmt.Errorf("too many cells to serialize")
	}

	s.cellList = append(s.cellList, largeBOCItem{
		hash:     hash,
		refIdx:   largeBOCEmptyRefs(),
		idx:      uint32(idx),
		extraIdx: ^uint32(0),
	})
	s.cellIndex.set(hash, uint32(idx), s)
	s.cellCount++
	return uint32(idx), true, nil
}

func (s *largeBOCSerializer) findImportedHash(hash Hash) (uint32, bool) {
	pos, found := s.cellIndex.get(hash, s)
	if found {
		s.cellList[int(pos)].setShouldCache()
	}
	return pos, found
}

func (s *largeBOCSerializer) computeInitialWeights() {
	for i := range s.cellList {
		if s.cellList[i].wt == 0 {
			s.computeInitialWeight(uint32(i))
		}
	}
}

func (s *largeBOCSerializer) computeInitialWeight(idx uint32) byte {
	item := &s.cellList[idx]
	if item.wt != 0 {
		return item.wt
	}

	sum := 1
	for i := 0; i < item.refsNum(); i++ {
		sum += int(s.computeInitialWeight(item.refIdx[i]))
	}

	item.wt = largeBOCWeight(sum)
	return item.wt
}

func (s *largeBOCSerializer) reorderCells() {
	s.intHashes = 0
	for i := s.cellCount - 1; i >= 0; i-- {
		item := &s.cellList[i]
		refsCnt := item.refsNum()
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
		for j := 0; j < item.refsNum(); j++ {
			sum += int(s.cellList[int(item.refIdx[j])].wt)
		}

		if sum <= int(item.wt) {
			item.wt = byte(sum)
			continue
		}

		item.wt = 0
		s.intHashes += item.levelMask().getHashesCount()
	}

	s.topHashes = 0
	for _, rootIdx := range s.roots {
		item := &s.cellList[int(rootIdx)]
		if !item.isRootCell() {
			item.setRootCell()
			if item.wt != 0 {
				s.topHashes += item.levelMask().getHashesCount()
			}
		}
	}

	if s.cellCount == 0 {
		return
	}

	for i := range s.cellList {
		s.cellList[i].idx = bocInvalidCellIndex
	}

	var nextIdx uint32
	for _, rootIdx := range s.roots {
		s.revisit(rootIdx, 0, &nextIdx)
		s.revisit(rootIdx, 1, &nextIdx)
	}
	for i, rootIdx := range s.roots {
		s.roots[i] = s.revisit(rootIdx, 2, &nextIdx)
	}

	s.reorderInPlace()
}

func (s *largeBOCSerializer) revisit(cellIdx uint32, force int, nextIdx *uint32) uint32 {
	pos := int(cellIdx)
	item := &s.cellList[pos]

	if item.idx < bocVisitLinked {
		return item.idx
	}

	if force == 0 {
		if item.idx != bocInvalidCellIndex {
			return item.idx
		}

		for j := item.refsNum() - 1; j >= 0; j-- {
			childIdx := item.refIdx[j]
			childForce := 0
			if s.cellList[int(childIdx)].isSpecial() {
				childForce = 1
			}
			s.revisit(childIdx, childForce, nextIdx)
		}

		item.idx = bocVisitScanned
		return item.idx
	}

	if force > 1 {
		idx := *nextIdx
		*nextIdx = idx + 1
		item.idx = idx
		return idx
	}

	if item.idx == bocVisitLinked {
		return item.idx
	}

	if item.isSpecial() {
		s.revisit(cellIdx, 0, nextIdx)
	}

	for j := item.refsNum() - 1; j >= 0; j-- {
		s.revisit(item.refIdx[j], 1, nextIdx)
	}
	for j := item.refsNum() - 1; j >= 0; j-- {
		item.refIdx[j] = s.revisit(item.refIdx[j], 2, nextIdx)
	}

	item.idx = bocVisitLinked
	return item.idx
}

func (s *largeBOCSerializer) reorderInPlace() {
	for i := 0; i < s.cellCount; i++ {
		for s.cellList[i].idx != uint32(i) {
			dst := int(s.cellList[i].idx)
			s.cellList[i], s.cellList[dst] = s.cellList[dst], s.cellList[i]
		}
	}
}

func (s *largeBOCSerializer) computeSizes(mode int) (uint64, int, int) {
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

func (s *largeBOCSerializer) estimateSerializedSize(mode int) (bocSerializeInfo, bool) {
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

func (s *largeBOCSerializer) shouldSerializeHashes(item *largeBOCItem, mode int) bool {
	withHashes := mode&bocModeWithIntHashes != 0 && item.wt == 0
	if item.isRootCell() && mode&bocModeWithTopHash != 0 {
		withHashes = true
	}
	return withHashes
}

func (s *largeBOCSerializer) loadPayloadBatch(start int, hashes []Hash, payloads []LargeBOCPayloadRecord) largeBOCPayloadBatch {
	end := start + s.loadBatchSize
	if end > s.cellCount {
		end = s.cellCount
	}

	hashes = hashes[:0]
	for i := start; i < end; i++ {
		hashes = append(hashes, s.cellList[s.cellCount-1-i].hash)
	}

	records, err := s.batchLoad.LoadPayload(hashes, payloads[:0])
	return largeBOCPayloadBatch{
		start:   start,
		end:     end,
		hashes:  hashes,
		records: records,
		err:     err,
	}
}

func (s *largeBOCSerializer) writePayloadBatch(w *bocStreamWriter, info bocSerializeInfo, mode int, batch largeBOCPayloadBatch, cellBuf []byte) error {
	if batch.err != nil {
		return batch.err
	}
	if len(batch.records) != batch.end-batch.start {
		return ErrLazyRefNotFound
	}

	for i := range batch.records {
		item := &s.cellList[s.cellCount-1-batch.start-i]
		if err := validatePayloadRecordForItem(&batch.records[i], item); err != nil {
			return err
		}

		n := s.serializeItemBOCTo(item, batch.records[i].Data, cellBuf, s.shouldSerializeHashes(item, mode))
		w.write(cellBuf[:n])

		for j := 0; j < item.refsNum(); j++ {
			w.writeUint(uint64(s.cellCount-1-int(item.refIdx[j])), info.refByteSize)
		}
		if w.err != nil {
			return w.err
		}
	}
	return nil
}

func (s *largeBOCSerializer) writePayloadBatches(w *bocStreamWriter, info bocSerializeInfo, mode int, cellBuf []byte) error {
	if s.cellCount == 0 {
		return nil
	}

	current := s.loadPayloadBatch(0, s.loadHashes[:0], s.loadPayloads[:0])
	var spareHashes []Hash
	var sparePayloads []LargeBOCPayloadRecord

	for {
		if current.err != nil {
			clearPayloadRecords(current.records)
			return current.err
		}
		if len(current.records) != current.end-current.start {
			clearPayloadRecords(current.records)
			return ErrLazyRefNotFound
		}

		nextStart := current.end
		var nextBatch chan largeBOCPayloadBatch
		if nextStart < s.cellCount {
			hashes := spareHashes
			payloads := sparePayloads
			nextBatch = make(chan largeBOCPayloadBatch, 1)
			go func() {
				nextBatch <- s.loadPayloadBatch(nextStart, hashes, payloads)
			}()
		}

		err := s.writePayloadBatch(w, info, mode, current, cellBuf)
		clearPayloadRecords(current.records)
		if err != nil {
			if nextBatch != nil {
				prefetched := <-nextBatch
				clearPayloadRecords(prefetched.records)
			}
			return err
		}

		if nextBatch == nil {
			s.loadHashes = current.hashes[:0]
			s.loadPayloads = current.records[:0]
			return nil
		}

		oldHashes := current.hashes[:0]
		oldPayloads := current.records[:0]
		current = <-nextBatch
		spareHashes = oldHashes
		sparePayloads = oldPayloads
	}
}

func (s *largeBOCSerializer) writeTo(dst io.Writer, mode int) error {
	info, ok := s.estimateSerializedSize(mode)
	if !ok {
		return fmt.Errorf("failed to serialize boc")
	}

	w := newBOCStreamWriter(dst, info.hasCRC32C, largeBOCStreamBufferSize)

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

	for _, rootIdx := range s.roots {
		w.writeUint(uint64(s.cellCount-1-int(rootIdx)), info.refByteSize)
	}

	if info.hasIndex {
		var offs uint64
		for n := s.cellCount; n > 0; n-- {
			item := &s.cellList[n-1]
			offs += uint64(s.itemSerializedBOCSize(item, s.shouldSerializeHashes(item, mode)) + item.refsNum()*info.refByteSize)

			offset := offs
			if info.hasCacheBits {
				offset = offs * 2
				if item.shouldCache() {
					offset++
				}
			}
			w.writeUint(offset, info.offsetByteSize)
		}
	}

	var cellBuf [2 + maxCellDataBytes + 4*(hashSize+depthSize)]byte
	if err := s.writePayloadBatches(&w, info, mode, cellBuf[:]); err != nil {
		return err
	}

	if info.hasCRC32C {
		var crcBuf [4]byte
		binary.LittleEndian.PutUint32(crcBuf[:], w.currentCRC())
		w.crcActive = false
		w.write(crcBuf[:])
	}

	return w.finish()
}

func (s *largeBOCSerializer) itemSerializedBOCSize(item *largeBOCItem, withHashes bool) int {
	size := largeBOCItemDataSize(item)
	if withHashes {
		size += item.levelMask().getHashesCount() * (hashSize + depthSize)
	}
	return size
}

func largeBOCItemDataSize(item *largeBOCItem) int {
	return 2 + recordBOCBodySize(item.bitsSz)
}

func itemDescriptors(item *largeBOCItem) (byte, byte) {
	ln := (uint(item.bitsSz) / 8) * 2
	if item.bitsSz%8 != 0 {
		ln++
	}
	return item.d1, byte(ln)
}

func recordBOCBodySize(bitsSz uint16) int {
	return int((uint(bitsSz) + 7) / 8)
}

func (s *largeBOCSerializer) serializeItemBOCTo(item *largeBOCItem, data []byte, dst []byte, withHashes bool) int {
	levelMask := item.levelMask()
	dst[0], dst[1] = itemDescriptors(item)

	offset := 2
	if withHashes {
		dst[0] |= 16

		hashesCount := levelMask.getHashesCount()
		offset = s.serializeItemHashesTo(item, dst, offset, hashesCount)
		offset = s.serializeItemDepthsTo(item, dst, offset, hashesCount)
	}

	return offset + serializeRecordBOCBodyTo(item, data, dst[offset:])
}

func (s *largeBOCSerializer) serializeItemHashesTo(item *largeBOCItem, dst []byte, offset int, hashesCount int) int {
	if hashesCount == 1 {
		copy(dst[offset:offset+hashSize], item.hash[:])
		return offset + hashSize
	}

	extra := &s.extra[item.extraIdx]
	for i := 0; i < hashesCount; i++ {
		copy(dst[offset:offset+hashSize], extra.hashes[i][:])
		offset += hashSize
	}
	return offset
}

func (s *largeBOCSerializer) serializeItemDepthsTo(item *largeBOCItem, dst []byte, offset int, hashesCount int) int {
	if hashesCount == 1 {
		binary.BigEndian.PutUint16(dst[offset:offset+depthSize], item.depth0)
		return offset + depthSize
	}

	extra := &s.extra[item.extraIdx]
	for i := 0; i < hashesCount; i++ {
		binary.BigEndian.PutUint16(dst[offset:offset+depthSize], extra.depths[i])
		offset += depthSize
	}
	return offset
}

func serializeRecordBOCBodyTo(item *largeBOCItem, data []byte, dst []byte) int {
	bodySize := recordBOCBodySize(item.bitsSz)
	if bodySize == 0 {
		return 0
	}

	copy(dst[:bodySize], data[:bodySize])
	if tailBits := item.bitsSz % 8; tailBits != 0 {
		dst[bodySize-1] |= 1 << (7 - tailBits)
	}
	return bodySize
}

func validatePayloadRecordForItem(record *LargeBOCPayloadRecord, item *largeBOCItem) error {
	if len(record.Data) < recordBOCBodySize(item.bitsSz) {
		return fmt.Errorf("not enough cell record data")
	}
	return nil
}

func validateCellMetaRecord(record *LargeBOCMetaRecord, expected Hash) error {
	levelMask := largeBOCRecordLevelMask(record)
	if levelMask.Mask > 0b111 {
		return fmt.Errorf("invalid cell record level mask")
	}
	if record.D1&0x10 != 0 {
		return fmt.Errorf("invalid cell record descriptor")
	}
	if largeBOCRecordRefsNum(record) > 4 {
		return ErrTooMuchRefs
	}
	if record.BitsSz > 1023 {
		return ErrNotFit1023
	}

	if levelMask.Mask == 0 {
		return nil
	}
	hashIndex := levelMask.Apply(_DataCellMaxLevel).getHashIndex()
	if record.Hashes[hashIndex] != expected {
		return fmt.Errorf("cell record hash mismatch")
	}
	return nil
}

func largeBOCRecordRefsNum(record *LargeBOCMetaRecord) uint8 {
	return record.D1 & 7
}

func largeBOCRecordLevelMask(record *LargeBOCMetaRecord) LevelMask {
	return LevelMask{Mask: record.D1 >> 5}
}

func clearPayloadRecords(records []LargeBOCPayloadRecord) {
	for i := range records {
		records[i] = LargeBOCPayloadRecord{}
	}
}

func largeBOCWeight(sumChildWeight int) byte {
	if sumChildWeight > 0xFF {
		return 0xFF
	}
	return byte(sumChildWeight)
}

type largeBOCHashIndex struct {
	indexes []uint32
	used    int
	growAt  int
}

func newLargeBOCHashIndex(capacityHint int) *largeBOCHashIndex {
	m := &largeBOCHashIndex{}
	if capacityHint <= 0 {
		return m
	}

	capacity := bocHashIndexInitialCapacity
	for capacityHint*4 > capacity*3 {
		capacity *= 2
	}

	m.indexes = make([]uint32, capacity)
	m.growAt = capacity * 3 / 4
	return m
}

func (m *largeBOCHashIndex) get(hash Hash, s *largeBOCSerializer) (uint32, bool) {
	if len(m.indexes) == 0 {
		return 0, false
	}

	fp := bocHashFingerprint(hash[:])
	mask := uint64(len(m.indexes) - 1)
	pos := fp & mask
	for {
		idxPlusOne := m.indexes[pos]
		if idxPlusOne == 0 {
			return 0, false
		}

		idx := idxPlusOne - 1
		if s.cellList[idx].hash == hash {
			return idx, true
		}

		pos = (pos + 1) & mask
	}
}

func (m *largeBOCHashIndex) set(hash Hash, idx uint32, s *largeBOCSerializer) {
	m.reserve(s)
	m.insert(hash, idx)
}

func (m *largeBOCHashIndex) reserve(s *largeBOCSerializer) {
	if m.used+1 > m.growAt {
		m.grow(s)
	}
}

func (m *largeBOCHashIndex) grow(s *largeBOCSerializer) {
	capacity := len(m.indexes) * 2
	if capacity == 0 {
		capacity = bocHashIndexInitialCapacity
	}
	for (m.used+1)*4 > capacity*3 {
		capacity *= 2
	}

	oldIndexes := m.indexes
	m.indexes = make([]uint32, capacity)
	m.growAt = capacity * 3 / 4

	if len(oldIndexes) == 0 {
		return
	}

	m.used = 0
	for _, idxPlusOne := range oldIndexes {
		if idxPlusOne == 0 {
			continue
		}
		idx := idxPlusOne - 1
		m.insert(s.cellList[idx].hash, idx)
	}
}

func (m *largeBOCHashIndex) insert(hash Hash, idx uint32) {
	fp := bocHashFingerprint(hash[:])
	mask := uint64(len(m.indexes) - 1)
	pos := fp & mask
	for m.indexes[pos] != 0 {
		pos = (pos + 1) & mask
	}

	m.indexes[pos] = idx + 1
	m.used++
}
