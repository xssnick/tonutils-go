package cell

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	_OrdinaryType    = 0x00
	_PrunedType      = 0x01
	_LibraryType     = 0x02
	_MerkleProofType = 0x03
	_UnknownType     = 0xFF
)

type Cell struct {
	special bool
	level   byte
	bitsSz  uint
	index   int
	data    []byte

	cachedHash []byte

	refs []*Cell
}

func (c *Cell) copy() *Cell {
	// copy data
	data := append([]byte{}, c.data...)

	refs := make([]*Cell, len(c.refs))
	for i, ref := range c.refs {
		refs[i] = ref.copy()
	}

	return &Cell{
		special:    c.special,
		level:      c.level,
		bitsSz:     c.bitsSz,
		data:       data,
		cachedHash: c.cachedHash,
		refs:       refs,
	}
}

func (c *Cell) BeginParse() *Slice {
	// copy data
	data := append([]byte{}, c.data...)

	refs := make([]*Slice, len(c.refs))
	for i, ref := range c.refs {
		refs[i] = ref.BeginParse()
	}

	return &Slice{
		special: c.special,
		level:   c.level,
		bitsSz:  c.bitsSz,
		data:    data,
		refs:    refs,
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

func (c *Cell) Dump() string {
	return c.dump(0, false)
}

func (c *Cell) DumpBits() string {
	return c.dump(0, true)
}

func (c *Cell) dump(deep int, bin bool) string {
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
	if c.level > 0 {
		str += fmt.Sprintf("{%d}", c.level)
	}
	if c.special {
		str += "*"
	}
	if len(c.refs) > 0 {
		str += " -> {"
		for i, ref := range c.refs {
			str += "\n" + ref.dump(deep+1, bin)
			if i == len(c.refs)-1 {
				str += "\n"
			} else {
				str += ","
			}
		}
		str += strings.Repeat("  ", deep)
		return str + "}"
	}
	return str
}

func (c *Cell) Hash() []byte {
	if c.cachedHash == nil {
		hash := sha256.New()
		hash.Write(c.serializeHash())
		c.cachedHash = hash.Sum(nil)
	}
	return c.cachedHash
}

func (c *Cell) Sign(key ed25519.PrivateKey) []byte {
	return ed25519.Sign(key, c.Hash())
}

func (c *Cell) getType() int {
	if !c.special {
		return _OrdinaryType
	}
	if c.BitsSize() < 8 {
		return _UnknownType
	}

	switch c.data[0] {
	case _MerkleProofType:
		if c.RefsNum() == 1 && c.BitsSize() == 280 {
			return _MerkleProofType
		}
	case _PrunedType:
		if c.BitsSize() >= 288 {
			lvl := uint(c.data[1])
			if lvl > 0 && lvl <= 3 && c.BitsSize() >= 16+(256+16)*lvl {
				return _PrunedType
			}
		}
	case _LibraryType:
		if c.BitsSize() == 8+256 {
			return _LibraryType
		}
	}
	return _UnknownType
}
