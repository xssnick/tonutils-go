package funcs

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestConfigParamNaNIsMissing(t *testing.T) {
	configRoot := mustConfigRoot(t, 1, cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell())

	t.Run("CONFIGPARAM", func(t *testing.T) {
		st := newFuncTestState(t, map[int]any{9: configRoot})
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("failed to push NaN: %v", err)
		}
		if err := CONFIGPARAM().Interpret(st); err != nil {
			t.Fatalf("CONFIGPARAM failed: %v", err)
		}
		found, err := st.Stack.PopBool()
		if err != nil {
			t.Fatalf("failed to pop CONFIGPARAM result: %v", err)
		}
		if found {
			t.Fatal("CONFIGPARAM returned true for NaN key")
		}
	})

	t.Run("CONFIGOPTPARAM", func(t *testing.T) {
		st := newFuncTestState(t, map[int]any{9: configRoot})
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("failed to push NaN: %v", err)
		}
		if err := CONFIGOPTPARAM().Interpret(st); err != nil {
			t.Fatalf("CONFIGOPTPARAM failed: %v", err)
		}
		value, err := st.Stack.PopAny()
		if err != nil {
			t.Fatalf("failed to pop CONFIGOPTPARAM result: %v", err)
		}
		if value != nil {
			t.Fatalf("CONFIGOPTPARAM returned %T for NaN key, want null", value)
		}
	})

	for _, tt := range []struct {
		name string
		op   vm.OP
	}{
		{name: "CONFIGPARAM validates C7 first", op: CONFIGPARAM()},
		{name: "CONFIGOPTPARAM validates C7 first", op: CONFIGOPTPARAM()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newFuncTestState(t, map[int]any{9: big.NewInt(7)})
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("failed to push NaN: %v", err)
			}
			err := tt.op.Interpret(st)
			if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeTypeCheck {
				t.Fatalf("error = %v, want C7 type check before treating NaN as missing", err)
			}
		})
	}
}

func TestCryptoRawNaNParity(t *testing.T) {
	fullSignature := cell.BeginCell().MustStoreSlice(make([]byte, 64), 512).ToSlice()
	p256Key := cell.BeginCell().MustStoreSlice(make([]byte, 33), 264).ToSlice()
	emptySlice := cell.BeginCell().ToSlice()
	zero := big.NewInt(0)
	one := big.NewInt(1)

	tests := []struct {
		name      string
		op        vm.OP
		stack     []any
		wantCode  int64
		wantBool  *bool
		wantGas   int64
		wantDepth int
	}{
		{name: "CHKSIGNU hash", op: CHKSIGNU(), stack: []any{vm.NaN{}, fullSignature, one}, wantCode: vmerr.CodeRangeCheck},
		{name: "CHKSIGNU key", op: CHKSIGNU(), stack: []any{zero, fullSignature, vm.NaN{}}, wantCode: vmerr.CodeRangeCheck},
		{name: "CHKSIGNS key", op: CHKSIGNS(), stack: []any{emptySlice, fullSignature, vm.NaN{}}, wantCode: vmerr.CodeRangeCheck},
		{name: "ECRECOVER hash", op: ECRECOVER(), stack: []any{vm.NaN{}, zero, zero, zero}, wantCode: vmerr.CodeRangeCheck},
		{name: "ECRECOVER r", op: ECRECOVER(), stack: []any{zero, zero, vm.NaN{}, zero}, wantCode: vmerr.CodeRangeCheck},
		{name: "ECRECOVER s", op: ECRECOVER(), stack: []any{zero, zero, zero, vm.NaN{}}, wantCode: vmerr.CodeRangeCheck},
		{name: "ECRECOVER v precedes NaN s export", op: ECRECOVER(), stack: []any{zero, big.NewInt(256), zero, vm.NaN{}}, wantCode: vmerr.CodeRangeCheck, wantDepth: 1},
		{name: "SECP256K1 key", op: SECP256K1_XONLY_PUBKEY_TWEAK_ADD(), stack: []any{vm.NaN{}, zero}, wantCode: vmerr.CodeRangeCheck},
		{name: "SECP256K1 tweak", op: SECP256K1_XONLY_PUBKEY_TWEAK_ADD(), stack: []any{zero, vm.NaN{}}, wantCode: vmerr.CodeRangeCheck},
		{name: "P256_CHKSIGNU hash", op: P256_CHKSIGNU(), stack: []any{vm.NaN{}, fullSignature, p256Key}, wantCode: vmerr.CodeRangeCheck},
		{name: "RIST255_FROMHASH", op: RIST255_FROMHASH(), stack: []any{zero, vm.NaN{}}, wantCode: vmerr.CodeRangeCheck, wantGas: vm.Rist255FromhashGasPrice},
		{name: "RIST255_VALIDATE", op: RIST255_VALIDATE(), stack: []any{vm.NaN{}}, wantCode: vmerr.CodeRangeCheck, wantGas: vm.Rist255ValidateGasPrice},
		{name: "RIST255_QVALIDATE", op: RIST255_QVALIDATE(), stack: []any{vm.NaN{}}, wantBool: boolTestPtr(false), wantGas: vm.Rist255ValidateGasPrice},
		{name: "RIST255_ADD", op: RIST255_ADD(), stack: []any{vm.NaN{}, zero}, wantCode: vmerr.CodeRangeCheck, wantGas: vm.Rist255AddGasPrice},
		{name: "RIST255_QADD", op: RIST255_QADD(), stack: []any{vm.NaN{}, zero}, wantBool: boolTestPtr(false), wantGas: vm.Rist255AddGasPrice},
		{name: "RIST255_SUB", op: RIST255_SUB(), stack: []any{vm.NaN{}, zero}, wantCode: vmerr.CodeRangeCheck, wantGas: vm.Rist255AddGasPrice},
		{name: "RIST255_QSUB", op: RIST255_QSUB(), stack: []any{vm.NaN{}, zero}, wantBool: boolTestPtr(false), wantGas: vm.Rist255AddGasPrice},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newFuncTestState(t, nil)
			st.GlobalVersion = 13
			pushFuncTestStack(t, st, tt.stack...)

			err := tt.op.Interpret(st)
			if tt.wantCode != 0 {
				code, ok := vmerr.ErrorCode(err)
				if !ok || code != tt.wantCode {
					t.Fatalf("error = %v, want code %d", err, tt.wantCode)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := st.Gas.Used(); got != tt.wantGas {
				t.Fatalf("gas used = %d, want %d", got, tt.wantGas)
			}
			if tt.wantBool != nil {
				got, popErr := st.Stack.PopBool()
				if popErr != nil {
					t.Fatalf("failed to pop quiet result: %v", popErr)
				}
				if got != *tt.wantBool {
					t.Fatalf("quiet result = %v, want %v", got, *tt.wantBool)
				}
			}
			if got := st.Stack.Len(); got != tt.wantDepth {
				t.Fatalf("stack depth = %d, want %d", got, tt.wantDepth)
			}
		})
	}
}

func TestBLSMulGasPrecedesFiniteScalarCheck(t *testing.T) {
	tests := []struct {
		name  string
		op    vm.OP
		point *cell.Slice
		gas   int64
	}{
		{name: "G1", op: BLS_G1_MUL(), point: cell.BeginCell().MustStoreSlice(make([]byte, 48), 384).ToSlice(), gas: vm.BlsG1MulGasPrice},
		{name: "G2", op: BLS_G2_MUL(), point: cell.BeginCell().MustStoreSlice(make([]byte, 96), 768).ToSlice(), gas: vm.BlsG2MulGasPrice},
	}

	for _, tt := range tests {
		t.Run(tt.name+" NaN", func(t *testing.T) {
			st := newFuncTestState(t, nil)
			st.GlobalVersion = 13
			pushFuncTestStack(t, st, tt.point, vm.NaN{})

			err := tt.op.Interpret(st)
			if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeIntOverflow {
				t.Fatalf("error = %v, want integer overflow", err)
			}
			if got := st.Gas.Used(); got != tt.gas {
				t.Fatalf("gas used = %d, want %d", got, tt.gas)
			}
			if got := st.Stack.Len(); got != 1 {
				t.Fatalf("stack depth = %d, want point left on stack", got)
			}
		})

		t.Run(tt.name+" out of gas", func(t *testing.T) {
			st := newFuncTestState(t, nil)
			st.GlobalVersion = 13
			st.Gas = vm.GasWithLimit(tt.gas - 1)
			pushFuncTestStack(t, st, tt.point, big.NewInt(1))

			err := tt.op.Interpret(st)
			if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeOutOfGas {
				t.Fatalf("error = %v, want out of gas", err)
			}
			if got := st.Stack.Len(); got != 2 {
				t.Fatalf("stack depth = %d, want operands untouched", got)
			}
		})
	}
}

func TestBLSCountRejectsUnavailableOperandsBeforeGas(t *testing.T) {
	slice := cell.BeginCell().ToSlice()
	tests := []struct {
		name  string
		op    vm.OP
		stack []any
	}{
		{name: "BLS_AGGREGATE", op: BLS_AGGREGATE(), stack: []any{slice, big.NewInt(2)}},
		{name: "BLS_FASTAGGREGATEVERIFY", op: BLS_FASTAGGREGATEVERIFY(), stack: []any{slice, big.NewInt(2), slice, slice}},
		{name: "BLS_AGGREGATEVERIFY", op: BLS_AGGREGATEVERIFY(), stack: []any{slice, big.NewInt(1), slice}},
		{name: "BLS_G1_MULTIEXP", op: BLS_G1_MULTIEXP(), stack: []any{slice, big.NewInt(1)}},
		{name: "BLS_G2_MULTIEXP", op: BLS_G2_MULTIEXP(), stack: []any{slice, big.NewInt(1)}},
		{name: "BLS_PAIRING", op: BLS_PAIRING(), stack: []any{slice, big.NewInt(1)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newFuncTestState(t, nil)
			st.GlobalVersion = 13
			pushFuncTestStack(t, st, tt.stack...)

			err := tt.op.Interpret(st)
			if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeRangeCheck {
				t.Fatalf("error = %v, want range check", err)
			}
			if got := st.Gas.Used(); got != 0 {
				t.Fatalf("gas used = %d, want 0", got)
			}
			if got := st.Stack.Len(); got != 1 {
				t.Fatalf("stack depth = %d, want one unavailable operand placeholder left", got)
			}
		})
	}
}

func TestBLSWireValidationOrderParity(t *testing.T) {
	shortG1 := funcBLSOrderByteSlice(10)
	shortG2 := funcBLSOrderByteSlice(10)
	fullG1 := funcBLSOrderByteSlice(48)
	fullG2 := funcBLSOrderByteSlice(96)
	msg := funcBLSOrderByteSlice(1)
	nonByteMsg := cell.BeginCell().MustStoreUInt(0, 1).ToSlice()

	tests := []struct {
		name      string
		op        vm.OP
		stack     []any
		wantCode  int64
		wantGas   int64
		wantDepth int
	}{
		{
			name:      "BLS_VERIFY short sig before bad pub type",
			op:        BLS_VERIFY(),
			stack:     []any{big.NewInt(7), msg, shortG2},
			wantCode:  vmerr.CodeCellUnderflow,
			wantGas:   vm.BlsVerifyGasPrice,
			wantDepth: 2,
		},
		{
			name:      "BLS_AGGREGATE all sizes before curve decode",
			op:        BLS_AGGREGATE(),
			stack:     []any{shortG2, fullG2, big.NewInt(2)},
			wantCode:  vmerr.CodeCellUnderflow,
			wantGas:   vm.BlsAggregateBaseGasPrice + 2*vm.BlsAggregateElementGasPrice,
			wantDepth: 0,
		},
		{
			name:      "BLS_FASTAGGREGATEVERIFY malformed msg after gas",
			op:        BLS_FASTAGGREGATEVERIFY(),
			stack:     []any{big.NewInt(0), nonByteMsg, fullG2},
			wantCode:  vmerr.CodeCellUnderflow,
			wantGas:   vm.BlsFastAggregateVerifyBaseGasPrice,
			wantDepth: 0,
		},
		{
			name:      "BLS_FASTAGGREGATEVERIFY bad pub type before short sig",
			op:        BLS_FASTAGGREGATEVERIFY(),
			stack:     []any{big.NewInt(7), big.NewInt(1), msg, shortG2},
			wantCode:  vmerr.CodeTypeCheck,
			wantGas:   vm.BlsFastAggregateVerifyBaseGasPrice + vm.BlsFastAggregateVerifyElementGasPrice,
			wantDepth: 0,
		},
		{
			name:      "BLS_AGGREGATEVERIFY bad pub type before short sig",
			op:        BLS_AGGREGATEVERIFY(),
			stack:     []any{big.NewInt(7), msg, big.NewInt(1), shortG2},
			wantCode:  vmerr.CodeTypeCheck,
			wantGas:   vm.BlsAggregateVerifyBaseGasPrice + vm.BlsAggregateVerifyElementGasPrice,
			wantDepth: 0,
		},
		{
			name:      "BLS_G1_ADD short top before bad bottom type",
			op:        BLS_G1_ADD(),
			stack:     []any{big.NewInt(7), shortG1},
			wantCode:  vmerr.CodeCellUnderflow,
			wantGas:   vm.BlsG1AddSubGasPrice,
			wantDepth: 1,
		},
		{
			name:      "BLS_G1_SUB short top before bad bottom type",
			op:        BLS_G1_SUB(),
			stack:     []any{big.NewInt(7), shortG1},
			wantCode:  vmerr.CodeCellUnderflow,
			wantGas:   vm.BlsG1AddSubGasPrice,
			wantDepth: 1,
		},
		{
			name:      "BLS_G1_MULTIEXP all sizes before curve decode",
			op:        BLS_G1_MULTIEXP(),
			stack:     []any{shortG1, big.NewInt(1), fullG1, big.NewInt(1), big.NewInt(2)},
			wantCode:  vmerr.CodeCellUnderflow,
			wantGas:   blsCalculateMultiexpGas(2, vm.BlsG1MultiexpBaseGasPrice, vm.BlsG1MultiexpCoef1GasPrice, vm.BlsG1MultiexpCoef2GasPrice),
			wantDepth: 0,
		},
		{
			name:      "BLS_G2_ADD short top before bad bottom type",
			op:        BLS_G2_ADD(),
			stack:     []any{big.NewInt(7), shortG2},
			wantCode:  vmerr.CodeCellUnderflow,
			wantGas:   vm.BlsG2AddSubGasPrice,
			wantDepth: 1,
		},
		{
			name:      "BLS_G2_SUB short top before bad bottom type",
			op:        BLS_G2_SUB(),
			stack:     []any{big.NewInt(7), shortG2},
			wantCode:  vmerr.CodeCellUnderflow,
			wantGas:   vm.BlsG2AddSubGasPrice,
			wantDepth: 1,
		},
		{
			name:      "BLS_G2_MULTIEXP all sizes before curve decode",
			op:        BLS_G2_MULTIEXP(),
			stack:     []any{shortG2, big.NewInt(1), fullG2, big.NewInt(1), big.NewInt(2)},
			wantCode:  vmerr.CodeCellUnderflow,
			wantGas:   blsCalculateMultiexpGas(2, vm.BlsG2MultiexpBaseGasPrice, vm.BlsG2MultiexpCoef1GasPrice, vm.BlsG2MultiexpCoef2GasPrice),
			wantDepth: 0,
		},
		{
			name:      "BLS_PAIRING short g2 before bad g1 type",
			op:        BLS_PAIRING(),
			stack:     []any{big.NewInt(7), shortG2, big.NewInt(1)},
			wantCode:  vmerr.CodeCellUnderflow,
			wantGas:   vm.BlsPairingBaseGasPrice + vm.BlsPairingElementGasPrice,
			wantDepth: 1,
		},
		{
			name:      "BLS_PAIRING all sizes before curve decode",
			op:        BLS_PAIRING(),
			stack:     []any{fullG1, shortG2, fullG1, fullG2, big.NewInt(2)},
			wantCode:  vmerr.CodeCellUnderflow,
			wantGas:   vm.BlsPairingBaseGasPrice + 2*vm.BlsPairingElementGasPrice,
			wantDepth: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newFuncTestState(t, nil)
			st.GlobalVersion = vm.MaxSupportedGlobalVersion
			pushFuncTestStack(t, st, tt.stack...)

			err := tt.op.Interpret(st)
			if code, ok := vmerr.ErrorCode(err); !ok || code != tt.wantCode {
				t.Fatalf("error = %v, want code %d", err, tt.wantCode)
			}
			if got := st.Gas.Used(); got != tt.wantGas {
				t.Fatalf("gas used = %d, want %d", got, tt.wantGas)
			}
			if got := st.Stack.Len(); got != tt.wantDepth {
				t.Fatalf("stack depth = %d, want %d", got, tt.wantDepth)
			}
		})
	}
}

func funcBLSOrderByteSlice(size int) *cell.Slice {
	return cell.BeginCell().MustStoreSlice(make([]byte, size), uint(size*8)).ToSlice()
}

func pushFuncTestStack(t *testing.T, st *vm.State, values ...any) {
	t.Helper()
	for _, value := range values {
		if err := st.Stack.PushAny(value); err != nil {
			t.Fatalf("failed to push %T: %v", value, err)
		}
	}
}

func boolTestPtr(v bool) *bool {
	return &v
}
