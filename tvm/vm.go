package tvm

import (
	"bytes"
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
	"sort"
)

type opPrefix struct {
	op     vm.OPGetter
	prefix *cell.Slice
}
type TVM struct {
	prefixes []opPrefix
}

func NewTVM() *TVM {
	var prefixes []opPrefix
	for _, op := range vm.List {
		for _, s := range op().GetPrefixes() {
			prefixes = append(prefixes, opPrefix{op, s})
		}
	}

	sort.Slice(prefixes, func(i, j int) bool {
		return prefixes[i].prefix.BitsLeft() > prefixes[j].prefix.BitsLeft()
	})

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
	for _, px := range tvm.prefixes {
		if state.CurrentCode.BitsLeft() < px.prefix.BitsLeft() {
			continue
		}

		if bytes.Equal(px.prefix.Copy().MustLoadSlice(px.prefix.BitsLeft()),
			state.CurrentCode.Copy().MustLoadSlice(px.prefix.BitsLeft())) {

			op := px.op()

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
