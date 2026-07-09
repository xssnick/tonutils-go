package math

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func fuzzMathVersion(raw int64) int {
	version := int(raw % int64(vm.MaxSupportedGlobalVersion+1))
	if version < 0 {
		version = -version
	}
	return version
}

func TestFuzzMathVersionCoversDefaultRange(t *testing.T) {
	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		if got := fuzzMathVersion(int64(version)); got != version {
			t.Fatalf("seed version %d mapped to %d", version, got)
		}
	}
	if got := fuzzMathVersion(-int64(vm.MaxSupportedGlobalVersion)); got != vm.MaxSupportedGlobalVersion {
		t.Fatalf("negative default version mapped to %d, want %d", got, vm.MaxSupportedGlobalVersion)
	}
	for _, raw := range []int64{
		-1,
		-int64(vm.MaxSupportedGlobalVersion) - 1,
		-int64(vm.MaxSupportedGlobalVersion) - 2,
		-123456789,
		123456789,
	} {
		got := fuzzMathVersion(raw)
		if got < 0 || got > vm.MaxSupportedGlobalVersion {
			t.Fatalf("raw version %d mapped to %d, want within [0, %d]", raw, got, vm.MaxSupportedGlobalVersion)
		}
	}
}

func fuzzMathSmallShift(raw int64) int64 {
	switch raw {
	case -1, 0, 1, 1022, 1023, 1024, 2048:
		return raw
	}
	if raw >= -2048 && raw <= 2048 {
		return raw
	}
	return fuzzMathNonNegativeModulo(raw, 4097) - 2048
}

func TestFuzzMathSmallShiftCoversBoundarySeeds(t *testing.T) {
	for _, shift := range []int64{-1, 0, 1, 1022, 1023, 1024, 2048} {
		if got := fuzzMathSmallShift(shift); got != shift {
			t.Fatalf("seed shift %d mapped to %d", shift, got)
		}
	}
	for _, raw := range []int64{
		-1 << 63,
		-123456789,
		-4098,
		-4097,
		-4096,
		-2049,
		-2048,
		2048,
		2049,
		4097,
		123456789,
		1<<63 - 1,
	} {
		got := fuzzMathSmallShift(raw)
		if got < -2048 || got > 2048 {
			t.Fatalf("raw shift %d mapped to %d, want within [-2048, 2048]", raw, got)
		}
	}
}

func fuzzMathSmallImmediate(raw int64) int8 {
	return int8(fuzzMathNonNegativeModulo(raw, 16) + 1)
}

func TestFuzzMathSmallImmediateNormalizesArbitrarySeeds(t *testing.T) {
	for raw := int64(0); raw < 16; raw++ {
		if got := fuzzMathSmallImmediate(raw); got != int8(raw+1) {
			t.Fatalf("seed immediate %d mapped to %d, want %d", raw, got, raw+1)
		}
	}
	for _, raw := range []int64{
		-1 << 63,
		-123456789,
		-17,
		-16,
		-15,
		-1,
		0,
		15,
		16,
		17,
		123456789,
		1<<63 - 1,
	} {
		got := fuzzMathSmallImmediate(raw)
		if got < 1 || got > 16 {
			t.Fatalf("raw immediate %d mapped to %d, want within [1, 16]", raw, got)
		}
	}
}

func fuzzMathNonNegativeModulo(raw, mod int64) int64 {
	res := raw % mod
	if res < 0 {
		res = -res
	}
	return res
}

func fuzzMathExpectVMError(t *testing.T, err error, code int64) {
	t.Helper()
	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) || vmErr.Code != code {
		t.Fatalf("error = %v, want VM error code %d", err, code)
	}
}

func FuzzTVMVersionedQuietDynamicShiftRanges(f *testing.F) {
	f.Add(int64(12), int64(7), int64(1024), uint8(0))
	f.Add(int64(13), int64(7), int64(1024), uint8(0))
	f.Add(int64(12), int64(7), int64(-1), uint8(1))
	f.Add(int64(13), int64(7), int64(-1), uint8(1))
	f.Add(int64(12), int64(0), int64(1024), uint8(2))
	f.Add(int64(13), int64(0), int64(1024), uint8(2))
	f.Add(int64(vm.MaxSupportedGlobalVersion), int64(7), int64(1024), uint8(0))
	f.Add(int64(vm.MaxSupportedGlobalVersion), int64(7), int64(-1), uint8(1))
	f.Add(int64(vm.MaxSupportedGlobalVersion), int64(0), int64(1024), uint8(2))
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for opKind := uint8(0); opKind < 3; opKind++ {
			f.Add(version, int64(7), int64(1024), opKind)
			f.Add(version, int64(7), int64(-1), opKind)
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawValue, rawShift int64, rawOp uint8) {
		version := fuzzMathVersion(rawVersion)
		shift := fuzzMathSmallShift(rawShift)
		st := newMathCoverageState()
		st.GlobalVersion = version

		opKind := rawOp % 3
		if opKind == 2 {
			if err := st.Stack.PushInt(big.NewInt(shift)); err != nil {
				t.Fatalf("push exponent: %v", err)
			}
		} else {
			if err := st.Stack.PushInt(big.NewInt(rawValue % 1024)); err != nil {
				t.Fatalf("push value: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(shift)); err != nil {
				t.Fatalf("push shift: %v", err)
			}
		}

		var err error
		switch opKind {
		case 0:
			err = QLSHIFT().Interpret(st)
		case 1:
			err = QRSHIFT().Interpret(st)
		default:
			err = QPOW2().Interpret(st)
		}

		invalidShift := shift < 0 || shift > 1023
		if invalidShift && version < 13 {
			fuzzMathExpectVMError(t, err, vmerr.CodeRangeCheck)
			return
		}
		if err != nil {
			t.Fatalf("quiet shift op version=%d shift=%d failed: %v", version, shift, err)
		}
		if invalidShift {
			got, err := st.Stack.PopAny()
			if err != nil {
				t.Fatalf("pop invalid shift result: %v", err)
			}
			requireMathNaN(t, got)
		}
	})
}

func FuzzTVMVersionedLogicNaNRules(f *testing.F) {
	f.Add(int64(12), int64(0), uint8(0), true, true)
	f.Add(int64(13), int64(0), uint8(0), true, true)
	f.Add(int64(12), int64(-1), uint8(1), true, true)
	f.Add(int64(13), int64(-1), uint8(1), true, true)
	f.Add(int64(12), int64(1), uint8(2), true, true)
	f.Add(int64(13), int64(1), uint8(2), true, true)
	f.Add(int64(12), int64(0), uint8(0), false, true)
	f.Add(int64(13), int64(0), uint8(0), false, true)
	f.Add(int64(12), int64(-1), uint8(1), false, true)
	f.Add(int64(13), int64(-1), uint8(1), false, true)
	f.Add(int64(12), int64(1), uint8(2), false, true)
	f.Add(int64(13), int64(1), uint8(2), false, true)
	f.Add(int64(vm.MaxSupportedGlobalVersion), int64(0), uint8(0), true, true)
	f.Add(int64(vm.MaxSupportedGlobalVersion), int64(-1), uint8(1), true, true)
	f.Add(int64(vm.MaxSupportedGlobalVersion), int64(1), uint8(2), true, true)
	f.Add(int64(vm.MaxSupportedGlobalVersion), int64(0), uint8(0), false, true)
	f.Add(int64(vm.MaxSupportedGlobalVersion), int64(-1), uint8(1), false, true)
	f.Add(int64(vm.MaxSupportedGlobalVersion), int64(1), uint8(2), false, true)
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for opKind := uint8(0); opKind < 3; opKind++ {
			for _, nanAtBottom := range []bool{true, false} {
				f.Add(version, int64(0), opKind, true, nanAtBottom)
				f.Add(version, int64(-1), opKind, true, nanAtBottom)
				f.Add(version, int64(1), opKind, true, nanAtBottom)
				f.Add(version, int64(0), opKind, false, nanAtBottom)
				f.Add(version, int64(-1), opKind, false, nanAtBottom)
				f.Add(version, int64(1), opKind, false, nanAtBottom)
			}
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawOther int64, rawOp uint8, quiet bool, nanAtBottom bool) {
		version := fuzzMathVersion(rawVersion)
		opKind := rawOp % 3
		other := big.NewInt(rawOther % 4)
		if rawOther%7 == 0 {
			other.SetInt64(0)
		}
		if rawOther%7 == 1 {
			other.SetInt64(-1)
		}

		st := newMathCoverageState()
		st.GlobalVersion = version
		if nanAtBottom {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
			if err := st.Stack.PushInt(other); err != nil {
				t.Fatalf("push operand: %v", err)
			}
		} else {
			if err := st.Stack.PushInt(other); err != nil {
				t.Fatalf("push operand: %v", err)
			}
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
		}

		var err error
		switch {
		case opKind == 0 && quiet:
			err = QAND().Interpret(st)
		case opKind == 0:
			err = AND().Interpret(st)
		case quiet:
			if opKind == 1 {
				err = QOR().Interpret(st)
			} else {
				err = QXOR().Interpret(st)
			}
		case opKind == 1:
			err = OR().Interpret(st)
		default:
			err = XOR().Interpret(st)
		}

		switch {
		case version < 13 && opKind == 0 && other.Sign() == 0:
			if err != nil {
				t.Fatalf("legacy AND with zero failed: %v", err)
			}
			if got := popMathCoverageInt(t, st); got != 0 {
				t.Fatalf("legacy AND result = %d, want 0", got)
			}
		case version < 13 && opKind == 1 && other.Cmp(bigIntMinusOne) == 0:
			if err != nil {
				t.Fatalf("legacy OR with minus one failed: %v", err)
			}
			if got := popMathCoverageInt(t, st); got != -1 {
				t.Fatalf("legacy OR result = %d, want -1", got)
			}
		case quiet:
			if err != nil {
				t.Fatalf("quiet logic version=%d failed: %v", version, err)
			}
			got, err := st.Stack.PopAny()
			if err != nil {
				t.Fatalf("pop quiet logic result: %v", err)
			}
			requireMathNaN(t, got)
		default:
			fuzzMathExpectVMError(t, err, vmerr.CodeIntOverflow)
		}
	})
}

func FuzzTVMVersionedImmediateShiftNaNRules(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for opKind := uint8(0); opKind < 4; opKind++ {
			f.Add(version, opKind, uint8(0))
			f.Add(version, opKind, uint8(255))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawOp, rawShift uint8) {
		version := fuzzMathVersion(rawVersion)
		st := newMathCoverageState()
		st.GlobalVersion = version
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push NaN: %v", err)
		}

		var err error
		switch rawOp % 4 {
		case 0:
			err = LSHIFTCODE(int8(rawShift)).Interpret(st)
			fuzzMathExpectVMError(t, err, vmerr.CodeIntOverflow)
			return
		case 1:
			err = RSHIFTCODE(int8(rawShift)).Interpret(st)
			if version >= 14 {
				fuzzMathExpectVMError(t, err, vmerr.CodeIntOverflow)
				return
			}
		case 2:
			err = QLSHIFTCODE(int8(rawShift)).Interpret(st)
			if err != nil {
				t.Fatalf("QLSHIFT# version=%d failed: %v", version, err)
			}
			got, err := st.Stack.PopAny()
			if err != nil {
				t.Fatalf("pop QLSHIFT# result: %v", err)
			}
			requireMathNaN(t, got)
			return
		default:
			err = QRSHIFTCODE(int8(rawShift)).Interpret(st)
			if version >= 14 {
				if err != nil {
					t.Fatalf("QRSHIFT# version=%d failed: %v", version, err)
				}
				got, err := st.Stack.PopAny()
				if err != nil {
					t.Fatalf("pop QRSHIFT# result: %v", err)
				}
				requireMathNaN(t, got)
				return
			}
		}

		if err != nil {
			t.Fatalf("immediate shift op version=%d kind=%d failed: %v", version, rawOp%4, err)
		}
		if got := popMathCoverageInt(t, st); got != 0 {
			t.Fatalf("immediate shift op version=%d kind=%d result = %d, want 0", version, rawOp%4, got)
		}
	})
}

func FuzzTVMRShiftFloorRelations(f *testing.F) {
	for _, x := range []int64{-1025, -7, -1, 0, 1, 7, 1025} {
		for _, shift := range []uint16{0, 1, 2, 255, 256, 257, 299} {
			f.Add(x, shift, false, false)
			f.Add(x, shift, true, false)
			f.Add(x, shift, false, true)
			f.Add(x, shift, true, true)
		}
	}

	f.Fuzz(func(t *testing.T, rawX int64, rawShift uint16, immediate, nanValue bool) {
		x := rawX % 1_000_000
		st := newMathCoverageState()
		if nanValue {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN value: %v", err)
			}
		} else {
			pushMathCoverageInts(t, st, x)
		}

		var shift uint
		var err error
		if immediate {
			encoded := uint64(uint8(rawShift))
			op := RSHIFTCODEFLOOR(0)
			if op.DeserializeSuffix == nil {
				t.Fatal("RSHIFT# floor must have a suffix decoder")
			}
			if err := op.DeserializeSuffix(vmCellWithByte(t, encoded)); err != nil {
				t.Fatalf("decode immediate floor shift: %v", err)
			}
			shift = uint(encoded + 1)
			err = op.Interpret(st)
		} else {
			shiftVal := int64(rawShift % 300)
			pushMathCoverageInts(t, st, shiftVal)
			err = RSHIFTFLOOR().Interpret(st)
			if shiftVal > 256 {
				fuzzMathExpectVMError(t, err, vmerr.CodeRangeCheck)
				return
			}
			if nanValue && shiftVal == 0 {
				fuzzMathExpectVMError(t, err, vmerr.CodeIntOverflow)
				return
			}
			shift = uint(shiftVal)
		}
		if err != nil {
			t.Fatalf("floor shift immediate=%t x=%d shift=%d failed: %v", immediate, x, shift, err)
		}

		want := new(big.Int).Rsh(big.NewInt(x), shift)
		if nanValue {
			want.SetInt64(0)
			if shift > 12 {
				want.SetInt64(-1)
			}
		}
		got, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("pop floor shift result: %v", err)
		}
		if got.Cmp(want) != 0 {
			t.Fatalf("floor shift immediate=%t x=%d shift=%d got=%s want=%s", immediate, x, shift, got, want)
		}
	})
}

func FuzzTVMRoundedRightShiftNaNRules(f *testing.F) {
	for _, shift := range []uint16{0, 1, 13, 256, 257, 299} {
		for opKind := uint8(0); opKind < 4; opKind++ {
			f.Add(shift, opKind)
		}
	}

	f.Fuzz(func(t *testing.T, rawShift uint16, rawOp uint8) {
		opKind := rawOp % 4
		st := newMathCoverageState()
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push NaN value: %v", err)
		}

		var err error
		switch opKind {
		case 0:
			shift := int64(rawShift % 300)
			pushMathCoverageInts(t, st, shift)
			err = RSHIFTR().Interpret(st)
			expectRoundedDynamicNaNShift(t, err, st, shift)
		case 1:
			shift := int64(rawShift % 300)
			pushMathCoverageInts(t, st, shift)
			err = RSHIFTC().Interpret(st)
			expectRoundedDynamicNaNShift(t, err, st, shift)
		case 2:
			op := RSHIFTRCODE(0)
			if err := op.DeserializeSuffix(vmCellWithByte(t, uint64(uint8(rawShift)))); err != nil {
				t.Fatalf("decode RSHIFTR# suffix: %v", err)
			}
			err = op.Interpret(st)
			expectRoundedImmediateNaNShift(t, err, st)
		default:
			op := RSHIFTCCODE(0)
			if err := op.DeserializeSuffix(vmCellWithByte(t, uint64(uint8(rawShift)))); err != nil {
				t.Fatalf("decode RSHIFTC# suffix: %v", err)
			}
			err = op.Interpret(st)
			expectRoundedImmediateNaNShift(t, err, st)
		}
	})
}

func expectRoundedDynamicNaNShift(t *testing.T, err error, st *vm.State, shift int64) {
	t.Helper()

	if shift > 256 {
		fuzzMathExpectVMError(t, err, vmerr.CodeRangeCheck)
		return
	}
	if shift == 0 {
		fuzzMathExpectVMError(t, err, vmerr.CodeIntOverflow)
		return
	}
	if err != nil {
		t.Fatalf("rounded dynamic shift=%d failed: %v", shift, err)
	}
	if got := popMathCoverageInt(t, st); got != 0 {
		t.Fatalf("rounded dynamic shift=%d result = %d, want 0", shift, got)
	}
}

func expectRoundedImmediateNaNShift(t *testing.T, err error, st *vm.State) {
	t.Helper()

	if err != nil {
		t.Fatalf("rounded immediate shift failed: %v", err)
	}
	if got := popMathCoverageInt(t, st); got != 0 {
		t.Fatalf("rounded immediate result = %d, want 0", got)
	}
}

func FuzzTVMFiniteDynamicShiftModNaNRules(f *testing.F) {
	for _, shift := range []uint16{0, 1, 13, 256, 257, 299} {
		for opKind := uint8(0); opKind < 6; opKind++ {
			f.Add(shift, opKind)
		}
	}

	f.Fuzz(func(t *testing.T, rawShift uint16, rawOp uint8) {
		shift := int64(rawShift % 300)
		st := newMathCoverageState()
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push NaN value: %v", err)
		}
		pushMathCoverageInts(t, st, shift)

		var err error
		switch rawOp % 6 {
		case 0:
			err = MODPOW2().Interpret(st)
		case 1:
			err = MODPOW2R().Interpret(st)
		case 2:
			err = MODPOW2C().Interpret(st)
		case 3:
			err = RSHIFTMOD().Interpret(st)
		case 4:
			err = RSHIFTMODR().Interpret(st)
		default:
			err = RSHIFTMODC().Interpret(st)
		}

		if shift > 256 {
			fuzzMathExpectVMError(t, err, vmerr.CodeRangeCheck)
			return
		}
		fuzzMathExpectVMError(t, err, vmerr.CodeIntOverflow)
	})
}

func FuzzTVMMulDynamicShiftModNaNRules(f *testing.F) {
	for _, shift := range []uint16{0, 1, 13, 256, 257, 299} {
		for opKind := uint8(0); opKind < 6; opKind++ {
			for nanSlot := uint8(0); nanSlot < 3; nanSlot++ {
				f.Add(shift, opKind, nanSlot)
			}
		}
	}

	f.Fuzz(func(t *testing.T, rawShift uint16, rawOp, rawNaNSlot uint8) {
		shift := int64(rawShift % 300)
		nanSlot := rawNaNSlot % 3

		st := newMathCoverageState()
		if nanSlot == 0 {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN x: %v", err)
			}
		} else {
			pushMathCoverageInts(t, st, 2)
		}
		if nanSlot == 1 {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN y: %v", err)
			}
		} else {
			pushMathCoverageInts(t, st, 3)
		}
		if nanSlot == 2 {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN shift: %v", err)
			}
		} else {
			pushMathCoverageInts(t, st, shift)
		}

		var err error
		switch rawOp % 6 {
		case 0:
			err = MULMODPOW2_VAR().Interpret(st)
		case 1:
			err = MULMODPOW2R_VAR().Interpret(st)
		case 2:
			err = MULMODPOW2C_VAR().Interpret(st)
		case 3:
			err = MULRSHIFTMOD_VAR().Interpret(st)
		case 4:
			err = MULRSHIFTRMOD_VAR().Interpret(st)
		default:
			err = MULRSHIFTCMOD_VAR().Interpret(st)
		}

		if nanSlot == 2 || shift > 256 {
			fuzzMathExpectVMError(t, err, vmerr.CodeRangeCheck)
			return
		}
		fuzzMathExpectVMError(t, err, vmerr.CodeIntOverflow)
	})
}

func FuzzTVMMulImmediateShiftModNaNRules(f *testing.F) {
	for shift := uint8(0); shift < 16; shift++ {
		for opKind := uint8(0); opKind < 6; opKind++ {
			for nanSlot := uint8(0); nanSlot < 2; nanSlot++ {
				f.Add(shift, opKind, nanSlot)
			}
		}
	}
	f.Add(uint8(255), uint8(0), uint8(0))
	f.Add(uint8(255), uint8(5), uint8(1))

	f.Fuzz(func(t *testing.T, rawShift, rawOp, rawNaNSlot uint8) {
		nanSlot := rawNaNSlot % 2

		st := newMathCoverageState()
		if nanSlot == 0 {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN x: %v", err)
			}
		} else {
			pushMathCoverageInts(t, st, 2)
		}
		if nanSlot == 1 {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN y: %v", err)
			}
		} else {
			pushMathCoverageInts(t, st, 3)
		}

		shift := int8(rawShift)
		var err error
		switch rawOp % 6 {
		case 0:
			err = MULMODPOW2CODE(shift).Interpret(st)
		case 1:
			err = MULMODPOW2RCODE(shift).Interpret(st)
		case 2:
			err = MULMODPOW2CCODE(shift).Interpret(st)
		case 3:
			err = MULRSHIFTCODEMOD(shift).Interpret(st)
		case 4:
			err = MULRSHIFTRCODEMOD(shift).Interpret(st)
		default:
			err = MULRSHIFTCCODEMOD(shift).Interpret(st)
		}

		fuzzMathExpectVMError(t, err, vmerr.CodeIntOverflow)
	})
}

func FuzzTVMImmediateAddrShiftCodeModRelations(f *testing.F) {
	seeds := []struct {
		x, y, w int64
		shift   uint8
	}{
		{x: 9, y: 2, w: 4, shift: 3},
		{x: -9, y: 2, w: 4, shift: 3},
		{x: -11, y: 2, w: 4, shift: 3},
		{x: 0, y: 0, w: -7, shift: 1},
	}
	for kind := uint8(0); kind < 6; kind++ {
		for _, seed := range seeds {
			f.Add(seed.x, seed.y, seed.w, seed.shift, kind)
		}
	}

	f.Fuzz(func(t *testing.T, rawX, rawY, rawW int64, rawShift, rawKind uint8) {
		x := rawX % 10_000
		y := rawY % 10_000
		w := rawW % 10_000
		shiftArg := fuzzMathSmallImmediate(int64(rawShift))
		shift := int(shiftArg)

		divisor := new(big.Int).Lsh(bigIntOne, uint(shift))
		var dividend *big.Int
		var opKind = rawKind % 6
		var op interface {
			Interpret(*vm.State) error
		}
		st := newMathCoverageState()
		switch opKind {
		case 0:
			op = ADDRSHIFTCODEMOD(shiftArg)
			pushMathCoverageInts(t, st, x, w)
			dividend = big.NewInt(x + w)
		case 1:
			op = ADDRSHIFTRCODEMOD(shiftArg)
			pushMathCoverageInts(t, st, x, w)
			dividend = big.NewInt(x + w)
		case 2:
			op = ADDRSHIFTCCODEMOD(shiftArg)
			pushMathCoverageInts(t, st, x, w)
			dividend = big.NewInt(x + w)
		case 3:
			op = MULADDRSHIFTCODEMOD(shiftArg)
			pushMathCoverageInts(t, st, x, y, w)
			dividend = big.NewInt(x*y + w)
		case 4:
			op = MULADDRSHIFTRCODEMOD(shiftArg)
			pushMathCoverageInts(t, st, x, y, w)
			dividend = big.NewInt(x*y + w)
		default:
			op = MULADDRSHIFTCCODEMOD(shiftArg)
			pushMathCoverageInts(t, st, x, y, w)
			dividend = big.NewInt(x*y + w)
		}

		if err := op.Interpret(st); err != nil {
			t.Fatalf("addrshift code op kind=%d failed: %v", opKind, err)
		}
		gotR := big.NewInt(popMathCoverageInt(t, st))
		gotQ := big.NewInt(popMathCoverageInt(t, st))

		wantQ, wantR := addrShiftCodeModExpected(dividend, divisor, opKind%3)
		if gotQ.Cmp(wantQ) != 0 || gotR.Cmp(wantR) != 0 {
			t.Fatalf("kind=%d x=%d y=%d w=%d shift=%d got q=%s r=%s want q=%s r=%s", opKind, x, y, w, shift, gotQ, gotR, wantQ, wantR)
		}
	})
}

func FuzzTVMImmediateLShiftDivCodeRelations(f *testing.F) {
	seeds := []struct {
		x, w, z int64
		shift   uint8
	}{
		{x: 5, w: 3, z: 2, shift: 3},
		{x: -5, w: 3, z: 2, shift: 3},
		{x: 5, w: -3, z: -2, shift: 3},
		{x: 5, w: 3, z: 0, shift: 3},
	}
	for d := uint8(0); d < 4; d++ {
		for round := uint8(0); round < 3; round++ {
			for _, seed := range seeds {
				f.Add(seed.x, seed.w, seed.z, seed.shift, d, round)
			}
		}
	}

	f.Fuzz(func(t *testing.T, rawX, rawW, rawZ int64, rawShift, rawD, rawRound uint8) {
		x := rawX % 10_000
		w := rawW % 10_000
		z := rawZ % 10_000
		d := int(rawD % 4)
		round := int(rawRound % 3)
		shiftArg := fuzzMathSmallImmediate(int64(rawShift))
		shift := int(shiftArg)

		st := newMathCoverageState()
		if d == 0 {
			pushMathCoverageInts(t, st, x, w, z)
		} else {
			pushMathCoverageInts(t, st, x, z)
		}

		op := lshiftDivCodeOp("FUZZ", 0xD0, d, round, shiftArg)
		err := op.Interpret(st)
		if z == 0 {
			fuzzMathExpectVMError(t, err, vmerr.CodeIntOverflow)
			return
		}
		if err != nil {
			t.Fatalf("lshift div code d=%d round=%d failed: %v", d, round, err)
		}

		dividend := new(big.Int).Lsh(big.NewInt(x), uint(shift))
		if d == 0 {
			dividend.Add(dividend, big.NewInt(w))
		}
		wantQ, wantR := addrShiftCodeModExpected(dividend, big.NewInt(z), uint8(round))
		switch d {
		case 1:
			gotQ := big.NewInt(popMathCoverageInt(t, st))
			if gotQ.Cmp(wantQ) != 0 {
				t.Fatalf("d=%d round=%d q=%s want %s", d, round, gotQ, wantQ)
			}
		case 2:
			gotR := big.NewInt(popMathCoverageInt(t, st))
			if gotR.Cmp(wantR) != 0 {
				t.Fatalf("d=%d round=%d r=%s want %s", d, round, gotR, wantR)
			}
		default:
			gotR := big.NewInt(popMathCoverageInt(t, st))
			gotQ := big.NewInt(popMathCoverageInt(t, st))
			if gotQ.Cmp(wantQ) != 0 || gotR.Cmp(wantR) != 0 {
				t.Fatalf("d=%d round=%d x=%d w=%d z=%d shift=%d got q=%s r=%s want q=%s r=%s", d, round, x, w, z, shift, gotQ, gotR, wantQ, wantR)
			}
		}
	})
}

func FuzzTVMQuietCompoundRelations(f *testing.F) {
	seeds := []struct {
		x, y, w, z int64
		shift      uint8
	}{
		{x: 35, y: 7, w: 4, z: 6, shift: 3},
		{x: -35, y: 7, w: 5, z: -6, shift: 4},
		{x: 35, y: -7, w: -5, z: 0, shift: 5},
	}
	for family := uint8(0); family < 5; family++ {
		for d := uint8(0); d < 4; d++ {
			for round := uint8(0); round < 3; round++ {
				for _, seed := range seeds {
					f.Add(seed.x, seed.y, seed.w, seed.z, seed.shift, family, d, round)
				}
			}
		}
	}

	f.Fuzz(func(t *testing.T, rawX, rawY, rawW, rawZ int64, rawShift, rawFamily, rawD, rawRound uint8) {
		x := rawX % 1_000
		y := rawY % 1_000
		w := rawW % 1_000
		z := rawZ % 1_000
		shift := uint(rawShift % 9)
		family := rawFamily % 5
		d := rawD % 4
		round := rawRound % 3
		args := (d << 2) | round

		st := newMathCoverageState()
		st.GlobalVersion = 4
		op, dividend, divisor := quietCompoundFuzzOpAndInputs(t, st, family, d, args, x, y, w, z, shift)

		err := op.Interpret(st)
		effectiveD := d
		if effectiveD == 0 {
			effectiveD = 3
		}
		if divisor.Sign() == 0 {
			if err != nil {
				t.Fatalf("quiet family=%d d=%d round=%d zero divisor failed: %v", family, d, round, err)
			}
			expectQuietCompoundNaNs(t, st, qInvalidResultCount(effectiveD))
			return
		}
		if err != nil {
			t.Fatalf("quiet family=%d d=%d round=%d failed: %v", family, d, round, err)
		}

		wantQ, wantR := addrShiftCodeModExpected(dividend, divisor, round)
		expectQuietCompoundResults(t, st, effectiveD, wantQ, wantR)
	})
}

func quietCompoundFuzzOpAndInputs(t *testing.T, st *vm.State, family, d, args uint8, x, y, w, z int64, shift uint) (*helpers.AdvancedOP, *big.Int, *big.Int) {
	t.Helper()

	xi := big.NewInt(x)
	yi := big.NewInt(y)
	wi := big.NewInt(w)
	zi := big.NewInt(z)
	shiftI := int64(shift)

	switch family {
	case 0:
		if d == 0 {
			pushMathCoverageInts(t, st, x, w, z)
			return qDivModFamily(args), new(big.Int).Add(new(big.Int).Set(xi), wi), zi
		}
		pushMathCoverageInts(t, st, x, z)
		return qDivModFamily(args), xi, zi
	case 1:
		divisor := new(big.Int).Lsh(bigIntOne, shift)
		if d == 0 {
			pushMathCoverageInts(t, st, x, w, shiftI)
			return qShrModFamily(args), new(big.Int).Add(new(big.Int).Set(xi), wi), divisor
		}
		pushMathCoverageInts(t, st, x, shiftI)
		return qShrModFamily(args), xi, divisor
	case 2:
		dividend := new(big.Int).Mul(new(big.Int).Set(xi), yi)
		if d == 0 {
			pushMathCoverageInts(t, st, x, y, w, z)
			dividend.Add(dividend, wi)
			return qMulDivModFamily(args), dividend, zi
		}
		pushMathCoverageInts(t, st, x, y, z)
		return qMulDivModFamily(args), dividend, zi
	case 3:
		divisor := new(big.Int).Lsh(bigIntOne, shift)
		dividend := new(big.Int).Mul(new(big.Int).Set(xi), yi)
		if d == 0 {
			pushMathCoverageInts(t, st, x, y, w, shiftI)
			dividend.Add(dividend, wi)
			return qMulShrModFamily(args), dividend, divisor
		}
		pushMathCoverageInts(t, st, x, y, shiftI)
		return qMulShrModFamily(args), dividend, divisor
	default:
		dividend := new(big.Int).Lsh(new(big.Int).Set(xi), shift)
		if d == 0 {
			pushMathCoverageInts(t, st, x, w, z, shiftI)
			dividend.Add(dividend, wi)
			return qShlDivModFamily(args), dividend, zi
		}
		pushMathCoverageInts(t, st, x, z, shiftI)
		return qShlDivModFamily(args), dividend, zi
	}
}

func expectQuietCompoundNaNs(t *testing.T, st *vm.State, count int) {
	t.Helper()

	if st.Stack.Len() != count {
		t.Fatalf("stack len = %d, want %d NaN results", st.Stack.Len(), count)
	}
	for i := 0; i < count; i++ {
		got, err := st.Stack.PopAny()
		if err != nil {
			t.Fatalf("pop NaN result %d: %v", i, err)
		}
		requireMathNaN(t, got)
	}
}

func expectQuietCompoundResults(t *testing.T, st *vm.State, d uint8, wantQ, wantR *big.Int) {
	t.Helper()

	switch d {
	case 1:
		gotQ := big.NewInt(popMathCoverageInt(t, st))
		if gotQ.Cmp(wantQ) != 0 {
			t.Fatalf("q = %s, want %s", gotQ, wantQ)
		}
	case 2:
		gotR := big.NewInt(popMathCoverageInt(t, st))
		if gotR.Cmp(wantR) != 0 {
			t.Fatalf("r = %s, want %s", gotR, wantR)
		}
	case 3:
		gotR := big.NewInt(popMathCoverageInt(t, st))
		gotQ := big.NewInt(popMathCoverageInt(t, st))
		if gotQ.Cmp(wantQ) != 0 || gotR.Cmp(wantR) != 0 {
			t.Fatalf("got q=%s r=%s, want q=%s r=%s", gotQ, gotR, wantQ, wantR)
		}
	default:
		t.Fatalf("invalid effective result selector %d", d)
	}
	if st.Stack.Len() != 0 {
		t.Fatalf("stack len after result pop = %d, want 0", st.Stack.Len())
	}
}

func addrShiftCodeModExpected(dividend, divisor *big.Int, round uint8) (*big.Int, *big.Int) {
	switch round {
	case 0:
		return helpers.DivFloor(dividend, divisor)
	case 1:
		q := helpers.DivRound(dividend, divisor)
		return q, new(big.Int).Sub(dividend, new(big.Int).Mul(divisor, q))
	default:
		q := helpers.DivCeil(dividend, divisor)
		return q, new(big.Int).Sub(dividend, new(big.Int).Mul(divisor, q))
	}
}
