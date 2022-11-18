package raptorq

import "C"
import (
	"errors"
	"fmt"
	discmath2 "github.com/xssnick/tonutils-go/adnl/rldp/raptorq/discmath"
)

var ErrNotEnoughSymbols = errors.New("not enough symbols")

func (p *params) createD(symbols []*Symbol) *discmath2.MatrixGF256 {
	symSz := uint32(len(symbols[0].Data))
	d := discmath2.NewMatrixGF256(p._S+p._H+uint32(len(symbols)), symSz)

	offset := p._S
	for _, symbol := range symbols {
		for i, b := range symbol.Data {
			d.Set(offset, uint32(i), b)
		}
		offset++
	}

	return d
}

func (p *params) solve(symbols []*Symbol) (*discmath2.MatrixGF256, error) {
	d := p.createD(symbols)

	eRows := make([]*encodingRow, 0, len(symbols))
	for _, symbol := range symbols {
		eRows = append(eRows, p.calcEncodingRow(symbol.ID))
	}

	aUpper := discmath2.NewSparseMatrixGF2(p._S+uint32(len(eRows)), p._L)

	// LDPC 1
	for i := uint32(0); i < p._B; i++ {
		a := 1 + i/p._S

		b := i % p._S
		aUpper.Set(b, i)

		b = (b + a) % p._S
		aUpper.Set(b, i)

		b = (b + a) % p._S
		aUpper.Set(b, i)
	}

	// Ident
	for i := uint32(0); i < p._S; i++ {
		aUpper.Set(i, i+p._B)
	}

	// LDPC 2
	for i := uint32(0); i < p._S; i++ {
		aUpper.Set(i, (i%p._P)+p._W)
		aUpper.Set(i, ((i+1)%p._P)+p._W)
	}

	// Encode
	for ri, row := range eRows {
		f := func(col uint32) {
			aUpper.Set(uint32(ri)+p._S, col)
		}
		row.encode(p, f)
	}

	uSize, rowPermutation, colPermutation := inactivateDecode(aUpper, p._P)

	for len(rowPermutation) < int(d.RowsNum()) {
		rowPermutation = append(rowPermutation, uint32(len(rowPermutation)))
	}

	d = d.ApplyPermutation(rowPermutation)

	aUpper = aUpper.ApplyRowsPermutation(rowPermutation)
	aUpper = aUpper.ApplyColsPermutation(colPermutation)

	e := aUpper.ToDense(0, uSize, uSize, p._L-uSize)

	c := discmath2.NewMatrixGF256(aUpper.ColsNum(), d.ColsNum())
	c.SetFrom(d.GetBlock(0, 0, uSize, d.ColsNum()), 0, 0)

	// Make U Identity matrix and calculate E and D_upper.
	for i := uint32(0); i < uSize; i++ {
		for _, row := range aUpper.GetCols(i) {
			if row == i {
				continue
			}
			if row >= uSize {
				break
			}
			e.RowAdd(row, e.GetRow(i))
			d.RowAdd(row, d.GetRow(i))
		}
	}

	hdpcMul := func(m *discmath2.MatrixGF256) *discmath2.MatrixGF256 {
		t := discmath2.NewMatrixGF256(p._KPadded+p._S, m.ColsNum())
		for i := uint32(0); i < m.RowsNum(); i++ {
			t.RowSet(colPermutation[i], m.GetRow(i))
		}
		return p.hdpcMultiply(t)
	}

	gLeft := aUpper.GetBlock(uSize, 0, aUpper.RowsNum()-uSize, uSize)

	smallAUpper := discmath2.NewMatrixGF256(aUpper.RowsNum()-uSize, aUpper.ColsNum()-uSize)
	aUpper.GetBlock(uSize, uSize, aUpper.RowsNum()-uSize, aUpper.ColsNum()-uSize).
		Each(func(row, col uint32) {
			smallAUpper.Set(row, col, 1)
		})

	smallAUpper = smallAUpper.Add(e.Mul(gLeft).ToGF256())

	// calculate small A lower
	smallALower := discmath2.NewMatrixGF256(p._H, aUpper.ColsNum()-uSize)
	for i := uint32(1); i <= p._H; i++ {
		smallALower.Set(smallALower.RowsNum()-i, smallALower.ColsNum()-i, 1)
	}

	// calculate HDPC right and set it into small A lower
	t := discmath2.NewMatrixGF256(p._KPadded+p._S, p._KPadded+p._S-uSize)
	for i := uint32(0); i < t.ColsNum(); i++ {
		t.Set(colPermutation[i+t.RowsNum()-t.ColsNum()], i, 1)
	}
	hdpcRight := p.hdpcMultiply(t)
	smallALower.SetFrom(hdpcRight, 0, 0)

	// ALower += hdpc(E)
	smallALower = smallALower.Add(hdpcMul(e.ToGF256()))

	dUpper := discmath2.NewMatrixGF256(uSize, d.ColsNum())
	dUpper.SetFrom(d.GetBlock(0, 0, dUpper.RowsNum(), dUpper.ColsNum()), 0, 0)

	smallDUpper := discmath2.NewMatrixGF256(aUpper.RowsNum()-uSize, d.ColsNum())
	smallDUpper.SetFrom(d.GetBlock(uSize, 0, smallDUpper.RowsNum(), smallDUpper.ColsNum()), 0, 0)
	smallDUpper = smallDUpper.Add(dUpper.MulSparse(gLeft))

	smallDLower := discmath2.NewMatrixGF256(p._H, d.ColsNum())
	smallDLower.SetFrom(d.GetBlock(aUpper.RowsNum(), 0, smallDLower.RowsNum(), smallDLower.ColsNum()), 0, 0)
	smallDLower = smallDLower.Add(hdpcMul(dUpper))

	// combine small A
	smallA := discmath2.NewMatrixGF256(smallAUpper.RowsNum()+smallALower.RowsNum(), smallAUpper.ColsNum())
	smallA.SetFrom(smallAUpper, 0, 0)
	smallA.SetFrom(smallALower, smallAUpper.RowsNum(), 0)

	// combine small D
	smallD := discmath2.NewMatrixGF256(smallDUpper.RowsNum()+smallDLower.RowsNum(), smallDUpper.ColsNum())
	smallD.SetFrom(smallDUpper, 0, 0)
	smallD.SetFrom(smallDLower, smallDUpper.RowsNum(), 0)

	smallC, err := discmath2.GaussianElimination(smallA, smallD)
	if err != nil {
		if err == discmath2.ErrNotSolvable {
			return nil, ErrNotEnoughSymbols
		}
		return nil, fmt.Errorf("failed to calc gauss elimination: %w", err)
	}

	c.SetFrom(smallC.GetBlock(0, 0, c.RowsNum()-uSize, c.ColsNum()), uSize, 0)
	for row := uint32(0); row < uSize; row++ {
		for _, col := range aUpper.GetRows(row) {
			if col == row {
				continue
			}
			c.RowAdd(row, c.GetRow(col))
		}
	}

	return c.ApplyPermutation(discmath2.InversePermutation(colPermutation)), nil
}
