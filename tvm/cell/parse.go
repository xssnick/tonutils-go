package cell

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
)

const hashSize = 32
const depthSize = 2

var castTable = crc32.MakeTable(crc32.Castagnoli)

var bocMagic = []byte{0xB5, 0xEE, 0x9C, 0x72}
var bocIdxMagic = []byte{0x68, 0xFF, 0x65, 0xF3}
var bocIdxCRC32CMagic = []byte{0xAC, 0xC3, 0xA7, 0x28}

// MaxBOCRoots limits how many roots may be decoded from a single BOC payload,
// including the improved compression/decompression path.
// Set it to 0 or a negative value to disable the limit.
var MaxBOCRoots = 16384

// MaxBOCCells limits how many cells may be decoded from a single BOC payload,
// including the improved compression/decompression path.
// Set it to 0 or a negative value to disable the limit.
var MaxBOCCells = 1 << 20

const maxSerializedBOCCellBytes = 2 + maxCellDataBytes + 4*4 + (hashSize+depthSize)*4

func maxBOCPayloadBytes() int {
	if MaxBOCCells <= 0 {
		return 0
	}
	if MaxBOCCells > math.MaxInt/maxSerializedBOCCellBytes {
		return math.MaxInt
	}
	return MaxBOCCells * maxSerializedBOCCellBytes
}

// BOCParseOptions configures BoC parsing.
type BOCParseOptions struct {
	// TrustedHashes trusts serialized hashes/depths when a cell stores them in
	// the BoC payload. Cells without serialized hashes are still hashed normally.
	TrustedHashes bool
	// NoCopyPayload lets parsed cells reference the BoC payload directly.
	// The caller must keep the source immutable while returned cells are alive.
	NoCopyPayload bool
	// Lazy keeps hash-bearing BoC subtrees behind lazy pruned boundaries.
	// It requires TrustedHashes because lazy boundaries cannot be verified
	// without materializing the hidden subtree.
	Lazy bool
}

const (
	bocPayloadArenaMinChunkSize = 4 << 10
	bocPayloadArenaMaxChunkSize = 128 << 20
)

type bocFlags struct {
	hasIndex     bool
	HasCrc32c    bool
	hasCacheBits bool
}

// BOCNoCopyReader wraps in-memory BoC bytes for the parser's direct read path.
type BOCNoCopyReader struct {
	base      []byte
	data      []byte
	remaining interface {
		Len() int
	}
	reader io.Reader

	crc       uint32
	crcActive bool
	crcDirect bool
	direct    bool
}

// NewBOCNoCopyReader creates a BoC reader over in-memory bytes.
func NewBOCNoCopyReader(data []byte) *BOCNoCopyReader {
	return &BOCNoCopyReader{
		base:   data,
		data:   data,
		direct: true,
	}
}

func newBOCStreamReader(reader io.Reader) *BOCNoCopyReader {
	if bocReader, ok := reader.(*BOCNoCopyReader); ok {
		return bocReader
	}

	r := &BOCNoCopyReader{
		reader: reader,
	}
	if remaining, ok := reader.(interface{ Len() int }); ok {
		r.remaining = remaining
	} else if _, ok := reader.(*bufio.Reader); !ok {
		r.reader = bufio.NewReaderSize(reader, 1<<20)
	}
	return r
}

// Read is a fallback for external io.Reader use. BoC parsing uses the direct
// path for BOCNoCopyReader and does not call Read.
func (r *BOCNoCopyReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r == nil || len(r.data) == 0 {
		return 0, io.EOF
	}

	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func (r *BOCNoCopyReader) Len() int {
	if r == nil {
		return 0
	}
	if r.direct {
		return len(r.data)
	}
	if r.remaining == nil {
		return 0
	}
	return r.remaining.Len()
}

func (r *BOCNoCopyReader) enableCRC(prefix []byte) {
	if r.direct && r.base != nil {
		r.crc = 0
		r.crcActive = false
		r.crcDirect = true
		return
	}

	r.crc = crc32.Update(0, castTable, prefix)
	r.crcActive = true
	r.crcDirect = false
}

func (r *BOCNoCopyReader) disableCRC() {
	r.crcActive = false
	r.crcDirect = false
}

func (r *BOCNoCopyReader) crc32() uint32 {
	return r.crc
}

func (r *BOCNoCopyReader) readByte() (byte, error) {
	if r.direct {
		data, err := r.readDirect(1)
		if err != nil {
			return 0, err
		}
		return data[0], nil
	}

	var buf [1]byte
	if err := r.readFull(buf[:]); err != nil {
		return 0, err
	}
	return buf[0], nil
}

func (r *BOCNoCopyReader) readBytes(num int) ([]byte, error) {
	if num < 0 {
		return nil, errors.New("invalid read size")
	}
	if num == 0 {
		return nil, nil
	}
	if r.direct {
		return r.readDirect(num)
	}

	buf := make([]byte, num)
	if err := r.readFull(buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (r *BOCNoCopyReader) readDynInt(size int) (int, error) {
	if size < 0 || size > 8 {
		return 0, errors.New("invalid dynamic integer size")
	}
	if r.direct {
		data, err := r.readDirect(size)
		if err != nil {
			return 0, err
		}
		return dynInt(data), nil
	}

	var buf [8]byte
	if err := r.readFull(buf[:size]); err != nil {
		return 0, err
	}
	return dynInt(buf[:size]), nil
}

func (r *BOCNoCopyReader) readFull(buf []byte) error {
	if len(buf) == 0 {
		return nil
	}
	if r.direct {
		data, err := r.readDirect(len(buf))
		if err != nil {
			return err
		}
		copy(buf, data)
		return nil
	}

	if _, err := io.ReadFull(r.reader, buf); err != nil {
		return err
	}
	if r.crcActive {
		r.crc = crc32.Update(r.crc, castTable, buf)
	}
	return nil
}

func (r *BOCNoCopyReader) readDirect(num int) ([]byte, error) {
	if r == nil || num > len(r.data) {
		return nil, io.ErrUnexpectedEOF
	}

	data := r.data[:num]
	r.data = r.data[num:]
	if r.crcActive {
		r.crc = crc32.Update(r.crc, castTable, data)
	}
	return data, nil
}

func (r *BOCNoCopyReader) leftLen() (int, bool) {
	if r.direct {
		return len(r.data), true
	}
	if r.remaining == nil {
		return 0, false
	}
	return r.remaining.Len(), true
}

func FromBOC(data []byte) (*Cell, error) {
	cells, err := FromBOCMultiRoot(data)
	if err != nil {
		return nil, err
	}
	if len(cells) != 1 {
		return nil, fmt.Errorf("boc should contain exactly one root, got %d", len(cells))
	}

	return cells[0], nil
}

func FromBOCMultiRoot(data []byte) ([]*Cell, error) {
	cells, _, err := parseBOCMultiRoot(NewBOCNoCopyReader(data), BOCParseOptions{})
	return cells, err
}

func FromBOCMultiRootReader(reader io.Reader, options BOCParseOptions) ([]*Cell, []Cell, error) {
	if reader == nil {
		return nil, nil, errors.New("invalid boc")
	}
	return parseBOCMultiRoot(newBOCStreamReader(reader), options)
}

func parseBOCMultiRoot(r *BOCNoCopyReader, options BOCParseOptions) ([]*Cell, []Cell, error) {
	readDyn := func(size int, name string) (int, error) {
		val, err := r.readDynInt(size)
		if err != nil {
			return 0, fmt.Errorf("failed to read %s: %w", name, err)
		}
		return val, nil
	}

	var magic [4]byte
	if err := r.readFull(magic[:]); err != nil {
		return nil, nil, errors.New("invalid boc")
	}

	legacyIndexed := false
	switch {
	case matchBOCMagic(magic[:], bocMagic):
	case matchBOCMagic(magic[:], bocIdxMagic):
		legacyIndexed = true
	case matchBOCMagic(magic[:], bocIdxCRC32CMagic):
		legacyIndexed = true
	default:
		return nil, nil, errors.New("invalid boc magic header")
	}

	flagsByte, err := r.readByte()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read boc flags: %w", err)
	}
	flags, cellNumSizeBytes := parseBOCFlags(flagsByte) // has_idx:(## 1) has_crc32c:(## 1)  has_cache_bits:(## 1) flags:(## 2) { flags = 0 } size:(## 3) { size <= 4 }
	if legacyIndexed {
		flags.hasIndex = true
		flags.HasCrc32c = matchBOCMagic(magic[:], bocIdxCRC32CMagic)
		flags.hasCacheBits = false
	}
	if flags.HasCrc32c {
		var crcPrefix [5]byte
		copy(crcPrefix[:4], magic[:])
		crcPrefix[4] = flagsByte
		r.enableCRC(crcPrefix[:])
	}
	if cellNumSizeBytes < 1 || cellNumSizeBytes > 4 {
		return nil, nil, errors.New("invalid boc size descriptor")
	}

	dataSize, err := r.readByte()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read boc offset descriptor: %w", err)
	}
	dataSizeBytes := int(dataSize) // off_bytes:(## 8) { off_bytes <= 8 }
	if dataSizeBytes < 1 || dataSizeBytes > 8 {
		return nil, nil, errors.New("invalid boc offset descriptor")
	}

	cellsNum, err := readDyn(cellNumSizeBytes, "cells count") // cells:(##(size * 8))
	if err != nil {
		return nil, nil, err
	}
	rootsNum, err := readDyn(cellNumSizeBytes, "roots count") // roots:(##(size * 8)) { roots >= 1 }
	if err != nil {
		return nil, nil, err
	}
	if cellsNum <= 0 || rootsNum <= 0 {
		return nil, nil, errors.New("invalid boc counters")
	}
	if legacyIndexed && rootsNum != 1 {
		return nil, nil, errors.New("invalid boc counters")
	}
	if MaxBOCCells > 0 && cellsNum > MaxBOCCells {
		return nil, nil, fmt.Errorf("too many cells in boc: %d > %d", cellsNum, MaxBOCCells)
	}
	if MaxBOCRoots > 0 && rootsNum > MaxBOCRoots {
		return nil, nil, fmt.Errorf("too many roots in boc: %d > %d", rootsNum, MaxBOCRoots)
	}

	// complete BOCs - ??? (absent:(##(size * 8)) { roots + absent <= cells })
	absentNum, err := readDyn(cellNumSizeBytes, "absent count")
	if err != nil {
		return nil, nil, err
	}
	if absentNum < 0 || absentNum > cellsNum || rootsNum+absentNum > cellsNum {
		return nil, nil, errors.New("invalid boc counters")
	}

	dataLen, err := readDyn(dataSizeBytes, "cells data size") // tot_cells_size:(##(off_bytes * 8))
	if err != nil {
		return nil, nil, err
	}
	if dataLen < cellsNum*2 {
		return nil, nil, errors.New("invalid boc cells data size")
	}
	maxPayloadBytes := maxBOCPayloadBytes()
	if maxPayloadBytes > 0 && dataLen > maxPayloadBytes {
		return nil, nil, fmt.Errorf("boc cells data size is too big: %d > %d", dataLen, maxPayloadBytes)
	}
	if cellsNum > 0 && dataLen > cellsNum*maxSerializedBOCCellBytes {
		return nil, nil, fmt.Errorf("boc cells data size is too big for cells count: data len %d, cells %d", dataLen, cellsNum)
	}

	if flags.hasCacheBits && !flags.hasIndex {
		return nil, nil, fmt.Errorf("cache flag cant be set without index flag")
	}

	rootsIndex := make([]uint32, rootsNum)
	if legacyIndexed {
		rootsIndex[0] = 0
	} else {
		for i := 0; i < rootsNum; i++ {
			rootIndex, err := readDyn(cellNumSizeBytes, "root index")
			if err != nil {
				return nil, nil, err
			}
			if rootIndex < 0 || rootIndex >= cellsNum {
				return nil, nil, errors.New("invalid root index")
			}
			rootsIndex[i] = uint32(rootIndex)
		}
	}

	var cellIndex bocCellIndex
	if flags.hasIndex {
		idxData, err := r.readBytes(cellsNum * dataSizeBytes)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read custom index, err: %v", err)
		}
		cellIndex = bocCellIndex{
			data:         idxData,
			offsetBytes:  dataSizeBytes,
			hasCacheBits: flags.hasCacheBits,
		}
	}

	if cellsNum > dataLen/2 {
		return nil, nil, fmt.Errorf("cells num looks malicious: data len %d, cells %d", dataLen, cellsNum)
	}

	if options.Lazy {
		cll, err := parseLazyBOC(rootsIndex, cellsNum, cellNumSizeBytes, dataLen, r, cellIndex, options)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse payload: %w", err)
		}

		if err := finishBOCRead(r, flags.HasCrc32c); err != nil {
			return nil, nil, err
		}

		return cll, nil, nil
	}

	cll, unique, err := parseCells(rootsIndex, cellsNum, cellNumSizeBytes, dataLen, r, cellIndex, options)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse payload: %w", err)
	}

	if err := finishBOCRead(r, flags.HasCrc32c); err != nil {
		return nil, nil, err
	}

	return cll, unique, nil
}

func finishBOCRead(r *BOCNoCopyReader, hasCRC32C bool) error {
	if hasCRC32C {
		if r.crcDirect {
			if len(r.data) < 4 {
				return fmt.Errorf("failed to read crc32c trailer: %w", io.ErrUnexpectedEOF)
			}

			consumed := len(r.base) - len(r.data)
			crc := crc32.Checksum(r.base[:consumed], castTable)
			if binary.LittleEndian.Uint32(r.data[:4]) != crc {
				r.disableCRC()
				return errors.New("checksum not matches")
			}
			r.data = r.data[4:]
		} else {
			r.disableCRC()
			var trailer [4]byte
			if err := r.readFull(trailer[:]); err != nil {
				return fmt.Errorf("failed to read crc32c trailer: %w", err)
			}
			if binary.LittleEndian.Uint32(trailer[:]) != r.crc32() {
				return errors.New("checksum not matches")
			}
		}
		r.disableCRC()
	}
	if left, ok := r.leftLen(); ok && left != 0 {
		return fmt.Errorf("unexpected trailing data after boc payload: %d bytes", left)
	}

	return nil
}

type bocCellIndex struct {
	data         []byte
	offsetBytes  int
	hasCacheBits bool
}

func (idx bocCellIndex) enabled() bool {
	return idx.data != nil
}

func (idx bocCellIndex) rawEnd(i int) int {
	off := i * idx.offsetBytes
	return dynInt(idx.data[off : off+idx.offsetBytes])
}

func (idx bocCellIndex) cellEnd(i int) int {
	end := idx.rawEnd(i)
	if idx.hasCacheBits {
		end /= 2
	}
	return end
}

func (idx bocCellIndex) cacheBit(i int) bool {
	return idx.rawEnd(i)%2 == 1
}

type bocPayloadArena struct {
	current       []byte
	nextChunkSize int
	remaining     int
}

func newBOCPayloadArena(maxPayloadBytes int) bocPayloadArena {
	nextChunkSize := maxPayloadBytes
	if nextChunkSize > bocPayloadArenaMinChunkSize {
		nextChunkSize = bocPayloadArenaMinChunkSize
	}

	return bocPayloadArena{
		nextChunkSize: nextChunkSize,
		remaining:     maxPayloadBytes,
	}
}

func (a *bocPayloadArena) alloc(size int) []byte {
	if size == 0 {
		return nil
	}
	if len(a.current)+size > cap(a.current) {
		a.grow(size)
	}

	off := len(a.current)
	a.current = a.current[:off+size]
	a.remaining -= size
	return a.current[off : off+size : off+size]
}

func (a *bocPayloadArena) grow(size int) {
	chunkSize := a.nextChunkSize
	if chunkSize < size {
		chunkSize = size
	}
	if chunkSize > a.remaining {
		chunkSize = a.remaining
	}
	if chunkSize < size {
		chunkSize = size
	}

	a.current = make([]byte, 0, chunkSize)

	nextChunkSize := chunkSize * 2
	if nextChunkSize < bocPayloadArenaMinChunkSize {
		nextChunkSize = bocPayloadArenaMinChunkSize
	}
	if nextChunkSize > bocPayloadArenaMaxChunkSize {
		nextChunkSize = bocPayloadArenaMaxChunkSize
	}
	a.nextChunkSize = nextChunkSize
}

func parseCells(rootsIndex []uint32, cellsNum, refSzBytes, dataLen int, r *BOCNoCopyReader, index bocCellIndex, options BOCParseOptions) ([]*Cell, []Cell, error) {
	if options.NoCopyPayload {
		return parseCellsNoCopy(rootsIndex, cellsNum, refSzBytes, dataLen, r, index, options)
	}

	cells := make([]Cell, cellsNum)
	var stored []storedHashesDepths
	var cacheRefs []uint8

	if index.hasCacheBits {
		cacheRefs = make([]uint8, cellsNum)
		for _, idx := range rootsIndex {
			if cacheRefs[idx] < 2 {
				cacheRefs[idx]++
			}
		}
	}

	offset := 0
	readFull := func(dst []byte) error {
		if len(dst) > dataLen-offset {
			return io.ErrUnexpectedEOF
		}
		if err := r.readFull(dst); err != nil {
			return err
		}
		offset += len(dst)
		return nil
	}

	payloadArena := newBOCPayloadArena(dataLen)
	storedArena := newBOCPayloadArena(dataLen)
	indexEnabled := index.enabled()
	for i := 0; i < cellsNum; i++ {
		cellEnd := dataLen
		if indexEnabled {
			cellEnd = index.cellEnd(i)
			if cellEnd < offset || cellEnd > dataLen {
				return nil, nil, errors.New("invalid cell index")
			}
			if cellEnd-offset < 2 {
				return nil, nil, errors.New("invalid cell index")
			}
		} else if dataLen-offset < 2 {
			return nil, nil, errors.New("failed to parse cell header, corrupted data")
		}

		var header [2]byte
		if err := readFull(header[:]); err != nil {
			return nil, nil, fmt.Errorf("failed to read cell header: %w", err)
		}

		// len(self.refs) + self.is_special() * 8 + self.level() * 32
		flags := header[0]
		refsNum := int(flags & 0b111)
		special := (flags & 0b1000) != 0
		withHashes := (flags & 0b10000) != 0
		levelMask := LevelMask{flags >> 5}

		if refsNum > 4 {
			return nil, nil, errors.New("too many refs in cell")
		}

		ln := header[1]
		// round to 1 byte, len in octets
		oneMore := ln % 2
		sz := int(ln/2 + oneMore)

		if withHashes {
			hashesNum := levelMask.getHashesCount()
			hashesSize := hashesNum * hashSize
			depthsSize := hashesNum * depthSize
			if cellEnd-offset < hashesSize+depthsSize {
				return nil, nil, errors.New("failed to parse cell hashes, corrupted data")
			}

			hashesDepths := storedArena.alloc(hashesSize + depthsSize)
			if err := readFull(hashesDepths); err != nil {
				return nil, nil, fmt.Errorf("failed to read cell hashes/depths: %w", err)
			}
			stored = append(stored, storedHashesDepths{
				cellIndex:   i,
				hashes:      hashesDepths[:hashesSize],
				depths:      hashesDepths[hashesSize:],
				hashesCount: uint8(hashesNum),
			})
		}

		if cellEnd-offset < sz {
			return nil, nil, errors.New("failed to parse cell payload, corrupted data")
		}
		payload := payloadArena.alloc(sz)
		if err := readFull(payload); err != nil {
			return nil, nil, fmt.Errorf("failed to read cell payload: %w", err)
		}

		if cellEnd-offset < refsNum*refSzBytes {
			return nil, nil, errors.New("failed to parse cell refs, corrupted data")
		}
		refsSize := refsNum * refSzBytes
		if refsSize > 0 {
			var refBuf [4 * 4]byte
			if err := readFull(refBuf[:refsSize]); err != nil {
				return nil, nil, fmt.Errorf("failed to read cell refs: %w", err)
			}
			var refIDs [4]int
			switch refSzBytes {
			case 1:
				for y := 0; y < refsNum; y++ {
					refIDs[y] = int(refBuf[y])
				}
			case 2:
				for y := 0; y < refsNum; y++ {
					off := y * 2
					refIDs[y] = int(binary.BigEndian.Uint16(refBuf[off:]))
				}
			case 3:
				for y := 0; y < refsNum; y++ {
					off := y * 3
					refIDs[y] = int(refBuf[off])<<16 | int(refBuf[off+1])<<8 | int(refBuf[off+2])
				}
			case 4:
				for y := 0; y < refsNum; y++ {
					off := y * 4
					refIDs[y] = int(binary.BigEndian.Uint32(refBuf[off:]))
				}
			default:
				return nil, nil, errors.New("invalid boc size descriptor")
			}
			for y := 0; y < refsNum; y++ {
				id := refIDs[y]

				if i == id {
					return nil, nil, errors.New("recursive reference of cells")
				}
				if id < i {
					return nil, nil, errors.New("reference to index which is behind parent cell")
				}
				if id < 0 || id >= len(cells) {
					return nil, nil, errors.New("invalid index, out of scope")
				}

				cells[i].refs[y] = &cells[id]
				if cacheRefs != nil && cacheRefs[id] < 2 {
					cacheRefs[id]++
				}
			}
		}

		bitsSz := sz * 8
		if int(ln)%2 != 0 {
			last := payload[len(payload)-1]
			terminatorBit := -1
			for y := 0; y < 7; y++ {
				if (last>>y)&1 == 1 {
					terminatorBit = y
					break
				}
			}
			if terminatorBit < 0 {
				return nil, nil, errors.New("overlong cell bits encoding")
			}
			bitsSz = (sz-1)*8 + 7 - terminatorBit
		}
		bodyBytes := (bitsSz + 7) / 8

		cellFlags := uint8(refsNum) << cellFlagRefsNumShift
		cellFlags |= (levelMask.Mask & 0b111) << cellFlagLevelMaskShift
		if special {
			cellFlags |= cellFlagSpecial
		}
		cells[i].flags = cellFlags
		cells[i].bitsSz = uint16(bitsSz)
		cells[i].data = payload[:bodyBytes:bodyBytes]

		if indexEnabled && offset != cellEnd {
			return nil, nil, errors.New("invalid indexed cell boundary")
		}
	}

	if offset != dataLen {
		if indexEnabled {
			return nil, nil, errors.New("invalid cell index")
		}
		return nil, nil, errors.New("failed to parse cells payload, corrupted data")
	}
	if cacheRefs != nil {
		for i := 0; i < cellsNum; i++ {
			if shouldCache := cacheRefs[i] > 1; shouldCache != index.cacheBit(i) {
				return nil, nil, fmt.Errorf("invalid cache flag for cell #%d", i)
			}
		}
	}

	return finalizeParsedCells(cells, rootsIndex, stored, options)
}

func parseCellsNoCopy(rootsIndex []uint32, cellsNum, refSzBytes, dataLen int, r *BOCNoCopyReader, index bocCellIndex, options BOCParseOptions) ([]*Cell, []Cell, error) {
	payload, err := r.readBytes(dataLen)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read payload, want %d: %w", dataLen, err)
	}

	cells := make([]Cell, cellsNum)
	var stored []storedHashesDepths
	var cacheRefs []uint8

	if index.hasCacheBits {
		cacheRefs = make([]uint8, cellsNum)
		for _, idx := range rootsIndex {
			if cacheRefs[idx] < 2 {
				cacheRefs[idx]++
			}
		}
	}

	offset := 0
	indexEnabled := index.enabled()
	for i := range cells {
		cellEnd := dataLen
		if indexEnabled {
			cellEnd = index.cellEnd(i)
			if cellEnd < offset || cellEnd > dataLen {
				return nil, nil, errors.New("invalid cell index")
			}
		}

		info, nextOffset, err := parseBOCPayloadCellInfo(payload, offset, cellEnd, refSzBytes, indexEnabled)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid cell #%d: %w", i, err)
		}

		refsNum := info.refsCount()
		levelMask := info.levelMask()
		if info.withHashes() {
			hashesCount := levelMask.getHashesCount()
			stored = append(stored, storedHashesDepths{
				cellIndex:   i,
				hashes:      info.hashes(payload),
				depths:      info.depths(payload),
				hashesCount: uint8(hashesCount),
			})
		}

		body := info.body(payload)
		bitsSz, err := cellBodyBitsSize(info.dsc2, body)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid cell #%d: %w", i, err)
		}

		cellFlags := uint8(refsNum) << cellFlagRefsNumShift
		cellFlags |= (levelMask.Mask & 0b111) << cellFlagLevelMaskShift
		if info.dsc1&0b1000 != 0 {
			cellFlags |= cellFlagSpecial
		}
		cells[i].flags = cellFlags
		cells[i].bitsSz = bitsSz
		cells[i].data = body

		for ref := 0; ref < refsNum; ref++ {
			id := info.refIndex(payload, ref, refSzBytes)
			if id == i {
				return nil, nil, errors.New("recursive reference of cells")
			}
			if id < i {
				return nil, nil, errors.New("reference to index which is behind parent cell")
			}
			if id < 0 || id >= len(cells) {
				return nil, nil, errors.New("invalid index, out of scope")
			}

			cells[i].refs[ref] = &cells[id]
			if cacheRefs != nil && cacheRefs[id] < 2 {
				cacheRefs[id]++
			}
		}

		offset = nextOffset
	}

	if offset != dataLen {
		if indexEnabled {
			return nil, nil, errors.New("invalid cell index")
		}
		return nil, nil, errors.New("failed to parse cells payload, corrupted data")
	}
	if cacheRefs != nil {
		for i := range cells {
			if shouldCache := cacheRefs[i] > 1; shouldCache != index.cacheBit(i) {
				return nil, nil, fmt.Errorf("invalid cache flag for cell #%d", i)
			}
		}
	}

	return finalizeParsedCells(cells, rootsIndex, stored, options)
}

func finalizeParsedCells(cells []Cell, rootsIndex []uint32, stored []storedHashesDepths, options BOCParseOptions) ([]*Cell, []Cell, error) {
	roots := make([]*Cell, len(rootsIndex))

	storedIdx := len(stored) - 1
	for idx := len(cells) - 1; idx >= 0; idx-- {
		var storedMeta storedHashesDepths
		hasStoredHashes := false
		if storedIdx >= 0 && stored[storedIdx].cellIndex == idx {
			storedMeta = stored[storedIdx]
			hasStoredHashes = true
			storedIdx--
		}

		if options.TrustedHashes && hasStoredHashes {
			if err := applyTrustedStoredHashesDepths(&cells[idx], storedMeta); err != nil {
				return nil, nil, fmt.Errorf("invalid cell #%d: %w", idx, err)
			}
		} else {
			if err := cells[idx].calculateHashes(); err != nil {
				return nil, nil, fmt.Errorf("invalid cell #%d: %w", idx, err)
			}
			if hasStoredHashes {
				if err := validateStoredHashesDepths(&cells[idx], storedMeta); err != nil {
					return nil, nil, fmt.Errorf("invalid cell #%d: %w", idx, err)
				}
			}
		}
		if cells[idx].IsSpecial() {
			if err := validateLoadedCell(&cells[idx]); err != nil {
				return nil, nil, fmt.Errorf("invalid cell #%d: %w", idx, err)
			}
		}
	}

	for i, idx := range rootsIndex {
		roots[i] = &cells[idx]
	}

	return roots, cells, nil
}

func applyTrustedStoredHashesDepths(c *Cell, stored storedHashesDepths) error {
	levelMask := c.LevelMask()
	expected := levelMask.getHashesCount()
	if int(stored.hashesCount) != expected {
		return errors.New("invalid serialized hashes/depth metadata")
	}
	if len(stored.hashes) != expected*hashSize || len(stored.depths) != expected*depthSize {
		return errors.New("invalid serialized hashes/depth metadata")
	}

	if c.GetType() == PrunedCellType {
		hashOff := (expected - 1) * hashSize
		depthOff := (expected - 1) * depthSize
		c.setHashAt(0, stored.hashes[hashOff:hashOff+hashSize])
		c.setDepthAt(0, binary.BigEndian.Uint16(stored.depths[depthOff:depthOff+depthSize]))
		return nil
	}

	for i := 0; i < expected; i++ {
		hashOff := i * hashSize
		depthOff := i * depthSize
		c.setHashAt(i, stored.hashes[hashOff:hashOff+hashSize])
		c.setDepthAt(i, binary.BigEndian.Uint16(stored.depths[depthOff:depthOff+depthSize]))
	}
	return nil
}

type storedHashesDepths struct {
	cellIndex   int
	hashes      []byte
	depths      []byte
	hashesCount uint8
}

func validateStoredHashesDepths(c *Cell, stored storedHashesDepths) error {
	levelMask := c.LevelMask()
	expected := levelMask.getHashesCount()
	if int(stored.hashesCount) != expected {
		return errors.New("invalid serialized hashes/depth metadata")
	}
	if len(stored.hashes) != expected*hashSize || len(stored.depths) != expected*depthSize {
		return errors.New("invalid serialized hashes/depth metadata")
	}

	idx := 0
	for level := 0; level <= levelMask.GetLevel(); level++ {
		if !levelMask.IsSignificant(level) {
			continue
		}
		hashOff := idx * hashSize
		if !bytes.Equal(stored.hashes[hashOff:hashOff+hashSize], c.getHash(level)) {
			return fmt.Errorf("serialized hash mismatch at level %d", level)
		}
		depthOff := idx * depthSize
		if binary.BigEndian.Uint16(stored.depths[depthOff:depthOff+depthSize]) != c.getDepth(level) {
			return fmt.Errorf("serialized depth mismatch at level %d", level)
		}
		idx++
	}

	return nil
}

func parseBOCFlags(data byte) (bocFlags, int) {
	return bocFlags{
		hasIndex:     data&(1<<7) > 0,
		HasCrc32c:    data&(1<<6) > 0,
		hasCacheBits: data&(1<<5) > 0,
	}, int(data & 0b00000111)
}

func dynInt(data []byte) int {
	switch len(data) {
	case 0:
		return 0
	case 1:
		return int(data[0])
	case 2:
		return int(binary.BigEndian.Uint16(data))
	case 3:
		return int(data[0])<<16 | int(data[1])<<8 | int(data[2])
	case 4:
		return int(binary.BigEndian.Uint32(data))
	}

	var v uint64
	for _, b := range data {
		v = (v << 8) | uint64(b)
	}
	return int(v)
}

func matchBOCMagic(magic, expected []byte) bool {
	return magic[0] == expected[0] && magic[1] == expected[1] && magic[2] == expected[2] && magic[3] == expected[3]
}
