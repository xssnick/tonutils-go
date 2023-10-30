package discmath

import (
	"fmt"
	"math/bits"
	"strings"
)

type MatrixGF2 struct {
	rows [][]uint8
}

func NewMatrixGF2(rows, cols uint32) *MatrixGF2 {
	// defaultMetrics.store(rows, cols) // only for tests

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

// PlainMatrixGF2 is a matrix of GF2 elements
// Data is stored in a byte array, each byte is contained 8 elements (bits)
type PlainMatrixGF2 struct {
	rows, cols uint32
	data       []byte
}

const (
	// elSize is a size of array's element in bits
	elSize = 8
	maxEl  = 1<<elSize - 1
)

func NewPlainMatrixGF2(rows, cols uint32) *PlainMatrixGF2 {
	// defaultMetrics.store(rows, cols) // only for tests

	matrixSize := rows * cols

	dataSize := matrixSize / elSize
	if matrixSize%elSize > 0 {
		dataSize++
	}

	return &PlainMatrixGF2{
		rows: rows,
		cols: cols,
		data: make([]byte, dataSize),
	}
}

func (m *PlainMatrixGF2) RowsNum() uint32 {
	return m.rows
}

func (m *PlainMatrixGF2) ColsNum() uint32 {
	return m.cols
}

func (m *PlainMatrixGF2) Get(row, col uint32) byte {
	idx, bit := m.getIdx(row, col)

	res := m.data[idx] & bit
	if res > 0 {
		return 1
	}

	return 0
}

func (m *PlainMatrixGF2) Set(row, col uint32) {
	idx, bit := m.getIdx(row, col)
	m.data[idx] |= bit
}

func (m *PlainMatrixGF2) Unset(row, col uint32) {
	idx, bit := m.getIdx(row, col)
	m.data[idx] &= ^bit
}

// GetRow returns row data, bit mask for the first byte and bit mask for the last byte
// Bitmask contains 1 for each bit that is part of the row
func (m *PlainMatrixGF2) GetRow(row uint32) ([]byte, byte, byte) {
	firstBitIdx := row * m.cols
	firstElIdx := firstBitIdx / elSize

	lastBitIdx := (row+1)*m.cols - 1
	lastElIdx := lastBitIdx / elSize

	firstBitmask := byte(maxEl - (1 << (firstBitIdx % elSize)) + 1)
	lastBitmask := byte((1 << (lastBitIdx%elSize + 1)) - 1)

	if firstElIdx == lastElIdx {
		firstBitmask &= lastBitmask
		lastBitmask = firstBitmask
	}

	if int(lastElIdx) == len(m.data) {
		return m.data[firstElIdx:], firstBitmask, lastBitmask
	}

	return m.data[firstElIdx : lastElIdx+1], firstBitmask, lastBitmask
}

func (m *PlainMatrixGF2) RowAdd(row uint32, what []byte, firstBitmask, lastBitmask byte) {
	col := uint32(0)
	for i, whatByte := range what {
		// compute bitmask of "what" row for current byte
		bitmask := byte(maxEl)
		if i == 0 {
			bitmask = firstBitmask
		} else if i == len(what)-1 {
			bitmask = lastBitmask
		}

		for j := bits.TrailingZeros8(bitmask); j < elSize-bits.LeadingZeros8(bitmask); j++ {
			whatBit := (whatByte & (1 << j)) >> j
			colBit := m.Get(row, col)

			if whatBit != colBit {
				m.Set(row, col)
			} else {
				m.Unset(row, col)
			}

			col++
		}
	}
}

func (m *PlainMatrixGF2) Mul(s *MatrixGF256) *PlainMatrixGF2 {
	mg := NewPlainMatrixGF2(s.RowsNum(), m.ColsNum())

	s.Each(func(row, col uint32) {
		rowData, firstBitmask, lastBitmask := m.GetRow(col)
		mg.RowAdd(row, rowData, firstBitmask, lastBitmask)
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
	for i := uint32(0); i < m.rows; i++ {
		var cols []string
		for j := uint32(0); j < m.cols; j++ {
			cols = append(cols, fmt.Sprintf("%02x", m.Get(i, j)))
		}

		rows = append(rows, strings.Join(cols, " "))
	}

	return strings.Join(rows, "\n")
}

// getRowUInt8 returns row data as array of uint8
func (m *PlainMatrixGF2) getRowUInt8(row uint32) []uint8 {
	rowUInt8 := make([]uint8, m.cols)
	for i := uint32(0); i < m.cols; i++ {
		rowUInt8[i] = m.Get(row, i)
	}

	return rowUInt8
}

// getIdx returns index in array of bytes and offset in this byte
func (m *PlainMatrixGF2) getIdx(row, col uint32) (uint32, byte) {
	idx := row*m.cols + col
	bitIdx := byte(idx % elSize)

	return idx / elSize, byte(1 << bitIdx)
}
