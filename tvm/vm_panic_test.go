package tvm

import (
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type panicTestOP struct{}

type lateUnderflowTestOP struct {
	panicTestOP
}

func (o *panicTestOP) GetPrefixes() []*cell.Slice {
	return []*cell.Slice{cell.BeginCell().MustStoreUInt(0xFF, 8).EndCell().MustBeginParse()}
}

func (o *panicTestOP) Deserialize(code *cell.Slice) error {
	_, err := code.LoadUInt(8)
	return err
}

func (o *panicTestOP) Serialize() *cell.Builder {
	return cell.BeginCell().MustStoreUInt(0xFF, 8)
}

func (o *panicTestOP) SerializeText() string {
	return "PANIC-TEST"
}

func (o *panicTestOP) Interpret(state *vmcore.State) error {
	panic("panic test")
}

func (o *lateUnderflowTestOP) Interpret(state *vmcore.State) error {
	if _, err := state.Stack.PopAny(); err != nil {
		return err
	}
	return vmerr.Err(vmerr.CodeStackUnderflow)
}

func TestOpcodeRegistryIsFrozenBeforeNewTVM(t *testing.T) {
	original := vmcore.List
	vmcore.List = append(append([]vmcore.OPGetter(nil), original...), func() vmcore.OP {
		return &panicTestOP{}
	})
	t.Cleanup(func() {
		vmcore.List = original
	})

	code := cell.BeginCell().MustStoreUInt(0xFF, 8).EndCell().MustBeginParse()
	getter := NewTVM().matchOpcode(code)
	if getter != nil {
		if _, late := getter().(*panicTestOP); late {
			t.Fatal("NewTVM observed an opcode registered after package initialization")
		}
	}
}

func TestVMRecoversOpcodePanicsAsFatalError(t *testing.T) {
	getter := func() vmcore.OP {
		return &panicTestOP{}
	}
	dispatch := newOpcodeDispatch()
	dispatch.addPrefix((&panicTestOP{}).GetPrefixes()[0], getter)
	dispatch.buildFastTable()

	machine := NewTVM()
	for i := range machine.dispatches {
		machine.dispatches[i] = dispatch
	}
	code := cell.BeginCell().MustStoreUInt(0xFF, 8).EndCell()

	res, err := machine.Execute(code, nil, tuple.Tuple{}, vmcore.NewGas(), vmcore.NewStack(), testExecutionConfig(t))
	if err == nil {
		t.Fatal("expected fatal error")
	}
	if res != nil {
		t.Fatalf("expected nil result on fatal execution error, got %#v", res)
	}
	if !strings.Contains(err.Error(), "vm panic: panic test") {
		t.Fatalf("expected panic context in error, got %v", err)
	}
}

func TestStepUsesReferencePreflightContractWithoutRollback(t *testing.T) {
	op := &lateUnderflowTestOP{}
	dispatch := newOpcodeDispatch()
	dispatch.addPrefix(op.GetPrefixes()[0], func() vmcore.OP {
		return &lateUnderflowTestOP{}
	})
	dispatch.buildFastTable()

	stack := vmcore.NewStack()
	if err := stack.PushSmallInt(7); err != nil {
		t.Fatalf("push: %v", err)
	}
	state := &vmcore.State{
		CurrentCode: op.Serialize().EndCell().MustBeginParse(),
		Stack:       stack,
		Gas:         vmcore.GasWithLimit(1_000),
	}

	err := (&TVM{}).stepWithDispatch(dispatch, state)
	if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeStackUnderflow {
		t.Fatalf("step error = %v, want stack underflow", err)
	}
	if stack.Len() != 0 {
		t.Fatalf("dispatcher rolled a handler mutation back, stack len = %d", stack.Len())
	}
}
