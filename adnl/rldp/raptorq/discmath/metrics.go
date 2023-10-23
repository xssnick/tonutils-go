package discmath

import "fmt"

var defaultMetrics *metrics

func init() {
	defaultMetrics = newMetrics()
}

type metrics struct {
	isFirst bool
	minRows uint32
	maxRows uint32
	avgRows uint32
	minCols uint32
	maxCols uint32
	avgCols uint32
	minSize uint32
	maxSize uint32
	avgSize uint32
}

func newMetrics() *metrics {
	return &metrics{}
}

func (m *metrics) store(rows, cols uint32) {
	size := rows * cols

	if m.isFirst {
		m.isFirst = false
		m.minRows = rows
		m.maxRows = rows
		m.avgRows = rows
		m.minCols = cols
		m.maxCols = cols
		m.avgCols = cols
		m.minSize = size
		m.maxSize = size
		m.avgSize = size

		return
	}

	if m.minRows > rows {
		m.minRows = rows
	}

	if m.maxRows < rows {
		m.maxRows = rows
	}

	m.avgRows = (m.avgRows + rows) / 2

	if m.minCols > cols {
		m.minCols = cols
	}

	if m.maxCols < cols {
		m.maxCols = cols
	}

	m.avgCols = (m.avgCols + cols) / 2

	if m.minSize > size {
		m.minSize = size
	}

	if m.maxSize < size {
		m.maxSize = size
	}

	m.avgSize = (m.avgSize + size) / 2
}

func (m *metrics) String() string {
	return fmt.Sprintf(
		"\n--- Rows ---\nmin=%d max=%d avg=%d\n--- Cols ---\nmin=%d max=%d avg=%d\n--- Size ---\nmin=%d max=%d avg=%d\n",
		m.minRows,
		m.maxRows,
		m.avgRows,
		m.minCols,
		m.maxCols,
		m.avgCols,
		m.minSize,
		m.maxSize,
		m.avgSize,
	)
}

func GetMetrics() string {
	return defaultMetrics.String()
}
