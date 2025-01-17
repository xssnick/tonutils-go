package raptorq

type Symbol struct {
	ID   uint32
	Data []byte
}

func splitToSymbols(symCount, symSz uint32, data []byte) []Symbol {
	symbols := make([]Symbol, symCount)
	sym := make([]byte, symSz*symCount)

	for i := uint32(0); i < symCount; i++ {
		offset := i * symSz
		if offset < uint32(len(data)) {
			copy(sym[offset:offset+symSz], data[offset:])
		}
		symbols[i] = Symbol{
			ID:   i,
			Data: sym[offset : offset+symSz],
		}
	}

	return symbols
}
