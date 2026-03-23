package cell

import (
	"bytes"
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
	w.data = append(w.data, make([]byte, needBytes-len(w.data))...)
}

func (w *bitWriter) WriteBit(bit byte) {
	w.ensure(1)
	if bit != 0 {
		w.data[w.bitLen/8] |= 1 << (7 - uint(w.bitLen%8))
	}
	w.bitLen++
}

func (w *bitWriter) WriteUint(value uint64, bits int) {
	for i := bits - 1; i >= 0; i-- {
		w.WriteBit(byte((value >> uint(i)) & 1))
	}
}

func (w *bitWriter) AppendSpan(span bitSpan) {
	for i := 0; i < span.Len(); i++ {
		w.WriteBit(span.Bit(i))
	}
}

func (w *bitWriter) AlignByteZero() {
	for w.bitLen%8 != 0 {
		w.WriteBit(0)
	}
}

func (w *bitWriter) Bytes() []byte {
	return append([]byte{}, w.data[:(w.bitLen+7)/8]...)
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

	return append([]byte{byte(algo)}, payload...), nil
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
	boc := ToBOCWithOptions(roots, BOCOptions{WithCRC32C: true})
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
	if span.Len() == 0 {
		return nil
	}
	return builder.StoreSlice(span.Bytes(), uint(span.Len()))
}

func cellBits(c *Cell) bitSpan {
	return bitSpan{data: c.data, bitLen: int(c.bitsSz)}
}

func finalizeCellFromBuilder(builder *Builder, special bool) (*Cell, error) {
	c := &Cell{
		special: special,
		bitsSz:  builder.bitsSz,
		data:    append([]byte{}, builder.data...),
		refs:    append([]*Cell{}, builder.refs...),
	}

	if !special {
		c.levelMask = ordinaryLevelMask(c.refs)
		c.calculateHashes()
		return c, nil
	}

	if c.bitsSz < 8 {
		return nil, fmt.Errorf("not enough data for a special cell")
	}

	switch Type(c.data[0]) {
	case PrunedCellType:
		if len(c.refs) != 0 {
			return nil, fmt.Errorf("pruned branch special cell has a cell reference")
		}
		if c.bitsSz < 16 {
			return nil, fmt.Errorf("not enough data for a pruned branch special cell")
		}
		c.levelMask = LevelMask{c.data[1]}
		level := c.levelMask.GetLevel()
		if level > _DataCellMaxLevel || level == 0 {
			return nil, fmt.Errorf("pruned branch has an invalid level")
		}
		expectedBits := (2 + c.levelMask.Apply(level-1).getHashesCount()*(hashSize+depthSize)) * 8
		if int(c.bitsSz) != expectedBits {
			return nil, fmt.Errorf("not enough data for a pruned branch special cell")
		}
	case LibraryCellType:
		if c.bitsSz != 8+256 {
			return nil, fmt.Errorf("not enough data for a library special cell")
		}
	case MerkleProofCellType:
		if c.bitsSz != 8+(hashSize+depthSize)*8 {
			return nil, fmt.Errorf("not enough data for a merkle proof special cell")
		}
		if len(c.refs) != 1 {
			return nil, fmt.Errorf("wrong references count for a merkle proof special cell")
		}
		if !bytes.Equal(c.data[1:1+hashSize], c.refs[0].getHash(0)) {
			return nil, fmt.Errorf("hash mismatch in a merkle proof special cell")
		}
		if binary.BigEndian.Uint16(c.data[1+hashSize:1+hashSize+depthSize]) != c.refs[0].getDepth(0) {
			return nil, fmt.Errorf("depth mismatch in a merkle proof special cell")
		}
		c.levelMask = LevelMask{c.refs[0].levelMask.Mask >> 1}
	case MerkleUpdateCellType:
		if c.bitsSz != 8+(hashSize+depthSize)*8*2 {
			return nil, fmt.Errorf("not enough data for a merkle update special cell")
		}
		if len(c.refs) != 2 {
			return nil, fmt.Errorf("wrong references count for a merkle update special cell")
		}
		if !bytes.Equal(c.data[1:1+hashSize], c.refs[0].getHash(0)) {
			return nil, fmt.Errorf("first hash mismatch in a merkle update special cell")
		}
		if !bytes.Equal(c.data[1+hashSize:1+hashSize*2], c.refs[1].getHash(0)) {
			return nil, fmt.Errorf("second hash mismatch in a merkle update special cell")
		}
		firstDepthOff := 1 + hashSize*2
		secondDepthOff := firstDepthOff + depthSize
		if binary.BigEndian.Uint16(c.data[firstDepthOff:firstDepthOff+depthSize]) != c.refs[0].getDepth(0) {
			return nil, fmt.Errorf("first depth mismatch in a merkle update special cell")
		}
		if binary.BigEndian.Uint16(c.data[secondDepthOff:secondDepthOff+depthSize]) != c.refs[1].getDepth(0) {
			return nil, fmt.Errorf("second depth mismatch in a merkle update special cell")
		}
		c.levelMask = LevelMask{(c.refs[0].levelMask.Mask | c.refs[1].levelMask.Mask) >> 1}
	default:
		return nil, fmt.Errorf("unknown special cell type")
	}

	c.calculateHashes()
	return c, nil
}

func createPrunedBranchFromCell(source *Cell, newLevel int) (*Cell, error) {
	if source == nil {
		return nil, fmt.Errorf("source cell is nil")
	}

	levelMask := source.levelMask.Apply(_DataCellMaxLevel)
	level := levelMask.GetLevel()
	if newLevel < level+1 {
		return nil, fmt.Errorf("invalid new pruned level")
	}

	builder := BeginCell().
		MustStoreUInt(uint64(PrunedCellType), 8).
		MustStoreUInt(uint64(levelMask.Mask|oneLevelMask(newLevel)), 8)

	for i := 0; i <= level; i++ {
		if !levelMask.IsSignificant(i) {
			continue
		}
		builder.MustStoreSlice(source.getHash(i), hashSize*8)
	}
	for i := 0; i <= level; i++ {
		if !levelMask.IsSignificant(i) {
			continue
		}
		builder.MustStoreUInt(uint64(source.getDepth(i)), depthSize*8)
	}

	return finalizeCellFromBuilder(builder, true)
}

func oneLevelMask(level int) byte {
	if level == 0 {
		return 0
	}
	return 1 << (level - 1)
}

func extractBalanceFromDepthBalanceCell(c *Cell) *big.Int {
	if c == nil {
		return nil
	}

	s := c.BeginParse()
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
	extra, err := s.LoadDict(32)
	if err != nil || extra != nil || s.BitsLeft() != 0 || s.RefsNum() != 0 {
		return nil
	}

	return grams
}

func processShardAccountsVertex(left, right *Cell) *big.Int {
	leftBalance := extractBalanceFromDepthBalanceCell(left)
	rightBalance := extractBalanceFromDepthBalanceCell(right)
	if leftBalance == nil || rightBalance == nil {
		return nil
	}
	return new(big.Int).Sub(rightBalance, leftBalance)
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
	if n < 0 {
		return nil, fmt.Errorf("lz4 decompression failed")
	}
	return dst[:n], nil
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

	cellHashes := map[string]int{}
	var bocGraph [][4]int
	var refsCnt []int
	var cellData []bitSpan
	var cellType []Type
	var pbLevelMask []byte
	var rootIndexes []int

	var buildGraph func(cell *Cell, mainMUCell *Cell, underMULeft, underMURight bool, leftCell *Cell, sumDiffOut *big.Int) (int, error)
	buildGraph = func(cell *Cell, mainMUCell *Cell, underMULeft, underMURight bool, leftCell *Cell, sumDiffOut *big.Int) (int, error) {
		if cell == nil {
			return 0, fmt.Errorf("error while importing a cell during serialization: cell is nil")
		}

		cellHash := string(cell.Hash())
		if idx, ok := cellHashes[cellHash]; ok {
			return idx, nil
		}

		currentCellID := len(bocGraph)
		cellHashes[cellHash] = currentCellID

		typ := cell.GetType()
		if cell.special && typ == UnknownCellType {
			head := byte(0)
			if len(cell.data) > 0 {
				head = cell.data[0]
			}
			return 0, fmt.Errorf("invalid loaded cell data: unknown special cell, bits=%d refs=%d mask=%d head=%d", cell.bitsSz, len(cell.refs), cell.levelMask.Mask, head)
		}

		bitsView := cellBits(cell)
		bocGraph = append(bocGraph, [4]int{})
		refsCnt = append(refsCnt, len(cell.refs))
		cellType = append(cellType, typ)
		pbLevelMask = append(pbLevelMask, 0)

		if len(cell.refs) > 4 {
			return 0, fmt.Errorf("invalid loaded cell data: too many refs")
		}

		if typ == PrunedCellType {
			if bitsView.Len() < 16 {
				return 0, fmt.Errorf("invalid loaded cell data: pruned branch is shorter than header")
			}
			cellData = append(cellData, bitsView.Subspan(16, bitsView.Len()-16))
			pbLevelMask[len(pbLevelMask)-1] = cell.data[1]
		} else {
			cellData = append(cellData, bitsView)
		}

		if compressMerkleUpdate && underMULeft {
			cellData[len(cellData)-1] = bitSpan{}
		}

		if muRemoveSubtreeSums && typ == MerkleUpdateCellType && mainMUCell != nil && bytes.Equal(cell.Hash(), mainMUCell.Hash()) {
			childLeftID, err := buildGraph(cell.refs[0], mainMUCell, true, false, nil, nil)
			if err != nil {
				return 0, err
			}
			bocGraph[currentCellID][0] = childLeftID

			childRightID, err := buildGraph(cell.refs[1], mainMUCell, false, true, cell.refs[0], nil)
			if err != nil {
				return 0, err
			}
			bocGraph[currentCellID][1] = childRightID
			return currentCellID, nil
		}

		if underMURight && leftCell != nil {
			sumChildDiff := big.NewInt(0)
			for i := 0; i < len(cell.refs); i++ {
				var pairedLeftChild *Cell
				if i < len(leftCell.refs) {
					pairedLeftChild = leftCell.refs[i]
				}
				childID, err := buildGraph(cell.refs[i], mainMUCell, false, true, pairedLeftChild, sumChildDiff)
				if err != nil {
					return 0, err
				}
				bocGraph[currentCellID][i] = childID
			}

			vertexDiff := processShardAccountsVertex(leftCell, cell)
			if !cell.special && vertexDiff != nil && sumChildDiff.Cmp(vertexDiff) == 0 {
				cellData[currentCellID] = bitSpan{}
				pbLevelMask[currentCellID] = 9
			}
			if sumDiffOut != nil && vertexDiff != nil {
				sumDiffOut.Add(sumDiffOut, vertexDiff)
			}
			return currentCellID, nil
		}

		for i := 0; i < len(cell.refs); i++ {
			childID, err := buildGraph(cell.refs[i], mainMUCell, underMULeft, underMURight, nil, nil)
			if err != nil {
				return 0, err
			}
			bocGraph[currentCellID][i] = childID
		}
		return currentCellID, nil
	}

	for _, root := range roots {
		var mainMUCell *Cell
		if len(root.refs) > kMUCellOrderInRoot {
			mainMUCell = root.refs[kMUCellOrderInRoot]
		}

		rootID, err := buildGraph(root, mainMUCell, false, false, nil, nil)
		if err != nil {
			return nil, err
		}
		rootIndexes = append(rootIndexes, rootID)
	}

	nodeCount := len(bocGraph)
	reverseGraph := make([][]int, nodeCount)
	edgeCount := 0
	for i := 0; i < nodeCount; i++ {
		for childIdx := 0; childIdx < refsCnt[i]; childIdx++ {
			child := bocGraph[i][childIdx]
			edgeCount++
			reverseGraph[child] = append(reverseGraph[child], i)
		}
	}

	isDataSmall := make([]bool, nodeCount)
	for i := 0; i < nodeCount; i++ {
		if cellType[i] != PrunedCellType {
			isDataSmall[i] = cellData[i].Len() < 128
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
		inDegree[i] = refsCnt[i]
		if inDegree[i] == 0 {
			ordinary := 0
			if cellType[i] == OrdinaryCellType {
				ordinary = 1
			}
			queue = append(queue, topoItem{a: ordinary, b: -cellData[i].Len(), c: -i})
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

		for _, parent := range reverseGraph[node] {
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

	var result bitWriter
	result.WriteUint(uint64(len(rootIndexes)), 32)
	for _, rootIdx := range rootIndexes {
		result.WriteUint(uint64(rank[rootIdx]), 32)
	}

	result.WriteUint(uint64(nodeCount), 32)
	for i := 0; i < nodeCount; i++ {
		node := topoOrder[i]
		currentCellType := 0
		if cellType[node] != OrdinaryCellType {
			currentCellType = 1
		}
		currentCellType += int(pbLevelMask[node])
		result.WriteUint(uint64(currentCellType), 4)

		currentRefsCnt := refsCnt[node]
		if cellType[node] == PrunedCellType && cellData[node].Len() == 0 {
			if currentRefsCnt != 0 {
				return nil, fmt.Errorf("invalid graph structure")
			}
			currentRefsCnt = 1
		}
		result.WriteUint(uint64(currentRefsCnt), 4)

		if cellType[node] != PrunedCellType && currentCellType != 9 {
			if isDataSmall[node] {
				result.WriteUint(1, 1)
				result.WriteUint(uint64(cellData[node].Len()), 7)
			} else {
				result.WriteUint(0, 1)
				result.WriteUint(uint64(1+cellData[node].Len()/8), 7)
			}
		}
	}

	for i := 0; i < nodeCount; i++ {
		node := topoOrder[i]
		for childIdx := 0; childIdx < refsCnt[node]; childIdx++ {
			child := bocGraph[node][childIdx]
			direct := 0
			if rank[child] == i+1 {
				direct = 1
			}
			result.WriteUint(uint64(direct), 1)
		}
	}

	for _, node := range topoOrder {
		if pbLevelMask[node] == 9 {
			continue
		}
		if cellType[node] != PrunedCellType && !isDataSmall[node] {
			continue
		}
		result.AppendSpan(cellData[node].Subspan(0, cellData[node].Len()%8))
	}

	for i := 0; i < nodeCount; i++ {
		node := topoOrder[i]
		if nodeCount <= i+3 {
			continue
		}
		for j := 0; j < refsCnt[node]; j++ {
			childRank := rank[bocGraph[node][j]]
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
		if pbLevelMask[node] == 9 {
			continue
		}
		if cellType[node] == PrunedCellType || isDataSmall[node] {
			prefixSize := cellData[node].Len() % 8
			result.AppendSpan(cellData[node].Subspan(prefixSize, cellData[node].Len()-prefixSize))
			continue
		}

		dataSize := cellData[node].Len() + 1
		padding := (8 - dataSize%8) % 8
		for i := 0; i < padding; i++ {
			result.WriteBit(0)
		}
		result.WriteBit(1)
		result.AppendSpan(cellData[node])
	}

	result.AlignByteZero()
	return compressWithSizeHeader(result.Bytes())
}

func decompressImprovedStructureLZ4(compressed []byte, maxSize int, decompressMerkleUpdate bool, state *Cell) ([]*Cell, error) {
	const (
		maxCellDataLengthBits = 1024
		maxNodeCount          = 1 << 20
		defaultMaxRoots       = 16384
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
	if rootCount < 1 || rootCount > defaultMaxRoots {
		return nil, fmt.Errorf("boc decompression failed: invalid root count")
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
	if nodeCount < 1 || nodeCount > maxNodeCount {
		return nil, fmt.Errorf("boc decompression failed: invalid node count")
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

	finalizeNodeFromBuilder := func(idx int, builder *Builder) error {
		for j := 0; j < cellRefsCnt[idx]; j++ {
			if err := builder.StoreRef(nodes[bocGraph[idx][j]]); err != nil {
				return fmt.Errorf("boc decompression failed: failed to finalize node: %w", err)
			}
		}
		node, err := finalizeCellFromBuilder(builder, isSpecial[idx])
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

		if int(sourceCell.levelMask.GetLevel()) == pbLevel {
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
			return buildPrunedBranchFromState(leftIdx, stateCell)
		}
		if len(stateCell.refs) != cellRefsCnt[leftIdx] {
			return fmt.Errorf("boc decompression failed: state subtree refs mismatch while restoring MerkleUpdate left subtree")
		}
		if isSpecial[leftIdx] != stateCell.special {
			return fmt.Errorf("boc decompression failed: state subtree special flag mismatch while restoring MerkleUpdate left subtree")
		}

		for j := 0; j < cellRefsCnt[leftIdx]; j++ {
			if err := buildLeftUnderMU(bocGraph[leftIdx][j], stateCell.refs[j]); err != nil {
				return err
			}
		}

		builder := BeginCell()
		if err := storeBitSpan(builder, cellBits(stateCell)); err != nil {
			return fmt.Errorf("boc decompression failed: %w", err)
		}
		return finalizeNodeFromBuilder(leftIdx, builder)
	}

	var buildRightUnderMU func(rightIdx int, leftIdx int, sumDiffOut *big.Int) error
	buildRightUnderMU = func(rightIdx int, leftIdx int, sumDiffOut *big.Int) error {
		if leftIdx != noNode && nodes[leftIdx] == nil {
			return fmt.Errorf("boc decompression failed: missing reconstructed left node under MerkleUpdate")
		}
		if nodes[rightIdx] != nil {
			if leftIdx != noNode && sumDiffOut != nil {
				vertexDiff := processShardAccountsVertex(nodes[leftIdx], nodes[rightIdx])
				if vertexDiff != nil {
					sumDiffOut.Add(sumDiffOut, vertexDiff)
				}
			}
			return nil
		}

		var curRightLeftDiff *big.Int
		sumChildDiff := big.NewInt(0)
		for j := 0; j < cellRefsCnt[rightIdx]; j++ {
			rightChild := bocGraph[rightIdx][j]
			leftChild := noNode
			if leftIdx != noNode && j < cellRefsCnt[leftIdx] {
				leftChild = bocGraph[leftIdx][j]
			}
			if err := buildRightUnderMU(rightChild, leftChild, sumChildDiff); err != nil {
				return err
			}
		}

		if isDepthBalance[rightIdx] {
			if leftIdx == noNode {
				return fmt.Errorf("boc decompression failed: depth-balance left vertex has no grams")
			}
			leftGrams := extractBalanceFromDepthBalanceCell(nodes[leftIdx])
			if leftGrams == nil {
				return fmt.Errorf("boc decompression failed: depth-balance left vertex has no grams")
			}

			expectedRightGrams := new(big.Int).Add(new(big.Int).Set(leftGrams), sumChildDiff)
			builder := BeginCell()
			if err := writeDepthBalanceGrams(builder, expectedRightGrams); err != nil {
				return fmt.Errorf("boc decompression failed: failed to write depth-balance grams: %w", err)
			}
			curRightLeftDiff = new(big.Int).Set(sumChildDiff)
			if err := finalizeNodeFromBuilder(rightIdx, builder); err != nil {
				return err
			}
		} else {
			if err := finalizeNode(rightIdx); err != nil {
				return err
			}
		}

		if curRightLeftDiff == nil && leftIdx != noNode {
			curRightLeftDiff = processShardAccountsVertex(nodes[leftIdx], nodes[rightIdx])
		}
		if sumDiffOut != nil && curRightLeftDiff != nil {
			sumDiffOut.Add(sumDiffOut, curRightLeftDiff)
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
			if err := buildRightUnderMU(rightIdx, leftIdx, nil); err != nil {
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

	roots := make([]*Cell, 0, rootCount)
	for _, idx := range rootIndexes {
		roots = append(roots, nodes[idx])
	}
	return roots, nil
}
