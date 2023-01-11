package raptorq

import (
	"github.com/xssnick/tonutils-go/adnl/rldp/raptorq/discmath"
)

type inactivateDecoder struct {
	l            *discmath.MatrixGF256
	cols         uint32
	rows         uint32
	wasRow       []bool
	wasCol       []bool
	colCnt       []uint32
	rowCnt       []uint32
	rowXor       []uint32
	rowCntOffset []uint32
	sortedRows   []uint32
	rowPos       []uint32

	pRows        []uint32
	pCols        []uint32
	inactiveCols []uint32
}

func inactivateDecode(l *discmath.MatrixGF256, pi uint32) (side uint32, pRows, pCols []uint32) {
	cols := l.ColsNum() - pi
	rows := l.RowsNum()

	dec := &inactivateDecoder{
		l:      l,
		cols:   cols,
		rows:   rows,
		wasRow: make([]bool, rows),
		wasCol: make([]bool, cols),
		colCnt: make([]uint32, cols),
		rowCnt: make([]uint32, rows),
		rowXor: make([]uint32, rows),
	}

	l.Each(func(row, col uint32) {
		if col >= cols {
			return
		}
		dec.colCnt[col]++
		dec.rowCnt[row]++
		dec.rowXor[row] ^= col
	})

	dec.sort()
	dec.loop()

	for row := uint32(0); row < dec.rows; row++ {
		if !dec.wasRow[row] {
			dec.pRows = append(dec.pRows, row)
		}
	}

	side = uint32(len(dec.pCols))
	for i, j := 0, len(dec.inactiveCols)-1; i < j; i, j = i+1, j-1 { // reverse array
		dec.inactiveCols[i], dec.inactiveCols[j] = dec.inactiveCols[j], dec.inactiveCols[i]
	}

	for _, col := range dec.inactiveCols {
		dec.pCols = append(dec.pCols, col)
	}

	for i := uint32(0); i < pi; i++ {
		dec.pCols = append(dec.pCols, dec.cols+i)
	}

	return side, dec.pRows, dec.pCols
}

func (dec *inactivateDecoder) sort() {
	offset := make([]uint32, dec.cols+2)
	for i := uint32(0); i < dec.rows; i++ {
		offset[dec.rowCnt[i]+1]++
	}
	for i := uint32(1); i <= dec.cols+1; i++ {
		offset[i] += offset[i-1]
	}
	dec.rowCntOffset = make([]uint32, dec.rows)
	copy(dec.rowCntOffset, offset)

	dec.sortedRows = make([]uint32, dec.rows)
	dec.rowPos = make([]uint32, dec.rows)
	for i := uint32(0); i < dec.rows; i++ {
		pos := offset[dec.rowCnt[i]]
		offset[dec.rowCnt[i]]++

		dec.sortedRows[pos] = i
		dec.rowPos[i] = pos
	}
}

func (dec *inactivateDecoder) loop() {
	// loop
	for dec.rowCntOffset[1] != dec.rows {
		row := dec.sortedRows[dec.rowCntOffset[1]]
		col := dec.chooseCol(row)

		cnt := dec.rowCnt[row]
		dec.pCols = append(dec.pCols, col)
		dec.pRows = append(dec.pRows, row)

		if cnt == 1 {
			dec.inactivate(col)
		} else {
			for _, x := range dec.l.GetRows(row) {
				if x >= dec.cols || dec.wasCol[x] {
					continue
				}
				if x != col {
					dec.inactiveCols = append(dec.inactiveCols, x)
				}
				dec.inactivate(x)
			}
		}
		dec.wasRow[row] = true
	}
}

func (dec *inactivateDecoder) chooseCol(row uint32) uint32 {
	cnt := dec.rowCnt[row]
	if cnt == 1 {
		return dec.rowXor[row]
	}

	bestCol := uint32(0xFFFFFFFF)
	for _, col := range dec.l.GetRows(row) {
		if col >= dec.cols || dec.wasCol[col] {
			continue
		}
		if bestCol == 0xFFFFFFFF || dec.colCnt[col] < dec.colCnt[bestCol] {
			bestCol = col
		}
	}
	return bestCol
}

func (dec *inactivateDecoder) inactivate(col uint32) {
	dec.wasCol[col] = true
	for _, row := range dec.l.GetCols(col) {
		if dec.wasRow[row] {
			continue
		}

		pos := dec.rowPos[row]
		cnt := dec.rowCnt[row]
		offset := dec.rowCntOffset[cnt]
		dec.sortedRows[pos], dec.sortedRows[offset] = dec.sortedRows[offset], dec.sortedRows[pos]

		dec.rowPos[dec.sortedRows[pos]] = pos
		dec.rowPos[dec.sortedRows[offset]] = offset
		dec.rowCntOffset[cnt]++
		dec.rowCnt[row]--
		dec.rowXor[row] ^= col
	}
}
