package tlb

import (
	"encoding/hex"
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

type BinTree struct {
	storage map[string]*cell.HashmapKV
}

func (b *BinTree) LoadFromCell(loader *cell.Slice) error {
	b.storage = map[string]*cell.HashmapKV{}
	var jumper func(next *cell.Slice, key *cell.Builder) error
	jumper = func(next *cell.Slice, key *cell.Builder) error {
		typ, err := next.LoadUInt(1)
		if err != nil {
			return fmt.Errorf("failed to load type flag: %w", err)
		}

		if typ == 0 {
			finalKey := key.EndCell()
			b.storage[hex.EncodeToString(finalKey.Hash())] = &cell.HashmapKV{
				Key:   finalKey,
				Value: next.MustToCell(),
			}
			return nil
		}

		left, err := next.LoadRef()
		if err != nil {
			return fmt.Errorf("failed to load left flag: %w", err)
		}
		leftKey := key.Copy()
		if err = leftKey.StoreUInt(0, 1); err != nil {
			return fmt.Errorf("failed to store left key: %w", err)
		}
		if err := jumper(left, leftKey); err != nil {
			return err
		}

		right, err := next.LoadRef()
		if err != nil {
			return fmt.Errorf("failed to load right flag: %w", err)
		}
		if err = key.StoreUInt(1, 1); err != nil {
			return fmt.Errorf("failed to store right key: %w", err)
		}
		if err := jumper(right, key); err != nil {
			return err
		}

		return nil
	}

	return jumper(loader, cell.BeginCell())
}

func (b *BinTree) Get(key *cell.Cell) *cell.Cell {
	return b.storage[hex.EncodeToString(key.Hash())].Value
}

func (b *BinTree) All() []*cell.HashmapKV {
	all := make([]*cell.HashmapKV, 0, len(b.storage))
	for _, v := range b.storage {
		all = append(all, v)
	}

	return all
}
