package tlb

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

type BinTree struct {
	root *cell.Cell
}

func isOpaqueBinTreeLeaf(node *cell.Cell) bool {
	return node != nil && node.IsSpecial() && node.GetType() == cell.PrunedCellType
}

func (b *BinTree) LoadFromCell(loader *cell.Slice) error {
	if loader == nil {
		return fmt.Errorf("failed to load BinTree: nil slice")
	}

	root, err := loader.WithoutObserver().ToCell()
	if err != nil {
		return fmt.Errorf("failed to load BinTree root: %w", err)
	}

	b.root = root
	return nil
}

func (b *BinTree) walk(node *cell.Cell, key *cell.Builder, fn func(key *cell.Cell, value *cell.Cell) error) error {
	if node == nil {
		return fmt.Errorf("failed to walk BinTree: nil node")
	}

	if isOpaqueBinTreeLeaf(node) {
		return fn(key.EndCell(), node)
	}
	if node.IsSpecial() {
		return fmt.Errorf("failed to load BinTree node: unexpected special cell type %v", node.GetType())
	}

	sl := node.BeginParse()
	typ, err := sl.LoadUInt(1)
	if err != nil {
		return fmt.Errorf("failed to load BinTree type flag: %w", err)
	}

	if typ == 0 {
		value, err := sl.ToCell()
		if err != nil {
			return fmt.Errorf("failed to load BinTree leaf: %w", err)
		}
		return fn(key.EndCell(), value)
	}

	if sl.BitsLeft() != 0 || sl.RefsNum() != 2 {
		return fmt.Errorf("failed to load BinTree fork: invalid shape")
	}

	leftKey := key.Copy()
	if err = leftKey.StoreUInt(0, 1); err != nil {
		return fmt.Errorf("failed to build BinTree left key: %w", err)
	}
	if err = b.walk(node.MustPeekRef(0), leftKey, fn); err != nil {
		return err
	}

	rightKey := key.Copy()
	if err = rightKey.StoreUInt(1, 1); err != nil {
		return fmt.Errorf("failed to build BinTree right key: %w", err)
	}
	return b.walk(node.MustPeekRef(1), rightKey, fn)
}

func (b *BinTree) Walk(fn func(key *cell.Cell, value *cell.Cell) error) error {
	if b == nil || b.root == nil {
		return nil
	}
	if fn == nil {
		return nil
	}
	return b.walk(b.root, cell.BeginCell(), fn)
}

func (b *BinTree) Get(key *cell.Cell) *cell.Cell {
	if b == nil || b.root == nil || key == nil {
		return nil
	}

	path := key.BeginParse()
	if path.RefsNum() != 0 {
		return nil
	}
	node := b.root

	for {
		if node == nil {
			return nil
		}

		if isOpaqueBinTreeLeaf(node) {
			if path.BitsLeft() == 0 {
				return node
			}
			return nil
		}
		if node.IsSpecial() {
			return nil
		}

		sl := node.BeginParse()
		typ, err := sl.LoadUInt(1)
		if err != nil {
			return nil
		}

		if typ == 0 {
			if path.BitsLeft() != 0 {
				return nil
			}
			value, err := sl.ToCell()
			if err != nil {
				return nil
			}
			return value
		}

		if path.BitsLeft() == 0 || sl.BitsLeft() != 0 || sl.RefsNum() != 2 {
			return nil
		}

		bit, err := path.LoadUInt(1)
		if err != nil {
			return nil
		}
		node = node.MustPeekRef(int(bit))
	}
}

func (b *BinTree) All() []*cell.HashmapKV {
	if b == nil || b.root == nil {
		return nil
	}

	all := make([]*cell.HashmapKV, 0)
	if err := b.Walk(func(key *cell.Cell, value *cell.Cell) error {
		all = append(all, &cell.HashmapKV{
			Key:   key,
			Value: value,
		})
		return nil
	}); err != nil {
		return nil
	}
	return all
}
