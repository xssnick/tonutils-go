package tlb

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

const MaxTextChunkSize = 127 - 2

type Text struct {
	MaxFirstChunkSize uint8
	Value             string
}

func (t *Text) LoadFromCell(loader *cell.Slice) error {
	num, err := loader.LoadUInt(8)
	if err != nil {
		return fmt.Errorf("failed to load chunks num: %w", err)
	}

	firstSz := uint8(0)
	var res string
	for i := 0; i < int(num); i++ {
		ln, err := loader.LoadUInt(8)
		if err != nil {
			return fmt.Errorf("failed to load len of chunk %d: %w", i, err)
		}

		if i == 0 {
			firstSz = uint8(ln)
		}

		data, err := loader.LoadSlice(uint(ln * 8))
		if err != nil {
			return fmt.Errorf("failed to load data of chunk %d: %w", i, err)
		}
		res += string(data)

		if i < int(num)-1 {
			loader, err = loader.LoadRef()
			if err != nil {
				return fmt.Errorf("failed to load next chunk of chunk %d: %w", i, err)
			}
		}
	}

	t.Value = res
	t.MaxFirstChunkSize = firstSz
	return nil
}

func (t Text) ToCell() (*cell.Cell, error) {
	if len(t.Value) == 0 {
		return cell.BeginCell().MustStoreUInt(0, 8).EndCell(), nil
	}

	if t.MaxFirstChunkSize > MaxTextChunkSize {
		return nil, fmt.Errorf("too big first chunk size")
	}
	if t.MaxFirstChunkSize == 0 {
		return nil, fmt.Errorf("first chunk size should be > 0")
	}

	val := []byte(t.Value)
	leftSz := len(val) - int(t.MaxFirstChunkSize)
	chunksNum := 1
	if leftSz > 0 {
		chunksNum += leftSz / MaxTextChunkSize
		if leftSz%MaxTextChunkSize > 0 {
			chunksNum++
		}
	}

	if chunksNum > 255 {
		return nil, fmt.Errorf("too big data")
	}

	var f func(depth int) *cell.Builder
	f = func(depth int) *cell.Builder {
		c := cell.BeginCell()
		sz := uint8(MaxTextChunkSize)
		if depth == 0 {
			sz = t.MaxFirstChunkSize
		}
		if int(sz) > len(val) {
			sz = uint8(len(val))
		}

		c.MustStoreUInt(uint64(sz), 8)
		c.MustStoreSlice(val[:sz], uint(sz)*8)
		val = val[sz:]

		if depth != chunksNum-1 {
			c.MustStoreRef(f(depth + 1).EndCell())
		}
		return c
	}

	return cell.BeginCell().
		MustStoreUInt(uint64(chunksNum), 8).
		MustStoreBuilder(f(0)).
		EndCell(), nil
}
