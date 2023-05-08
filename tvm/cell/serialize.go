package cell

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"math"
)

var ErrTooBigValue = errors.New("too big value")
var ErrNegative = errors.New("value should be non negative")
var ErrRefCannotBeNil = errors.New("ref cannot be nil")
var ErrSmallSlice = errors.New("too small slice for this size")
var ErrTooBigSize = errors.New("too big size")
var ErrTooMuchRefs = errors.New("too much refs")
var ErrNotFit1023 = errors.New("cell data size should fit into 1023 bits")
var ErrNoMoreRefs = errors.New("no more refs exists")
var ErrAddressTypeNotSupported = errors.New("address type is not supported")

func (c *Cell) ToBOC() []byte {
	return c.ToBOCWithFlags(true)
}

func (c *Cell) ToBOCWithFlags(withCRC bool) []byte {
	// recursively go through cells, build hash index and store unique in slice
	orderCells := flattenIndex([]*Cell{c})

	// bytes needed to store num of cells
	cellSizeBits := math.Log2(float64(len(orderCells)) + 1)
	cellSizeBytes := byte(math.Ceil(cellSizeBits / 8))

	var payload []byte
	for i := 0; i < len(orderCells); i++ {
		// serialize each cell
		payload = append(payload, orderCells[i].serialize(uint(cellSizeBytes))...)
	}

	// bytes needed to store len of payload
	sizeBits := math.Log2(float64(len(payload)) + 1)
	sizeBytes := byte(math.Ceil(sizeBits / 8))

	// has_idx 1bit, hash_crc32 1bit,  has_cache_bits 1bit, flags 2bit, size_bytes 3 bit
	flags := byte(0b0_0_0_00_000)
	if withCRC {
		flags |= 0b0_1_0_00_000
	}

	flags |= cellSizeBytes

	var data []byte

	data = append(data, bocMagic...)
	data = append(data, flags)

	// bytes needed to store size
	data = append(data, sizeBytes)

	// cells num
	data = append(data, dynamicIntBytes(uint64(len(orderCells)), uint(cellSizeBytes))...)

	// roots num (only 1 supported for now)
	data = append(data, dynamicIntBytes(1, uint(cellSizeBytes))...)

	// complete BOCs = 0
	data = append(data, dynamicIntBytes(0, uint(cellSizeBytes))...)

	// len of data
	data = append(data, dynamicIntBytes(uint64(len(payload)), uint(sizeBytes))...)

	// root should have index 0
	data = append(data, dynamicIntBytes(0, uint(cellSizeBytes))...)
	data = append(data, payload...)

	if withCRC {
		checksum := make([]byte, 4)
		binary.LittleEndian.PutUint32(checksum, crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli)))

		data = append(data, checksum...)
	}

	return data
}

func (c *Cell) serialize(refIndexSzBytes uint) []byte {
	body := c.BeginParse().MustLoadSlice(c.bitsSz)

	data := make([]byte, 2+len(body))
	data[0], data[1] = c.descriptors(c.levelMask)
	copy(data[2:], body)

	unusedBits := 8 - (c.bitsSz % 8)
	if unusedBits != 8 {
		// we need to set bit at the end if not whole byte was used
		data[2+len(body)-1] += 1 << (unusedBits - 1)
	}

	for _, ref := range c.refs {
		data = append(data, dynamicIntBytes(uint64(ref.index), refIndexSzBytes)...)
	}

	return data
}

func (c *Cell) descriptors(lvl LevelMask) (byte, byte) {
	// calc size
	ln := (c.bitsSz / 8) * 2
	if c.bitsSz%8 != 0 {
		ln++
	}

	specBit := byte(0)
	if c.special {
		specBit = 8
	}

	return byte(len(c.refs)) + specBit + lvl.mask*32, byte(ln)
}

func dynamicIntBytes(val uint64, sz uint) []byte {
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, val)

	return data[8-sz:]
}
