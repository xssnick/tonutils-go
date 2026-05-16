package cell

import "fmt"

func loadSingleRefValue(value *Slice) (*Cell, error) {
	if value == nil {
		return nil, ErrNoSuchKeyInDict
	}
	if value.BitsLeft() != 0 || value.RefsNum() != 1 {
		return nil, fmt.Errorf("value is not a single ref")
	}
	return value.PeekRefCell()
}
