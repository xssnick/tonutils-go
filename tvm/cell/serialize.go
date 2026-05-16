package cell

import (
	"encoding/binary"
	"errors"
)

var ErrTooBigValue = errors.New("too big value")
var ErrNegative = errors.New("value should be non negative")
var ErrInvalidSize = errors.New("size should be positive")
var ErrNilBigInt = errors.New("value cannot be nil")
var ErrRefCannotBeNil = errors.New("ref cannot be nil")
var ErrCellDepthLimit = errors.New("cell depth exceeds max depth")
var ErrSmallSlice = errors.New("too small slice for this size")
var ErrTooBigSize = errors.New("too big size")
var ErrTooMuchRefs = errors.New("too much refs")
var ErrNotFit1023 = errors.New("cell data size should fit into 1023 bits")
var ErrNoMoreRefs = errors.New("no more refs exists")
var ErrAddressTypeNotSupported = errors.New("address type is not supported")

func (c *Cell) ToBOC() []byte {
	return c.ToBOCWithOptions(BOCSerializeOptions{WithCRC32C: true})
}

// ToBOCWithFlags serializes the cell into BOC using the legacy boolean-flag API.
//
// Deprecated: use [(*Cell).ToBOCWithOptions] instead.
func (c *Cell) ToBOCWithFlags(flags ...bool) []byte {
	return ToBOCWithFlags([]*Cell{c}, flags...)
}

// ToBOCWithFlags serializes roots into BOC using the legacy boolean-flag API.
// Flags are: first - withCRC, second - withIndex, third - withCache,
// fourth - withTopHash, fifth - withIntHashes.
//
// Deprecated: use [ToBOCWithOptions] instead.
func ToBOCWithFlags(roots []*Cell, flags ...bool) []byte {
	if len(roots) == 0 {
		return nil
	}

	withCRC := len(flags) > 0 && flags[0]
	withIndex := len(flags) > 1 && flags[1]
	withCache := len(flags) > 2 && flags[2]
	withTopHash := len(flags) > 3 && flags[3]
	withIntHashes := len(flags) > 4 && flags[4]

	return ToBOCWithOptions(roots, BOCSerializeOptions{
		WithCRC32C:    withCRC,
		WithIndex:     withIndex,
		WithCacheBits: withCache,
		WithTopHash:   withTopHash,
		WithIntHashes: withIntHashes,
	})
}

func (c *Cell) descriptors(lvl LevelMask) (byte, byte) {
	// calc size
	ln := (uint(c.bitsSz) / 8) * 2
	if c.bitsSz%8 != 0 {
		ln++
	}

	specBit := byte(0)
	if c.IsSpecial() {
		specBit = 8
	}

	return byte(c.refsCount()) + specBit + lvl.Mask*32, byte(ln)
}

func storeUintTo(dst []byte, val uint64, sz int) {
	switch sz {
	case 1:
		dst[0] = byte(val)
	case 2:
		binary.BigEndian.PutUint16(dst, uint16(val))
	case 3:
		dst[0] = byte(val >> 16)
		dst[1] = byte(val >> 8)
		dst[2] = byte(val)
	case 4:
		binary.BigEndian.PutUint32(dst, uint32(val))
	default:
		for i := sz - 1; i >= 0; i-- {
			dst[i] = byte(val)
			val >>= 8
		}
	}
}
