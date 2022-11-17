package discmath

import (
	"fmt"
	"strings"
)

type MatrixGF256 struct {
	rows []GF256
}

func NewMatrixGF256(rows, cols uint32) *MatrixGF256 {
	data := make([]GF256, rows)
	for i := range data {
		data[i].data = make([]uint8, cols)
	}

	return &MatrixGF256{
		rows: data,
	}
}

func (m *MatrixGF256) RowsNum() uint32 {
	return uint32(len(m.rows))
}

func (m *MatrixGF256) ColsNum() uint32 {
	return uint32(len(m.rows[0].data))
}

func (m *MatrixGF256) RowMul(i uint32, x uint8) {
	m.rows[i].Mul(x)
}

func (m *MatrixGF256) RowAddMul(i uint32, g2 *GF256, x uint8) {
	if x == 0 {
		return
	}

	if x == 1 {
		m.RowAdd(i, g2)
		return
	}

	m.rows[i].AddMul(g2, x)
}

func (m *MatrixGF256) RowAdd(i uint32, g2 *GF256) {
	m.rows[i].Add(g2)
}

func (m *MatrixGF256) Set(row, col uint32, val uint8) {
	m.rows[row].data[col] = val
}

func (m *MatrixGF256) RowSet(row uint32, r *GF256) {
	g := GF256{data: make([]uint8, len(r.data))}
	copy(g.data, r.data)
	m.rows[row] = g
}

func (m *MatrixGF256) SetFrom(g *MatrixGF256, rowOffset, colOffset uint32) {
	for i, row := range g.rows {
		copy(m.rows[rowOffset+uint32(i)].data[colOffset:], row.data)
	}
}

func (m *MatrixGF256) Get(row, col uint32) uint8 {
	return m.rows[row].data[col]
}

func (m *MatrixGF256) GetRow(row uint32) *GF256 {
	return &m.rows[row]
}

func (m *MatrixGF256) GetBlock(rowOffset, colOffset, rowSize, colSize uint32) *MatrixGF256 {
	res := NewMatrixGF256(rowSize, colSize)
	for row := rowOffset; row < rowSize+rowOffset; row++ {
		col := make([]uint8, colSize)
		copy(col, m.rows[row].data[colOffset:])

		res.rows[row-rowOffset] = GF256{col}
	}
	return res
}

func (m *MatrixGF256) ApplyPermutation(permutation []uint32) *MatrixGF256 {
	res := NewMatrixGF256(m.RowsNum(), m.ColsNum())
	for row := uint32(0); row < m.RowsNum(); row++ {
		res.rows[row] = m.rows[permutation[row]]
	}
	return res
}

func (m *MatrixGF256) MulSparse(s *SparseMatrixGF2) *MatrixGF256 {
	mg := NewMatrixGF256(s.RowsNum(), m.ColsNum())
	s.Each(func(row, col uint32) {
		mg.RowAdd(row, m.GetRow(col))
	})
	return mg
}

func (m *MatrixGF256) Add(s *MatrixGF256) *MatrixGF256 {
	mg := m.Copy()
	for i := uint32(0); i < s.RowsNum(); i++ {
		mg.RowAdd(i, s.GetRow(i))
	}
	return mg
}

func (m *MatrixGF256) Copy() *MatrixGF256 {
	mg := NewMatrixGF256(m.RowsNum(), m.ColsNum())
	for i, row := range m.rows {
		mg.RowSet(uint32(i), &row)
	}
	return mg
}

func (m *MatrixGF256) String() string {
	var rows []string
	for _, r := range m.rows {
		var cols []string
		for _, c := range r.data {
			cols = append(cols, fmt.Sprintf("%02x", c))
		}
		rows = append(rows, strings.Join(cols, " "))
	}
	return strings.Join(rows, "\n")
}
