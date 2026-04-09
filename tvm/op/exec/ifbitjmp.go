package exec

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return newIfBitJmpOp(0, false) },
		func() vm.OP { return newIfBitJmpRefOp(0, false, nil) },
	)
}

func IFBITJMP(bit uint8) *helpers.AdvancedOP {
	return newIfBitJmpOp(bit, false)
}

func IFNBITJMP(bit uint8) *helpers.AdvancedOP {
	return newIfBitJmpOp(bit, true)
}

func IFBITJMPREF(bit uint8, ref *cell.Cell) vm.OP {
	return bindRefCodeOp(newIfBitJmpRefOp(bit, false, nil), ref)
}

func IFNBITJMPREF(bit uint8, ref *cell.Cell) vm.OP {
	return bindRefCodeOp(newIfBitJmpRefOp(bit, true, nil), ref)
}

func newIfBitJmpOp(bit uint8, negate bool) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		FixedSizeBits: 6,
		Action: func(state *vm.State) error {
			cont, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}
			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			val := x.Bit(int(bit)) != 0
			if err = state.Stack.PushInt(x); err != nil {
				return err
			}
			if val != negate {
				return state.Jump(cont)
			}
			return nil
		},
		NameSerializer: func() string {
			if negate {
				return fmt.Sprintf("IFNBITJMP %d", bit)
			}
			return fmt.Sprintf("IFBITJMP %d", bit)
		},
		BitPrefix: helpers.SlicePrefix(10, []byte{0xE3, 0x80}),
		SerializeSuffix: func() *cell.Builder {
			raw := uint64(bit & 0x1F)
			if negate {
				raw |= 0x20
			}
			return cell.BeginCell().MustStoreUInt(raw, 6)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			raw, err := code.LoadUInt(6)
			if err != nil {
				return err
			}
			bit = uint8(raw & 0x1F)
			negate = raw&0x20 != 0
			return nil
		},
	}
}

func newIfBitJmpRefOp(bit uint8, negate bool, ref *cell.Cell) *refCodeOp {
	op := newRefCodeOp("", helpers.SlicePrefix(10, []byte{0xE3, 0xC0}), 1, func(state *vm.State, refs []*cell.Cell) error {
		x, err := state.Stack.PopIntFinite()
		if err != nil {
			return err
		}
		val := x.Bit(int(bit)) != 0
		if err = state.Stack.PushInt(x); err != nil {
			return err
		}
		if val != negate {
			cont, loadErr := loadContinuationFromCodeCell(state, refs[0])
			if loadErr != nil {
				return loadErr
			}
			return state.Jump(cont)
		}
		return nil
	})
	op.name = "IFBITJMPREF"
	op.fixedBits = 6
	op.serializeSuffix = func() *cell.Builder {
		raw := uint64(bit & 0x1F)
		if negate {
			raw |= 0x20
		}
		return cell.BeginCell().MustStoreUInt(raw, 6)
	}
	op.deserializeSuffix = func(code *cell.Slice) error {
		raw, err := code.LoadUInt(6)
		if err != nil {
			return err
		}
		bit = uint8(raw & 0x1F)
		negate = raw&0x20 != 0
		if negate {
			op.name = "IFNBITJMPREF"
		} else {
			op.name = "IFBITJMPREF"
		}
		return nil
	}
	if negate {
		op.name = "IFNBITJMPREF"
	}
	if ref != nil {
		op.refs[0] = ref
	}
	return op
}
