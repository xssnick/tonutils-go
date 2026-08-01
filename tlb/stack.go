package tlb

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

var ErrStackEmpty = errors.New("stack is empty")

const maxStackDepth = 1024

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

func NewStackFromVM(s *vm.Stack) (*Stack, error) {
	s = s.Copy()

	ns := &Stack{}
	for range s.Len() {
		val, err := s.PopAny()
		if err != nil {
			return nil, err
		}
		ns.Push(val)
	}

	return ns, nil
}

func newStackFromVMView(s *vm.Stack) (*Stack, error) {
	ns := &Stack{}
	for i := 0; i < s.Len(); i++ {
		val, err := s.Get(i)
		if err != nil {
			return nil, err
		}
		ns.Push(val)
	}

	return ns, nil
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
	return s.toCell(newContinuationEncoder())
}

func (s *Stack) toCell(encoder *continuationEncoder) (*cell.Cell, error) {
	var unwrap []*StackElement
	elem := s.top
	for elem != nil {
		unwrap = append(unwrap, elem)
		elem = elem.next
	}

	// C++ Stack::serialize bounds the depth only by the uint24 field and by the
	// cell depth limit reached while building the list, maxStackDepth is enforced
	// on the parsing side alone.
	root := cell.BeginCell()
	if err := root.StoreUInt(uint64(len(unwrap)), 24); err != nil {
		return nil, fmt.Errorf("failed to store stack depth: %w", err)
	}

	if len(unwrap) == 0 {
		return root.EndCell(), nil
	}

	next := cell.BeginCell()
	for i := 0; i < len(unwrap); i++ {
		b := cell.BeginCell()
		if err := b.StoreRef(next.EndCell()); err != nil {
			return nil, fmt.Errorf("failed to store %d stack element rest: %w", i, err)
		}

		if err := serializeStackValue(b, unwrap[i].value, encoder); err != nil {
			return nil, fmt.Errorf("failed to serialize %d stack element: %w", i, err)
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

	if depth > maxStackDepth {
		return fmt.Errorf("stack depth exceeds %d", maxStackDepth)
	}

	var loaded Stack
	next := loader
	for i := uint64(0); i < depth; i++ {
		current := next
		ref, err := next.LoadRef()
		if err != nil {
			return fmt.Errorf("failed to load stack next ref, err: %w", err)
		}

		val, err := ParseStackValue(next)
		if err != nil {
			return fmt.Errorf("failed to parse stack value, err: %w", err)
		}

		if i > 0 && (current.BitsLeft() != 0 || current.RefsNum() != 0) {
			return fmt.Errorf("stack cons cell %d has trailing data", i)
		}

		loaded.Push(val)

		next = ref
	}
	if depth > 0 && (next.BitsLeft() != 0 || next.RefsNum() != 0) {
		return fmt.Errorf("stack nil cell has trailing data")
	}

	s.top = loaded.top
	return nil
}

func SerializeStackValue(b *cell.Builder, val any) error {
	return serializeStackValue(b, val, newContinuationEncoder())
}

func serializeStackValue(b *cell.Builder, val any, encoder *continuationEncoder) error {
	var err error
	val, err = vmStackValueToTLB(val)
	if err != nil {
		return fmt.Errorf("failed to convert vm stack value: %w", err)
	}

	if vl, ok := val.(*big.Int); ok {
		if vl == nil {
			return errors.New("cannot serialize nil big integer")
		}
		if vl.IsInt64() {
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
	case vm.NaN:
		b.MustStoreSlice([]byte{0x02, 0xFF}, 16)
	case int, int8, int16, int32, int64, uint8, uint16, uint32:
		b.MustStoreUInt(0x01, 8)

		// cast to int64
		vl := reflect.ValueOf(v).Convert(reflect.TypeOf(int64(0))).Interface().(int64)
		b.MustStoreInt(vl, 64)
	case uint:
		if uint64(v) <= math.MaxInt64 {
			b.MustStoreUInt(0x01, 8)
			b.MustStoreInt(int64(v), 64)
			break
		}

		b.MustStoreUInt(0x0200/2, 15)
		if err := b.StoreBigInt(new(big.Int).SetUint64(uint64(v)), 257); err != nil {
			return fmt.Errorf("failed to store stack integer: %w", err)
		}
	case uint64:
		if v <= math.MaxInt64 {
			b.MustStoreUInt(0x01, 8)
			b.MustStoreInt(int64(v), 64)
			break
		}

		b.MustStoreUInt(0x0200/2, 15)
		if err := b.StoreBigInt(new(big.Int).SetUint64(v), 257); err != nil {
			return fmt.Errorf("failed to store stack integer: %w", err)
		}
	case *big.Int:
		b.MustStoreUInt(0x0200/2, 15)
		if err := b.StoreBigInt(v, 257); err != nil {
			return fmt.Errorf("failed to store stack integer: %w", err)
		}

	case StackNaN:
		b.MustStoreSlice([]byte{0x02, 0xFF}, 16)
	case *cell.Cell:
		b.MustStoreUInt(0x03, 8)
		// deep values fail the cell depth limit, like CellBuilder in the
		// reference VM; that must be an error, not a panic
		if err := b.StoreRef(v); err != nil {
			return err
		}
	case *cell.Slice:
		return serializeStackSlice(b, v, true)
	case *cell.Builder:
		b.MustStoreUInt(0x05, 8)
		if err := b.StoreRef(v.EndCell()); err != nil {
			return err
		}
	case vm.Continuation:
		b.MustStoreUInt(0x06, 8)
		return encoder.serialize(b, v)
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
				if err := b.StoreRef(n.EndCell()); err != nil {
					return err
				}
			} else if i == 1 {
				n2 := cell.BeginCell()
				if err := serializeStackValue(n2, v[i-1], encoder); err != nil {
					return fmt.Errorf("failed to serialize tuple %d element: %w", i-1, err)
				}
				if err := b.StoreRef(n2.EndCell()); err != nil {
					return err
				}
			}

			n2 := cell.BeginCell()
			if err := serializeStackValue(n2, v[i], encoder); err != nil {
				return fmt.Errorf("failed to serialize tuple %d element: %w", i, err)
			}
			if err := b.StoreRef(n2.EndCell()); err != nil {
				return err
			}

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
		subTyp, err := slice.PreloadUInt(8)
		if err != nil {
			return nil, fmt.Errorf("failed to load stack value sub type, err: %w", err)
		}
		if subTyp == 0xFF {
			if _, err = slice.LoadUInt(8); err != nil {
				return nil, fmt.Errorf("failed to load stack value sub type, err: %w", err)
			}
			return StackNaN{}, nil
		}

		prefix, err := slice.LoadUInt(7)
		if err != nil {
			return nil, fmt.Errorf("failed to load stack value int prefix, err: %w", err)
		}
		if prefix != 0 {
			return nil, fmt.Errorf("unknown stack value int prefix")
		}

		bInt, err := slice.LoadBigInt(257)
		if err != nil {
			return nil, fmt.Errorf("failed to load stack value big int, err: %w", err)
		}
		return bInt, nil
	case 0x03:
		val, err := slice.LoadRef()
		if err != nil {
			return nil, fmt.Errorf("failed to load cell stack value, err: %w", err)
		}
		return val.MustToCell(), nil
	case 0x04:
		return parseStackSlice(slice)
	case 0x05:
		val, err := slice.LoadRef()
		if err != nil {
			return nil, fmt.Errorf("failed to load cell stack value, err: %w", err)
		}
		return val.MustToCell().ToBuilder(), nil
	case 0x06:
		return parseContinuation(slice)
	case 0x07:
		ln, err := slice.LoadUInt(16)
		if err != nil {
			return nil, fmt.Errorf("failed to load tuple stack value's len, err: %w", err)
		}

		tuple := make([]any, 0)

		if ln == 0 {
			return tuple, nil
		}

		loadValue := func(i int, root *cell.Slice) error {
			ref, err := root.LoadRef()
			if err != nil {
				return fmt.Errorf("failed to load tuple's %d ref, err: %w", i, err)
			}

			val, err := ParseStackValue(ref)
			if err != nil {
				return fmt.Errorf("failed to parse tuple's %d value, err: %w", i, err)
			}
			if ref.BitsLeft() != 0 || ref.RefsNum() != 0 {
				return fmt.Errorf("tuple's %d value has trailing data", i)
			}
			tuple = append(tuple, val)

			return nil
		}

		var dive func(i int, root *cell.Slice) error
		dive = func(i int, root *cell.Slice) error {
			if i < 0 {
				return nil
			}

			if i > 1 {
				next, err := root.LoadRef()
				if err != nil {
					return fmt.Errorf("failed to load tuple's %d next element, err: %w", i, err)
				}

				if err = dive(i-1, next); err != nil {
					return err
				}
				if next.BitsLeft() != 0 || next.RefsNum() != 0 {
					return fmt.Errorf("tuple's %d pair has trailing data", i)
				}
			} else if i == 1 {
				if err := loadValue(0, root); err != nil {
					return err
				}
			}

			return loadValue(i, root)
		}

		if err = dive(int(ln)-1, slice); err != nil {
			return nil, fmt.Errorf("failed to load tuple, err: %w", err)
		}

		return tuple, nil
	}

	return nil, errors.New("unknown value type")
}

func serializeStackSlice(b *cell.Builder, value *cell.Slice, withTag bool) error {
	if value == nil {
		return fmt.Errorf("slice stack value is nil")
	}
	if withTag {
		if err := b.StoreUInt(0x04, 8); err != nil {
			return fmt.Errorf("failed to store slice stack value type: %w", err)
		}
	}

	base := value.BaseCell()
	if base == nil {
		base = cell.BeginCell().EndCell()
	}
	if err := b.StoreRef(base); err != nil {
		return fmt.Errorf("failed to store slice stack value cell: %w", err)
	}

	start, end := value.BitRange()
	startRef, endRef := value.RefRange()
	if err := b.StoreUInt(uint64(start), 10); err != nil {
		return fmt.Errorf("failed to store slice stack value start: %w", err)
	}
	if err := b.StoreUInt(uint64(end), 10); err != nil {
		return fmt.Errorf("failed to store slice stack value end: %w", err)
	}
	if err := b.StoreUInt(uint64(startRef), 3); err != nil {
		return fmt.Errorf("failed to store slice stack value start ref: %w", err)
	}
	if err := b.StoreUInt(uint64(endRef), 3); err != nil {
		return fmt.Errorf("failed to store slice stack value end ref: %w", err)
	}

	return nil
}

func parseStackSlice(slice *cell.Slice) (*cell.Slice, error) {
	value, err := slice.LoadRef()
	if err != nil {
		return nil, fmt.Errorf("failed to load slice stack value cell: %w", err)
	}

	start, err := slice.LoadUInt(10)
	if err != nil {
		return nil, fmt.Errorf("failed to load slice stack value's start: %w", err)
	}
	end, err := slice.LoadUInt(10)
	if err != nil {
		return nil, fmt.Errorf("failed to load slice stack value's end: %w", err)
	}
	if start > end {
		return nil, fmt.Errorf("start index > end index")
	}

	startRef, err := slice.LoadUInt(3)
	if err != nil {
		return nil, fmt.Errorf("failed to load slice stack value's start ref: %w", err)
	}
	endRef, err := slice.LoadUInt(3)
	if err != nil {
		return nil, fmt.Errorf("failed to load slice stack value's end ref: %w", err)
	}
	if startRef > endRef {
		return nil, fmt.Errorf("start ref index > end ref index")
	}
	if endRef > 4 {
		return nil, fmt.Errorf("end ref index > 4")
	}

	if err = value.SkipBitsAndRefs(uint(start), int(startRef)); err != nil {
		return nil, fmt.Errorf("failed to skip slice stack value's prefix: %w", err)
	}
	out, err := value.PreloadSubslice(uint(end-start), int(endRef-startRef))
	if err != nil {
		return nil, fmt.Errorf("failed to load slice stack value: %w", err)
	}

	return out, nil
}
