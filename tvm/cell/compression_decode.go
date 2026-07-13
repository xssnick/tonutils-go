package cell

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

// decompressDeferredHasher collects cells built during state-aware BoC
// decompression whose sha256 hashes can be computed later in parallel waves.
// Depths for every significant level are preset at construction time, so
// parents can validate ref depth limits before the hashes exist. Hashes are
// only read inside the decompressor after an explicit flush.
type decompressDeferredHasher struct {
	cells       []*Cell
	presetLevel []bool
}

const decompressParallelHashMinWave = 128

func (d *decompressDeferredHasher) append(c *Cell, presetLevel0 bool) {
	d.cells = append(d.cells, c)
	d.presetLevel = append(d.presetLevel, presetLevel0)
}

// finalizeFromBuilder mirrors finalizeCellFromBuilder but defers hash
// computation for ordinary cells. Special merkle cells force a flush first
// because their boundary validation reads child hashes.
func (d *decompressDeferredHasher) finalizeFromBuilder(builder *Builder, special bool) (*Cell, error) {
	if special && builder.usedBytes() > 0 {
		switch Type(builder.data[0]) {
		case MerkleProofCellType, MerkleUpdateCellType:
			if err := d.flush(); err != nil {
				return nil, err
			}
		}
	}

	c, err := buildCellShellFromBuilder(builder, special)
	if err != nil {
		return nil, err
	}
	if special {
		if err := c.calculateHashes(); err != nil {
			return nil, err
		}
		return c, nil
	}

	c.resolveType()
	if err := presetDeferredDepths(c); err != nil {
		return nil, err
	}
	d.append(c, false)
	return c, nil
}

// flush computes hashes of all deferred cells in waves grouped by depth:
// every cell only depends on cells of strictly smaller depth, so cells inside
// one wave hash independently and can run in parallel.
func (d *decompressDeferredHasher) flush() error {
	n := len(d.cells)
	if n == 0 {
		return nil
	}

	maxCellDepth := 0
	for _, c := range d.cells {
		if int(c.depth0) > maxCellDepth {
			maxCellDepth = int(c.depth0)
		}
	}

	// counting sort by depth0 keeps wave grouping allocation-light
	counts := make([]int32, maxCellDepth+2)
	for _, c := range d.cells {
		counts[int(c.depth0)+1]++
	}
	for i := 1; i < len(counts); i++ {
		counts[i] += counts[i-1]
	}
	ordered := make([]int32, n)
	fill := make([]int32, maxCellDepth+1)
	for i, c := range d.cells {
		depth := int(c.depth0)
		ordered[counts[depth]+fill[depth]] = int32(i)
		fill[depth]++
	}

	workers := runtime.GOMAXPROCS(0)
	if workers > 16 {
		workers = 16
	}

	var firstErr atomic.Pointer[error]
	compute := func(idx int32) {
		var err error
		if d.presetLevel[idx] {
			err = d.cells[idx].calculateHashesPresetLevel0()
		} else {
			err = d.cells[idx].calculateHashes()
		}
		if err != nil {
			// copy to a fresh variable so only the failure path allocates
			errCopy := err
			firstErr.CompareAndSwap(nil, &errCopy)
		}
	}

	for depth := 0; depth <= maxCellDepth; depth++ {
		wave := ordered[counts[depth]:counts[depth+1]]
		if len(wave) == 0 {
			continue
		}

		if workers <= 1 || len(wave) < decompressParallelHashMinWave {
			for _, idx := range wave {
				compute(idx)
			}
		} else {
			chunk := (len(wave) + workers - 1) / workers
			var wg sync.WaitGroup
			for start := 0; start < len(wave); start += chunk {
				end := start + chunk
				if end > len(wave) {
					end = len(wave)
				}
				wg.Add(1)
				go func(part []int32) {
					defer wg.Done()
					for _, idx := range part {
						compute(idx)
					}
				}(wave[start:end])
			}
			wg.Wait()
		}

		if errPtr := firstErr.Load(); errPtr != nil {
			d.cells = d.cells[:0]
			d.presetLevel = d.presetLevel[:0]
			return *errPtr
		}
	}

	d.cells = d.cells[:0]
	d.presetLevel = d.presetLevel[:0]
	return nil
}

// presetDeferredDepths computes and stores depths for every significant level
// of an ordinary cell, matching what calculateHashes would produce, so depth
// reads are valid before the deferred hashes are flushed.
func presetDeferredDepths(c *Cell) error {
	levelMask := c.getLevelMask()
	level := levelMask.GetLevel()
	refCnt := c.refsCount()

	hashIndex := 0
	for levelIndex := 0; levelIndex <= level; levelIndex++ {
		if !levelMask.IsSignificant(levelIndex) {
			continue
		}

		var depth uint16
		for i := 0; i < refCnt; i++ {
			childDepth := c.refs[i].getDepth(levelIndex)
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
		c.setDepthAt(hashIndex, depth)
		hashIndex++
	}
	return nil
}

// calculateHashesPresetLevel0 computes hashes for significant levels >= 1 of
// an ordinary cell whose level-0 hash and depth were copied from an equal
// already-hashed cell (the previous-state cell it was rebuilt from).
func (c *Cell) calculateHashesPresetLevel0() error {
	levelMask := c.getLevelMask()
	level := levelMask.GetLevel()
	refCnt := c.refsCount()

	meta := c.ensureMeta()
	if meta.extraHashes == nil {
		meta.extraHashes = new([3]Hash)
	}

	var hashBuf [2 + maxCellDataBytes + (4 * depthSize) + (4 * hashSize)]byte

	hashIndex := 1
	for levelIndex := 1; levelIndex <= level; levelIndex++ {
		if !levelMask.IsSignificant(levelIndex) {
			continue
		}

		dsc1, dsc2 := c.descriptors(levelMask.Apply(levelIndex))
		hashBuf[0], hashBuf[1] = dsc1, dsc2
		bufPos := 2
		bufPos += copy(hashBuf[bufPos:], c.hashAt(hashIndex-1))

		var depth uint16
		for i := 0; i < refCnt; i++ {
			childDepth := c.refs[i].getDepth(levelIndex)
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

		for i := 0; i < refCnt; i++ {
			bufPos += copy(hashBuf[bufPos:], c.refs[i].getHash(levelIndex))
		}

		c.setDepthAt(hashIndex, depth)
		sum := sha256.Sum256(hashBuf[:bufPos])
		c.setHashAt(hashIndex, sum[:])
		hashIndex++
	}
	return nil
}

// reuseStateCellForMULeft rebuilds a left-side MerkleUpdate cell from its
// previous-state twin without re-hashing level 0: the reconstructed cell has
// the same data and the same level-0 child hashes (pruned branches preserve
// source hashes), so its level-0 hash and depth are copied from the state
// cell and only higher-level hashes remain to be computed. Returns nil when
// the state cell shape does not qualify for the fast path.
func reuseStateCellForMULeft(stateCell *Cell, refs []*Cell, d *decompressDeferredHasher) (*Cell, error) {
	if stateCell.IsSpecial() || stateCell.meta != nil || stateCell.IsLazy() {
		return nil, nil
	}

	mask := byte(0)
	for _, ref := range refs {
		mask |= ref.getLevelMask().Mask
	}
	if mask == 0 {
		// no pruned boundaries below: the subtree is bit-identical to the
		// state subtree, reuse the state cell itself
		return stateCell, nil
	}

	c := &Cell{
		data:   stateCell.data,
		bitsSz: stateCell.bitsSz,
	}
	copy(c.refs[:], refs)
	c.setRefsCount(len(refs))
	c.setLevelMask(LevelMask{Mask: mask})
	c.resolveType()
	if err := validateCellRefDepthLimit(refs); err != nil {
		return nil, err
	}
	if err := presetDeferredDepths(c); err != nil {
		return nil, err
	}
	c.hash0 = stateCell.hash0
	c.depth0 = stateCell.depth0

	d.append(c, true)
	return c, nil
}

// decompressedBOCGraph carries the deduplicated cell graph produced by
// state-aware decompression: nodes are unique by max-level hash and edges
// always point to strictly larger indices.
type decompressedBOCGraph struct {
	nodes       []*Cell
	graph       [][4]int
	refsCnt     []int
	rootIndexes []int
}

func (g *decompressedBOCGraph) roots() []*Cell {
	roots := make([]*Cell, len(g.rootIndexes))
	for i, idx := range g.rootIndexes {
		roots[i] = g.nodes[idx]
	}
	return roots
}

// DecompressBOCSerialized decompresses like DecompressBOC and additionally
// serializes the result with opts in one go. For structure-aware payloads the
// serializer is fed the already-deduplicated decompression graph directly,
// skipping the per-cell hash lookups a fresh ToBOCWithOptions pass would do.
// The produced bytes are identical to ToBOCWithOptions over the same roots.
func DecompressBOCSerialized(data []byte, maxSize int, state *Cell, opts BOCSerializeOptions) ([]*Cell, []byte, error) {
	if len(data) == 0 {
		return nil, nil, fmt.Errorf("can't decompress empty data")
	}

	var graph *decompressedBOCGraph
	var err error
	switch CompressionAlgorithm(data[0]) {
	case CompressionBaselineLZ4:
		roots, err := decompressBaselineLZ4(data[1:], maxSize)
		if err != nil {
			return nil, nil, err
		}
		boc, err := ToBOCWithOptionsErr(roots, opts)
		if err != nil {
			return nil, nil, err
		}
		return roots, boc, nil
	case CompressionImprovedStructureLZ4:
		graph, err = decompressImprovedStructureLZ4Graph(data[1:], maxSize, false, nil)
	case CompressionImprovedStructureLZ4WithState:
		graph, err = decompressImprovedStructureLZ4Graph(data[1:], maxSize, true, state)
	default:
		return nil, nil, fmt.Errorf("unknown compression algorithm")
	}
	if err != nil {
		return nil, nil, err
	}

	boc, err := serializeDecompressedGraph(graph, opts)
	if err != nil {
		return nil, nil, err
	}
	return graph.roots(), boc, nil
}

// serializeDecompressedGraph builds the BoC serializer cell list straight
// from the decompression graph — the graph is already deduplicated, so the
// hash-index probes of the generic import pass are unnecessary. Ordering,
// weights and the output bytes match ToBOCWithOptions exactly because the
// shared reorder and append passes are reused.
func serializeDecompressedGraph(g *decompressedBOCGraph, opts BOCSerializeOptions) ([]byte, error) {
	if len(g.rootIndexes) == 0 {
		return nil, fmt.Errorf("no root cells to serialize")
	}

	s := &bocSerializer{
		roots:    make([]bocRoot, len(g.rootIndexes)),
		maxDepth: maxDepth,
		cellList: make([]bocSerializeItem, 0, len(g.nodes)),
	}

	// listPos[i] is the cellList index of graph node i, plus one; 0 = not
	// yet imported. Mirrors importCell's post-order discovery.
	listPos := make([]uint32, len(g.nodes))

	var importNode func(idx int, depth int) (uint32, error)
	importNode = func(idx int, depth int) (uint32, error) {
		if depth > s.maxDepth {
			return 0, fmt.Errorf("cell depth too large")
		}
		if pos := listPos[idx]; pos != 0 {
			s.cellList[pos-1].shouldCache = true
			return pos - 1, nil
		}

		cell := g.nodes[idx].rawCell()
		refCnt := g.refsCnt[idx]
		refs := [4]uint32{
			bocInvalidCellIndex,
			bocInvalidCellIndex,
			bocInvalidCellIndex,
			bocInvalidCellIndex,
		}
		sumChildWeight := 1
		for i := 0; i < refCnt; i++ {
			refIdx, err := importNode(g.graph[idx][i], depth+1)
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

		newIdx := s.cellCount
		if uint64(newIdx) >= uint64(bocVisitLinked) {
			return 0, fmt.Errorf("too many cells to serialize")
		}

		s.cellList = append(s.cellList, bocSerializeItem{
			cell:   cell,
			refIdx: refs,
			refNum: uint8(refCnt),
			wt:     byte(wt),
			hcnt:   byte(cell.getLevelMask().getHashesCount()),
		})
		listPos[idx] = uint32(newIdx) + 1
		s.dataBytes += uint64(cell.serializedBOCSize(false))

		s.cellCount++
		return uint32(newIdx), nil
	}

	for i, rootIdx := range g.rootIndexes {
		idx, err := importNode(rootIdx, 0)
		if err != nil {
			return nil, err
		}
		s.roots[i] = bocRoot{cell: g.nodes[rootIdx], idx: idx}
	}

	s.reorderCells()
	if s.cellCount == 0 {
		return nil, fmt.Errorf("no cells to serialize")
	}

	return s.appendTo(nil, opts.mode())
}
