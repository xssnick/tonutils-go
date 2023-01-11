package raptorq

import (
	"fmt"
	"github.com/xssnick/tonutils-go/adnl/rldp/raptorq/discmath"
)

type Encoder struct {
	symbolSz uint32
	relaxed  *discmath.MatrixGF256
	symbols  []*Symbol
	params   *raptorParams
}

func (r *RaptorQ) CreateEncoder(data []byte) (*Encoder, error) {
	param, err := r.calcParams(uint32(len(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to calc params: %w", err)
	}

	symbols := splitToSymbols(param._KPadded, r.symbolSz, data)

	rx, err := param.Solve(symbols)
	if err != nil {
		return nil, fmt.Errorf("failed to relax symbols: %w", err)
	}

	return &Encoder{
		symbolSz: r.symbolSz,
		relaxed:  rx,
		symbols:  symbols,
		params:   param,
	}, nil
}

func (e *Encoder) GenSymbol(id uint32) []byte {
	if id < e.params._K {
		x := make([]byte, len(e.symbols[id].Data))
		copy(x, e.symbols[id].Data)
		return x
	}

	return e.params.genSymbol(e.relaxed, e.symbolSz, id+e.params._KPadded-e.params._K)
}

func (e *Encoder) BaseSymbolsNum() uint32 {
	return e.params._K
}
