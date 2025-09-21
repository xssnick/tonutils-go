package tlb

import (
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/address"
	"math/big"
	"reflect"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

var ErrStackEmpty = errors.New("stack is empty")

type Stack struct {
	top *StackElement
}

type StackElement struct {
	value any
	next  *StackElement
}

type StackNaN struct{}

func NewStack() *Stack {
	return &Stack{}
}

func (s *Stack) Depth() uint {
	var depth uint
	v := s.top
	for v != nil {
		depth++
		v = v.next
	}
	return depth
}

func (s *Stack) Push(obj any) {
	s.top = &StackElement{
		value: obj,
		next:  s.top,
	}
}

func (s *Stack) Pop() (any, error) {
	if s.top == nil {
		return nil, ErrStackEmpty
	}

	val := s.top.value
	s.top = s.top.next

	return val, nil
}

func (s *Stack) ToCell() (*cell.Cell, error) {
	var unwrap []*StackElement
	elem := s.top
	for elem != nil {
		unwrap = append(unwrap, elem)
		elem = elem.next
	}

	root := cell.BeginCell()
	root.MustStoreUInt(uint64(len(unwrap)), 24) // depth

	if len(unwrap) == 0 {
		return root.EndCell(), nil
	}

	next := cell.BeginCell()
	for i := 0; i < len(unwrap); i++ {
		b := cell.BeginCell()
		b.MustStoreRef(next.EndCell())

		if err := SerializeStackValue(b, unwrap[i].value); err != nil {
			return nil, fmt.Errorf("faled to serialize %d stack element: %w", i, err)
		}

		next = b
	}

	return root.MustStoreBuilder(next).EndCell(), nil
}

func (s *Stack) LoadFromCell(loader *cell.Slice) error {
	depth, err := loader.LoadUInt(24)
	if err != nil {
		return fmt.Errorf("failed to load depth, err: %w", err)
	}

	// reset stack
	s.top = nil

	next := loader
	for i := uint64(0); i < depth; i++ {
		ref, err := next.LoadRef()
		if err != nil {
			return fmt.Errorf("failed to load stack next ref, err: %w", err)
		}

		val, err := ParseStackValue(next)
		if err != nil {
			return fmt.Errorf("failed to parse stack value, err: %w", err)
		}

		s.Push(val)

		next = ref
	}

	return nil
}

func SerializeStackValue(b *cell.Builder, val any) error {
	if vl, ok := val.(*big.Int); ok {
		if vl.BitLen() < 64 {
			val = vl.Int64()
		}
	}

	// address is often used as a value, but it was not obvious
	// that it should be a slice, so we convert it internally
	if addr, ok := val.(*address.Address); ok {
		ab := cell.BeginCell()
		if err := ab.StoreAddr(addr); err != nil {
			return fmt.Errorf("failed to store address: %w", err)
		}
		val = ab.ToSlice()
	}

	switch v := val.(type) {
	case nil:
		b.MustStoreUInt(0x00, 8)
	case int, int8, int16, int32, int64, uint8, uint16, uint32:
		b.MustStoreUInt(0x01, 8)

		// cast to int64
		vl := reflect.ValueOf(v).Convert(reflect.TypeOf(int64(0))).Interface().(int64)
		b.MustStoreInt(vl, 64)
	case uint, uint64, *big.Int:
		// https://github.com/ton-blockchain/ton/blob/24dc184a2ea67f9c47042b4104bbb4d82289fac1/crypto/vm/stack.cpp#L739
		b.MustStoreUInt(0x0200/2, 15)

		var bi *big.Int
		switch vv := v.(type) {
		case uint64:
			bi = new(big.Int).SetUint64(vv)
		case uint:
			bi = new(big.Int).SetUint64(uint64(vv))
		case *big.Int:
			bi = vv
		}

		b.MustStoreBigInt(bi, 257)
	case StackNaN, *StackNaN:
		b.MustStoreSlice([]byte{0x02, 0xFF}, 16)
	case *cell.Cell:
		b.MustStoreUInt(0x03, 8)
		b.MustStoreRef(v)
	case *cell.Slice:
		b.MustStoreUInt(0x04, 8)

		// start data offset
		b.MustStoreUInt(0, 10)
		// end data offset
		b.MustStoreUInt(uint64(v.BitsLeft()), 10)

		// start refs offset
		b.MustStoreUInt(0, 3)
		// end refs offset
		b.MustStoreUInt(uint64(v.RefsNum()), 3)

		b.MustStoreRef(v.MustToCell())
	case *cell.Builder:
		b.MustStoreUInt(0x05, 8)
		b.MustStoreRef(v.EndCell())
	case []any:
		b.MustStoreUInt(0x07, 8)
		b.MustStoreUInt(uint64(len(v)), 16)

		var dive func(b *cell.Builder, i int) error
		dive = func(b *cell.Builder, i int) error {
			if i < 0 {
				return nil
			}

			if i > 1 {
				n := cell.BeginCell()
				if err := dive(n, i-1); err != nil {
					return err
				}
				b.MustStoreRef(n.EndCell())
			} else if i == 1 {
				n2 := cell.BeginCell()
				if err := SerializeStackValue(n2, v[i-1]); err != nil {
					return fmt.Errorf("faled to serialize tuple %d element: %w", i-1, err)
				}
				b.MustStoreRef(n2.EndCell())
			}

			n2 := cell.BeginCell()
			if err := SerializeStackValue(n2, v[i]); err != nil {
				return fmt.Errorf("faled to serialize tuple %d element: %w", i, err)
			}
			b.MustStoreRef(n2.EndCell())

			return nil
		}

		if err := dive(b, len(v)-1); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown type")
	}
	return nil
}

func ParseStackValue(slice *cell.Slice) (any, error) {
	typ, err := slice.LoadUInt(8)
	if err != nil {
		return nil, fmt.Errorf("failed to load stack value type, err: %w", err)
	}

	switch typ {
	case 0x00:
		return nil, nil
	case 0x01:
		val, err := slice.LoadBigInt(64)
		if err != nil {
			return nil, fmt.Errorf("failed to load tiny int stack value, err: %w", err)
		}
		return val, nil
	case 0x02:
		subTyp, err := slice.LoadUInt(8)
		if err != nil {
			return nil, fmt.Errorf("failed to load stack value sub type, err: %w", err)
		}

		switch subTyp {
		case 0xFF:
			return StackNaN{}, nil
		default:
			bInt, err := slice.LoadBigUInt(256)
			if err != nil {
				return nil, fmt.Errorf("failed to load stack value big int, err: %w", err)
			}

			// 1st bit of int257 indicates sign, it is loaded in type
			if subTyp > 0 {
				bInt.Mul(bInt, big.NewInt(-1))
			}

			return bInt, nil
		}
	case 0x03:
		val, err := slice.LoadRef()
		if err != nil {
			return nil, fmt.Errorf("failed to load cell stack value, err: %w", err)
		}
		return val.MustToCell(), nil
	case 0x04:
		start, err := slice.LoadUInt(10)
		if err != nil {
			return nil, fmt.Errorf("failed to load slice stack value's start, err: %w", err)
		}
		end, err := slice.LoadUInt(10)
		if err != nil {
			return nil, fmt.Errorf("failed to load slice stack value's end, err: %w", err)
		}
		if start > end {
			return nil, fmt.Errorf("start index > end index")
		}

		startRef, err := slice.LoadUInt(3)
		if err != nil {
			return nil, fmt.Errorf("failed to load slice stack value's start ref, err: %w", err)
		}
		endRef, err := slice.LoadUInt(3)
		if err != nil {
			return nil, fmt.Errorf("failed to load slice stack value's end ref, err: %w", err)
		}
		if startRef > endRef {
			return nil, fmt.Errorf("start ref index > end ref index")
		}

		val, err := slice.LoadRef()
		if err != nil {
			return nil, fmt.Errorf("failed to load cell stack value, err: %w", err)
		}

		cl := cell.BeginCell()

		if start > 0 {
			_, err = val.LoadSlice(uint(start))
			if err != nil {
				return nil, fmt.Errorf("load prefix err: %w", err)
			}
		}

		if end > 0 {
			sz := uint(end - start)
			data, err := val.LoadSlice(sz)
			if err != nil {
				return nil, fmt.Errorf("load prefix err: %w", err)
			}

			err = cl.StoreSlice(data, sz)
			if err != nil {
				return nil, fmt.Errorf("store slice err: %w", err)
			}
		}

		for x := uint64(0); x < startRef; x++ {
			_, err := val.LoadRef()
			if err != nil {
				return nil, fmt.Errorf("failed to load slice stack value's ref, err: %w", err)
			}
		}

		for x := uint64(0); x < endRef-startRef; x++ {
			sliceRef, err := val.LoadRef()
			if err != nil {
				return nil, fmt.Errorf("failed to load slice stack value's ref, err: %w", err)
			}

			err = cl.StoreRef(sliceRef.MustToCell())
			if err != nil {
				return nil, fmt.Errorf("failed to store slice stack value's ref, err: %w", err)
			}
		}
		return cl.EndCell().BeginParse(), nil
	case 0x05:
		val, err := slice.LoadRef()
		if err != nil {
			return nil, fmt.Errorf("failed to load cell stack value, err: %w", err)
		}
		return val.MustToCell().ToBuilder(), nil
	case 0x07:
		ln, err := slice.LoadUInt(16)
		if err != nil {
			return nil, fmt.Errorf("failed to load tuple stack value's len, err: %w", err)
		}

		var tuple []any

		last2index := int(ln) - 2
		if last2index < 0 {
			last2index = 0
		}

		var dive func(i int, root *cell.Slice) error
		dive = func(i int, root *cell.Slice) error {
			if i == last2index {
				// load first one
				err = dive(i+1, root)
				if err != nil {
					return err
				}
			} else if i < last2index {
				next, err := root.LoadRef()
				if err != nil {
					return fmt.Errorf("failed to load tuple's %d next element, err: %w", i, err)
				}

				err = dive(i+1, next)
				if err != nil {
					return err
				}
			}

			if root.RefsNum() == 0 {
				return nil
			}

			ref, err := root.LoadRef()
			if err != nil {
				return fmt.Errorf("failed to load tuple's %d ref, err: %w", i, err)
			}

			val, err := ParseStackValue(ref)
			if err != nil {
				return fmt.Errorf("failed to parse tuple's %d value, err: %w", i, err)
			}
			tuple = append(tuple, val)

			return nil
		}

		if err = dive(0, slice); err != nil {
			return nil, fmt.Errorf("failed to load tuple, err: %w", err)
		}

		return tuple, nil
	}

	return nil, errors.New("unknown value type")
}
