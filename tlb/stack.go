package tlb

import (
	"errors"
	"fmt"
	"math"
	"math/big"

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

	if err := root.StoreBuilder(next); err != nil {
		return nil, fmt.Errorf("failed to store stack top: %w", err)
	}

	return root.EndCell(), nil
}

func (s *Stack) LoadFromCell(loader *cell.Slice) error {
	// Stack::deserialize clears the destination before attempting to read it and
	// leaves it empty on every failure.
	s.top = nil

	depth, err := loader.LoadUInt(24)
	if err != nil {
		return fmt.Errorf("failed to load depth, err: %w", err)
	}

	if depth > maxStackDepth {
		return fmt.Errorf("stack depth exceeds %d", maxStackDepth)
	}

	var loaded Stack
	next := loader
	var rest *cell.Cell
	for i := uint64(0); i < depth; i++ {
		current := next
		rest, err = current.LoadRefCell()
		if err != nil {
			return fmt.Errorf("failed to load stack next ref, err: %w", err)
		}

		val, err := ParseStackValue(current)
		if err != nil {
			return fmt.Errorf("failed to parse stack value, err: %w", err)
		}

		if i > 0 && (current.BitsLeft() != 0 || current.RefsNum() != 0) {
			return fmt.Errorf("stack cons cell %d has trailing data", i)
		}

		loaded.Push(val)

		if i+1 < depth {
			next, err = loadOrdinaryStackCell(rest)
			if err != nil {
				return fmt.Errorf("failed to load stack cons cell %d: %w", i+1, err)
			}
		}
	}
	if depth > 0 {
		tail, err := loadOrdinaryStackCell(rest)
		if err != nil {
			return fmt.Errorf("failed to load stack nil cell: %w", err)
		}
		if tail.BitsLeft() != 0 || tail.RefsNum() != 0 {
			return fmt.Errorf("stack nil cell has trailing data")
		}
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
		if err := b.StoreUInt(0x00, 8); err != nil {
			return err
		}
	case vm.NaN:
		if err := b.StoreUInt(0x02ff, 16); err != nil {
			return err
		}
	case int, int8, int16, int32, int64, uint8, uint16, uint32:
		if err := b.StoreUInt(0x01, 8); err != nil {
			return err
		}

		// Keep native integer serialization allocation-free; reflection here is
		// visible on every result-stack scalar.
		var vl int64
		switch v := v.(type) {
		case int:
			vl = int64(v)
		case int8:
			vl = int64(v)
		case int16:
			vl = int64(v)
		case int32:
			vl = int64(v)
		case int64:
			vl = v
		case uint8:
			vl = int64(v)
		case uint16:
			vl = int64(v)
		case uint32:
			vl = int64(v)
		}
		if err := b.StoreInt(vl, 64); err != nil {
			return err
		}
	case uint:
		if uint64(v) <= math.MaxInt64 {
			if err := b.StoreUInt(0x01, 8); err != nil {
				return err
			}
			if err := b.StoreInt(int64(v), 64); err != nil {
				return err
			}
			break
		}

		if err := b.StoreUInt(0x0200/2, 15); err != nil {
			return err
		}
		if err := b.StoreBigInt(new(big.Int).SetUint64(uint64(v)), 257); err != nil {
			return fmt.Errorf("failed to store stack integer: %w", err)
		}
	case uint64:
		if v <= math.MaxInt64 {
			if err := b.StoreUInt(0x01, 8); err != nil {
				return err
			}
			if err := b.StoreInt(int64(v), 64); err != nil {
				return err
			}
			break
		}

		if err := b.StoreUInt(0x0200/2, 15); err != nil {
			return err
		}
		if err := b.StoreBigInt(new(big.Int).SetUint64(v), 257); err != nil {
			return fmt.Errorf("failed to store stack integer: %w", err)
		}
	case *big.Int:
		if err := b.StoreUInt(0x0200/2, 15); err != nil {
			return err
		}
		if err := b.StoreBigInt(v, 257); err != nil {
			return fmt.Errorf("failed to store stack integer: %w", err)
		}

	case StackNaN:
		if err := b.StoreUInt(0x02ff, 16); err != nil {
			return err
		}
	case *cell.Cell:
		if err := b.StoreUInt(0x03, 8); err != nil {
			return err
		}
		// deep values fail the cell depth limit, like CellBuilder in the
		// reference VM; that must be an error, not a panic
		if err := b.StoreRef(v); err != nil {
			return err
		}
	case *cell.Slice:
		return serializeStackSlice(b, v, true)
	case *cell.Builder:
		if err := b.StoreUInt(0x05, 8); err != nil {
			return err
		}
		if v == nil {
			return errors.New("cannot serialize nil builder reference")
		}
		if err := b.StoreRef(v.EndCell()); err != nil {
			return err
		}
	case vm.Continuation:
		if err := b.StoreUInt(0x06, 8); err != nil {
			return err
		}
		return encoder.serialize(b, v)
	case []any:
		// Match StackEntry::serialize: build the reference chain before touching
		// the destination. Apart from matching failure side effects, the loop
		// avoids recursion for large externally supplied tuples.
		var head, tail *cell.Cell
		for i := range v {
			head, tail = tail, head
			if i > 1 {
				pair := cell.BeginCell()
				if err := pair.StoreRef(tail); err != nil {
					return err
				}
				if err := pair.StoreRef(head); err != nil {
					return err
				}
				head = pair.EndCell()
			}

			value := cell.BeginCell()
			if err := serializeStackValue(value, v[i], encoder); err != nil {
				return fmt.Errorf("failed to serialize tuple %d element: %w", i, err)
			}
			tail = value.EndCell()
		}

		if err := b.StoreUInt(0x07, 8); err != nil {
			return err
		}
		if err := b.StoreUInt(uint64(len(v)), 16); err != nil {
			return err
		}
		if head != nil {
			if err := b.StoreRef(head); err != nil {
				return err
			}
		}
		if tail != nil {
			if err := b.StoreRef(tail); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unknown type")
	}
	return nil
}

func ParseStackValue(slice *cell.Slice) (any, error) {
	typ, err := slice.PreloadUInt(8)
	if err != nil {
		return nil, fmt.Errorf("failed to load stack value type, err: %w", err)
	}

	switch typ {
	case 0x00:
		if err = slice.SkipBits(8); err != nil {
			return nil, fmt.Errorf("failed to load null stack value, err: %w", err)
		}
		return nil, nil
	case 0x01:
		if err = slice.SkipBits(8); err != nil {
			return nil, fmt.Errorf("failed to load tiny int stack value type, err: %w", err)
		}
		val, err := slice.LoadBigInt(64)
		if err != nil {
			return nil, fmt.Errorf("failed to load tiny int stack value, err: %w", err)
		}
		return val, nil
	case 0x02:
		if slice.BitsLeft() >= 16 {
			subTyp, err := slice.PreloadUInt(16)
			if err != nil {
				return nil, fmt.Errorf("failed to load stack value sub type, err: %w", err)
			}
			if subTyp&0x1ff == 0xff {
				if err = slice.SkipBits(16); err != nil {
					return nil, fmt.Errorf("failed to load stack value sub type, err: %w", err)
				}
				return StackNaN{}, nil
			}
		}

		prefix, err := slice.LoadUInt(15)
		if err != nil {
			return nil, fmt.Errorf("failed to load stack value int prefix, err: %w", err)
		}
		if prefix != 0x0200/2 {
			return nil, fmt.Errorf("unknown stack value int prefix")
		}

		bInt, err := slice.LoadBigInt(257)
		if err != nil {
			return nil, fmt.Errorf("failed to load stack value big int, err: %w", err)
		}
		return bInt, nil
	case 0x03:
		// StackEntry::deserialize checks have_refs() before advancing past the
		// tag, so a missing reference leaves the input cursor unchanged.
		if slice.RefsNum() == 0 {
			return nil, fmt.Errorf("failed to load cell stack value: no references left")
		}
		if err = slice.SkipBits(8); err != nil {
			return nil, fmt.Errorf("failed to load cell stack value type, err: %w", err)
		}
		// StackEntry::deserialize uses CellSlice::fetch_ref here. Keep the
		// referenced cell lazy; parsing it would register a cell load that the
		// reference implementation does not perform.
		val, err := slice.LoadRefCell()
		if err != nil {
			return nil, fmt.Errorf("failed to load cell stack value, err: %w", err)
		}
		return val, nil
	case 0x04:
		if err = slice.SkipBits(8); err != nil {
			return nil, fmt.Errorf("failed to load slice stack value type, err: %w", err)
		}
		return parseStackSlice(slice)
	case 0x05:
		if err = slice.SkipBits(8); err != nil {
			return nil, fmt.Errorf("failed to load builder stack value type, err: %w", err)
		}
		base, err := slice.LoadRefCell()
		if err != nil {
			return nil, fmt.Errorf("failed to load cell stack value, err: %w", err)
		}
		val, err := loadOrdinaryStackCell(base)
		if err != nil {
			return nil, fmt.Errorf("failed to load builder stack value: %w", err)
		}
		return val.MustToCell().ToBuilder(), nil
	case 0x06:
		if err = slice.SkipBits(8); err != nil {
			return nil, fmt.Errorf("failed to load continuation stack value type, err: %w", err)
		}
		return parseContinuation(slice)
	case 0x07:
		if err = slice.SkipBits(8); err != nil {
			return nil, fmt.Errorf("failed to load tuple stack value type, err: %w", err)
		}
		ln, err := slice.LoadUInt(16)
		if err != nil {
			return nil, fmt.Errorf("failed to load tuple stack value's len, err: %w", err)
		}

		values := make([]any, int(ln))
		loadValue := func(i int, value *cell.Cell) error {
			valueSlice, err := loadOrdinaryStackCell(value)
			if err != nil {
				return fmt.Errorf("failed to load tuple's %d value cell: %w", i, err)
			}
			val, err := ParseStackValue(valueSlice)
			if err != nil {
				return fmt.Errorf("failed to parse tuple's %d value, err: %w", i, err)
			}
			if valueSlice.BitsLeft() != 0 || valueSlice.RefsNum() != 0 {
				return fmt.Errorf("tuple's %d value has trailing data", i)
			}
			values[i] = val
			return nil
		}

		if ln == 0 {
			return values, nil
		}

		if ln == 1 {
			value, err := slice.LoadRefCell()
			if err != nil {
				return nil, fmt.Errorf("failed to load tuple's 0 ref, err: %w", err)
			}
			if err = loadValue(0, value); err != nil {
				return nil, fmt.Errorf("failed to load tuple, err: %w", err)
			}
			return values, nil
		}

		head, err := slice.LoadRefCell()
		if err != nil {
			return nil, fmt.Errorf("failed to load tuple's %d next element, err: %w", ln-1, err)
		}
		tail, err := slice.LoadRefCell()
		if err != nil {
			return nil, fmt.Errorf("failed to load tuple's %d ref, err: %w", ln-1, err)
		}
		if err = loadValue(int(ln)-1, tail); err != nil {
			return nil, fmt.Errorf("failed to load tuple, err: %w", err)
		}

		for i := int(ln) - 2; i > 0; i-- {
			pair, loadErr := loadOrdinaryStackCell(head)
			if loadErr != nil {
				return nil, fmt.Errorf("failed to load tuple's %d pair, err: %w", i, loadErr)
			}
			head, err = pair.LoadRefCell()
			if err != nil {
				return nil, fmt.Errorf("failed to load tuple's %d next element, err: %w", i, err)
			}
			tail, err = pair.LoadRefCell()
			if err != nil {
				return nil, fmt.Errorf("failed to load tuple's %d ref, err: %w", i, err)
			}
			if pair.BitsLeft() != 0 || pair.RefsNum() != 0 {
				return nil, fmt.Errorf("failed to load tuple, err: tuple's %d pair has trailing data", i)
			}
			if err = loadValue(i, tail); err != nil {
				return nil, fmt.Errorf("failed to load tuple, err: %w", err)
			}
		}

		if err = loadValue(0, head); err != nil {
			return nil, fmt.Errorf("failed to load tuple, err: %w", err)
		}
		return values, nil
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
		return fmt.Errorf("slice stack value has no base cell")
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
	base, err := slice.LoadRefCell()
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
	// XCTOS deliberately creates slices over special cells, and
	// StackEntry::serialize preserves that base cell in result stacks. Decode
	// the raw slice here; builder, tuple, continuation, and stack-container
	// paths retain the ordinary-cell rule.
	value, err := base.BeginParse()
	if err != nil {
		return nil, fmt.Errorf("failed to load slice stack value: %w", err)
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

func loadOrdinaryStackCell(value *cell.Cell) (*cell.Slice, error) {
	// Stack ABI decoding in this package runs without a VM state. This matches
	// load_cell_slice with no VmStateInterface: special cells are rejected.
	// Active C++ VM decoding can additionally resolve library cells through its
	// state, but tlb parsing has no library resolver boundary.
	loaded, err := value.BeginParse()
	if err != nil {
		return nil, err
	}
	// A lazy ordinary cell is represented by a special pruned placeholder.
	// Match load_cell_slice_impl by checking the materialized cell, not the
	// placeholder.
	if loaded.RawCell().IsSpecial() {
		return nil, fmt.Errorf("cannot load special cell without VM resolver")
	}
	return loaded, nil
}
