package cell

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
