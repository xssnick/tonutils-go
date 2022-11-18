package raptorq

type Symbol struct {
	ID   uint32
	Data []byte
}

func splitToSymbols(symCount, symSz uint32, data []byte) []*Symbol {
	symbols := make([]*Symbol, symCount)
	for i := uint32(0); i < symCount; i++ {
		sym := make([]byte, symSz)

		offset := i * symSz
		if offset < uint32(len(data)) {
			copy(sym, data[offset:])
		}
		symbols[i] = &Symbol{
			ID:   i,
			Data: sym,
		}
	}

	return symbols
}
