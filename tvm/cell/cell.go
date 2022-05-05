package cell

import (
	"encoding/hex"
	"github.com/xssnick/tonutils-go/address"
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

func (c *Cell) ParseAddr() (*address.Address, error) {
	loader := c.BeginParse()

	//TODO ??? skipping 3 bits as in tonweb - why?
	_, err := loader.LoadUInt(3)
	if err != nil {
		return nil, err
	}

	data, err := loader.LoadSlice(264)
	if err != nil {
		return nil, err
	}

	a := address.NewAddressFromBytes(data)
	return a, nil
}

func (c *Cell) Dump() string {
	return c.dump(0)
}

func (c *Cell) dump(deep int) string {
	str := "\n" + strings.Repeat("  ", deep) + "[" + hex.EncodeToString(c.data) + "]" + " -> {"
	for _, ref := range c.refs {
		str += ref.dump(deep+1) + ", "
	}

	return str + "\n" + strings.Repeat("  ", deep) + "}"
}
