package roundrobin

import "errors"

type Encoder struct {
	data         []byte
	symbolSize   uint32
	symbolsCount uint32
}

type Decoder struct {
	data         []byte
	mask         []bool
	left         uint32
	symbolSize   uint32
	symbolsCount uint32
}

func NewEncoder(data []byte, maxSymbolSize uint32) (*Encoder, error) {
	syms := (len(data) + int(maxSymbolSize) - 1) / int(maxSymbolSize)
	if syms == 0 {
		return nil, errors.New("data must be non-empty")
	}

	return &Encoder{
		data:         data,
		symbolSize:   maxSymbolSize,
		symbolsCount: uint32(syms),
	}, nil
}

func (e *Encoder) GenSymbol(id uint32) []byte {
	if e.symbolsCount == 0 {
		return nil
	}
	pos := id % e.symbolsCount
	offset := pos * e.symbolSize
	end := offset + e.symbolSize

	out := make([]byte, e.symbolSize)
	if int(offset) < len(e.data) {
		if int(end) > len(e.data) {
			end = uint32(len(e.data))
		}
		copy(out, e.data[offset:end])
	}
	return out
}

func NewDecoder(symbolSize uint32, dataSize uint32) (*Decoder, error) {
	syms := (dataSize + symbolSize - 1) / symbolSize
	if syms == 0 {
		return nil, errors.New("dataSize must be > 0")
	}

	return &Decoder{
		data:         make([]byte, dataSize),
		mask:         make([]bool, syms),
		left:         syms,
		symbolSize:   symbolSize,
		symbolsCount: syms,
	}, nil
}

func (d *Decoder) AddSymbol(id uint32, sym []byte) (bool, error) {
	if uint32(len(sym)) != d.symbolSize {
		return false, errors.New("invalid symbol length")
	}

	if d.symbolsCount == 0 {
		return false, errors.New("decoder not initialized")
	}

	pos := id % d.symbolsCount
	idx := int(pos)
	if d.mask[idx] {
		return d.left == 0, nil
	}

	offset := idx * int(d.symbolSize)
	end := offset + int(d.symbolSize)
	if offset < len(d.data) {
		if end > len(d.data) {
			end = len(d.data)
		}
		copy(d.data[offset:end], sym[:end-offset])
	}

	d.mask[idx] = true
	d.left--
	return d.left == 0, nil
}

func (d *Decoder) Decode() (bool, []byte, error) {
	if d.left != 0 {
		return false, nil, errors.New("not ready")
	}
	return true, d.data, nil
}
