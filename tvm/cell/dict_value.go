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

func refValueBuilder(value *Cell) (*Builder, error) {
	if value == nil {
		return nil, fmt.Errorf("value ref is nil")
	}

	b := BeginCell()
	if err := b.StoreRef(value); err != nil {
		return nil, err
	}
	return b, nil
}
