package raptorq

type RaptorQ struct {
	symbolSz uint32
}

func NewRaptorQ(symbolSz uint32) *RaptorQ {
	return &RaptorQ{
		symbolSz: symbolSz,
	}
}

func (r *RaptorQ) GetSymbolSize() uint32 {
	return r.symbolSz
}
