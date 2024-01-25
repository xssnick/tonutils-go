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

func (c *Cell) copy() *Cell {
	refs := make([]*Cell, len(c.refs))
	for i, ref := range c.refs {
		refs[i] = ref.copy()
	}

	return &Cell{
		special:     c.special,
		levelMask:   c.levelMask,
		bitsSz:      c.bitsSz,
		data:        append([]byte{}, c.data...),
		hashes:      append([]byte{}, c.hashes...),
		depthLevels: append([]uint16{}, c.depthLevels...),
		refs:        refs,
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
	c.hashes = c.hashes[:0]
	c.calculateHashes()
}

func (c *Cell) PeekRef(i int) (*Cell, error) {
	if i >= len(c.refs) {
		return nil, ErrNoMoreRefs
	}
	return c.refs[i], nil
}

func (c *Cell) Dump(limitLength ...int) string {
	var lim = (1024 << 20) * 16
	if len(limitLength) > 0 {
		// 16 MB default lim
		lim = limitLength[0]
	}
	return c.dump(0, false, lim)
}

func (c *Cell) DumpBits(limitLength ...int) string {
	var lim = (1024 << 20) * 16
	if len(limitLength) > 0 {
		// 16 MB default lim
		lim = limitLength[0]
	}
	return c.dump(0, true, lim)
}

func (c *Cell) dump(deep int, bin bool, limitLength int) string {
	sz, data, _ := c.BeginParse().RestBits()

	var val string
	if bin {
		for _, n := range data {
			val += fmt.Sprintf("%08b", n)
		}
		if sz%8 != 0 {
			val = val[:uint(len(val))-(8-(sz%8))]
		}
	} else {
		val = strings.ToUpper(hex.EncodeToString(data))
		if sz%8 <= 4 && sz%8 > 0 {
			// fift hex
			val = val[:len(val)-1] + "_"
		}
	}

	str := strings.Repeat("  ", deep) + fmt.Sprint(sz) + "[" + val + "]"
	if c.levelMask.GetLevel() > 0 {
		str += fmt.Sprintf("{%d}", c.levelMask.GetLevel())
	}
	if c.special {
		str += "*"
	}
	if len(c.refs) > 0 {
		str += " -> {"
		for i, ref := range c.refs {
			str += "\n" + ref.dump(deep+1, bin, limitLength)
			if i == len(c.refs)-1 {
				str += "\n"
			} else {
				str += ","
			}

			if len(str) > limitLength {
				break
			}
		}
		str += strings.Repeat("  ", deep) + "}"
	}

	if len(str) > limitLength {
		str = str[:limitLength]
	}

	return str
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

	data, err := base64.StdEncoding.DecodeString(string(bytes))
	if err != nil {
		return err
	}

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
