package discmath

import "sort"

type exists struct{}

type SparseMatrixGF2 struct {
	data map[uint64]exists
	rows uint32
	cols uint32
}

func NewSparseMatrixGF2(rows, cols uint32) *SparseMatrixGF2 {
	return &SparseMatrixGF2{
		data: map[uint64]exists{},
		rows: rows,
		cols: cols,
	}
}

func (s *SparseMatrixGF2) Set(row, col uint32) {
	if row >= s.rows {
		panic("row is out of range")
	}
	if col >= s.cols {
		panic("col is out of range")
	}

	s.data[(uint64(row)<<32)|uint64(col)] = exists{}
}

func (s *SparseMatrixGF2) NonZeroes() int {
	return len(s.data)
}

func (s *SparseMatrixGF2) ColsNum() uint32 {
	return s.cols
}

func (s *SparseMatrixGF2) RowsNum() uint32 {
	return s.rows
}

func (s *SparseMatrixGF2) Transpose() *SparseMatrixGF2 {
	m := NewSparseMatrixGF2(s.cols, s.rows)
	for k := range s.data {
		row := k >> 32
		col := (k << 32) >> 32
		m.data[(row<<32)|col] = exists{}
	}
	return m
}

func (s *SparseMatrixGF2) Each(f func(row, col uint32)) {
	for k := range s.data {
		row := k >> 32
		col := (k << 32) >> 32
		f(uint32(row), uint32(col))
	}
}

func (s *SparseMatrixGF2) GetCols(row uint32) (cols []uint32) {
	for k := range s.data {
		if ((k << 32) >> 32) == uint64(row) {
			col := k >> 32
			cols = append(cols, uint32(col))
		}
	}
	sort.Slice(cols, func(i, j int) bool { return cols[i] < cols[j] })
	return cols
}

func (s *SparseMatrixGF2) GetRows(col uint32) (rows []uint32) {
	for k := range s.data {
		if (k >> 32) == uint64(col) {
			row := (k << 32) >> 32
			rows = append(rows, uint32(row))
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i] < rows[j] })
	return rows
}

func (s *SparseMatrixGF2) ApplyRowsPermutation(permutation []uint32) *SparseMatrixGF2 {
	permutation = InversePermutation(permutation)

	res := NewSparseMatrixGF2(s.rows, s.cols)
	s.Each(func(row, col uint32) {
		res.Set(permutation[row], col)
	})
	return res
}

func (s *SparseMatrixGF2) ApplyColsPermutation(permutation []uint32) *SparseMatrixGF2 {
	permutation = InversePermutation(permutation)

	res := NewSparseMatrixGF2(s.rows, s.cols)
	s.Each(func(row, col uint32) {
		res.Set(row, permutation[col])
	})
	return res
}

func (s *SparseMatrixGF2) ToDense(rowFrom, colFrom, rowSize, colSize uint32) *PlainOffsetMatrixGF2 {
	m := NewPlainOffsetMatrixGF2(rowSize, colSize)
	s.Each(func(row, col uint32) {
		if (row >= rowFrom && row < rowFrom+rowSize) &&
			(col >= colFrom && col < colFrom+colSize) {
			m.Set(row-rowFrom, col-colFrom)
		}
	})
	return m
}

func (s *SparseMatrixGF2) GetBlock(rowOffset, colOffset, rowSize, colSize uint32) *SparseMatrixGF2 {
	res := NewSparseMatrixGF2(rowSize, colSize)
	s.Each(func(row, col uint32) {
		if (row >= rowOffset && row < rowSize+rowOffset) &&
			(col >= colOffset && col < colSize+colOffset) {
			res.Set(row-rowOffset, col-colOffset)
		}
	})
	return res
}

func InversePermutation(mut []uint32) []uint32 {
	res := make([]uint32, len(mut))
	for i, u := range mut {
		res[u] = uint32(i)
	}
	return res
}
