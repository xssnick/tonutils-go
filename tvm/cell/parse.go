package cell

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

const hashSize = 32
const depthSize = 2

var castTable = crc32.MakeTable(crc32.Castagnoli)

var bocMagic = []byte{0xB5, 0xEE, 0x9C, 0x72}

// MaxBOCRoots limits how many roots may be decoded from a single BOC payload,
// including the improved compression/decompression path.
// Set it to 0 or a negative value to disable the limit.
var MaxBOCRoots = 16384

// MaxBOCCells limits how many cells may be decoded from a single BOC payload,
// including the improved compression/decompression path.
// Set it to 0 or a negative value to disable the limit.
var MaxBOCCells = 1 << 20

type bocFlags struct {
	hasIndex     bool
	HasCrc32c    bool
	hasCacheBits bool
}

func FromBOC(data []byte) (*Cell, error) {
	cells, err := FromBOCMultiRoot(data)
	if err != nil {
		return nil, err
	}

	return cells[0], nil
}

func FromBOCMultiRoot(data []byte) ([]*Cell, error) {
	if len(data) < 10 {
		return nil, errors.New("invalid boc")
	}

	r := newReader(data)
	readDyn := func(size int, name string) (int, error) {
		b, err := r.ReadBytes(size)
		if err != nil {
			return 0, fmt.Errorf("failed to read %s: %w", name, err)
		}
		return dynInt(b), nil
	}

	magic := r.MustReadBytes(4)
	if !matchBOCMagic(magic, bocMagic) {
		return nil, errors.New("invalid boc magic header")
	}

	flags, cellNumSizeBytes := parseBOCFlags(r.MustReadByte()) // has_idx:(## 1) has_crc32c:(## 1)  has_cache_bits:(## 1) flags:(## 2) { flags = 0 } size:(## 3) { size <= 4 }
	if cellNumSizeBytes < 1 || cellNumSizeBytes > 4 {
		return nil, errors.New("invalid boc size descriptor")
	}

	dataSizeBytes := int(r.MustReadByte()) // off_bytes:(## 8) { off_bytes <= 8 }
	if dataSizeBytes < 1 || dataSizeBytes > 8 {
		return nil, errors.New("invalid boc offset descriptor")
	}

	cellsNum, err := readDyn(cellNumSizeBytes, "cells count") // cells:(##(size * 8))
	if err != nil {
		return nil, err
	}
	rootsNum, err := readDyn(cellNumSizeBytes, "roots count") // roots:(##(size * 8)) { roots >= 1 }
	if err != nil {
		return nil, err
	}
	if cellsNum <= 0 || rootsNum <= 0 {
		return nil, errors.New("invalid boc counters")
	}
	if MaxBOCCells > 0 && cellsNum > MaxBOCCells {
		return nil, fmt.Errorf("too many cells in boc: %d > %d", cellsNum, MaxBOCCells)
	}
	if MaxBOCRoots > 0 && rootsNum > MaxBOCRoots {
		return nil, fmt.Errorf("too many roots in boc: %d > %d", rootsNum, MaxBOCRoots)
	}

	// complete BOCs - ??? (absent:(##(size * 8)) { roots + absent <= cells })
	absentNum, err := readDyn(cellNumSizeBytes, "absent count")
	if err != nil {
		return nil, err
	}
	if absentNum < 0 || absentNum > cellsNum || rootsNum+absentNum > cellsNum {
		return nil, errors.New("invalid boc counters")
	}

	dataLen, err := readDyn(dataSizeBytes, "cells data size") // tot_cells_size:(##(off_bytes * 8))
	if err != nil {
		return nil, err
	}
	if dataLen < cellsNum*2 {
		return nil, errors.New("invalid boc cells data size")
	}

	// with checksum
	if flags.HasCrc32c {
		crc := crc32.Checksum(data[:len(data)-4], castTable)
		if binary.LittleEndian.Uint32(data[len(data)-4:]) != crc {
			return nil, errors.New("checksum not matches")
		}
	}

	if flags.hasCacheBits && !flags.hasIndex {
		return nil, fmt.Errorf("cache flag cant be set without index flag")
	}

	rootsIndex := make([]int, rootsNum)
	for i := 0; i < rootsNum; i++ {
		rootsIndex[i], err = readDyn(cellNumSizeBytes, "root index")
		if err != nil {
			return nil, err
		}
		if rootsIndex[i] < 0 || rootsIndex[i] >= cellsNum {
			return nil, errors.New("invalid root index")
		}
	}

	var index []int
	if flags.hasIndex {
		index = make([]int, cellsNum)
		idxData, err := r.ReadBytes(cellsNum * dataSizeBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to read custom index, err: %v", err)
		}

		for i := 0; i < cellsNum; i++ {
			off := i * dataSizeBytes
			val := dynInt(idxData[off : off+dataSizeBytes])
			if flags.hasCacheBits {
				// we don't need a cache, cause our loader uses memory
				val /= 2
			}
			index[i] = val
		}
	}

	if cellsNum > dataLen/2 {
		return nil, fmt.Errorf("cells num looks malicious: data len %d, cells %d", dataLen, cellsNum)
	}

	payload, err := r.ReadBytes(dataLen)
	if err != nil {
		return nil, fmt.Errorf("failed to read payload, want %d, has %d", dataLen, r.LeftLen())
	}
	expectedTrailer := 0
	if flags.HasCrc32c {
		expectedTrailer = 4
	}
	if left := r.LeftLen(); left != expectedTrailer {
		if left > expectedTrailer {
			return nil, fmt.Errorf("unexpected trailing data after boc payload: %d bytes", left-expectedTrailer)
		}
		return nil, fmt.Errorf("invalid boc trailer size, want %d bytes, got %d", expectedTrailer, left)
	}

	cll, err := parseCells(rootsIndex, cellsNum, cellNumSizeBytes, payload, index)
	if err != nil {
		return nil, fmt.Errorf("failed to parse payload: %w", err)
	}

	return cll, nil
}

func parseCells(rootsIndex []int, cellsNum, refSzBytes int, data []byte, index []int) ([]*Cell, error) {
	cells := make([]Cell, cellsNum)
	refIndex := make([][]int, cellsNum)
	storedHashes := make([][][]byte, cellsNum)
	storedDepths := make([][]uint16, cellsNum)

	if index != nil {
		prevEnd := 0
		for i, end := range index {
			if end < prevEnd || end > len(data) {
				return nil, errors.New("invalid cell index")
			}
			if i == len(index)-1 && end != len(data) {
				return nil, errors.New("invalid cell index")
			}
			prevEnd = end
		}
	}

	offset := 0
	for i := 0; i < cellsNum; i++ {
		cellEnd := len(data)

		if index != nil {
			offset = 0
			if i > 0 {
				offset = index[i-1]
			}
			cellEnd = index[i]
		}

		if cellEnd-offset < 2 {
			return nil, errors.New("failed to parse cell header, corrupted data")
		}

		// len(self.refs) + self.is_special() * 8 + self.level() * 32
		flags := data[offset]
		refsNum := int(flags & 0b111)
		special := (flags & 0b1000) != 0
		withHashes := (flags & 0b10000) != 0
		levelMask := LevelMask{flags >> 5}

		if refsNum > 4 {
			return nil, errors.New("too many refs in cell")
		}

		ln := data[offset+1]
		// round to 1 byte, len in octets
		oneMore := ln % 2
		sz := int(ln/2 + oneMore)

		offset += 2
		if cellEnd-offset < sz {
			return nil, errors.New("failed to parse cell payload, corrupted data")
		}

		if withHashes {
			hashesNum := levelMask.getHashesCount()
			hashesSize := hashesNum*hashSize + hashesNum*depthSize
			if cellEnd-offset < hashesSize {
				return nil, errors.New("failed to parse cell hashes, corrupted data")
			}
			storedHashes[i] = make([][]byte, 0, hashesNum)
			for level := 0; level <= levelMask.GetLevel(); level++ {
				if !levelMask.IsSignificant(level) {
					continue
				}
				storedHashes[i] = append(storedHashes[i], append([]byte{}, data[offset:offset+hashSize]...))
				offset += hashSize
			}
			storedDepths[i] = make([]uint16, 0, hashesNum)
			for level := 0; level <= levelMask.GetLevel(); level++ {
				if !levelMask.IsSignificant(level) {
					continue
				}
				storedDepths[i] = append(storedDepths[i], binary.BigEndian.Uint16(data[offset:offset+depthSize]))
				offset += depthSize
			}
		}

		payload := data[offset : offset+sz]

		offset += sz
		if cellEnd-offset < refsNum*refSzBytes {
			return nil, errors.New("failed to parse cell refs, corrupted data")
		}

		if refsNum > 0 {
			refIndex[i] = make([]int, 0, refsNum)
		}
		for y := 0; y < refsNum; y++ {
			id := dynInt(data[offset : offset+refSzBytes])
			offset += refSzBytes

			if i == id {
				return nil, errors.New("recursive reference of cells")
			}
			if id < i && index == nil { // compatibility with c++ implementation
				return nil, errors.New("reference to index which is behind parent cell")
			}
			if id < 0 || id >= len(cells) {
				return nil, errors.New("invalid index, out of scope")
			}

			refIndex[i] = append(refIndex[i], id)
			cells[i].setRef(y, &cells[id])
		}
		cells[i].setRefsCount(refsNum)

		bitsSz := int(ln) * 4

		// if not full byte
		if int(ln)%2 != 0 {
			// find last bit of byte which indicates the end and cut it and next
			for y := 0; y < 8; y++ {
				if (payload[len(payload)-1]>>y)&1 == 1 {
					bitsSz += 3 - y
					break
				}
			}
		}
		bodyBytes := (bitsSz + 7) / 8

		cells[i].setSpecial(special)
		cells[i].bitsSz = uint16(bitsSz)
		cells[i].setLevelMask(levelMask)
		cells[i].data = payload[:bodyBytes]

		if index != nil && offset != cellEnd {
			return nil, errors.New("invalid indexed cell boundary")
		}
	}

	if index == nil && offset != len(data) {
		return nil, errors.New("failed to parse cells payload, corrupted data")
	}

	order, err := topologicalCellOrder(refIndex)
	if err != nil {
		return nil, err
	}

	roots := make([]*Cell, len(rootsIndex))

	for _, idx := range order {
		if err := cells[idx].calculateHashesSafe(); err != nil {
			return nil, fmt.Errorf("invalid cell #%d: %w", idx, err)
		}
		if err := validateStoredHashesDepths(&cells[idx], storedHashes[idx], storedDepths[idx]); err != nil {
			return nil, fmt.Errorf("invalid cell #%d: %w", idx, err)
		}
		if err := validateLoadedCell(&cells[idx]); err != nil {
			return nil, fmt.Errorf("invalid cell #%d: %w", idx, err)
		}
	}

	for i, idx := range rootsIndex {
		roots[i] = &cells[idx]
	}

	return roots, nil
}

func validateStoredHashesDepths(c *Cell, storedHashes [][]byte, storedDepths []uint16) error {
	if len(storedHashes) == 0 && len(storedDepths) == 0 {
		return nil
	}

	levelMask := c.LevelMask()
	expected := levelMask.getHashesCount()
	if len(storedHashes) != expected || len(storedDepths) != expected {
		return errors.New("invalid serialized hashes/depth metadata")
	}

	idx := 0
	for level := 0; level <= levelMask.GetLevel(); level++ {
		if !levelMask.IsSignificant(level) {
			continue
		}
		if !bytes.Equal(storedHashes[idx], c.Hash(level)) {
			return fmt.Errorf("serialized hash mismatch at level %d", level)
		}
		if storedDepths[idx] != c.Depth(level) {
			return fmt.Errorf("serialized depth mismatch at level %d", level)
		}
		idx++
	}

	return nil
}

func topologicalCellOrder(refIndex [][]int) ([]int, error) {
	type frame struct {
		idx  int
		next int
	}

	state := make([]byte, len(refIndex))
	order := make([]int, 0, len(refIndex))

	for start := range refIndex {
		if state[start] != 0 {
			continue
		}

		state[start] = 1
		stack := []frame{{idx: start}}
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.next >= len(refIndex[top.idx]) {
				state[top.idx] = 2
				order = append(order, top.idx)
				stack = stack[:len(stack)-1]
				continue
			}

			child := refIndex[top.idx][top.next]
			top.next++
			switch state[child] {
			case 0:
				state[child] = 1
				stack = append(stack, frame{idx: child})
			case 1:
				return nil, errors.New("recursive reference of cells")
			}
		}
	}

	return order, nil
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
