package tvm

import (
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	_ "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	_ "github.com/xssnick/tonutils-go/tvm/op/exec"
	_ "github.com/xssnick/tonutils-go/tvm/op/funcs"
	_ "github.com/xssnick/tonutils-go/tvm/op/math"
	_ "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
	"unsafe"
)

type TVM struct {
	prefixes map[uint64]vm.OPGetter
}

func NewTVM() *TVM {
	var prefixes = map[uint64]vm.OPGetter{}

	for _, op := range vm.List {
		for _, s := range op().GetPrefixes() {
			var buf [8]byte
			opBits := s.BitsLeft()
			bts := s.MustPreloadSlice(opBits)
			if len(bts) > 7 {
				panic("too long prefix for opcode " + op().SerializeText())
			}

			buf[0] = uint8(opBits)
			copy(buf[1:], bts)

			prefixes[*(*uint64)(unsafe.Pointer(&buf[0]))] = op
		}
	}

	return &TVM{
		prefixes: prefixes,
	}
}

func (tvm *TVM) Execute(code, data *cell.Cell, c7 tuple.Tuple, gas vm.Gas, stack *vm.Stack) (err error) {
	err = tvm.execute(&vm.State{
		CurrentCode: code.BeginParse(),
		Gas:         gas,
		Reg: vm.Register{
			C: [4]vm.Continuation{
				&vm.QuitContinuation{ExitCode: 0},
				&vm.QuitContinuation{ExitCode: 1},
				&vm.ExcQuitContinuation{},
				&vm.QuitContinuation{ExitCode: vmerr.ErrUnknown.Code},
			},
			D: [2]*cell.Cell{
				data,                       // c4
				cell.BeginCell().EndCell(), // c5
			},
			C7: c7,
		},
		Stack: stack,
	})

	var e vmerr.VMError
	if errors.As(err, &e) {
		if e.Code == 0 {
			return nil
		}
	}
	return err
}

func (tvm *TVM) execute(state *vm.State) (err error) {
	for {
		for state.CurrentCode.BitsLeft() > 0 || state.CurrentCode.RefsNum() > 0 {
			if state.CurrentCode.BitsLeft() == 0 {
				cc, err := state.CurrentCode.LoadRef()
				if err != nil {
					return err
				}

				c := &vm.OrdinaryContinuation{
					Data: vm.ControlData{
						CP:      vm.CP,
						NumArgs: vm.ControlDataAllArgs,
					},
					Code: cc,
				}

				// implicit JMPREF
				if err = state.Jump(c); err != nil {
					return err
				}
			}

			if err = tvm.step(state); err != nil {
				// TODO: check vm err (try catch logic)
				return err
			}
		}

		if state.Reg.C[0] == nil {
			return fmt.Errorf("something wrong, c0 is nil")
		}

		if err = state.Jump(state.Reg.C[0]); err != nil {
			return err
		}
	}
}

func (tvm *TVM) step(state *vm.State) (err error) {
	// we are doing 2 rounds of lookup, first one is fast and covers 99% of opcodes, if not found we are trying to check each bit
	for _, move := range []uint{4, 1} {
		var buf [8]byte
		for prefixLen := uint(4); prefixLen < 7*8; prefixLen += move {
			if state.CurrentCode.BitsLeft() < prefixLen {
				break
			}

			buf[0] = uint8(prefixLen)
			pfx := state.CurrentCode.MustPreloadSlice(prefixLen)
			copy(buf[1:], pfx)

			px := tvm.prefixes[*(*uint64)(unsafe.Pointer(&buf[0]))]
			if px == nil {
				continue
			}

			op := px()

			err = op.Deserialize(state.CurrentCode)
			if err != nil {
				return fmt.Errorf("deserialize opcode [%s] error: %w", op.SerializeText(), err)
			}

			println(op.SerializeText())
			err = op.Interpret(state)
			if err != nil {
				return err
			}
			// TODO: consume gas

			return nil
		}
	}

	return fmt.Errorf("opcode not found: %w (%s)", vm.ErrCorruptedOpcode, state.CurrentCode.String())
}
