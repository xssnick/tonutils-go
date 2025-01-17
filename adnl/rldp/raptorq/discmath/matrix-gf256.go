package discmath

type MatrixGF256 struct {
	Rows uint32
	Cols uint32
	Data []uint8
}

func NewMatrixGF256(rows, cols uint32) *MatrixGF256 {
	return &MatrixGF256{
		Rows: rows,
		Cols: cols,
		Data: make([]uint8, cols*rows),
	}
}

func (m *MatrixGF256) RowsNum() uint32 {
	return m.Rows
}

func (m *MatrixGF256) ColsNum() uint32 {
	return m.Cols
}

func (m *MatrixGF256) RowMul(i uint32, x uint8) {
	OctVecMul(m.GetRow(i), x)
}

func (m *MatrixGF256) RowAddMul(i uint32, g2 []uint8, x uint8) {
	if x == 0 {
		return
	}

	if x == 1 {
		m.RowAdd(i, g2)
		return
	}

	OctVecMulAdd(m.GetRow(i), g2, x)
}

func (m *MatrixGF256) RowAdd(i uint32, g2 []uint8) {
	OctVecAdd(m.GetRow(i), g2)
}

func (m *MatrixGF256) Set(row, col uint32, val uint8) {
	m.Data[row*m.Cols+col] = val
}

func (m *MatrixGF256) RowSet(row uint32, r []uint8) {
	copy(m.Data[row*m.Cols:(row+1)*m.Cols], r)
}

func (m *MatrixGF256) SetFrom(g *MatrixGF256, rowOffset, colOffset uint32) {
	for r := uint32(0); r < g.Rows; r++ {
		copy(m.GetRow(rowOffset + r)[colOffset:], g.GetRow(r))
	}
}

func (m *MatrixGF256) SetFromBlock(blockFrom *MatrixGF256, blockRowOffset, blockColOffset, blockRowSize, blockColSize, setRowOffset, setColOffset uint32) {
	//m.SetFrom(blockFrom.GetBlock(blockRowOffset, blockColOffset, blockRowSize, blockColSize), setRowOffset, setColOffset)
	for row := blockRowOffset; row < blockRowSize+blockRowOffset; row++ {
		copy(m.GetRow(setRowOffset + row - blockRowOffset)[setColOffset:], blockFrom.GetRow(row)[blockColOffset:blockColOffset+blockColSize])
	}
}

func (m *MatrixGF256) Get(row, col uint32) uint8 {
	return m.Data[row*m.Cols+col]
}

func (m *MatrixGF256) GetRow(row uint32) []uint8 {
	return m.Data[row*m.Cols : (row+1)*m.Cols]
}

func (m *MatrixGF256) GetBlock(rowOffset, colOffset, rowSize, colSize uint32) *MatrixGF256 {
	res := NewMatrixGF256(rowSize, colSize)
	for row := rowOffset; row < rowSize+rowOffset; row++ {
		res.RowSet(row-rowOffset, m.GetRow(row)[colOffset:])
	}
	return res
}

func (m *MatrixGF256) ApplyPermutation(permutation []uint32) *MatrixGF256 {
	res := NewMatrixGF256(m.RowsNum(), m.ColsNum())
	for row := uint32(0); row < m.RowsNum(); row++ {
		res.RowSet(row, m.GetRow(permutation[row]))
	}
	return res
}

func (m *MatrixGF256) MulSparse(s *MatrixGF256) *MatrixGF256 {
	mg := NewMatrixGF256(s.RowsNum(), m.ColsNum())
	for i, val := range s.Data {
		if val != 0 {
			row := uint32(i) / s.Cols
			col := uint32(i) % s.Cols

			mg.RowAdd(row, m.GetRow(col))
		}
	}
	return mg
}

func (m *MatrixGF256) Add(s *MatrixGF256) *MatrixGF256 {
	for i := uint32(0); i < s.RowsNum(); i++ {
		m.RowAdd(i, s.GetRow(i))
	}
	return m
}

func (m *MatrixGF256) ToGF2(rowFrom, colFrom, rowSize, colSize uint32) *PlainMatrixGF2 {
	mGF2 := NewPlainMatrixGF2(rowSize, colSize)
	for i, val := range m.Data {
		if val != 0 {
			row := uint32(i) / m.Cols
			col := uint32(i) % m.Cols

			if (row >= rowFrom && row < rowFrom+rowSize) &&
				(col >= colFrom && col < colFrom+colSize) {
				mGF2.Set(row-rowFrom, col-colFrom)
			}
		}
	}
	return mGF2
}

func (m *MatrixGF256) GetCols(buf []uint32, col uint32) []uint32 {
	buf = buf[:0]
	for i := uint32(0); i < m.Rows; i++ {
		if c := m.Get(i, col); c == 1 {
			buf = append(buf, i)
		}
	}
	return buf
}

func (m *MatrixGF256) GetRows(buf []uint32, row uint32) []uint32 {
	buf = buf[:0]
	for i, v := range m.GetRow(row) {
		if v == 1 {
			buf = append(buf, uint32(i))
		}
	}
	return buf
}
