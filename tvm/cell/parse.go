package cell

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

func dynInt(data []byte) int {
	tmp := make([]byte, 8)
	copy(tmp[8-len(data):], data)

	return int(binary.BigEndian.Uint64(tmp))
}

func FromBOC(data []byte) (Cell, error) {
	cells, err := fromBOCMultiRoot(data)
	if err != nil {
		return Cell{}, err
	}

	return cells[0], nil
}

func fromBOCMultiRoot(data []byte) ([]Cell, error) {
	if len(data) < 10 {
		return nil, errors.New("invalid boc")
	}

	r := newReader(data)

	if !bytes.Equal(r.MustReadBytes(4), magic) {
		return nil, errors.New("invalid boc magic header")
	}

	flags := r.MustReadByte()

	cellNumSizeBytes := int(flags & 0b111)
	dataSizeBytes := int(r.MustReadByte())

	cellsNum := dynInt(r.MustReadBytes(cellNumSizeBytes))
	rootsNum := int(r.MustReadByte())

	// complete BOCs - ???
	_ = r.MustReadByte()

	dataLen := dynInt(r.MustReadBytes(dataSizeBytes))

	// with checksum
	if flags&0b01000000 != 0 {
		crc := crc32.Checksum(data[:len(data)-4], crc32.MakeTable(crc32.Castagnoli))
		if binary.LittleEndian.Uint32(data[len(data)-4:]) != crc {
			return nil, errors.New("checksum not matches")
		}
	}

	rootIndex := r.MustReadByte()
	if rootIndex != 0 {
		return nil, fmt.Errorf("first root index should be 0, but it is %d", rootIndex)
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

		cells[i] = Cell{
			bitsSz: int(ln) * 4,
			data:   payload,
			refs:   refs,
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
