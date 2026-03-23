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
	_ "github.com/xssnick/tonutils-go/tvm/op/tuple"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
	"math/big"
)

type trieNode struct {
	next [2]*trieNode
	op   vm.OPGetter
}

type matchedDeserializer interface {
	DeserializeMatched(code *cell.Slice) error
}

type TVM struct {
	trie         *trieNode
	maxPrefixLen uint
}

func NewTVM() *TVM {
	tvm := &TVM{
		trie: &trieNode{},
	}

	for _, op := range vm.List {
		for _, s := range op().GetPrefixes() {
			tvm.addTriePrefix(s, op)
		}
	}

	return tvm
}

func bitAt(data []byte, bit uint) uint8 {
	return (data[bit/8] >> (7 - (bit % 8))) & 1
}

func (tvm *TVM) addTriePrefix(prefix *cell.Slice, op vm.OPGetter) {
	n := tvm.trie
	bits := prefix.BitsLeft()
	raw := prefix.MustPreloadSlice(bits)

	if bits > tvm.maxPrefixLen {
		tvm.maxPrefixLen = bits
	}

	for i := uint(0); i < bits; i++ {
		b := bitAt(raw, i)
		if n.next[b] == nil {
			n.next[b] = &trieNode{}
		}
		n = n.next[b]
	}

	n.op = op
}

func (tvm *TVM) matchOpcode(code *cell.Slice) vm.OPGetter {
	limit := code.BitsLeft()
	if limit == 0 {
		return nil
	}
	if limit > tvm.maxPrefixLen {
		limit = tvm.maxPrefixLen
	}

	raw := code.MustPreloadSlice(limit)
	n := tvm.trie
	var matched vm.OPGetter

	for i := uint(0); i < limit; i++ {
		n = n.next[bitAt(raw, i)]
		if n == nil {
			break
		}
		if n.op != nil {
			matched = n.op
		}
	}

	return matched
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
				&vm.QuitContinuation{ExitCode: vmerr.CodeUnknown},
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
		if e.Code == 0 || e.Code == 1 {
			return nil
		}
	}
	return err
}

func (tvm *TVM) execute(state *vm.State) (err error) {
	var steps uint32
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

				if err = state.Gas.Consume(vm.ImplicitJmprefGasPrice); err != nil {
					return err
				}

				// implicit JMPREF
				vm.Tracef("implicit JMPREF")
				if err = state.Jump(c); err != nil {
					return err
				}
			}

			if steps > 100000 {
				return fmt.Errorf("too many steps")
			}

			if err = tvm.step(state); err != nil {
				var e vmerr.VMError
				if state.Reg.C[2] != nil && errors.As(err, &e) && e.Code != 0 {
					vm.Tracef("[EXCEPTION] %d %s", e.Code, e.Msg)

					if err = state.ThrowException(big.NewInt(e.Code)); err == nil {
						continue
					}
				}

				return err
			}
			steps++
		}

		vm.Tracef("implicit RET")
		if err = state.Gas.Consume(vm.ImplicitRetGasPrice); err != nil {
			return err
		}

		if err = state.Return(); err != nil {
			return err
		}
	}
}

func (tvm *TVM) step(state *vm.State) (err error) {
	px := tvm.matchOpcode(state.CurrentCode)
	if px == nil {
		return fmt.Errorf("opcode not found: %w (%s)", vm.ErrCorruptedOpcode, state.CurrentCode.String())
	}

	op := px()

	if fast, ok := op.(matchedDeserializer); ok {
		err = fast.DeserializeMatched(state.CurrentCode)
	} else {
		err = op.Deserialize(state.CurrentCode)
	}
	if err != nil {
		return fmt.Errorf("deserialize opcode [%s] error: %w", op.SerializeText(), err)
	}

	vm.Tracef("%s", op.SerializeText())
	err = op.Interpret(state)
	if err != nil {
		return err
	}

	return nil
}
