package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func lshiftCodeFromSuffix(t *testing.T, suffix uint8) mathInterpretOp {
	t.Helper()

	op := LSHIFTCODE(0)
	code := cell.BeginCell().
		MustStoreUInt(0xAA, 8).
		MustStoreUInt(uint64(suffix), 8).
		EndCell().
		MustBeginParse()
	if err := op.Deserialize(code); err != nil {
		t.Fatalf("deserialize LSHIFT#: %v", err)
	}
	return op
}

func pushHistoricalNaN(t *testing.T, state *vm.State) {
	t.Helper()
	if err := state.Stack.PushAny(vm.NaN{}); err != nil {
		t.Fatalf("push NaN: %v", err)
	}
}

func requireHistoricalStackInts(t *testing.T, state *vm.State, want ...int64) {
	t.Helper()
	for i := len(want) - 1; i >= 0; i-- {
		if got := popMathCoverageInt(t, state); got != want[i] {
			t.Fatalf("stack[%d] = %d, want %d", i, got, want[i])
		}
	}
	if state.Stack.Len() != 0 {
		t.Fatalf("stack has %d unexpected values", state.Stack.Len())
	}
}

func TestHistoricalImmediateLShiftNaNParity(t *testing.T) {
	tests := []struct {
		name    string
		op      mathInterpretOp
		version int
		wantNaN bool
		wantErr bool
	}{
		{name: "LSHIFT#V13Shift51Overflow", op: lshiftCodeFromSuffix(t, 50), version: 13, wantErr: true},
		{name: "LSHIFT#V13Shift52Zero", op: lshiftCodeFromSuffix(t, 51), version: 13},
		{name: "LSHIFT#V13Shift256Zero", op: lshiftCodeFromSuffix(t, 255), version: 13},
		{name: "LSHIFT#V14Shift52Overflow", op: lshiftCodeFromSuffix(t, 51), version: 14, wantErr: true},
		{name: "QLSHIFT#V13Shift51NaN", op: quietShiftCodeFromSuffix(t, false, 50), version: 13, wantNaN: true},
		{name: "QLSHIFT#V13Shift52Zero", op: quietShiftCodeFromSuffix(t, false, 51), version: 13},
		{name: "QLSHIFT#V13Shift256Zero", op: quietShiftCodeFromSuffix(t, false, 255), version: 13},
		{name: "QLSHIFT#V14Shift52NaN", op: quietShiftCodeFromSuffix(t, false, 51), version: 14, wantNaN: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newMathCoverageState()
			state.GlobalVersion = tt.version
			pushHistoricalNaN(t, state)

			err := tt.op.Interpret(state)
			if tt.wantErr {
				assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
				return
			}
			if err != nil {
				t.Fatalf("interpret: %v", err)
			}
			if tt.wantNaN {
				requireMathStackNaN(t, state)
				return
			}
			requireHistoricalStackInts(t, state, 0)
		})
	}
}

func TestHistoricalLShiftDivFamiliesResurrectNaN(t *testing.T) {
	variableCases := []struct {
		name string
		op   mathInterpretOp
		add  bool
		want []int64
	}{
		{name: "LSHIFTDIV", op: LSHIFTDIV(), want: []int64{0}},
		{name: "LSHIFTDIVR", op: LSHIFTDIVR(), want: []int64{0}},
		{name: "LSHIFTDIVC", op: LSHIFTDIVC(), want: []int64{0}},
		{name: "LSHIFTMOD", op: LSHIFTMOD(), want: []int64{0}},
		{name: "LSHIFTMODR", op: LSHIFTMODR(), want: []int64{0}},
		{name: "LSHIFTMODC", op: LSHIFTMODC(), want: []int64{0}},
		{name: "LSHIFTDIVMOD", op: LSHIFTDIVMOD(), want: []int64{0, 0}},
		{name: "LSHIFTDIVMODR", op: LSHIFTDIVMODR(), want: []int64{0, 0}},
		{name: "LSHIFTDIVMODC", op: LSHIFTDIVMODC(), want: []int64{0, 0}},
		{name: "LSHIFTADDDIVMOD", op: LSHIFTADDDIVMOD(), add: true, want: []int64{2, 1}},
		{name: "LSHIFTADDDIVMODR", op: LSHIFTADDDIVMODR(), add: true, want: []int64{2, 1}},
		{name: "LSHIFTADDDIVMODC", op: LSHIFTADDDIVMODC(), add: true, want: []int64{3, -2}},
	}

	for _, tt := range variableCases {
		t.Run(tt.name, func(t *testing.T) {
			state := newMathCoverageState()
			state.GlobalVersion = 12
			pushHistoricalNaN(t, state)
			if tt.add {
				pushMathCoverageInts(t, state, 7)
			}
			pushMathCoverageInts(t, state, 3, 52)

			if err := tt.op.Interpret(state); err != nil {
				t.Fatalf("interpret: %v", err)
			}
			requireHistoricalStackInts(t, state, tt.want...)
		})
	}

	immediateCases := []struct {
		name string
		op   mathInterpretOp
		add  bool
		want []int64
	}{
		{name: "LSHIFTDIV#", op: lshiftDivCodeOp("LSHIFTDIV#", 0xD4, 1, 0, 52), want: []int64{0}},
		{name: "LSHIFTDIVMODC#", op: lshiftDivCodeOp("LSHIFTDIVMODC#", 0xDE, 3, 2, 52), want: []int64{0, 0}},
		{name: "LSHIFTADDDIVMOD#", op: lshiftDivCodeOp("LSHIFTADDDIVMOD#", 0xD0, 0, 0, 52), add: true, want: []int64{2, 1}},
	}

	for _, tt := range immediateCases {
		t.Run(tt.name, func(t *testing.T) {
			state := newMathCoverageState()
			state.GlobalVersion = 12
			pushHistoricalNaN(t, state)
			if tt.add {
				pushMathCoverageInts(t, state, 7)
			}
			pushMathCoverageInts(t, state, 3)

			if err := tt.op.Interpret(state); err != nil {
				t.Fatalf("interpret: %v", err)
			}
			requireHistoricalStackInts(t, state, tt.want...)
		})
	}

	t.Run("V13PreservesNaN", func(t *testing.T) {
		state := newMathCoverageState()
		state.GlobalVersion = 13
		pushHistoricalNaN(t, state)
		pushMathCoverageInts(t, state, 3, 52)
		assertMathCoverageVMError(t, LSHIFTDIV().Interpret(state), vmerr.CodeIntOverflow)
	})
}

func TestHistoricalMulRShiftFamiliesResurrectNaN(t *testing.T) {
	tests := []struct {
		name      string
		op        mathInterpretOp
		variable  bool
		shift     int64
		want      int64
		version   int
		wantError bool
	}{
		{name: "MULRSHIFTShift1", op: MULRSHIFT(), variable: true, shift: 1, version: 12},
		{name: "MULRSHIFTShift13", op: MULRSHIFT(), variable: true, shift: 13, want: -1, version: 12},
		{name: "MULRSHIFTR", op: MULRSHIFTR(), variable: true, shift: 13, version: 12},
		{name: "MULRSHIFTC", op: MULRSHIFTC(), variable: true, shift: 13, version: 12},
		{name: "MULRSHIFT#", op: MULRSHIFTCODE(13), shift: 13, want: -1, version: 12},
		{name: "MULRSHIFTR#", op: MULRSHIFTRCODE(13), shift: 13, version: 12},
		{name: "MULRSHIFTC#", op: MULRSHIFTCCODE(13), shift: 13, version: 12},
		{name: "MULRSHIFTRV13", op: MULRSHIFTR(), variable: true, shift: 1, version: 13, wantError: true},
		{name: "MULRSHIFTZero", op: MULRSHIFT(), variable: true, shift: 0, version: 12, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newMathCoverageState()
			state.GlobalVersion = tt.version
			pushHistoricalNaN(t, state)
			pushMathCoverageInts(t, state, 2)
			if tt.variable {
				pushMathCoverageInts(t, state, tt.shift)
			}

			err := tt.op.Interpret(state)
			if tt.wantError {
				assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
				return
			}
			if err != nil {
				t.Fatalf("interpret: %v", err)
			}
			requireHistoricalStackInts(t, state, tt.want)
		})
	}
}

func TestHistoricalQuietShiftCompoundNaNParity(t *testing.T) {
	for _, tt := range []struct {
		name          string
		op            mathInterpretOp
		version       int
		mul           bool
		want          int64
		wantNaN       bool
		withRemainder bool
	}{
		{name: "QRSHIFTFloor", op: qShrModFamily(4), version: 13, want: -1},
		{name: "QRSHIFTRound", op: qShrModFamily(5), version: 13},
		{name: "QRSHIFTCeil", op: qShrModFamily(6), version: 13},
		{name: "QRSHIFTV14", op: qShrModFamily(4), version: 14, wantNaN: true},
		{name: "QADDRSHIFTRound", op: qShrModFamily(1), version: 13, withRemainder: true},
		{name: "QMULRSHIFTFloor", op: qMulShrModFamily(4), version: 12, mul: true, want: -1},
		{name: "QMULRSHIFTRound", op: qMulShrModFamily(5), version: 12, mul: true},
		{name: "QMULRSHIFTCeil", op: qMulShrModFamily(6), version: 12, mul: true},
		{name: "QMULRSHIFTRoundV13", op: qMulShrModFamily(5), version: 13, mul: true, wantNaN: true},
		{name: "QMULRSHIFTMODRound", op: qMulShrModFamily(13), version: 12, mul: true, withRemainder: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			state := newMathCoverageState()
			state.GlobalVersion = tt.version
			pushHistoricalNaN(t, state)
			if tt.mul {
				pushMathCoverageInts(t, state, 2)
			}
			if tt.withRemainder && !tt.mul {
				pushMathCoverageInts(t, state, 7)
			}
			pushMathCoverageInts(t, state, 13)

			if err := tt.op.Interpret(state); err != nil {
				t.Fatalf("interpret: %v", err)
			}
			if tt.withRemainder {
				requireMathStackNaN(t, state)
			}
			if tt.wantNaN {
				requireMathStackNaN(t, state)
				return
			}
			requireHistoricalStackInts(t, state, tt.want)
		})
	}
}

func TestLegacyRShiftNaNRounding(t *testing.T) {
	tests := []struct {
		name      string
		roundMode int
		want      int64
	}{
		{name: "Floor", roundMode: cppRoundFloor, want: -1},
		{name: "Nearest", roundMode: cppRoundNearest},
		{name: "Ceil", roundMode: cppRoundCeil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := legacyRShiftNaNResult(12, 13, tt.roundMode)
			if got == nil || got.Cmp(big.NewInt(tt.want)) != 0 {
				t.Fatalf("legacy result = %v, want %d", got, tt.want)
			}
		})
	}
}
