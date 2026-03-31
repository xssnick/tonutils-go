package stack

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type OpXCHG struct {
	helpers.Prefixed
	a uint8
	b uint8
}

func init() {
	vm.List = append(vm.List, func() vm.OP { return XCHG(0, 0) })
}

func XCHG(a, b uint8) *OpXCHG {
	return &OpXCHG{
		Prefixed: helpers.NewPrefixed(
			helpers.UIntPrefix(0x0, 4),
			helpers.UIntPrefix(0x1, 4),
			helpers.UIntPrefix(0x10, 8),
		),
		a: a, b: b,
	}
}

func (op *OpXCHG) Deserialize(code *cell.Slice) error {
	a, err := code.LoadUInt(4)
	if err != nil {
		return err
	}

	b, err := code.LoadUInt(4)
	if err != nil {
		return err
	}

	if a == 1 && b == 0 {
		a, err = code.LoadUInt(4)
		if err != nil {
			return err
		}

		b, err = code.LoadUInt(4)
		if err != nil {
			return err
		}
	}

	op.a = uint8(a)
	op.b = uint8(b)

	return nil
}

func (op *OpXCHG) Serialize() *cell.Builder {
	if op.a == 0 || op.b == 0 {
		with := op.a
		if with == 0 {
			with = op.b
		}
		return helpers.Builder([]byte{0x00 | with})
	}
	if op.a == 1 || op.b == 1 {
		with := op.a
		if with == 1 {
			with = op.b
		}
		return helpers.Builder([]byte{0x10 | with})
	}
	return helpers.Builder([]byte{0x10, (op.a << 4) | op.b})
}

func (op *OpXCHG) SerializeText() string {
	return fmt.Sprintf("s%d,s%d XCHG", op.a, op.b)
}

func (op *OpXCHG) InstructionBits() int64 {
	if op.a == 0 || op.b == 0 || op.a == 1 || op.b == 1 {
		return 8
	}
	return 16
}

func (op *OpXCHG) Interpret(state *vm.State) error {
	return state.Stack.Exchange(int(op.a), int(op.b))
}
