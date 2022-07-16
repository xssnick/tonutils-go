package cell

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

type Cell struct {
	special bool
	level   byte
	bitsSz  uint
	index   int
	data    []byte

	refs []*Cell
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
	hash := sha256.New()
	hash.Write(c.serialize(0, true))
	return hash.Sum(nil)
}

func (c *Cell) Sign(key ed25519.PrivateKey) []byte {
	return ed25519.Sign(key, c.Hash())
}
