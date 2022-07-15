package tlb

import (
	"errors"
	"fmt"
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

		switch v := unwrap[i].value.(type) {
		case nil:
			b.MustStoreUInt(0x00, 8)
		case int, int8, int16, int32, int64, uint8, uint16, uint32:
			b.MustStoreUInt(0x01, 8)

			// cast to int64
			val := reflect.ValueOf(v).Convert(reflect.TypeOf(int64(0))).Interface().(int64)
			b.MustStoreInt(val, 64)
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
		default:
			return nil, fmt.Errorf("unknown type at %d pos in stack", i)
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

		typ, err := next.LoadUInt(8)
		if err != nil {
			return fmt.Errorf("failed to load stack value type, err: %w", err)
		}

		switch typ {
		case 0x00:
			s.Push(nil)
		case 0x01:
			val, err := next.LoadInt(64)
			if err != nil {
				return fmt.Errorf("failed to load tiny int stack value, err: %w", err)
			}
			s.Push(val)
		case 0x02:
			subTyp, err := next.LoadUInt(8)
			if err != nil {
				return fmt.Errorf("failed to load stack value sub type, err: %w", err)
			}

			switch subTyp {
			case 0xFF:
				s.Push(StackNaN{})
			default:
				bInt, err := next.LoadBigUInt(256)
				if err != nil {
					return fmt.Errorf("failed to load stack value big int, err: %w", err)
				}

				// 1st bit of int257 indicates sign, it is loaded in type
				if subTyp > 0 {
					bInt.Mul(bInt, big.NewInt(-1))
				}

				s.Push(bInt)
			}
		case 0x03:
			val, err := next.LoadRef()
			if err != nil {
				return fmt.Errorf("failed to load cell stack value, err: %w", err)
			}
			s.Push(val.MustToCell())
		case 0x04:
			start, err := next.LoadUInt(10)
			if err != nil {
				return fmt.Errorf("failed to load slice stack value's start, err: %w", err)
			}
			end, err := next.LoadUInt(10)
			if err != nil {
				return fmt.Errorf("failed to load slice stack value's end, err: %w", err)
			}
			if start > end {
				return fmt.Errorf("start index > end index")
			}

			startRef, err := next.LoadUInt(3)
			if err != nil {
				return fmt.Errorf("failed to load slice stack value's start ref, err: %w", err)
			}
			endRef, err := next.LoadUInt(3)
			if err != nil {
				return fmt.Errorf("failed to load slice stack value's end ref, err: %w", err)
			}
			if startRef > endRef {
				return fmt.Errorf("start ref index > end ref index")
			}

			val, err := next.LoadRef()
			if err != nil {
				return fmt.Errorf("failed to load cell stack value, err: %w", err)
			}

			cl := cell.BeginCell()

			if start > 0 {
				_, err = val.LoadSlice(uint(start))
				if err != nil {
					return fmt.Errorf("load prefix err: %w", err)
				}
			}

			if end > 0 {
				sz := uint(end - start)
				data, err := val.LoadSlice(sz)
				if err != nil {
					return fmt.Errorf("load prefix err: %w", err)
				}

				err = cl.StoreSlice(data, sz)
				if err != nil {
					return fmt.Errorf("store slice err: %w", err)
				}
			}

			for x := uint64(0); x < startRef; x++ {
				_, err := val.LoadRef()
				if err != nil {
					return fmt.Errorf("failed to load slice stack value's ref, err: %w", err)
				}
			}

			for x := uint64(0); x < endRef-startRef; x++ {
				sliceRef, err := val.LoadRef()
				if err != nil {
					return fmt.Errorf("failed to load slice stack value's ref, err: %w", err)
				}

				err = cl.StoreRef(sliceRef.MustToCell())
				if err != nil {
					return fmt.Errorf("failed to store slice stack value's ref, err: %w", err)
				}
			}
			s.Push(cl.EndCell().BeginParse())
		case 0x05:
			val, err := next.LoadRef()
			if err != nil {
				return fmt.Errorf("failed to load cell stack value, err: %w", err)
			}
			s.Push(val.MustToCell().ToBuilder())
		}

		next = ref
	}

	return nil
}
