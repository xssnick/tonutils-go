package cell

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

const hashSize = 32
const depthSize = 2

var castTable = crc32.MakeTable(crc32.Castagnoli)

var bocMagic = []byte{0xB5, 0xEE, 0x9C, 0x72}

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

	cll, err := parseCells(rootsIndex, cellsNum, cellNumSizeBytes, payload, index)
	if err != nil {
		return nil, fmt.Errorf("failed to parse payload: %w", err)
	}

	return cll, nil
}

func parseCells(rootsIndex []int, cellsNum, refSzBytes int, data []byte, index []int) ([]*Cell, error) {
	cells := make([]*Cell, cellsNum)
	for i := 0; i < cellsNum; i++ {
		// initialize them one by one for flexible gc and memory usage
		cells[i] = &Cell{}
	}

	// index = nil
	offset := 0
	for i := 0; i < cellsNum; i++ {
		if len(data)-offset < 2 {
			return nil, errors.New("failed to parse cell header, corrupted data")
		}

		if index != nil {
			// if we have index, then set offset from it, it stores end of each cell
			offset = 0
			if i > 0 {
				offset = index[i-1]
			}
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
		if len(data)-offset < sz {
			return nil, errors.New("failed to parse cell payload, corrupted data")
		}

		if withHashes {
			hashesNum := levelMask.getHashesCount()
			hashesSize := hashesNum*hashSize + hashesNum*depthSize
			if len(data)-offset < hashesSize {
				return nil, errors.New("failed to parse cell hashes, corrupted data")
			}

			offset += hashesSize
			// TODO: check depth and hashes
		}

		payload := data[offset : offset+sz]

		offset += sz
		if len(data)-offset < refsNum*refSzBytes {
			return nil, errors.New("failed to parse cell refs, corrupted data")
		}

		refs := make([]*Cell, refsNum)
		for y := 0; y < refsNum; y++ {
			id := dynInt(data[offset : offset+refSzBytes])
			offset += refSzBytes

			if i == id {
				return nil, errors.New("recursive reference of cells")
			}
			if id < i && index == nil { // compatibility with c++ implementation
				return nil, errors.New("reference to index which is behind parent cell")
			}
			if id >= len(cells) {
				return nil, errors.New("invalid index, out of scope")
			}

			refs[y] = cells[id]
		}

		bitsSz := uint(int(ln) * 4)

		// if not full byte
		if int(ln)%2 != 0 {
			// find last bit of byte which indicates the end and cut it and next
			for y := uint(0); y < 8; y++ {
				if (payload[len(payload)-1]>>y)&1 == 1 {
					bitsSz += 3 - y
					break
				}
			}
		}

		cells[i].special = special
		cells[i].bitsSz = bitsSz
		cells[i].levelMask = levelMask
		cells[i].data = payload
		cells[i].refs = refs
	}

	roots := make([]*Cell, len(rootsIndex))

	for i := len(cells) - 1; i >= 0; i-- {
		cells[i].calculateHashes()
		if err := validateLoadedCell(cells[i]); err != nil {
			return nil, fmt.Errorf("invalid cell #%d: %w", i, err)
		}
	}

	for i, idx := range rootsIndex {
		roots[i] = cells[idx]
	}

	return roots, nil
}

func parseBOCFlags(data byte) (bocFlags, int) {
	return bocFlags{
		hasIndex:     data&(1<<7) > 0,
		HasCrc32c:    data&(1<<6) > 0,
		hasCacheBits: data&(1<<5) > 0,
	}, int(data & 0b00000111)
}

func dynInt(data []byte) int {
	var v uint64
	for _, b := range data {
		v = (v << 8) | uint64(b)
	}
	return int(v)
}

func matchBOCMagic(magic, expected []byte) bool {
	return magic[0] == expected[0] && magic[1] == expected[1] && magic[2] == expected[2] && magic[3] == expected[3]
}
