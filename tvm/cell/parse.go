package cell

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"

	"github.com/xssnick/tonutils-go/tvm/boc"
)

func dynInt(data []byte) int {
	tmp := make([]byte, 8)
	copy(tmp[8-len(data):], data)

	return int(binary.BigEndian.Uint64(tmp))
}

func FromBOC(data []byte) (*Cell, error) {
	cells, err := FromBOCMultiRoot(data)
	if err != nil {
		return nil, err
	}

	return &cells[0], nil
}

func FromBOCMultiRoot(data []byte) ([]Cell, error) {
	if len(data) < 10 {
		return nil, errors.New("invalid boc")
	}

	r := newReader(data)

	if !bytes.Equal(r.MustReadBytes(4), boc.Magic) {
		return nil, errors.New("invalid boc magic header")
	}

	flags, cellNumSizeBytes := boc.ParseFlags(r.MustReadByte()) // has_idx:(## 1) has_crc32c:(## 1)  has_cache_bits:(## 1) flags:(## 2) { flags = 0 } size:(## 3) { size <= 4 }
	dataSizeBytes := int(r.MustReadByte())                      // off_bytes:(## 8) { off_bytes <= 8 }

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
	if rootIndex != 0 {
		// return nil, fmt.Errorf("first root index should be 0, but it is %d", rootIndex)
	}

	payload, err := r.ReadBytes(dataLen)
	if err != nil {
		return nil, fmt.Errorf("failed to read paylooad, want %d, has %d", dataLen, r.LeftLen())
	}

	cll, err := parseCells(rootsNum, cellsNum, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to parse paylooad: %w", err)
	}

	return cll, nil
}

func parseCells(rootsNum, cellsNum int, data []byte) ([]Cell, error) {
	r := newReader(data)

	cells := make([]Cell, cellsNum)
	referred := make([]bool, cellsNum)

	i := 0
	for i < cellsNum {
		flags, err := r.ReadByte()
		if err != nil {
			return nil, errors.New("failed to parse cell refs num, corrupted data")
		}

		// len(self.refs) + self.is_special() * 8 + self.level() * 32
		// TODO: levels and special support
		refsNum := flags & 0b111
		special := (flags & 0b1000) != 0
		level := flags >> 5

		ln, err := r.ReadByte()
		if err != nil {
			return nil, errors.New("failed to parse cell length, corrupted data")
		}

		// round to 1 byte, len in octets
		oneMore := ln % 2

		payload, err := r.ReadBytes(int(ln/2 + oneMore))
		if err != nil {
			return nil, errors.New("failed to parse cell payload, corrupted data")
		}

		refsIndex, err := r.ReadBytes(int(refsNum))
		if err != nil {
			return nil, errors.New("failed to parse cell references, corrupted data")
		}

		refs := make([]*Cell, len(refsIndex))
		for y, id := range refsIndex {
			refs[y] = &cells[id]
			referred[id] = true
		}

		bitsSz := int(ln) * 4

		// if not full byte
		if int(ln)%2 != 0 {
			lenBytes := int(ln)/2 + 1

			// find last bit of byte which indicates the end and cut it and next
			for y := 0; y < 8; y++ {
				if (payload[lenBytes-1]>>y)&1 == 1 {
					bitsSz += 3 - y
					break
				}
			}
		}

		cells[i] = Cell{
			special: special,
			bitsSz:  bitsSz,
			level:   level,
			data:    payload,
			refs:    refs,
		}

		i++
	}

	var roots []Cell

	// get cells which are not referenced by another, its root cells
	for y, isRef := range referred {
		if !isRef {
			roots = append(roots, cells[y])
		}
	}

	if len(roots) != rootsNum {
		return nil, errors.New("roots num not match actual num")
	}

	return roots, nil
}
