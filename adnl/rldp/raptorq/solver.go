package raptorq

import (
	"errors"
	"fmt"

	"github.com/xssnick/tonutils-go/adnl/rldp/raptorq/discmath"
)

var ErrNotEnoughSymbols = errors.New("not enough symbols")

func (p *raptorParams) createD(symbols []Symbol) *discmath.MatrixGF256 {
	symSz := uint32(len(symbols[0].Data))
	d := discmath.NewMatrixGF256(p._S+p._H+uint32(len(symbols)), symSz)

	offset := p._S
	for i := range symbols {
		d.RowSet(offset, symbols[i].Data)
		offset++
	}

	return d
}

func (p *raptorParams) Solve(symbols []Symbol) (*discmath.MatrixGF256, error) {
	d := p.createD(symbols)

	eRows := make([]*encodingRow, 0, len(symbols))
	for _, symbol := range symbols {
		eRows = append(eRows, p.calcEncodingRow(symbol.ID))
	}

	aUpper := discmath.NewMatrixGF256(p._S+uint32(len(eRows)), p._L)

	// LDPC 1
	for i := uint32(0); i < p._B; i++ {
		a := 1 + i/p._S

		b := i % p._S
		aUpper.Set(b, i, 1)
		b = (b + a) % p._S
		aUpper.Set(b, i, 1)

		b = (b + a) % p._S
		aUpper.Set(b, i, 1)

	}

	// Ident
	for i := uint32(0); i < p._S; i++ {
		aUpper.Set(i, i+p._B, 1)
	}

	// LDPC 2
	for i := uint32(0); i < p._S; i++ {
		aUpper.Set(i, (i%p._P)+p._W, 1)
		aUpper.Set(i, ((i+1)%p._P)+p._W, 1)
	}

	// Encode
	for ri, row := range eRows {
		row.encode(aUpper, uint32(ri), p)
	}

	uSize, rowPermutation, colPermutation := inactivateDecode(aUpper, p._P)

	for len(rowPermutation) < int(d.RowsNum()) {
		rowPermutation = append(rowPermutation, uint32(len(rowPermutation)))
	}

	d = d.ApplyPermutation(rowPermutation)

	rPermut := inversePermutation(rowPermutation)
	cPermut := inversePermutation(colPermutation)

	aUpperMutRow := discmath.NewMatrixGF256(aUpper.RowsNum(), aUpper.ColsNum())
	for i, val := range aUpper.Data {
		if val != 0 {
			row := uint32(i) / aUpper.Cols
			col := uint32(i) % aUpper.Cols

			// TODO: do in place?
			aUpperMutRow.Set(rPermut[row], col, 1)
		}
	}
	aUpper = aUpperMutRow

	aUpperMutCol := discmath.NewMatrixGF256(aUpper.RowsNum(), aUpper.ColsNum())
	for i, val := range aUpper.Data {
		if val != 0 {
			row := uint32(i) / aUpper.Cols
			col := uint32(i) % aUpper.Cols

			// TODO: do in place?
			aUpperMutCol.Set(row, cPermut[col], 1)
		}
	}
	aUpper = aUpperMutCol

	e := aUpper.ToGF2(0, uSize, uSize, p._L-uSize)

	c := discmath.NewMatrixGF256(aUpper.ColsNum(), d.ColsNum())
	c.SetFromBlock(d, 0, 0, uSize, d.ColsNum(), 0, 0)

	// Make U Identity matrix and calculate E and D_upper.
	colsBuf := make([]uint32, 0, aUpper.Rows)
	for i := uint32(0); i < uSize; i++ {
		for _, row := range aUpper.GetCols(colsBuf, i) {
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

	hdpcMul := func(m *discmath.MatrixGF256) *discmath.MatrixGF256 {
		t := discmath.NewMatrixGF256(p._KPadded+p._S, m.ColsNum())
		for i := uint32(0); i < m.RowsNum(); i++ {
			t.RowSet(colPermutation[i], m.GetRow(i))
		}
		return p.hdpcMultiply(t)
	}

	gLeft := aUpper.GetBlock(uSize, 0, aUpper.RowsNum()-uSize, uSize)

	smallAUpper := discmath.NewMatrixGF256(aUpper.RowsNum()-uSize, aUpper.ColsNum()-uSize)
	ub := aUpper.GetBlock(uSize, uSize, aUpper.RowsNum()-uSize, aUpper.ColsNum()-uSize)

	for i, val := range ub.Data {
		if val != 0 {
			row := uint32(i) / ub.Cols
			col := uint32(i) % ub.Cols

			smallAUpper.Set(row, col, 1)
		}
	}

	smallAUpper = smallAUpper.Add(e.Mul(gLeft).ToGF256())

	// calculate small A lower
	smallALower := discmath.NewMatrixGF256(p._H, aUpper.ColsNum()-uSize)
	for i := uint32(1); i <= p._H; i++ {
		smallALower.Set(smallALower.RowsNum()-i, smallALower.ColsNum()-i, 1)
	}

	// calculate HDPC right and set it into small A lower
	t := discmath.NewMatrixGF256(p._KPadded+p._S, p._KPadded+p._S-uSize)
	for i := uint32(0); i < t.ColsNum(); i++ {
		t.Set(colPermutation[i+t.RowsNum()-t.ColsNum()], i, 1)
	}
	hdpcRight := p.hdpcMultiply(t)
	smallALower.SetFrom(hdpcRight, 0, 0)

	// ALower += hdpc(E)
	smallALower = smallALower.Add(hdpcMul(e.ToGF256()))

	dUpper := discmath.NewMatrixGF256(uSize, d.ColsNum())
	dUpper.SetFromBlock(d, 0, 0, dUpper.RowsNum(), dUpper.ColsNum(), 0, 0)

	smallDUpper := discmath.NewMatrixGF256(aUpper.RowsNum()-uSize, d.ColsNum())
	smallDUpper.SetFromBlock(d, uSize, 0, smallDUpper.RowsNum(), smallDUpper.ColsNum(), 0, 0)
	smallDUpper = smallDUpper.Add(dUpper.MulSparse(gLeft))

	smallDLower := discmath.NewMatrixGF256(p._H, d.ColsNum())
	smallDLower.SetFromBlock(d, aUpper.RowsNum(), 0, smallDLower.RowsNum(), smallDLower.ColsNum(), 0, 0)
	smallDLower = smallDLower.Add(hdpcMul(dUpper))

	// combine small A
	smallA := discmath.NewMatrixGF256(smallAUpper.RowsNum()+smallALower.RowsNum(), smallAUpper.ColsNum())
	smallA.SetFrom(smallAUpper, 0, 0)
	smallA.SetFrom(smallALower, smallAUpper.RowsNum(), 0)

	// combine small D
	smallD := discmath.NewMatrixGF256(smallDUpper.RowsNum()+smallDLower.RowsNum(), smallDUpper.ColsNum())
	smallD.SetFrom(smallDUpper, 0, 0)
	smallD.SetFrom(smallDLower, smallDUpper.RowsNum(), 0)

	smallC, err := discmath.GaussianElimination(smallA, smallD)
	if err != nil {
		if err == discmath.ErrNotSolvable {
			return nil, ErrNotEnoughSymbols
		}
		return nil, fmt.Errorf("failed to calc gauss elimination: %w", err)
	}

	c.SetFromBlock(smallC, 0, 0, c.RowsNum()-uSize, c.ColsNum(), uSize, 0)
	rowsBuf := make([]uint32, 0, aUpper.Rows)
	for row := uint32(0); row < uSize; row++ {
		for _, col := range aUpper.GetRows(rowsBuf, row) {
			if col == row {
				continue
			}
			c.RowAdd(row, c.GetRow(col))
		}
	}

	return c.ApplyPermutation(inversePermutation(colPermutation)), nil
}

func inversePermutation(mut []uint32) []uint32 {
	res := make([]uint32, len(mut))
	for i, u := range mut {
		res[u] = uint32(i)
	}
	return res
}
