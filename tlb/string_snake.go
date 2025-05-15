package tlb

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
)

//nolint:recvcheck
type StringSnake struct {
	Value string
}

func (s *StringSnake) LoadFromCell(loader *cell.Slice) error {
	str, err := loader.LoadStringSnake()
	if err != nil {
		return err
	}

	s.Value = str

	return nil
}

// non-pointer receiver is ok here.
func (s StringSnake) ToCell() (*cell.Cell, error) {
	c := cell.BeginCell()
	err := c.StoreStringSnake(s.Value)
	if err != nil {
		return nil, err
	}

	return c.EndCell(), nil
}
