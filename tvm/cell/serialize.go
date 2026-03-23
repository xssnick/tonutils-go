package cell

import (
	"encoding/binary"
	"errors"
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
	return c.ToBOCWithOptions(BOCOptions{WithCRC32C: true})
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

	return ToBOCWithOptions(roots, BOCOptions{
		WithCRC32C:    withCRC,
		WithIndex:     withIndex,
		WithCacheBits: withCache,
		WithTopHash:   withTopHash,
		WithIntHashes: withIntHashes,
	})
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
