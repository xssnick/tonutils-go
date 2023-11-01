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

func (m *MatrixGF2) Mul(s *MatrixGF256) *MatrixGF2 {
	mg := NewMatrixGF2(s.RowsNum(), m.ColsNum())
	s.Each(func(row, col uint32) {
		mg.RowAdd(row, m.GetRow(col))
	})
	return mg
}

func (m *MatrixGF2) ToGF256() *MatrixGF256 {
	mg := NewMatrixGF256(m.RowsNum(), m.ColsNum())
	for i, v := range m.rows {
		mg.rows[i] = GF256{v}
	}

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

// elSize is a size of array's element in bits
const elSize = 8

type PlainMatrixGF2 struct {
	rows, cols uint32
	rowSize    uint32
	data       []byte
}

func NewPlainMatrixGF2(rows, cols uint32) *PlainMatrixGF2 {
	rowSize := cols / elSize
	if cols%elSize > 0 {
		rowSize++
	}

	data := make([]byte, rows*rowSize)

	return &PlainMatrixGF2{
		rows:    rows,
		cols:    cols,
		rowSize: rowSize,
		data:    data,
	}
}

func (m *PlainMatrixGF2) RowsNum() uint32 {
	return m.rows
}

func (m *PlainMatrixGF2) ColsNum() uint32 {
	return m.cols
}

func (m *PlainMatrixGF2) Get(row, col uint32) byte {
	return m.getElement(row, col)
}

func (m *PlainMatrixGF2) Set(row, col uint32) {
	elIdx, colIdx := m.getElementPosition(row, col)
	m.data[elIdx] |= 1 << colIdx
}

func (m *PlainMatrixGF2) Unset(row, col uint32) {
	elIdx, colIdx := m.getElementPosition(row, col)
	m.data[elIdx] &= ^(1 << colIdx)
}

func (m *PlainMatrixGF2) GetRow(row uint32) []byte {
	firstElIdx, _ := m.getElementPosition(row, 0)
	lastElIdx := firstElIdx + (m.cols-1)/elSize + 1

	return m.data[firstElIdx:lastElIdx]
}

func (m *PlainMatrixGF2) RowAdd(row uint32, what []byte) {
	firstElIdx, _ := m.getElementPosition(row, 0)
	for i, whatByte := range what {
		m.data[firstElIdx+uint32(i)] ^= whatByte
	}
}

func (m *PlainMatrixGF2) Mul(s *MatrixGF256) *PlainMatrixGF2 {
	mg := NewPlainMatrixGF2(s.RowsNum(), m.ColsNum())

	s.Each(func(row, col uint32) {
		mRow := m.GetRow(col)
		mg.RowAdd(row, mRow)
	})

	return mg
}

func (m *PlainMatrixGF2) ToGF256() *MatrixGF256 {
	mg := NewMatrixGF256(m.RowsNum(), m.ColsNum())

	for i := uint32(0); i < m.rows; i++ {
		mg.rows[i] = GF256{data: m.getRowUInt8(i)}
	}

	return mg
}

func (m *PlainMatrixGF2) String() string {
	var rows []string
	for row := uint32(0); row < m.rows; row++ {
		var cols []string
		for col := uint32(0); col < m.cols; col++ {
			cols = append(cols, fmt.Sprintf("%02x", m.getElement(row, col)))
		}

		rows = append(rows, strings.Join(cols, " "))
	}

	return strings.Join(rows, "\n")
}

func (m *PlainMatrixGF2) getRowUInt8(row uint32) []uint8 {
	result := make([]uint8, m.cols)
	for col := uint32(0); col < m.cols; col++ {
		result[col] = m.getElement(row, col)
	}

	return result
}

// getElement returns element in matrix by row and col. Possible values: 0 or 1
func (m *PlainMatrixGF2) getElement(row, col uint32) byte {
	elIdx, colIdx := m.getElementPosition(row, col)

	return (m.data[elIdx] & (1 << colIdx)) >> colIdx
}

// getElementPosition returns index of element in array and offset in this element
func (m *PlainMatrixGF2) getElementPosition(row, col uint32) (uint32, byte) {
	return (row * m.rowSize) + col/elSize, byte(col % elSize)
}
