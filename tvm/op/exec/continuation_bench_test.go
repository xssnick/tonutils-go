package exec

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

var benchmarkContinuationSink vm.Continuation
var benchmarkIntSink *big.Int
var benchmarkBoolSink bool

func BenchmarkSETCONTARGSOwnedContinuation(b *testing.B) {
	for _, depth := range []int{1, 255, 1024} {
		b.Run(fmt.Sprintf("depth-%d", depth), func(b *testing.B) {
			captured := vm.NewStack()
			for range depth {
				if err := captured.PushSmallInt(0); err != nil {
					b.Fatal(err)
				}
			}
			cont := vm.Continuation(&vm.OrdinaryContinuation{
				Data: vm.ControlData{
					Stack:   captured,
					NumArgs: vm.ControlDataAllArgs,
					CP:      vm.CP,
				},
			})
			state := newTestState()
			op := SETCONTARGS(0, -1)

			b.ReportAllocs()
			for b.Loop() {
				if err := state.Stack.PushOwnedContinuation(cont); err != nil {
					b.Fatal(err)
				}
				if err := op.Interpret(state); err != nil {
					b.Fatal(err)
				}
				var err error
				cont, err = state.Stack.PopContinuation()
				if err != nil {
					b.Fatal(err)
				}
				benchmarkContinuationSink = cont
			}
		})
	}
}

func BenchmarkREPEATZero(b *testing.B) {
	state := newTestState()
	baseCode := state.CurrentCode
	baseC0 := state.Reg.C[0]
	body := vm.Continuation(&vm.QuitContinuation{ExitCode: 0})
	op := REPEAT()

	b.ReportAllocs()
	for b.Loop() {
		state.CurrentCode = baseCode
		state.Reg.C[0] = baseC0
		if err := state.Stack.PushSmallInt(0); err != nil {
			b.Fatal(err)
		}
		if err := state.Stack.PushOwnedContinuation(body); err != nil {
			b.Fatal(err)
		}
		if err := op.Interpret(state); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIsNullContinuationBuiltIn(b *testing.B) {
	cont := vm.Continuation(&vm.OrdinaryContinuation{})

	b.Run("direct-nil", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkBoolSink = cont == nil
		}
	})
	b.Run("typed-nil-parity", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkBoolSink = vm.IsNullContinuation(cont)
		}
	})
}

func BenchmarkRepeatContinuationSavedC0(b *testing.B) {
	body := &vm.OrdinaryContinuation{
		Data: vm.ControlData{Save: vm.Register{C: [4]vm.Continuation{
			&vm.QuitContinuation{},
		}}},
	}
	loop := &vm.RepeatContinuation{
		Count: 1,
		Body:  body,
		After: &vm.QuitContinuation{},
	}
	state := &vm.State{Stack: vm.NewStack()}

	b.ReportAllocs()
	for b.Loop() {
		var err error
		benchmarkContinuationSink, err = loop.Jump(state)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIFBITJMPNotTakenLargeInt(b *testing.B) {
	state := newTestState()
	cont := vm.Continuation(&vm.QuitContinuation{ExitCode: 0})
	x := new(big.Int).Lsh(big.NewInt(1), 200)
	op := IFBITJMP(0)

	b.ReportAllocs()
	for b.Loop() {
		if err := state.Stack.PushOwnedInt(x); err != nil {
			b.Fatal(err)
		}
		if err := state.Stack.PushOwnedContinuation(cont); err != nil {
			b.Fatal(err)
		}
		if err := op.Interpret(state); err != nil {
			b.Fatal(err)
		}
		var err error
		x, err = state.Stack.PopIntFinite()
		if err != nil {
			b.Fatal(err)
		}
		benchmarkIntSink = x
	}
}

func BenchmarkCONDSELCHKLargeInt(b *testing.B) {
	state := newTestState()
	x := new(big.Int).Lsh(big.NewInt(1), 200)
	y := new(big.Int).Lsh(big.NewInt(1), 201)
	op := CONDSELCHK()

	b.ReportAllocs()
	for b.Loop() {
		if err := state.Stack.PushSmallInt(-1); err != nil {
			b.Fatal(err)
		}
		if err := state.Stack.PushOwnedInt(x); err != nil {
			b.Fatal(err)
		}
		// y is discarded by the opcode and never mutated, so its immutable
		// benchmark fixture can be reused on the next iteration.
		if err := state.Stack.PushOwnedInt(y); err != nil {
			b.Fatal(err)
		}
		if err := op.Interpret(state); err != nil {
			b.Fatal(err)
		}
		var err error
		x, err = state.Stack.PopIntFinite()
		if err != nil {
			b.Fatal(err)
		}
		benchmarkIntSink = x
	}
}
