package cell

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
)

const hashSize = 32
const depthSize = 2

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

	if !bytes.Equal(r.MustReadBytes(4), bocMagic) {
		return nil, errors.New("invalid boc magic header")
	}

	flags, cellNumSizeBytes := parseBOCFlags(r.MustReadByte()) // has_idx:(## 1) has_crc32c:(## 1)  has_cache_bits:(## 1) flags:(## 2) { flags = 0 } size:(## 3) { size <= 4 }
	dataSizeBytes := int(r.MustReadByte())                     // off_bytes:(## 8) { off_bytes <= 8 }

	cellsNum := dynInt(r.MustReadBytes(cellNumSizeBytes)) // cells:(##(size * 8))
	rootsNum := dynInt(r.MustReadBytes(cellNumSizeBytes)) // roots:(##(size * 8)) { roots >= 1 }

	// complete BOCs - ??? (absent:(##(size * 8)) { roots + absent <= cells })
	_ = r.MustReadBytes(cellNumSizeBytes)

	dataLen := dynInt(r.MustReadBytes(dataSizeBytes)) // tot_cells_size:(##(off_bytes * 8))

	// with checksum
	if flags.HasCrc32c {
		crc := crc32.Checksum(data[:len(data)-4], crc32.MakeTable(crc32.Castagnoli))
		if binary.LittleEndian.Uint32(data[len(data)-4:]) != crc {
			return nil, errors.New("checksum not matches")
		}
	}

	rootList := r.MustReadBytes(rootsNum * cellNumSizeBytes) // root_list:(roots * ##(size * 8))
	rootIndex := dynInt(rootList[0:cellNumSizeBytes])
	_ = rootIndex

	if flags.hasCacheBits && !flags.hasIndex {
		return nil, fmt.Errorf("cache flag cant be set without index flag")
	}

	var index []int
	if flags.hasIndex {
		index = make([]int, 0, cellsNum)
		idxData, err := r.ReadBytes(cellsNum * dataSizeBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to read custom index, err: %v", err)
		}
		n := 0
		for i := 0; i < cellsNum; i++ {
			off := i * dataSizeBytes
			val := dynInt(idxData[off : off+dataSizeBytes])
			if flags.hasCacheBits {
				// TODO: check caches
				if val%2 == 1 {
					n++
				}
				val /= 2
			}
			index = append(index, val)
		}
	}

	payload, err := r.ReadBytes(dataLen)
	if err != nil {
		return nil, fmt.Errorf("failed to read paylooad, want %d, has %d", dataLen, r.LeftLen())
	}

	cll, err := parseCells(rootsNum, cellsNum, cellNumSizeBytes, payload, index)
	if err != nil {
		return nil, fmt.Errorf("failed to parse payload: %w", err)
	}

	return cll, nil
}

func parseCells(rootsNum, cellsNum, refSzBytes int, data []byte, index []int) ([]*Cell, error) {
	cells := make([]Cell, cellsNum)
	referred := make([]bool, cellsNum)

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
			maskBits := int(math.Ceil(math.Log2(float64(levelMask.Mask) + 1)))
			hashesNum := maskBits + 1

			offset += hashesNum*hashSize + hashesNum*depthSize
			// TODO: check depth and hashes
		}

		payload := data[offset : offset+sz]

		offset += sz
		if len(data)-offset < refsNum*refSzBytes {
			return nil, errors.New("failed to parse cell refs, corrupted data")
		}

		refsIndex := make([]int, 0, refsNum)
		for y := 0; y < refsNum; y++ {
			refIndex := data[offset : offset+refSzBytes]

			refsIndex = append(refsIndex, dynInt(refIndex))
			offset += refSzBytes
		}

		refs := make([]*Cell, len(refsIndex))
		for y, id := range refsIndex {
			if i == id {
				return nil, errors.New("recursive reference of cells")
			}
			if id < i && index == nil { // compatibility with c++ implementation
				return nil, errors.New("reference to index which is behind parent cell")
			}
			if id >= len(cells) {
				return nil, errors.New("invalid index, out of scope")
			}

			refs[y] = &cells[id]

			referred[id] = true
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

	roots := make([]*Cell, 0, rootsNum)

	for i := len(cells) - 1; i >= 0; i-- {
		cells[i].calculateHashes()
	}

	// get cells which are not referenced by another, its root cells
	for y, isRef := range referred {
		if !isRef {
			roots = append(roots, &cells[y])
		}
	}

	if len(roots) != rootsNum {
		return nil, errors.New("roots num not match actual num")
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
	tmp := make([]byte, 8)
	copy(tmp[8-len(data):], data)

	return int(binary.BigEndian.Uint64(tmp))
}
