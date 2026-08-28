package cell

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"math/bits"
	"sort"

	"github.com/pierrec/lz4/v4"
)

const (
	kDecompressedSizeBytes = 4
	kMUCellOrderInRoot     = 2
)

type CompressionAlgorithm byte

const (
	CompressionBaselineLZ4 CompressionAlgorithm = iota
	CompressionImprovedStructureLZ4
	CompressionImprovedStructureLZ4WithState
)

type bitSpan struct {
	data      []byte
	bitOffset int
	bitLen    int
}

func (s bitSpan) Len() int {
	return s.bitLen
}

func (s bitSpan) Bit(i int) byte {
	if i < 0 || i >= s.bitLen {
		return 0
	}
	bit := s.bitOffset + i
	return (s.data[bit/8] >> (7 - uint(bit%8))) & 1
}

func (s bitSpan) Subspan(offset, ln int) bitSpan {
	if offset < 0 {
		offset = 0
	}
	if ln < 0 {
		ln = 0
	}
	if offset > s.bitLen {
		offset = s.bitLen
	}
	if offset+ln > s.bitLen {
		ln = s.bitLen - offset
	}
	return bitSpan{
		data:      s.data,
		bitOffset: s.bitOffset + offset,
		bitLen:    ln,
	}
}

func (s bitSpan) Bytes() []byte {
	if s.bitLen == 0 {
		return nil
	}

	if s.bitOffset%8 == 0 && s.bitLen%8 == 0 {
		start := s.bitOffset / 8
		end := start + s.bitLen/8
		out := make([]byte, end-start)
		copy(out, s.data[start:end])
		return out
	}

	out := make([]byte, (s.bitLen+7)/8)
	for i := 0; i < s.bitLen; i++ {
		if s.Bit(i) == 0 {
			continue
		}
		out[i/8] |= 1 << (7 - uint(i%8))
	}
	return out
}

type bitWriter struct {
	data   []byte
	bitLen int
}

func (w *bitWriter) Len() int {
	return w.bitLen
}

func (w *bitWriter) ensure(bits int) {
	needBytes := (w.bitLen + bits + 7) / 8
	if needBytes <= len(w.data) {
		return
	}

	newLen := len(w.data) * 2
	if newLen < 32 {
		newLen = 32
	}
	for newLen < needBytes {
		newLen *= 2
	}

	data := make([]byte, newLen)
	copy(data, w.data)
	w.data = data
}

func (w *bitWriter) WriteBit(bit byte) {
	w.ensure(1)
	if bit != 0 {
		w.data[w.bitLen/8] |= 1 << (7 - uint(w.bitLen%8))
	}
	w.bitLen++
}

func (w *bitWriter) WriteUint(value uint64, bits int) {
	if bits == 0 {
		return
	}
	if w.bitLen%8 == 0 && bits%8 == 0 {
		w.ensure(bits)

		byteLen := bits / 8
		dst := w.data[w.bitLen/8 : w.bitLen/8+byteLen]
		for i := byteLen - 1; i >= 0; i-- {
			dst[i] = byte(value)
			value >>= 8
		}
		w.bitLen += bits
		return
	}

	var data [8]byte
	binary.BigEndian.PutUint64(data[:], value)
	w.ensure(bits)
	appendBitRange(w.data, uint(w.bitLen), data[:], uint(64-bits), uint(bits))
	w.bitLen += bits
}

func (w *bitWriter) AppendSpan(span bitSpan) {
	if span.bitLen == 0 {
		return
	}

	w.ensure(span.bitLen)
	appendBitRange(w.data, uint(w.bitLen), span.data, uint(span.bitOffset), uint(span.bitLen))
	w.bitLen += span.bitLen
}

func (w *bitWriter) AlignByteZero() {
	for w.bitLen%8 != 0 {
		w.WriteBit(0)
	}
}

func (w *bitWriter) Bytes() []byte {
	return w.data[:(w.bitLen+7)/8]
}

type bitReader struct {
	data []byte
	pos  int
	size int
}

func newBitReader(data []byte) *bitReader {
	return &bitReader{data: data, size: len(data) * 8}
}

func (r *bitReader) BitsLeft() int {
	return r.size - r.pos
}

func (r *bitReader) ReadBit() (byte, error) {
	if r.BitsLeft() < 1 {
		return 0, fmt.Errorf("not enough bits to read")
	}
	bit := (r.data[r.pos/8] >> (7 - uint(r.pos%8))) & 1
	r.pos++
	return bit, nil
}

func (r *bitReader) PeekBit() (byte, error) {
	if r.BitsLeft() < 1 {
		return 0, fmt.Errorf("not enough bits to read")
	}
	return (r.data[r.pos/8] >> (7 - uint(r.pos%8))) & 1, nil
}

func (r *bitReader) ReadUint(bits int) (uint64, error) {
	if bits < 0 || bits > 64 {
		return 0, fmt.Errorf("unsupported uint width %d", bits)
	}
	if r.BitsLeft() < bits {
		return 0, fmt.Errorf("not enough bits to read")
	}
	if bits == 0 {
		return 0, nil
	}
	if r.pos%8 == 0 && bits%8 == 0 {
		byteLen := bits / 8
		data := r.data[r.pos/8 : r.pos/8+byteLen]
		var v uint64
		for _, b := range data {
			v = (v << 8) | uint64(b)
		}
		r.pos += bits
		return v, nil
	}

	var v uint64
	for i := 0; i < bits; i++ {
		v <<= 1
		v |= uint64((r.data[(r.pos+i)/8] >> (7 - uint((r.pos+i)%8))) & 1)
	}
	r.pos += bits
	return v, nil
}

func (r *bitReader) Span(bits int) (bitSpan, error) {
	if bits < 0 || r.BitsLeft() < bits {
		return bitSpan{}, fmt.Errorf("not enough bits to read")
	}
	span := bitSpan{
		data:      r.data,
		bitOffset: r.pos,
		bitLen:    bits,
	}
	r.pos += bits
	return span, nil
}

func NeedStateForDecompression(data []byte) (bool, error) {
	if len(data) == 0 {
		return false, fmt.Errorf("can't check algorithm on empty data")
	}

	switch CompressionAlgorithm(data[0]) {
	case CompressionBaselineLZ4, CompressionImprovedStructureLZ4:
		return false, nil
	case CompressionImprovedStructureLZ4WithState:
		return true, nil
	default:
		return false, fmt.Errorf("unknown compression algorithm")
	}
}

func CompressBOC(roots []*Cell, algo CompressionAlgorithm, state *Cell) ([]byte, error) {
	if len(roots) == 0 {
		return nil, fmt.Errorf("cannot compress empty boc roots")
	}

	var payload []byte
	var err error
	switch algo {
	case CompressionBaselineLZ4:
		payload, err = compressBaselineLZ4(roots)
	case CompressionImprovedStructureLZ4:
		payload, err = compressImprovedStructureLZ4(roots, false, nil)
	case CompressionImprovedStructureLZ4WithState:
		payload, err = compressImprovedStructureLZ4(roots, true, state)
	default:
		return nil, fmt.Errorf("unknown compression algorithm")
	}
	if err != nil {
		return nil, err
	}

	out := make([]byte, 1+len(payload))
	out[0] = byte(algo)
	copy(out[1:], payload)
	return out, nil
}

func DecompressBOC(data []byte, maxSize int, state *Cell) ([]*Cell, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("can't decompress empty data")
	}

	switch CompressionAlgorithm(data[0]) {
	case CompressionBaselineLZ4:
		return decompressBaselineLZ4(data[1:], maxSize)
	case CompressionImprovedStructureLZ4:
		return decompressImprovedStructureLZ4(data[1:], maxSize, false, nil)
	case CompressionImprovedStructureLZ4WithState:
		return decompressImprovedStructureLZ4(data[1:], maxSize, true, state)
	default:
		return nil, fmt.Errorf("unknown compression algorithm")
	}
}

func compressBaselineLZ4(roots []*Cell) ([]byte, error) {
	boc := ToBOCWithOptions(roots, BOCSerializeOptions{WithCRC32C: true})
	if len(boc) == 0 {
		return nil, fmt.Errorf("failed to serialize boc")
	}

	dst := make([]byte, lz4.CompressBlockBound(len(boc)))
	n, err := lz4.CompressBlock(boc, dst, nil)
	if err != nil {
		return nil, fmt.Errorf("lz4 compression failed: %w", err)
	}
	if n <= 0 {
		return nil, fmt.Errorf("lz4 compression failed")
	}
	dst = dst[:n]

	out := make([]byte, kDecompressedSizeBytes+len(dst))
	binary.BigEndian.PutUint32(out, uint32(len(boc)))
	copy(out[kDecompressedSizeBytes:], dst)
	return out, nil
}

func decompressBaselineLZ4(compressed []byte, maxSize int) ([]*Cell, error) {
	if len(compressed) < kDecompressedSizeBytes {
		return nil, fmt.Errorf("boc decompression failed: input too small for header")
	}

	decompressedSize := int(binary.BigEndian.Uint32(compressed[:kDecompressedSizeBytes]))
	if decompressedSize <= 0 || decompressedSize > maxSize {
		return nil, fmt.Errorf("boc decompression failed: invalid decompressed size")
	}

	dst := make([]byte, decompressedSize)
	n, err := lz4.UncompressBlock(compressed[kDecompressedSizeBytes:], dst)
	if err != nil {
		return nil, fmt.Errorf("lz4 decompression failed: %w", err)
	}
	if n < 0 {
		return nil, fmt.Errorf("lz4 decompression failed")
	}
	return FromBOCMultiRoot(dst[:n])
}

func storeBitSpan(builder *Builder, span bitSpan) error {
	return builder.storeBitRange(span.data, uint(span.bitOffset), uint(span.bitLen))
}

func cellBits(c *Cell) bitSpan {
	return bitSpan{data: c.data, bitLen: int(c.bitsSz)}
}

func createPrunedBranchFromCell(source *Cell, newLevel int) (*Cell, error) {
	return createPrunedBranchInto(source, newLevel, _DataCellMaxLevel, nil)
}

// createPrunedBranchFromCellInto is createPrunedBranchFromCell for a boundary
// that belongs to a proof body being assembled, so it comes from that proof's
// arena instead of its own allocation.
func createPrunedBranchFromCellInto(source *Cell, newLevel int, arena *proofCellArena) (*Cell, error) {
	return createPrunedBranchInto(source, newLevel, _DataCellMaxLevel, arena)
}

// CreatePrunedBranch returns a pruned-branch boundary for source as seen at
// virtualLevel and bounded by newLevel. Loaded leaf cells are returned as-is.
func CreatePrunedBranch(source *Cell, newLevel, virtualLevel int) (*Cell, error) {
	return createPrunedBranchInto(source, newLevel, virtualLevel, nil)
}

func createPrunedBranchInto(source *Cell, newLevel, virtualLevel int, arena *proofCellArena) (*Cell, error) {
	virtualLevel = max(0, min(virtualLevel, _DataCellMaxLevel))
	if !source.IsLazy() && !source.IsVirtualized() && source.Level() <= virtualLevel && source.refsCount() == 0 {
		return materializePrunedBranchBoundary(source)
	}
	return buildPrunedBranchFromCellAtDepth(source, newLevel, virtualLevel, arena)
}

// prunedBranchCell packs a boundary cell with the payload it is built from.
// A boundary keeps its hashes in that payload rather than in cell metadata, so
// the two are allocated and freed together and nothing else ever points at one
// without the other.
type prunedBranchCell struct {
	c       Cell
	payload [2 + hashSize + depthSize]byte
}

func buildPrunedBranchFromCellAtDepth(source *Cell, newLevel, virtLevel int, arena *proofCellArena) (*Cell, error) {
	virtLevel = max(0, min(virtLevel, _DataCellMaxLevel))
	levelMask := source.getLevelMask().Apply(virtLevel)
	level := levelMask.GetLevel()
	if newLevel < level+1 {
		return nil, fmt.Errorf("invalid new pruned level")
	}

	hashesCount := levelMask.getHashesCount()
	// A boundary cut from a level-0 subtree carries one hash, which is every
	// boundary a merkle update or a usage proof emits; giving that case its
	// payload from the same object as the cell makes it one allocation instead
	// of two. Wider masks are rare enough to keep the plain slice.
	var (
		pruned *Cell
		data   []byte
	)
	if hashesCount == 1 {
		fused := arena.prunedBranch()
		pruned, data = &fused.c, fused.payload[:]
	} else {
		pruned = new(Cell)
		data = make([]byte, 2+hashesCount*(hashSize+depthSize))
	}
	data[0] = byte(PrunedCellType)
	data[1] = levelMask.Mask | oneLevelMask(newLevel)
	hashOff := 2
	depthOff := 2 + hashesCount*hashSize
	hashIndex := 0
	for i := 0; i <= level; i++ {
		if !levelMask.IsSignificant(i) {
			continue
		}
		copy(data[hashOff+hashIndex*hashSize:], source.getHash(i))
		binary.BigEndian.PutUint16(data[depthOff+hashIndex*depthSize:], source.getDepth(i))
		hashIndex++
	}

	pruned.bitsSz = uint16(len(data) * 8)
	pruned.data = data
	pruned.setSpecial(true)
	pruned.setLevelMask(LevelMask{Mask: data[1]})
	if err := validateBoundaryCell(pruned); err != nil {
		return nil, err
	}
	if err := pruned.calculateHashes(); err != nil {
		return nil, err
	}
	return pruned, nil
}

func oneLevelMask(level int) byte {
	if level == 0 {
		return 0
	}
	return 1 << (level - 1)
}

func extractBalanceFromDepthBalanceCell(c *Cell) *big.Int {
	var s Slice
	return extractBalanceFromDepthBalanceCellInto(c, &s)
}

func extractBalanceFromDepthBalanceCellInto(c *Cell, s *Slice) *big.Int {
	// depth_balance_info starts with a zero 2-bit label and a zero 5-bit
	// split depth; reject everything else before slice setup
	if c.bitsSz < 12 || c.data[0]&0xFE != 0 {
		return nil
	}

	if err := c.BeginParseInto(s); err != nil {
		return nil
	}
	label, err := s.LoadUInt(2)
	if err != nil || label != 0 {
		return nil
	}

	depth, err := s.LoadUInt(5)
	if err != nil || depth != 0 {
		return nil
	}

	grams, err := s.LoadBigCoins()
	if err != nil {
		return nil
	}
	extra, err := s.LoadMaybeRef()
	if err != nil || extra != nil || s.BitsLeft() != 0 {
		return nil
	}

	return grams
}

func extractBalanceFromDepthBalanceCellValueInto(c *Cell, s, maybe *Slice, dst *big.Int) bool {
	if c.bitsSz < 12 || c.data[0]&0xFE != 0 {
		return false
	}
	if err := c.BeginParseInto(s); err != nil {
		return false
	}
	label, err := s.LoadUInt(2)
	if err != nil || label != 0 {
		return false
	}
	depth, err := s.LoadUInt(5)
	if err != nil || depth != 0 {
		return false
	}
	if err := s.loadBigCoinsInto(dst); err != nil {
		return false
	}
	present, err := s.LoadMaybeRefInto(maybe)
	return err == nil && !present && s.BitsLeft() == 0
}

type balanceDiffScratch struct {
	slice Slice
	maybe Slice
	left  big.Int
	right big.Int
	diff  big.Int
}

func (s *balanceDiffScratch) process(left, right *Cell) *big.Int {
	if !extractBalanceFromDepthBalanceCellValueInto(left, &s.slice, &s.maybe, &s.left) {
		return nil
	}
	if !extractBalanceFromDepthBalanceCellValueInto(right, &s.slice, &s.maybe, &s.right) {
		return nil
	}
	return s.diff.Sub(&s.right, &s.left)
}

// balanceDiffSum treats nil as zero. Most Merkle-update vertices carry no
// balance delta, so this avoids allocating an empty big.Int at every recursive
// node. The input is copied because balanceDiffScratch reuses its value.
type balanceDiffSum struct {
	value *big.Int
}

type compressionGraphNode struct {
	refs    [4]int
	data    bitSpan
	balance balanceDiffSum
	refsCnt int
	typ     Type
	pbMask  byte
	small   bool
}

func (s *balanceDiffSum) add(diff *big.Int) {
	if diff == nil || diff.Sign() == 0 {
		return
	}
	if s.value == nil {
		s.value = new(big.Int).Set(diff)
		return
	}
	s.value.Add(s.value, diff)
}

func (s *balanceDiffSum) equal(value *big.Int) bool {
	if s.value == nil {
		return value == nil || value.Sign() == 0
	}
	return value != nil && s.value.Cmp(value) == 0
}

func writeDepthBalanceGrams(builder *Builder, grams *big.Int) error {
	if grams == nil {
		return fmt.Errorf("grams is nil")
	}
	if err := builder.StoreUInt(0, 7); err != nil {
		return err
	}
	if err := builder.StoreBigCoins(grams); err != nil {
		return err
	}
	return builder.StoreDict(nil)
}

func isMerkleUpdateNode(isSpecial bool, pbLevelMask byte, prefix, suffix bitSpan) bool {
	if !isSpecial || pbLevelMask != 0 || prefix.Len()+suffix.Len() < 8 {
		return false
	}

	var firstByte byte
	for i := 0; i < 8; i++ {
		var bit byte
		if i < prefix.Len() {
			bit = prefix.Bit(i)
		} else {
			bit = suffix.Bit(i - prefix.Len())
		}
		firstByte = (firstByte << 1) | bit
	}
	return firstByte == byte(MerkleUpdateCellType)
}

func compressWithSizeHeader(data []byte) ([]byte, error) {
	dst := make([]byte, lz4.CompressBlockBound(len(data)))
	n, err := lz4.CompressBlock(data, dst, nil)
	if err != nil {
		return nil, fmt.Errorf("lz4 compression failed: %w", err)
	}
	if n <= 0 {
		return nil, fmt.Errorf("lz4 compression failed")
	}
	dst = dst[:n]

	out := make([]byte, kDecompressedSizeBytes+len(dst))
	binary.BigEndian.PutUint32(out, uint32(len(data)))
	copy(out[kDecompressedSizeBytes:], dst)
	return out, nil
}

func decompressWithSizeHeader(compressed []byte, maxSize int) ([]byte, error) {
	if len(compressed) < kDecompressedSizeBytes {
		return nil, fmt.Errorf("boc decompression failed: input too small for header")
	}

	decompressedSize := int(binary.BigEndian.Uint32(compressed[:kDecompressedSizeBytes]))
	if decompressedSize <= 0 || decompressedSize > maxSize {
		return nil, fmt.Errorf("boc decompression failed: invalid decompressed size")
	}

	dst := make([]byte, decompressedSize)
	n, err := lz4.UncompressBlock(compressed[kDecompressedSizeBytes:], dst)
	if err != nil {
		return nil, fmt.Errorf("lz4 decompression failed: %w", err)
	}
	// reference decompression requires the payload to match the declared size
	// exactly
	if n != decompressedSize {
		return nil, fmt.Errorf("boc decompression failed: decompressed size mismatch")
	}
	return dst, nil
}

func compressImprovedStructureLZ4(roots []*Cell, compressMerkleUpdate bool, _ *Cell) ([]byte, error) {
	const muRemoveSubtreeSums = true

	if len(roots) == 0 {
		return nil, fmt.Errorf("no root cells were provided for serialization")
	}
	for _, root := range roots {
		if root == nil {
			return nil, fmt.Errorf("cannot serialize a nil cell reference into a bag of cells")
		}
	}

	cellHashes := map[Hash]int{}
	var graph []compressionGraphNode
	rootIndexes := make([]int, len(roots))

	var mainMUHash Hash
	hasMainMUCell := false
	var balanceScratch balanceDiffScratch

	var buildGraph func(cell *Cell, underMULeft, underMURight bool, leftCell *Cell, sumDiffOut int) (int, error)
	buildGraph = func(cell *Cell, underMULeft, underMURight bool, leftCell *Cell, sumDiffOut int) (int, error) {
		if cell == nil {
			return 0, fmt.Errorf("error while importing a cell during serialization: cell is nil")
		}

		cellHash := cell.HashKey()
		if idx, ok := cellHashes[cellHash]; ok {
			return idx, nil
		}

		currentCellID := len(graph)
		cellHashes[cellHash] = currentCellID

		typ := cell.GetType()
		if cell.IsSpecial() && typ == UnknownCellType {
			head := byte(0)
			if len(cell.data) > 0 {
				head = cell.data[0]
			}
			return 0, fmt.Errorf("invalid loaded cell data: unknown special cell, bits=%d refs=%d mask=%d head=%d", cell.bitsSz, cell.refsCount(), cell.getLevelMask().Mask, head)
		}

		refCnt := cell.refsCount()
		bitsView := cellBits(cell)
		graph = append(graph, compressionGraphNode{refsCnt: refCnt, typ: typ})

		if refCnt > 4 {
			return 0, fmt.Errorf("invalid loaded cell data: too many refs")
		}

		if typ == PrunedCellType {
			if bitsView.Len() < 16 {
				return 0, fmt.Errorf("invalid loaded cell data: pruned branch is shorter than header")
			}
			graph[currentCellID].data = bitsView.Subspan(16, bitsView.Len()-16)
			graph[currentCellID].pbMask = cell.data[1]
		} else {
			graph[currentCellID].data = bitsView
		}

		if compressMerkleUpdate && underMULeft {
			graph[currentCellID].data = bitSpan{}
		}

		if muRemoveSubtreeSums && typ == MerkleUpdateCellType && hasMainMUCell && cellHash == mainMUHash {
			childLeftID, err := buildGraph(cell.ref(0), true, false, nil, -1)
			if err != nil {
				return 0, err
			}
			graph[currentCellID].refs[0] = childLeftID

			childRightID, err := buildGraph(cell.ref(1), false, true, cell.ref(0), -1)
			if err != nil {
				return 0, err
			}
			graph[currentCellID].refs[1] = childRightID
			return currentCellID, nil
		}

		if underMURight && leftCell != nil {
			leftRefCnt := leftCell.refsCount()
			for i := 0; i < refCnt; i++ {
				var pairedLeftChild *Cell
				if i < leftRefCnt {
					pairedLeftChild = leftCell.ref(i)
				}
				childID, err := buildGraph(cell.ref(i), false, true, pairedLeftChild, currentCellID)
				if err != nil {
					return 0, err
				}
				graph[currentCellID].refs[i] = childID
			}

			vertexDiff := balanceScratch.process(leftCell, cell)
			if !cell.IsSpecial() && vertexDiff != nil && graph[currentCellID].balance.equal(vertexDiff) {
				graph[currentCellID].data = bitSpan{}
				graph[currentCellID].pbMask = 9
			}
			if sumDiffOut >= 0 && vertexDiff != nil {
				graph[sumDiffOut].balance.add(vertexDiff)
			}
			return currentCellID, nil
		}

		for i := 0; i < refCnt; i++ {
			childID, err := buildGraph(cell.ref(i), underMULeft, underMURight, nil, -1)
			if err != nil {
				return 0, err
			}
			graph[currentCellID].refs[i] = childID
		}
		return currentCellID, nil
	}

	for i, root := range roots {
		hasMainMUCell = false
		if root.refsCount() > kMUCellOrderInRoot {
			mainMUHash = root.ref(kMUCellOrderInRoot).HashKey()
			hasMainMUCell = true
		}

		rootID, err := buildGraph(root, false, false, nil, -1)
		if err != nil {
			return nil, err
		}
		rootIndexes[i] = rootID
	}

	nodeCount := len(graph)
	reverseStarts := make([]int, nodeCount+1)
	for i := 0; i < nodeCount; i++ {
		for childIdx := 0; childIdx < graph[i].refsCnt; childIdx++ {
			child := graph[i].refs[childIdx]
			reverseStarts[child+1]++
		}
	}
	for i := 1; i < len(reverseStarts); i++ {
		reverseStarts[i] += reverseStarts[i-1]
	}
	reverseParents := make([]int, reverseStarts[nodeCount])
	// Fill backwards so parents keep the same ascending order as the old
	// per-node append slices, while the cumulative ends turn into starts.
	for i := nodeCount - 1; i >= 0; i-- {
		for childIdx := graph[i].refsCnt - 1; childIdx >= 0; childIdx-- {
			child := graph[i].refs[childIdx]
			reverseStarts[child+1]--
			reverseParents[reverseStarts[child+1]] = i
		}
	}

	for i := 0; i < nodeCount; i++ {
		if graph[i].typ != PrunedCellType {
			graph[i].small = graph[i].data.Len() < 128
		}
	}

	type topoItem struct {
		a int
		b int
		c int
	}

	topoOrder := make([]int, 0, nodeCount)
	rank := make([]int, nodeCount)
	inDegree := make([]int, nodeCount)
	queue := make([]topoItem, 0, nodeCount)

	for i := 0; i < nodeCount; i++ {
		inDegree[i] = graph[i].refsCnt
		if inDegree[i] == 0 {
			ordinary := 0
			if graph[i].typ == OrdinaryCellType {
				ordinary = 1
			}
			queue = append(queue, topoItem{a: ordinary, b: -graph[i].data.Len(), c: -i})
		}
	}
	if len(queue) == 0 {
		return nil, fmt.Errorf("cycle detected in cell references")
	}

	sort.Slice(queue, func(i, j int) bool {
		if queue[i].a != queue[j].a {
			return queue[i].a < queue[j].a
		}
		if queue[i].b != queue[j].b {
			return queue[i].b < queue[j].b
		}
		return queue[i].c < queue[j].c
	})

	for len(queue) > 0 {
		node := -queue[len(queue)-1].c
		queue = queue[:len(queue)-1]
		topoOrder = append(topoOrder, node)

		parentsEnd := len(reverseParents)
		if node+2 < len(reverseStarts) {
			parentsEnd = reverseStarts[node+2]
		}
		for _, parent := range reverseParents[reverseStarts[node+1]:parentsEnd] {
			inDegree[parent]--
			if inDegree[parent] == 0 {
				queue = append(queue, topoItem{c: -parent})
			}
		}
	}

	if len(topoOrder) != nodeCount {
		return nil, fmt.Errorf("invalid graph structure")
	}

	for i, j := 0, len(topoOrder)-1; i < j; i, j = i+1, j-1 {
		topoOrder[i], topoOrder[j] = topoOrder[j], topoOrder[i]
	}
	for i, node := range topoOrder {
		rank[node] = i
	}

	estimatedBits := 64 + len(rootIndexes)*32 + 14 // fixed headers and byte alignment
	maxDeltaBits := bits.Len32(uint32(nodeCount))
	for node := 0; node < nodeCount; node++ {
		// Metadata is at most 16 bits. Payload needs at most one marker byte,
		// and each edge carries its direct flag plus a bounded delta encoding.
		estimatedBits += 16 + graph[node].data.Len() + 8 + graph[node].refsCnt*(3+maxDeltaBits)
	}
	result := bitWriter{data: make([]byte, (estimatedBits+7)/8)}
	result.WriteUint(uint64(len(rootIndexes)), 32)
	for _, rootIdx := range rootIndexes {
		result.WriteUint(uint64(rank[rootIdx]), 32)
	}

	result.WriteUint(uint64(nodeCount), 32)
	for i := 0; i < nodeCount; i++ {
		node := topoOrder[i]
		currentCellType := 0
		if graph[node].typ != OrdinaryCellType {
			currentCellType = 1
		}
		currentCellType += int(graph[node].pbMask)
		result.WriteUint(uint64(currentCellType), 4)

		currentRefsCnt := graph[node].refsCnt
		if graph[node].typ == PrunedCellType && graph[node].data.Len() == 0 {
			if currentRefsCnt != 0 {
				return nil, fmt.Errorf("invalid graph structure")
			}
			currentRefsCnt = 1
		}
		result.WriteUint(uint64(currentRefsCnt), 4)

		if graph[node].typ != PrunedCellType && currentCellType != 9 {
			if graph[node].small {
				result.WriteUint(1, 1)
				result.WriteUint(uint64(graph[node].data.Len()), 7)
			} else {
				result.WriteUint(0, 1)
				result.WriteUint(uint64(1+graph[node].data.Len()/8), 7)
			}
		}
	}

	for i := 0; i < nodeCount; i++ {
		node := topoOrder[i]
		for childIdx := 0; childIdx < graph[node].refsCnt; childIdx++ {
			child := graph[node].refs[childIdx]
			direct := 0
			if rank[child] == i+1 {
				direct = 1
			}
			result.WriteUint(uint64(direct), 1)
		}
	}

	for _, node := range topoOrder {
		if graph[node].pbMask == 9 {
			continue
		}
		if graph[node].typ != PrunedCellType && !graph[node].small {
			continue
		}
		result.AppendSpan(graph[node].data.Subspan(0, graph[node].data.Len()%8))
	}

	for i := 0; i < nodeCount; i++ {
		node := topoOrder[i]
		if nodeCount <= i+3 {
			continue
		}
		for j := 0; j < graph[node].refsCnt; j++ {
			childRank := rank[graph[node].refs[j]]
			if childRank <= i+1 {
				continue
			}

			delta := childRank - i - 2
			requiredBits := bits.Len32(uint32(nodeCount - i - 3))
			availableBitsAfterFlag := 8 - ((result.Len() + 1) % 8)
			if requiredBits < availableBitsAfterFlag+1 {
				result.WriteUint(uint64(delta), requiredBits)
			} else if delta < (1 << availableBitsAfterFlag) {
				result.WriteUint(1, 1)
				result.WriteUint(uint64(delta), availableBitsAfterFlag)
			} else {
				result.WriteUint(0, 1)
				result.WriteUint(uint64(delta), requiredBits)
			}
		}
	}

	result.AlignByteZero()

	for _, node := range topoOrder {
		if graph[node].pbMask == 9 {
			continue
		}
		if graph[node].typ == PrunedCellType || graph[node].small {
			prefixSize := graph[node].data.Len() % 8
			result.AppendSpan(graph[node].data.Subspan(prefixSize, graph[node].data.Len()-prefixSize))
			continue
		}

		dataSize := graph[node].data.Len() + 1
		padding := (8 - dataSize%8) % 8
		for i := 0; i < padding; i++ {
			result.WriteBit(0)
		}
		result.WriteBit(1)
		result.AppendSpan(graph[node].data)
	}

	result.AlignByteZero()
	return compressWithSizeHeader(result.Bytes())
}

func decompressImprovedStructureLZ4(compressed []byte, maxSize int, decompressMerkleUpdate bool, state *Cell) ([]*Cell, error) {
	graph, err := decompressImprovedStructureLZ4Graph(compressed, maxSize, decompressMerkleUpdate, state)
	if err != nil {
		return nil, err
	}
	return graph.roots(), nil
}

func decompressImprovedStructureLZ4Graph(compressed []byte, maxSize int, decompressMerkleUpdate bool, state *Cell) (*decompressedBOCGraph, error) {
	const (
		maxCellDataLengthBits = 1024
		noNode                = -1
	)

	if decompressMerkleUpdate && state == nil {
		return nil, fmt.Errorf("boc decompression failed: state is required for MU decompressing")
	}

	serialized, err := decompressWithSizeHeader(compressed, maxSize)
	if err != nil {
		return nil, err
	}

	reader := newBitReader(serialized)
	origSize := reader.size

	rootCount64, err := reader.ReadUint(32)
	if err != nil {
		return nil, fmt.Errorf("boc decompression failed: %w", err)
	}
	rootCount := int(rootCount64)
	if rootCount < 1 {
		return nil, fmt.Errorf("boc decompression failed: invalid root count")
	}
	if MaxBOCRoots > 0 && rootCount > MaxBOCRoots {
		return nil, fmt.Errorf("boc decompression failed: too many roots: %d > %d", rootCount, MaxBOCRoots)
	}
	if rootCount > len(serialized) {
		return nil, fmt.Errorf("boc decompression failed: invalid root count")
	}

	rootIndexes := make([]int, rootCount)
	for i := 0; i < rootCount; i++ {
		val, err := reader.ReadUint(32)
		if err != nil {
			return nil, fmt.Errorf("boc decompression failed: %w", err)
		}
		rootIndexes[i] = int(val)
	}

	nodeCount64, err := reader.ReadUint(32)
	if err != nil {
		return nil, fmt.Errorf("boc decompression failed: %w", err)
	}
	nodeCount := int(nodeCount64)
	if nodeCount < 1 {
		return nil, fmt.Errorf("boc decompression failed: invalid node count")
	}
	if MaxBOCCells > 0 && nodeCount > MaxBOCCells {
		return nil, fmt.Errorf("boc decompression failed: too many nodes: %d > %d", nodeCount, MaxBOCCells)
	}
	if nodeCount > len(serialized) {
		return nil, fmt.Errorf("boc decompression failed: incorrect node count provided")
	}
	for _, idx := range rootIndexes {
		if idx < 0 || idx >= nodeCount {
			return nil, fmt.Errorf("boc decompression failed: invalid root index")
		}
	}

	cellDataLength := make([]int, nodeCount)
	isDataSmall := make([]bool, nodeCount)
	isSpecial := make([]bool, nodeCount)
	cellRefsCnt := make([]int, nodeCount)
	isDepthBalance := make([]bool, nodeCount)
	hasDepthBalance := false
	pbLevelMask := make([]byte, nodeCount)
	cellDataPrefix := make([]bitSpan, nodeCount)
	cellDataSuffix := make([]bitSpan, nodeCount)
	bocGraph := make([][4]int, nodeCount)

	for i := 0; i < nodeCount; i++ {
		if reader.BitsLeft() < 8 {
			return nil, fmt.Errorf("boc decompression failed: not enough bits for cell metadata")
		}

		cellType, err := reader.ReadUint(4)
		if err != nil {
			return nil, fmt.Errorf("boc decompression failed: %w", err)
		}
		isSpecial[i] = cellType != 0 && cellType != 9
		isDepthBalance[i] = cellType == 9
		hasDepthBalance = hasDepthBalance || isDepthBalance[i]
		if isSpecial[i] {
			pbLevelMask[i] = byte(cellType - 1)
		}

		refsCnt, err := reader.ReadUint(4)
		if err != nil {
			return nil, fmt.Errorf("boc decompression failed: %w", err)
		}
		cellRefsCnt[i] = int(refsCnt)
		if cellRefsCnt[i] > 4 {
			return nil, fmt.Errorf("boc decompression failed: invalid cell refs count")
		}

		switch {
		case isDepthBalance[i]:
			cellDataLength[i] = 0
		case pbLevelMask[i] != 0:
			cellDataLength[i] = (hashSize + depthSize) * 8 * bits.OnesCount8(pbLevelMask[i])
			if cellRefsCnt[i] == 1 {
				cellRefsCnt[i] = 0
				cellDataLength[i] = 0
			} else if cellRefsCnt[i] > 1 {
				return nil, fmt.Errorf("boc decompression failed: pruned branch cannot have references")
			}
		default:
			if reader.BitsLeft() < 8 {
				return nil, fmt.Errorf("boc decompression failed: not enough bits for data length")
			}

			small, err := reader.ReadUint(1)
			if err != nil {
				return nil, fmt.Errorf("boc decompression failed: %w", err)
			}
			isDataSmall[i] = small != 0

			length, err := reader.ReadUint(7)
			if err != nil {
				return nil, fmt.Errorf("boc decompression failed: %w", err)
			}
			cellDataLength[i] = int(length)
			if !isDataSmall[i] {
				cellDataLength[i] *= 8
				if cellDataLength[i] == 0 {
					cellDataLength[i] += 1024
				}
			}
		}

		if cellDataLength[i] > maxCellDataLengthBits {
			return nil, fmt.Errorf("boc decompression failed: invalid cell data length")
		}
	}

	for i := 0; i < nodeCount; i++ {
		for j := 0; j < cellRefsCnt[i]; j++ {
			edge, err := reader.ReadUint(1)
			if err != nil {
				return nil, fmt.Errorf("boc decompression failed: %w", err)
			}
			if edge != 0 {
				bocGraph[i][j] = i + 1
			}
		}
	}

	for i := 0; i < nodeCount; i++ {
		if isDepthBalance[i] {
			continue
		}
		remainderBits := cellDataLength[i] % 8
		if reader.BitsLeft() < remainderBits {
			return nil, fmt.Errorf("boc decompression failed: not enough bits for initial cell data")
		}
		cellDataPrefix[i], err = reader.Span(remainderBits)
		if err != nil {
			return nil, fmt.Errorf("boc decompression failed: %w", err)
		}
		cellDataLength[i] -= remainderBits
	}

	for i := 0; i < nodeCount; i++ {
		if nodeCount <= i+3 {
			for j := 0; j < cellRefsCnt[i]; j++ {
				if bocGraph[i][j] == 0 {
					bocGraph[i][j] = i + 2
				}
			}
			continue
		}

		for j := 0; j < cellRefsCnt[i]; j++ {
			if bocGraph[i][j] != 0 {
				continue
			}

			prefSize := origSize - reader.BitsLeft()
			requiredBits := bits.Len32(uint32(nodeCount - i - 3))
			if requiredBits < 8-((prefSize+1)%8)+1 {
				val, err := reader.ReadUint(requiredBits)
				if err != nil {
					return nil, fmt.Errorf("boc decompression failed: %w", err)
				}
				bocGraph[i][j] = int(val) + i + 2
				continue
			}

			edgeConnection, err := reader.ReadUint(1)
			if err != nil {
				return nil, fmt.Errorf("boc decompression failed: %w", err)
			}
			if edgeConnection != 0 {
				prefSize = origSize - reader.BitsLeft()
				availableBits := 8 - (prefSize % 8)
				val, err := reader.ReadUint(availableBits)
				if err != nil {
					return nil, fmt.Errorf("boc decompression failed: %w", err)
				}
				bocGraph[i][j] = int(val) + i + 2
			} else {
				val, err := reader.ReadUint(requiredBits)
				if err != nil {
					return nil, fmt.Errorf("boc decompression failed: %w", err)
				}
				bocGraph[i][j] = int(val) + i + 2
			}
		}
	}

	for node := 0; node < nodeCount; node++ {
		for j := 0; j < cellRefsCnt[node]; j++ {
			childNode := bocGraph[node][j]
			if childNode >= nodeCount {
				return nil, fmt.Errorf("boc decompression failed: invalid graph connection")
			}
			if childNode <= node {
				return nil, fmt.Errorf("boc decompression failed: circular reference in graph")
			}
		}
	}

	for (origSize-reader.BitsLeft())%8 != 0 {
		bit, err := reader.ReadUint(1)
		if err != nil {
			return nil, fmt.Errorf("boc decompression failed: %w", err)
		}
		if bit != 0 {
			return nil, fmt.Errorf("boc decompression failed: non-zero padding bits")
		}
	}

	for i := 0; i < nodeCount; i++ {
		if isDepthBalance[i] {
			continue
		}

		paddingBits := 0
		if pbLevelMask[i] == 0 && !isDataSmall[i] {
			for reader.BitsLeft() > 0 {
				bit, err := reader.PeekBit()
				if err != nil {
					return nil, fmt.Errorf("boc decompression failed: %w", err)
				}
				if bit != 0 {
					break
				}
				reader.pos++
				paddingBits++
			}

			if _, err = reader.ReadUint(1); err != nil {
				return nil, fmt.Errorf("boc decompression failed: %w", err)
			}
			paddingBits++
		}

		if cellDataLength[i] < paddingBits {
			return nil, fmt.Errorf("boc decompression failed: invalid cell data length")
		}
		remainingDataBits := cellDataLength[i] - paddingBits
		if reader.BitsLeft() < remainingDataBits {
			return nil, fmt.Errorf("boc decompression failed: not enough bits for remaining cell data")
		}
		cellDataSuffix[i], err = reader.Span(remainingDataBits)
		if err != nil {
			return nil, fmt.Errorf("boc decompression failed: %w", err)
		}
	}

	if reader.BitsLeft() > 7 {
		return nil, fmt.Errorf("boc decompression failed: trailing unused data")
	}
	for reader.BitsLeft() > 0 {
		bit, err := reader.ReadUint(1)
		if err != nil {
			return nil, fmt.Errorf("boc decompression failed: %w", err)
		}
		if bit != 0 {
			return nil, fmt.Errorf("boc decompression failed: trailing unused data")
		}
	}

	nodeDepth := make([]uint16, nodeCount)
	for node := nodeCount - 1; node >= 0; node-- {
		var maxChildDepth uint16
		for j := 0; j < cellRefsCnt[node]; j++ {
			if nodeDepth[bocGraph[node][j]] > maxChildDepth {
				maxChildDepth = nodeDepth[bocGraph[node][j]]
			}
		}
		if cellRefsCnt[node] != 0 {
			if maxChildDepth >= maxDepth {
				return nil, fmt.Errorf("boc decompression failed: cell depth too large")
			}
			nodeDepth[node] = maxChildDepth + 1
		}
	}

	nodes := make([]*Cell, nodeCount)
	var balanceSums []balanceDiffSum
	if hasDepthBalance {
		balanceSums = make([]balanceDiffSum, nodeCount)
	}
	var hasher decompressDeferredHasher
	hasher.cells = make([]*Cell, 0, nodeCount)
	hasher.presetLevel = make([]bool, 0, nodeCount)

	finalizeNodeFromBuilder := func(idx int, builder *Builder) error {
		for j := 0; j < cellRefsCnt[idx]; j++ {
			if err := builder.StoreRef(nodes[bocGraph[idx][j]]); err != nil {
				return fmt.Errorf("boc decompression failed: failed to finalize node: %w", err)
			}
		}
		node, err := hasher.finalizeFromBuilder(builder, isSpecial[idx])
		if err != nil {
			return fmt.Errorf("boc decompression failed: failed to finalize node: %w", err)
		}
		nodes[idx] = node
		return nil
	}

	var finalizeNode func(idx int) error
	finalizeNode = func(idx int) error {
		if isDepthBalance[idx] {
			return fmt.Errorf("boc decompression failed: depth-balance node must be reconstructed under MerkleUpdate")
		}

		builder := BeginCell()
		if pbLevelMask[idx] != 0 {
			builder.MustStoreUInt(uint64(PrunedCellType), 8)
			builder.MustStoreUInt(uint64(pbLevelMask[idx]), 8)
		}
		if err := storeBitSpan(builder, cellDataPrefix[idx]); err != nil {
			return fmt.Errorf("boc decompression failed: %w", err)
		}
		if err := storeBitSpan(builder, cellDataSuffix[idx]); err != nil {
			return fmt.Errorf("boc decompression failed: %w", err)
		}
		return finalizeNodeFromBuilder(idx, builder)
	}

	buildPrunedBranchFromState := func(idx int, sourceCell *Cell) error {
		levelMask := LevelMask{pbLevelMask[idx]}
		pbLevel := levelMask.GetLevel()
		if pbLevel == 0 || pbLevel > _DataCellMaxLevel {
			return fmt.Errorf("boc decompression failed: invalid level for prunned branch under MerkleUpdate")
		}

		if sourceCell.Level() == pbLevel {
			nodes[idx] = sourceCell
			return nil
		}

		pbCell, err := createPrunedBranchFromCell(sourceCell, pbLevel)
		if err != nil {
			return fmt.Errorf("boc decompression failed: failed to create pruned branch from state: %w", err)
		}
		nodes[idx] = pbCell
		return nil
	}

	var buildLeftUnderMU func(leftIdx int, stateCell *Cell) error
	buildLeftUnderMU = func(leftIdx int, stateCell *Cell) error {
		if stateCell == nil {
			return fmt.Errorf("boc decompression failed: missing state subtree for MerkleUpdate left branch")
		}
		if nodes[leftIdx] != nil {
			return nil
		}

		if pbLevelMask[leftIdx] != 0 {
			// pruned boundaries only need hashes/depths, which lazy state
			// cells carry without loading the payload
			return buildPrunedBranchFromState(leftIdx, stateCell)
		}

		// interior cells need data and refs; lazy state cells load on demand
		stateCell, err := stateCell.load()
		if err != nil {
			return fmt.Errorf("boc decompression failed: failed to load state subtree cell: %w", err)
		}
		if stateCell.refsCount() != cellRefsCnt[leftIdx] {
			return fmt.Errorf("boc decompression failed: state subtree refs mismatch while restoring MerkleUpdate left subtree")
		}
		if isSpecial[leftIdx] != stateCell.IsSpecial() {
			return fmt.Errorf("boc decompression failed: state subtree special flag mismatch while restoring MerkleUpdate left subtree")
		}

		for j := 0; j < cellRefsCnt[leftIdx]; j++ {
			if err := buildLeftUnderMU(bocGraph[leftIdx][j], stateCell.ref(j)); err != nil {
				return err
			}
		}

		var refsBuf [4]*Cell
		refs := refsBuf[:cellRefsCnt[leftIdx]]
		for j := range refs {
			refs[j] = nodes[bocGraph[leftIdx][j]]
		}
		reused, err := reuseStateCellForMULeft(stateCell, refs, &hasher)
		if err != nil {
			return fmt.Errorf("boc decompression failed: %w", err)
		}
		if reused != nil {
			nodes[leftIdx] = reused
			return nil
		}

		builder := BeginCell()
		if err := storeBitSpan(builder, cellBits(stateCell)); err != nil {
			return fmt.Errorf("boc decompression failed: %w", err)
		}
		return finalizeNodeFromBuilder(leftIdx, builder)
	}

	var balanceScratch balanceDiffScratch
	var buildRightUnderMU func(rightIdx int, leftIdx int, sumDiffOut int) error
	buildRightUnderMU = func(rightIdx int, leftIdx int, sumDiffOut int) error {
		if leftIdx != noNode && nodes[leftIdx] == nil {
			return fmt.Errorf("boc decompression failed: missing reconstructed left node under MerkleUpdate")
		}
		if nodes[rightIdx] != nil {
			if leftIdx != noNode && sumDiffOut >= 0 && balanceSums != nil {
				vertexDiff := balanceScratch.process(nodes[leftIdx], nodes[rightIdx])
				balanceSums[sumDiffOut].add(vertexDiff)
			}
			return nil
		}

		var curRightLeftDiff *big.Int
		for j := 0; j < cellRefsCnt[rightIdx]; j++ {
			rightChild := bocGraph[rightIdx][j]
			leftChild := noNode
			if leftIdx != noNode && j < cellRefsCnt[leftIdx] {
				leftChild = bocGraph[leftIdx][j]
			}
			if err := buildRightUnderMU(rightChild, leftChild, rightIdx); err != nil {
				return err
			}
		}

		if isDepthBalance[rightIdx] {
			if leftIdx == noNode {
				return fmt.Errorf("boc decompression failed: depth-balance left vertex has no grams")
			}
			if !extractBalanceFromDepthBalanceCellValueInto(nodes[leftIdx], &balanceScratch.slice, &balanceScratch.maybe, &balanceScratch.left) {
				return fmt.Errorf("boc decompression failed: depth-balance left vertex has no grams")
			}

			expectedRightGrams := new(big.Int).Set(&balanceScratch.left)
			if balanceSums[rightIdx].value != nil {
				expectedRightGrams.Add(expectedRightGrams, balanceSums[rightIdx].value)
			}
			builder := BeginCell()
			if err := writeDepthBalanceGrams(builder, expectedRightGrams); err != nil {
				return fmt.Errorf("boc decompression failed: failed to write depth-balance grams: %w", err)
			}
			curRightLeftDiff = balanceSums[rightIdx].value
			if err := finalizeNodeFromBuilder(rightIdx, builder); err != nil {
				return err
			}
		} else {
			if err := finalizeNode(rightIdx); err != nil {
				return err
			}
		}

		if curRightLeftDiff == nil && leftIdx != noNode {
			curRightLeftDiff = balanceScratch.process(nodes[leftIdx], nodes[rightIdx])
		}
		if sumDiffOut >= 0 && curRightLeftDiff != nil && balanceSums != nil {
			balanceSums[sumDiffOut].add(curRightLeftDiff)
		}
		return nil
	}

	var buildNode func(idx int, mainMUCellIdx int) error
	buildNode = func(idx int, mainMUCellIdx int) error {
		if nodes[idx] != nil {
			return nil
		}

		isMUNode := idx == mainMUCellIdx && isMerkleUpdateNode(isSpecial[idx], pbLevelMask[idx], cellDataPrefix[idx], cellDataSuffix[idx])
		if isMUNode {
			if cellRefsCnt[idx] != 2 {
				return fmt.Errorf("boc decompression failed: MerkleUpdate node expected to have 2 references")
			}

			leftIdx := bocGraph[idx][0]
			rightIdx := bocGraph[idx][1]
			if decompressMerkleUpdate {
				if err := buildLeftUnderMU(leftIdx, state); err != nil {
					return err
				}
			} else {
				if err := buildNode(leftIdx, noNode); err != nil {
					return err
				}
			}
			if err := buildRightUnderMU(rightIdx, leftIdx, -1); err != nil {
				return err
			}
			return finalizeNode(idx)
		}

		for j := 0; j < cellRefsCnt[idx]; j++ {
			if err := buildNode(bocGraph[idx][j], mainMUCellIdx); err != nil {
				return err
			}
		}
		return finalizeNode(idx)
	}

	for _, rootIndex := range rootIndexes {
		mainMUCellIdx := noNode
		if cellRefsCnt[rootIndex] > kMUCellOrderInRoot {
			mainMUCellIdx = bocGraph[rootIndex][kMUCellOrderInRoot]
		}
		if err := buildNode(rootIndex, mainMUCellIdx); err != nil {
			return nil, err
		}
	}

	if err := hasher.flush(); err != nil {
		return nil, fmt.Errorf("boc decompression failed: %w", err)
	}

	return &decompressedBOCGraph{
		nodes:       nodes,
		graph:       bocGraph,
		refsCnt:     cellRefsCnt,
		rootIndexes: rootIndexes,
	}, nil
}
