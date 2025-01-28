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

	payload             []byte
	payloadSymbolBitmap []byte

	slowSymbols map[uint32][]byte

	pm *raptorParams
}

func (r *RaptorQ) CreateDecoder(dataSize uint32) (*Decoder, error) {
	param, err := r.calcParams(dataSize)
	if err != nil {
		return nil, fmt.Errorf("failed to calc params: %w", err)
	}

	return &Decoder{
		symbolSz:            r.symbolSz,
		pm:                  param,
		payload:             make([]byte, param._K*r.symbolSz),
		payloadSymbolBitmap: make([]byte, param._K/8+1),
		dataSz:              dataSize,
		slowSymbols:         make(map[uint32][]byte),
	}, nil
}

func (d *Decoder) AddSymbol(id uint32, data []byte) (bool, error) {
	if uint32(len(data)) != d.symbolSz {
		return false, fmt.Errorf("incorrect symbol size %d, should be %d", len(data), d.symbolSz)
	}

	if id < d.pm._K {
		// add fast symbol
		if d.payloadSymbolBitmap[id/8]&(1<<(id%8)) != 0 {
			return d.fastNum+d.slowNum >= d.pm._K, nil
		}
		d.payloadSymbolBitmap[id/8] |= 1 << (id % 8)
		d.fastNum++
		copy(d.payload[id*d.symbolSz:], data)
	} else if _, ok := d.slowSymbols[id]; !ok {
		d.slowSymbols[id] = append([]byte{}, data...)
		d.slowNum++
	}

	return d.fastNum+d.slowNum >= d.pm._K, nil
}

func (d *Decoder) FastSymbolsNumRequired() uint32 {
	return d.pm._K
}

func (d *Decoder) Decode() (bool, []byte, error) {
	if d.fastNum+d.slowNum < d.pm._K {
		return false, nil, fmt.Errorf("not enough symbols to decode")
	}

	if d.fastNum < d.pm._K {
		sz := d.pm._K + uint32(len(d.slowSymbols))
		if sz < d.pm._KPadded {
			sz = d.pm._KPadded
		}

		toRelax := make([]Symbol, 0, sz)

		buf := make([]byte, len(d.payload))
		copy(buf, d.payload)

		// add known symbols
		for k := uint32(0); k < d.pm._K; k++ {
			if d.payloadSymbolBitmap[k/8]&(1<<(k%8)) == 0 {
				continue
			}

			off := k * d.symbolSz
			toRelax = append(toRelax, Symbol{
				ID:   k,
				Data: buf[off : off+d.symbolSz],
			})
		}

		for k, v := range d.slowSymbols {
			data := make([]byte, len(v))
			copy(data, v)

			if k >= d.pm._K {
				// add offset for additional symbols
				k = k + d.pm._KPadded - d.pm._K
			}

			toRelax = append(toRelax, Symbol{
				ID:   k,
				Data: data,
			})
		}

		// add padding empty symbols
		for i := uint32(len(toRelax)); i < d.pm._KPadded; i++ {
			zero := make([]byte, d.symbolSz)
			toRelax = append(toRelax, Symbol{
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
			if d.payloadSymbolBitmap[i/8]&(1<<(i%8)) != 0 {
				continue
			}
			copy(d.payload[i*d.symbolSz:], d.pm.genSymbol(relaxed, d.symbolSz, i))
		}
	}

	return true, d.payload[:d.dataSz], nil
}
