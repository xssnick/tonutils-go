package cell

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

type Type uint8

const (
	OrdinaryCellType     Type = 0x00
	PrunedCellType       Type = 0x01
	LibraryCellType      Type = 0x02
	MerkleProofCellType  Type = 0x03
	MerkleUpdateCellType Type = 0x04
	UnknownCellType      Type = 0xFF
)

const maxDepth = 1024

type Cell struct {
	special   bool
	levelMask LevelMask
	bitsSz    uint
	data      []byte

	hashes      []byte
	depthLevels []uint16

	refs []*Cell
}

type RawUnsafeCell struct {
	IsSpecial bool
	LevelMask LevelMask
	BitsSz    uint
	Data      []byte

	Refs []*Cell
}

func (c *Cell) copy() *Cell {
	return &Cell{
		special:     c.special,
		levelMask:   c.levelMask,
		bitsSz:      c.bitsSz,
		data:        append([]byte{}, c.data...),
		hashes:      append([]byte{}, c.hashes...),
		depthLevels: append([]uint16{}, c.depthLevels...),
		refs:        append([]*Cell{}, c.refs...),
	}
}

func (c *Cell) BeginParse() *Slice {
	// copy data
	data := append([]byte{}, c.data...)

	return &Slice{
		special:   c.special,
		levelMask: c.levelMask,
		bitsSz:    c.bitsSz,
		data:      data,
		refs:      c.refs,
	}
}

func (c *Cell) ToBuilder() *Builder {
	// copy data
	data := append([]byte{}, c.data...)

	return &Builder{
		bitsSz: c.bitsSz,
		data:   data,
		refs:   c.refs,
	}
}

func (c *Cell) ToRawUnsafe() RawUnsafeCell {
	return RawUnsafeCell{
		IsSpecial: c.special,
		LevelMask: c.levelMask,
		BitsSz:    c.bitsSz,
		Data:      c.data,
		Refs:      c.refs,
	}
}

func FromRawUnsafe(u RawUnsafeCell) *Cell {
	c := &Cell{
		special:   u.IsSpecial,
		levelMask: u.LevelMask,
		bitsSz:    u.BitsSz,
		data:      u.Data,
		refs:      u.Refs,
	}
	c.calculateHashes()
	return c
}

func (c *Cell) BitsSize() uint {
	return c.bitsSz
}

func (c *Cell) RefsNum() uint {
	return uint(len(c.refs))
}

func (c *Cell) MustPeekRef(i int) *Cell {
	return c.refs[i]
}

func (c *Cell) UnsafeModify(levelMask LevelMask, special bool) {
	c.special = special
	c.levelMask = levelMask
	c.calculateHashes()
}

func (c *Cell) PeekRef(i int) (*Cell, error) {
	if i >= len(c.refs) {
		return nil, ErrNoMoreRefs
	}
	return c.refs[i], nil
}

func (c *Cell) Dump(limitLength ...int) string {
	var lim = uint64(1024<<20) * 16
	if len(limitLength) > 0 {
		// 16 MB default lim
		lim = uint64(limitLength[0])
	}
	return c.dump(0, false, lim)
}

func (c *Cell) DumpBits(limitLength ...int) string {
	var lim = uint64(1024<<20) * 16
	if len(limitLength) > 0 {
		// 16 MB default lim
		lim = uint64(limitLength[0])
	}
	return c.dump(0, true, lim)
}

func (c *Cell) dump(deep int, bin bool, limitLength uint64) string {
	sz, data, _ := c.BeginParse().RestBits()

	builder := strings.Builder{}

	if bin {
		for _, n := range data {
			builder.WriteString(fmt.Sprintf("%08b", n))
		}
		if sz%8 != 0 {
			tmp := builder.String()
			builder.Reset()
			builder.WriteString(tmp[:uint(len(tmp))-(8-(sz%8))])
		}
	} else {
		tmp := make([]byte, len(data)*2)
		hex.Encode(tmp, data)
		builder.WriteString(strings.ToUpper(string(tmp)))

		if sz%8 <= 4 && sz%8 > 0 {
			tmp := builder.String()
			builder.Reset()
			builder.WriteString(tmp[:len(tmp)-1])
			builder.WriteByte('_')

		}
	}

	val := builder.String()
	builder.Reset()
	builder.WriteString(strings.Repeat("  ", deep))
	builder.WriteString(strconv.FormatUint(uint64(sz), 10))
	builder.WriteByte('[')
	builder.WriteString(val)
	builder.WriteByte(']')

	if c.levelMask.GetLevel() > 0 {
		builder.WriteByte('{')
		builder.WriteString(strconv.Itoa(c.levelMask.GetLevel()))
		builder.WriteByte('}')

	}
	if c.special {
		builder.WriteByte('*')
	}
	if len(c.refs) > 0 {

		builder.WriteString(" -> {")

		for i, ref := range c.refs {

			builder.WriteByte('\n')
			builder.WriteString(ref.dump(deep+1, bin, limitLength))

			if i == len(c.refs)-1 {
				builder.WriteByte('\n')
			} else {
				builder.WriteByte(',')
			}

			if uint64(builder.Len()) > limitLength {
				break
			}
		}
		builder.WriteString(strings.Repeat("  ", deep))
		builder.WriteByte('}')
	}

	if uint64(builder.Len()) > limitLength {
		tmp := builder.String()
		builder.Reset()
		builder.WriteString(tmp[:limitLength])
	}

	return builder.String()
}

const _DataCellMaxLevel = 3

// Hash - calculates a hash of cell recursively
// Once calculated, it is cached and can be reused cheap.
func (c *Cell) Hash(level ...int) []byte {
	if len(level) > 0 {
		return append([]byte{}, c.getHash(level[0])...)
	}
	return append([]byte{}, c.getHash(_DataCellMaxLevel)...)
}

func (c *Cell) Depth(level ...int) uint16 {
	if len(level) > 0 {
		return c.getDepth(level[0])
	}
	return c.getDepth(_DataCellMaxLevel)
}

func (c *Cell) Sign(key ed25519.PrivateKey) []byte {
	return ed25519.Sign(key, c.Hash())
}

func (c *Cell) Verify(key ed25519.PublicKey, signature []byte) bool {
	return ed25519.Verify(key, c.Hash(), signature)
}

func (c *Cell) GetType() Type {
	if !c.special {
		return OrdinaryCellType
	}
	if c.BitsSize() < 8 {
		return UnknownCellType
	}

	switch Type(c.data[0]) {
	case PrunedCellType:
		if c.BitsSize() >= 288 {
			msk := LevelMask{c.data[1]}
			lvl := msk.GetLevel()
			if lvl > 0 && lvl <= 3 && c.BitsSize() >= 16+(256+16)*(uint(msk.Apply(lvl-1).getHashIndex()+1)) {
				return PrunedCellType
			}
		}
	case MerkleProofCellType:
		if c.RefsNum() == 1 && c.BitsSize() == 280 {
			return MerkleProofCellType
		}
	case MerkleUpdateCellType:
		if c.RefsNum() == 2 && c.BitsSize() == 552 {
			return MerkleUpdateCellType
		}
	case LibraryCellType:
		if c.BitsSize() == 8+256 {
			return LibraryCellType
		}
	}
	return UnknownCellType
}

func (c *Cell) UnmarshalJSON(bytes []byte) error {
	if len(bytes) < 2 || bytes[0] != '"' || bytes[len(bytes)-1] != '"' {
		return fmt.Errorf("invalid data")
	}
	bytes = bytes[1 : len(bytes)-1]

	data := make([]byte, base64.StdEncoding.DecodedLen(len(bytes)))

	n, err := base64.StdEncoding.Decode(data, bytes)
	if err != nil {
		return err
	}
	data = data[:n]

	cl, err := FromBOC(data)
	if err != nil {
		return err
	}
	*c = *cl
	return nil
}

func (c *Cell) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(base64.StdEncoding.EncodeToString(c.ToBOC()))), nil
}
