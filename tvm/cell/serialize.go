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

func (c *Cell) ToBOCWithFlags(flags ...bool) []byte {
	return ToBOCWithFlags([]*Cell{c}, flags...)
}

// ToBOCWithFlags - flags are: first - withCRC, second - withIndex, third - withCache
func ToBOCWithFlags(roots []*Cell, flags ...bool) []byte {
	if len(roots) == 0 {
		return nil
	}

	withCRC := len(flags) > 0 && flags[0]
	withIndex := len(flags) > 1 && flags[1]
	withCache := len(flags) > 2 && flags[2]
	withTopHash := len(flags) > 3 && flags[3]
	withIntHashes := len(flags) > 4 && flags[4]

	// recursively go through cells, build hash index and store unique in slice
	sortedCells, index := flattenIndex(roots, withTopHash, withIntHashes)

	// bytes needed to store num of cells
	cellSizeBits := math.Log2(float64(len(sortedCells)) + 1)
	cellSizeBytes := byte(math.Ceil(cellSizeBits / 8))

	dynBuffer := make([]byte, 8)
	var payload []byte
	for i := 0; i < len(sortedCells); i++ {
		// serialize each cell
		payload = append(payload, sortedCells[i].cell.serialize(uint(cellSizeBytes), index, sortedCells[i].withHash, dynBuffer)...)
		sortedCells[i].dataIndex = len(payload)
	}

	// bytes needed to store len of payload
	sizeBits := math.Log2(float64(len(payload)) + 1)
	sizeBytes := byte(math.Ceil(sizeBits / 8))

	// has_idx 1bit, hash_crc32 1bit,  has_cache_bits 1bit, flags 2bit, size_bytes 3 bit
	flagsByte := byte(0b0_0_0_00_000)
	if withIndex {
		flagsByte |= 0b1_0_0_00_000
	}
	if withCRC {
		flagsByte |= 0b0_1_0_00_000
	}
	if withCache {
		flagsByte |= 0b0_0_1_00_000
	}

	flagsByte |= cellSizeBytes

	var data []byte

	data = append(data, bocMagic...)
	data = append(data, flagsByte)

	// bytes needed to store size
	data = append(data, sizeBytes)

	// cells num
	data = append(data, dynamicIntBytes(uint64(len(sortedCells)), uint(cellSizeBytes), dynBuffer)...)

	// roots num
	data = append(data, dynamicIntBytes(uint64(len(roots)), uint(cellSizeBytes), dynBuffer)...)

	// complete BOCs = 0
	data = append(data, dynamicIntBytes(0, uint(cellSizeBytes), dynBuffer)...)

	// len of data
	data = append(data, dynamicIntBytes(uint64(len(payload)), uint(sizeBytes), dynBuffer)...)

	// root index
	for _, r := range roots {
		data = append(data, dynamicIntBytes(index[string(r.Hash())].index, uint(cellSizeBytes), dynBuffer)...)
	}

	cached := 0
	if withIndex {
		for _, cell := range sortedCells {
			idx := cell.dataIndex
			if withCache {
				idx *= 2
				if cell.repeats > 0 {
					// cache cells which has refs
					idx++
					cached++
				}
			}
			data = append(data, dynamicIntBytes(uint64(idx), uint(sizeBytes), dynBuffer)...)
		}
	}
	data = append(data, payload...)

	if withCRC {
		checksum := make([]byte, 4)
		binary.LittleEndian.PutUint32(checksum, crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli)))

		data = append(data, checksum...)
	}

	return data
}

func (c *Cell) serialize(refIndexSzBytes uint, index map[string]*idxItem, withHash bool, dynBuffer []byte) []byte {
	body := c.data // optimization
	if c.bitsSz%8 != 0 {
		body = c.BeginParse().MustLoadSlice(c.bitsSz)

		unusedBits := 8 - (c.bitsSz % 8)
		// we need to set a bit at the end if not whole byte was used
		body[len(body)-1] += 1 << (unusedBits - 1)
	}

	refsLn := len(c.refs) * int(refIndexSzBytes)
	bufLn := 2 + len(body) + refsLn
	// if withHash {
	// bufLn += c.levelMask.getHashIndex() * (32 + 2)
	//}

	data := make([]byte, bufLn)
	data[0], data[1] = c.descriptors(c.levelMask)
	//if withHash {
	// TODO: support hash serialization
	// data[0] |= 16
	//}
	copy(data[2:], body)

	refsOffset := bufLn - refsLn
	for i, ref := range c.refs {
		copy(data[refsOffset+i*int(refIndexSzBytes):], dynamicIntBytes(index[string(ref.Hash())].index, refIndexSzBytes, dynBuffer))
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

	return byte(len(c.refs)) + specBit + lvl.Mask*32, byte(ln)
}

func dynamicIntBytes(val uint64, sz uint, buffer []byte) []byte {
	binary.BigEndian.PutUint64(buffer, val)

	return buffer[8-sz:]
}
