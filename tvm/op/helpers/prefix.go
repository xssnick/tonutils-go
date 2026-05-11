package helpers

import "github.com/xssnick/tonutils-go/tvm/cell"

type BitPrefix struct {
	Bits uint
	Data []byte
}

type Prefixed struct {
	Prefixes []BitPrefix
}

func NewPrefixed(prefixes ...BitPrefix) Prefixed {
	cp := make([]BitPrefix, len(prefixes))
	copy(cp, prefixes)
	return Prefixed{Prefixes: cp}
}

func SinglePrefixed(prefix BitPrefix) Prefixed {
	return NewPrefixed(prefix)
}

func BytesPrefix(data ...byte) BitPrefix {
	return BitPrefix{
		Bits: uint(len(data) * 8),
		Data: append([]byte{}, data...),
	}
}

func SlicePrefix(bits uint, data []byte) BitPrefix {
	return BitPrefix{
		Bits: bits,
		Data: append([]byte{}, data...),
	}
}

func UIntPrefix(value uint64, bits uint) BitPrefix {
	return BitPrefix{
		Bits: bits,
		Data: cell.BeginCell().MustStoreUInt(value, bits).EndCell().BeginParse().MustPreloadSlice(bits),
	}
}

func PrefixSlices(prefixes ...BitPrefix) []*cell.Slice {
	res := make([]*cell.Slice, 0, len(prefixes))
	for _, prefix := range prefixes {
		res = append(res, prefix.ToSlice())
	}
	return res
}

func (p Prefixed) GetPrefixes() []*cell.Slice {
	return PrefixSlices(p.Prefixes...)
}

func (p BitPrefix) ToSlice() *cell.Slice {
	return cell.BeginCell().MustStoreSlice(p.Data, p.Bits).EndCell().BeginParse()
}
