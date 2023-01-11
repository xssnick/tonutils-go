package raptorq

import (
	"errors"
	"fmt"
)

type Decoder struct {
	symbolSz uint32
	dataSz   uint32

	fastNum uint32
	slowNum uint32

	symbols map[uint32][]byte

	pm *raptorParams
}

func (r *RaptorQ) CreateDecoder(dataSize uint32) (*Decoder, error) {
	param, err := r.calcParams(dataSize)
	if err != nil {
		return nil, fmt.Errorf("failed to calc params: %w", err)
	}

	return &Decoder{
		symbolSz: r.symbolSz,
		pm:       param,
		symbols:  map[uint32][]byte{},
		dataSz:   dataSize,
	}, nil
}

func (d *Decoder) AddSymbol(id uint32, data []byte) (bool, error) {
	tmp := make([]byte, len(data))
	copy(tmp, data)
	data = tmp

	if uint32(len(data)) < d.symbolSz {
		return false, fmt.Errorf("incorrect symbol size %d, should be %d", len(data), d.symbolSz)
	}

	if id < d.pm._K {
		// add fast symbol
		if d.symbols[id] != nil {
			return d.fastNum+d.slowNum >= d.pm._K, nil
		}
		d.fastNum++
		d.symbols[id] = data
	}

	if d.fastNum+d.slowNum >= d.pm._K+10 {
		// we have enough symbols
		return true, nil
	}

	// check if we already have this symbol
	if _, ok := d.symbols[id]; ok {
		return d.fastNum+d.slowNum >= d.pm._K, nil
	}

	d.slowNum++
	d.symbols[id] = data
	return d.fastNum+d.slowNum >= d.pm._K, nil
}

func (d *Decoder) Decode() (bool, []byte, error) {
	if d.fastNum+d.slowNum < d.pm._K {
		return false, nil, fmt.Errorf("not enough symbols to decode")
	}

	if d.fastNum < d.pm._K {
		toRelax := make([]*Symbol, 0)

		// add known symbols
		for k, v := range d.symbols {
			data := make([]byte, len(v))
			copy(data, v)

			if k >= d.pm._K {
				// add offset for additional symbols
				k = k + d.pm._KPadded - d.pm._K
			}

			toRelax = append(toRelax, &Symbol{
				ID:   k,
				Data: data,
			})
		}

		// add padding empty symbols
		for i := uint32(len(toRelax)); i < d.pm._KPadded; i++ {
			zero := make([]byte, d.symbolSz)
			toRelax = append(toRelax, &Symbol{
				ID:   i,
				Data: zero,
			})
		}

		// we have not all fast symbols, try to recover them from slow
		relaxed, err := d.pm.Solve(toRelax)
		if err != nil {
			if errors.Is(err, ErrNotEnoughSymbols) {
				return false, nil, nil
			}
			return false, nil, fmt.Errorf("failed to relax known symbols, err: %w", err)
		}

		for i := uint32(0); i < d.pm._K; i++ {
			if d.symbols[i] != nil {
				continue
			}
			d.symbols[i] = d.pm.genSymbol(relaxed, d.symbolSz, i)
		}
	}

	data := make([]byte, d.dataSz)
	for i := uint32(0); i < d.pm._K; i++ {
		copy(data[i*d.symbolSz:], d.symbols[i])
	}
	return true, data, nil
}
