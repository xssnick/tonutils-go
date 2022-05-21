package cell

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

type Cell struct {
	bitsSz int
	index  int
	data   []byte

	refs []*Cell
}

func (c *Cell) BeginParse() *LoadCell {
	// copy data
	data := append([]byte{}, c.data...)

	refs := make([]*LoadCell, len(c.refs))
	for i, ref := range c.refs {
		refs[i] = ref.BeginParse()
	}

	return &LoadCell{
		bitsSz:   c.bitsSz,
		loadedSz: 0,
		data:     data,
		refs:     refs,
	}
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
	} else {
		val = hex.EncodeToString(data)
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
	hash.Write(c.data)
	return hash.Sum(nil)

	/*
		writeFully(descriptors())
		writeFully(augmentedBytes())
		references.forEach { reference ->
			val depth = reference.maxDepth
			writeInt(depth)
		}
		references.forEach { reference ->
			val hash = reference.hash()
			writeFully(hash)
		}
	*/
}
