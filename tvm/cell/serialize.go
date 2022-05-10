package cell

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"hash/crc64"
)

var ErrTooBigValue = errors.New("too big value")
var ErrSmallSlice = errors.New("too small slice for this size")
var ErrTooBigSize = errors.New("too big size")
var ErrTooMuchRefs = errors.New("too much refs")
var ErNotFit1024 = errors.New("cell data size should fit into 1024 bits")
var ErrNoMoreRefs = errors.New("no more refs exists")

var magic = []byte{0xB5, 0xEE, 0x9C, 0x72}

func (c *Cell) ToBOC() []byte {
	return c.ToBOCWithFlags(true)
}

func (c *Cell) ToBOCWithFlags(withCRC bool) []byte {
	var index int
	hashIndex := map[uint64]int{}
	var orderCells []*Cell

	// recursively go through cells, build hash index and store unique in slice
	var indexCells func(cl *Cell)
	indexCells = func(cl *Cell) {
		h := cl.hash()

		id, ok := hashIndex[h]
		if !ok {
			id = index
			index++

			hashIndex[h] = id
			orderCells = append(orderCells, cl)
		}
		cl.index = id

		for i := range cl.refs {
			indexCells(cl.refs[i])
		}
	}
	indexCells(c)

	var payload []byte
	for i := 0; i < len(orderCells); i++ {
		// serialize each cell
		payload = append(payload, orderCells[i].serialize()...)
	}

	// bytes needed to store len of payload
	sizeBytes := byte(len(payload) / 256)
	if sizeBytes == 0 || len(payload)%256 != 0 {
		sizeBytes++
	}

	// bytes needed to store num of cells
	cellSizeBytes := byte(len(orderCells) / 256)
	if len(orderCells)%256 != 0 {
		cellSizeBytes++
	}

	// has_idx 1bit, hash_crc32 1bit,  has_cache_bits 1bit, flags 2bit, size_bytes 3 bit
	flags := byte(0b0_0_0_00_000)
	if withCRC {
		flags |= 0b0_1_0_00_000
	}

	flags |= cellSizeBytes

	var data []byte

	data = append(data, magic...)
	data = append(data, flags)

	// bytes needed to store size
	data = append(data, sizeBytes)

	// cells num
	data = append(data, dynamicIntBytes(uint64(calcCells(c)), int(sizeBytes))...)

	// roots num (only 1 supported for now)
	data = append(data, 1)

	// complete BOCs = 0
	data = append(data, 0)

	// len of data
	data = append(data, dynamicIntBytes(uint64(len(payload)), int(sizeBytes))...)

	// root should have index 0
	data = append(data, 0)
	data = append(data, payload...)

	if withCRC {
		checksum := make([]byte, 4)
		binary.LittleEndian.PutUint32(checksum, crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli)))

		data = append(data, checksum...)
	}

	return data
}

func calcCells(cell *Cell) int {
	m := map[uint64]*Cell{}
	// calc unique cells
	uniqCells(m, cell)

	return len(m)
}

func uniqCells(m map[uint64]*Cell, cell *Cell) {
	m[cell.hash()] = cell

	for _, ref := range cell.refs {
		uniqCells(m, ref)
	}
}

func (c *Cell) hash() uint64 {
	var refsHashes = make([]byte, 8*len(c.refs)+len(c.data))
	for _, ref := range c.refs {
		var hData = make([]byte, 8)
		binary.BigEndian.PutUint64(hData, ref.hash())

		refsHashes = append(refsHashes, hData...)
	}
	return crc64.Checksum(append(refsHashes, c.data...), crc64.MakeTable(crc64.ECMA))
}

func (c *Cell) serialize() []byte {
	unusedBits := 8 - (c.bitsSz % 8)
	if unusedBits != 8 {
		// we need to set bit at the end if not whole byte was used
		c.data[len(c.data)-1] += 1 << (unusedBits - 1)
	}

	ceilBytes := c.bitsSz / 8
	if c.bitsSz%8 != 0 {
		ceilBytes++
	}

	// calc size
	ln := ceilBytes + c.bitsSz/8

	data := append([]byte{byte(len(c.refs)), byte(ln)}, c.data...)
	for _, ref := range c.refs {
		data = append(data, byte(ref.index))
	}

	return data
}

func dynamicIntBytes(val uint64, sz int) []byte {
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, val)

	return data[8-sz:]
}
