package discmath

import (
	"fmt"
	"strings"
)

type MatrixGF2 struct {
	rows [][]uint8
}

func NewMatrixGF2(rows, cols uint32) *MatrixGF2 {
	data := make([][]uint8, rows)
	for i := range data {
		data[i] = make([]uint8, cols)
	}

	return &MatrixGF2{
		rows: data,
	}
}

func (m *MatrixGF2) RowsNum() uint32 {
	return uint32(len(m.rows))
}

func (m *MatrixGF2) ColsNum() uint32 {
	return uint32(len(m.rows[0]))
}

func (m *MatrixGF2) RowAdd(row uint32, what []uint8) {
	for i, u := range what {
		if u > 1 {
			panic("not GF2 data")
		}
		m.rows[row][i] ^= u
	}
}

func (m *MatrixGF2) Set(row, col uint32) {
	m.rows[row][col] = 1
}

func (m *MatrixGF2) Unset(row, col uint32) {
	m.rows[row][col] = 0
}

func (m *MatrixGF2) Get(row, col uint32) bool {
	return m.rows[row][col] > 0
}

func (m *MatrixGF2) GetRow(row uint32) []uint8 {
	return m.rows[row]
}

func (m *MatrixGF2) Mul(s *SparseMatrixGF2) *MatrixGF2 {
	mg := NewMatrixGF2(s.RowsNum(), m.ColsNum())
	s.Each(func(row, col uint32) {
		mg.RowAdd(row, m.GetRow(col))
	})
	return mg
}

func (m *MatrixGF2) ToGF256() *MatrixGF256 {
	mg := NewMatrixGF256(m.RowsNum(), m.ColsNum())
	for i, v := range m.rows {
		mg.rows[i] = GF256{v} // GF256FromGF2(v, ((m.ColsNum()+7)/8+3)/4*4)
	}

	/*
		for (size_t i = 0; i < rows(); i++) {
		      Simd::gf256_from_gf2(res.row(i).data(), row(i).data(), ((cols_ + 7) / 8 + 3) / 4 * 4);
		    }
	*/
	return mg
}

func (m *MatrixGF2) String() string {
	var rows []string
	for _, r := range m.rows {
		var cols []string
		for _, c := range r {
			cols = append(cols, fmt.Sprintf("%02x", c))
		}
		rows = append(rows, strings.Join(cols, " "))
	}
	return strings.Join(rows, "\n")
}
