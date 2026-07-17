package cellslice

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestStoreIntExtFitHelpersAndNames(t *testing.T) {
	if signedStoreFits(nil, 8) {
		t.Fatal("nil signed value should not fit")
	}
	if unsignedStoreFits(nil, 8) {
		t.Fatal("nil unsigned value should not fit")
	}
	if !signedStoreFits(big.NewInt(0), 0) || signedStoreFits(big.NewInt(1), 0) || signedStoreFits(big.NewInt(-1), 0) {
		t.Fatal("unexpected signed zero-width fit rule")
	}
	if !unsignedStoreFits(big.NewInt(0), 0) || unsignedStoreFits(big.NewInt(1), 0) || unsignedStoreFits(big.NewInt(-1), 0) {
		t.Fatal("unexpected unsigned zero-width fit rule")
	}
	if !signedStoreFits(big.NewInt(-128), 8) || !signedStoreFits(big.NewInt(127), 8) {
		t.Fatal("signed 8-bit bounds should fit")
	}
	if signedStoreFits(big.NewInt(-129), 8) || signedStoreFits(big.NewInt(128), 8) {
		t.Fatal("signed 8-bit out-of-bounds values should not fit")
	}
	if unsignedStoreFits(big.NewInt(-1), 8) || !unsignedStoreFits(big.NewInt(255), 8) || unsignedStoreFits(big.NewInt(256), 8) {
		t.Fatal("unexpected unsigned 8-bit fit rule")
	}

	bits := uint(8)
	for _, tt := range []struct {
		mode     uint8
		variable bool
		text     string
	}{
		{mode: 0, variable: true, text: "STIX"},
		{mode: 1, variable: true, text: "STUX"},
		{mode: 2, variable: true, text: "STIXR"},
		{mode: 3, variable: true, text: "STUXR"},
		{mode: 4, variable: true, text: "STIXQ"},
		{mode: 5, variable: true, text: "STUXQ"},
		{mode: 6, variable: true, text: "STIXRQ"},
		{mode: 7, variable: true, text: "STUXRQ"},
		{mode: 0, text: "STI 8"},
		{mode: 1, text: "STU 8"},
		{mode: 6, text: "STIRQ 8"},
		{mode: 7, text: "STURQ 8"},
	} {
		t.Run(tt.text, func(t *testing.T) {
			var got string
			if tt.variable {
				got = storeIntExtName(tt.mode, nil, true)
			} else {
				got = storeIntExtName(tt.mode, &bits, false)
			}
			if got != tt.text {
				t.Fatalf("name = %q, want %q", got, tt.text)
			}
		})
	}
}

func TestStoreIntExtSerializeDeserializeEdges(t *testing.T) {
	varOp := storeIntVarExtOp(7)
	varDecoded := storeIntVarExtOp(0)
	if err := varDecoded.Deserialize(varOp.Serialize().EndCell().MustBeginParse()); err != nil {
		t.Fatalf("STUXRQ deserialize: %v", err)
	}
	if got := varDecoded.SerializeText(); got != "STUXRQ" {
		t.Fatalf("variable text after deserialize = %q, want STUXRQ", got)
	}
	if err := storeIntVarExtOp(0).Deserialize(cell.BeginCell().MustStoreUInt(0xCF00>>3, 13).EndCell().MustBeginParse()); err == nil {
		t.Fatal("variable short suffix deserialize unexpectedly succeeded")
	}

	fixedOp := storeIntFixedExtOp(6, 9)
	fixedDecoded := storeIntFixedExtOp(0, 1)
	if err := fixedDecoded.Deserialize(fixedOp.Serialize().EndCell().MustBeginParse()); err != nil {
		t.Fatalf("STIRQ deserialize: %v", err)
	}
	if got := fixedDecoded.SerializeText(); got != "STIRQ 9" {
		t.Fatalf("fixed text after deserialize = %q, want STIRQ 9", got)
	}
	if err := storeIntFixedExtOp(0, 1).Deserialize(cell.BeginCell().MustStoreUInt(0xCF08>>3, 13).EndCell().MustBeginParse()); err == nil {
		t.Fatal("fixed short suffix deserialize unexpectedly succeeded")
	}
}

func TestStoreIntExtActionEdges(t *testing.T) {
	t.Run("VariableSigned257Success", func(t *testing.T) {
		st := newCellSliceState()
		pushStoreIntExtOperands(t, st, 0, big.NewInt(-1), cell.BeginCell(), 257, true)
		if err := storeIntVarExtOp(0).Interpret(st); err != nil {
			t.Fatalf("STIX failed: %v", err)
		}
		sl := popCellSliceBuilder(t, st).EndCell().MustBeginParse()
		if got, err := sl.LoadBigInt(257); err != nil || got.Sign() != -1 {
			t.Fatalf("stored signed 257-bit value = %v, err=%v", got, err)
		}
	})

	t.Run("FixedUnsignedReverseSuccess", func(t *testing.T) {
		st := newCellSliceState()
		pushStoreIntExtOperands(t, st, 3, big.NewInt(255), cell.BeginCell(), 8, false)
		if err := storeIntFixedExtOp(3, 8).Interpret(st); err != nil {
			t.Fatalf("STUR failed: %v", err)
		}
		sl := popCellSliceBuilder(t, st).EndCell().MustBeginParse()
		if got, err := sl.LoadBigUInt(8); err != nil || got.Uint64() != 255 {
			t.Fatalf("stored unsigned value = %v, err=%v", got, err)
		}
	})

	t.Run("QuietSuccessStatus", func(t *testing.T) {
		st := newCellSliceState()
		pushStoreIntExtOperands(t, st, 5, big.NewInt(7), cell.BeginCell(), 8, false)
		if err := storeIntFixedExtOp(5, 8).Interpret(st); err != nil {
			t.Fatalf("STUQ failed: %v", err)
		}
		if got := popCellSliceInt(t, st); got != 0 {
			t.Fatalf("quiet success status = %d, want 0", got)
		}
		sl := popCellSliceBuilder(t, st).EndCell().MustBeginParse()
		if got, err := sl.LoadBigUInt(8); err != nil || got.Uint64() != 7 {
			t.Fatalf("quiet stored unsigned value = %v, err=%v", got, err)
		}
	})

	t.Run("QuietRangeFailurePreservesNonReverseOrder", func(t *testing.T) {
		st := newCellSliceState()
		pushStoreIntExtOperands(t, st, 4, big.NewInt(128), cell.BeginCell(), 8, false)
		if err := storeIntFixedExtOp(4, 8).Interpret(st); err != nil {
			t.Fatalf("STIQ range failure should be quiet: %v", err)
		}
		if got := popCellSliceInt(t, st); got != 1 {
			t.Fatalf("quiet range status = %d, want 1", got)
		}
		if got := popCellSliceBuilder(t, st); got.BitsUsed() != 0 {
			t.Fatalf("quiet range builder bits = %d, want 0", got.BitsUsed())
		}
		if got := popStoreIntExtValue(t, st); got.Cmp(big.NewInt(128)) != 0 {
			t.Fatalf("quiet range preserved value = %s, want 128", got)
		}
	})

	t.Run("QuietOverflowFailurePreservesReverseOrder", func(t *testing.T) {
		st := newCellSliceState()
		full := cell.BeginCell().MustStoreSlice(make([]byte, 128), 1023)
		pushStoreIntExtOperands(t, st, 6, big.NewInt(1), full, 8, false)
		if err := storeIntFixedExtOp(6, 8).Interpret(st); err != nil {
			t.Fatalf("STIRQ overflow failure should be quiet: %v", err)
		}
		if got := popCellSliceInt(t, st); got != -1 {
			t.Fatalf("quiet overflow status = %d, want -1", got)
		}
		if got := popStoreIntExtValue(t, st); got.Sign() != 1 {
			t.Fatalf("quiet overflow preserved value = %s, want 1", got)
		}
		if got := popCellSliceBuilder(t, st); got.BitsUsed() != 1023 {
			t.Fatalf("quiet overflow builder bits = %d, want 1023", got.BitsUsed())
		}
	})

	t.Run("QuietNaNRangeFailurePreservesNaN", func(t *testing.T) {
		st := newCellSliceState()
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push NaN: %v", err)
		}
		pushCellSliceBuilder(t, st, cell.BeginCell())
		if err := storeIntFixedExtOp(4, 8).Interpret(st); err != nil {
			t.Fatalf("STIQ NaN failure should be quiet: %v", err)
		}
		if got := popCellSliceInt(t, st); got != 1 {
			t.Fatalf("quiet NaN status = %d, want 1", got)
		}
		_ = popCellSliceBuilder(t, st)
		got, err := st.Stack.PopAny()
		if err != nil {
			t.Fatalf("pop preserved NaN: %v", err)
		}
		if _, ok := got.(vm.NaN); !ok {
			t.Fatalf("preserved value type = %T, want vm.NaN", got)
		}
	})

	t.Run("NonQuietOverflowAndRangeErrors", func(t *testing.T) {
		st := newCellSliceState()
		full := cell.BeginCell().MustStoreSlice(make([]byte, 128), 1023)
		pushStoreIntExtOperands(t, st, 0, big.NewInt(1), full, 8, false)
		assertCellSliceVMErrorCode(t, storeIntFixedExtOp(0, 8).Interpret(st), vmerr.CodeCellOverflow)

		st = newCellSliceState()
		pushStoreIntExtOperands(t, st, 1, big.NewInt(-1), cell.BeginCell(), 8, false)
		assertCellSliceVMErrorCode(t, storeIntFixedExtOp(1, 8).Interpret(st), vmerr.CodeRangeCheck)
	})

	t.Run("StackOrderAndWidthErrors", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceInt(t, st, 1)
		pushCellSliceBuilder(t, st, cell.BeginCell())
		assertCellSliceVMErrorCode(t, storeIntVarExtOp(0).Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceInt(t, st, 1)
		assertCellSliceVMErrorCode(t, storeIntFixedExtOp(0, 8).Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceInt(t, st, 1)
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, storeIntVarExtOp(0).Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceInt(t, st, 1)
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceInt(t, st, 8)
		assertCellSliceVMErrorCode(t, storeIntVarExtOp(0).Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceInt(t, st, 8)
		assertCellSliceVMErrorCode(t, storeIntVarExtOp(2).Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceInt(t, st, 8)
		assertCellSliceVMErrorCode(t, storeIntVarExtOp(0).Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceInt(t, st, 1)
		pushCellSliceInt(t, st, 8)
		assertCellSliceVMErrorCode(t, storeIntVarExtOp(2).Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushStoreIntExtOperands(t, st, 1, big.NewInt(0), cell.BeginCell(), 257, true)
		assertCellSliceVMErrorCode(t, storeIntVarExtOp(1).Interpret(st), vmerr.CodeRangeCheck)

		st = newCellSliceState()
		pushStoreIntExtOperands(t, st, 0, big.NewInt(0), cell.BeginCell(), 258, true)
		assertCellSliceVMErrorCode(t, storeIntVarExtOp(0).Interpret(st), vmerr.CodeRangeCheck)
	})
}

func FuzzTVMStoreIntExtRules(f *testing.F) {
	for _, seed := range []struct {
		mode, variable, full byte
		x, bits              int64
	}{
		{mode: 0, variable: 1, x: -1, bits: 257},
		{mode: 1, variable: 1, x: 5, bits: 3},
		{mode: 1, variable: 1, x: 0, bits: 257},
		{mode: 4, variable: 0, x: 128, bits: 8},
		{mode: 6, variable: 0, full: 1, x: 1, bits: 8},
		{mode: 5, variable: 0, full: 1, x: -1, bits: 1},
	} {
		f.Add(seed.mode, seed.variable, seed.full, seed.x, seed.bits)
	}

	f.Fuzz(func(t *testing.T, rawMode, rawVariable, rawFull byte, rawX, rawBits int64) {
		mode := rawMode % 8
		variable := rawVariable%2 == 0
		signed := mode&1 == 0
		quiet := mode&4 != 0

		bits := storeIntExtFuzzBits(rawBits)
		if !variable {
			bits = positiveStoreIntExtMod(rawBits, 256) + 1
		}

		x := storeIntExtFuzzValue(rawX, bits, signed)
		builder := cell.BeginCell()
		full := rawFull%3 == 0
		if full {
			builder = cell.BeginCell().MustStoreSlice(make([]byte, 128), 1023)
		}
		initialBits := builder.BitsUsed()

		st := newCellSliceState()
		pushStoreIntExtOperands(t, st, mode, x, builder, bits, variable)

		var err error
		if variable {
			err = storeIntVarExtOp(mode).Interpret(st)
		} else {
			err = storeIntFixedExtOp(mode, uint(bits)).Interpret(st)
		}

		maxBits := int64(256)
		if signed {
			maxBits = 257
		}
		if variable && (bits < 0 || bits > maxBits) {
			assertCellSliceVMErrorCode(t, err, vmerr.CodeRangeCheck)
			return
		}

		if full && bits > 0 {
			if quiet {
				if err != nil {
					t.Fatalf("quiet overflow failed with error: %v", err)
				}
				if got := popCellSliceInt(t, st); got != -1 {
					t.Fatalf("quiet overflow status = %d, want -1", got)
				}
				return
			}
			assertCellSliceVMErrorCode(t, err, vmerr.CodeCellOverflow)
			return
		}

		fits := unsignedStoreFits(x, uint(bits))
		if signed {
			fits = signedStoreFits(x, uint(bits))
		}
		if !fits {
			if quiet {
				if err != nil {
					t.Fatalf("quiet range failed with error: %v", err)
				}
				if got := popCellSliceInt(t, st); got != 1 {
					t.Fatalf("quiet range status = %d, want 1", got)
				}
				return
			}
			assertCellSliceVMErrorCode(t, err, vmerr.CodeRangeCheck)
			return
		}

		if err != nil {
			t.Fatalf("store int ext unexpectedly failed: %v", err)
		}
		if quiet {
			if got := popCellSliceInt(t, st); got != 0 {
				t.Fatalf("quiet success status = %d, want 0", got)
			}
		}
		got := popCellSliceBuilder(t, st)
		wantBits := initialBits + uint(bits)
		if got.BitsUsed() != wantBits {
			t.Fatalf("stored bits = %d, want %d", got.BitsUsed(), wantBits)
		}
	})
}

func pushStoreIntExtOperands(t *testing.T, st *vm.State, mode uint8, x *big.Int, builder *cell.Builder, bits int64, variable bool) {
	t.Helper()
	if mode&2 != 0 {
		pushCellSliceBuilder(t, st, builder)
		pushCellSliceBigInt(t, st, x)
	} else {
		pushCellSliceBigInt(t, st, x)
		pushCellSliceBuilder(t, st, builder)
	}
	if variable {
		pushCellSliceInt(t, st, bits)
	}
}

func pushCellSliceBigInt(t *testing.T, st *vm.State, x *big.Int) {
	t.Helper()
	if err := st.Stack.PushInt(new(big.Int).Set(x)); err != nil {
		t.Fatalf("failed to push big int %s: %v", x, err)
	}
}

func popStoreIntExtValue(t *testing.T, st *vm.State) *big.Int {
	t.Helper()
	x, err := st.Stack.PopInt()
	if err != nil {
		t.Fatalf("pop preserved value: %v", err)
	}
	if x == nil {
		t.Fatal("preserved value is NaN")
	}
	return x
}

func storeIntExtFuzzBits(raw int64) int64 {
	return positiveStoreIntExtMod(raw, 280) - 10
}

func storeIntExtFuzzValue(raw, bits int64, signed bool) *big.Int {
	x := big.NewInt(positiveStoreIntExtMod(raw, 513) - 256)
	if raw%17 == 0 {
		return big.NewInt(-1)
	}
	if signed && bits > 0 && bits < 257 && raw%19 == 0 {
		return new(big.Int).Lsh(big.NewInt(1), uint(bits-1))
	}
	if bits == 0 && raw%23 == 0 {
		return big.NewInt(1)
	}
	return x
}

func TestStoreIntExtFuzzBitsNormalizesArbitrarySeeds(t *testing.T) {
	for _, raw := range []int64{
		-1 << 63,
		-123456789,
		-281,
		-280,
		-279,
		-11,
		-10,
		-1,
		0,
		269,
		270,
		280,
		123456789,
		1<<63 - 1,
	} {
		got := storeIntExtFuzzBits(raw)
		if got < -10 || got > 269 {
			t.Fatalf("raw bits %d mapped to %d, want within [-10, 269]", raw, got)
		}
	}
}

func TestStoreIntExtFuzzValueNormalizesArbitrarySeeds(t *testing.T) {
	for _, raw := range []int64{
		-1 << 63,
		-123456789,
		-514,
		-513,
		-512,
		-257,
		-256,
		0,
		256,
		257,
		513,
		123456789,
		1<<63 - 1,
	} {
		got := storeIntExtFuzzValue(raw, 8, false)
		if got.Cmp(big.NewInt(-256)) < 0 || got.Cmp(big.NewInt(256)) > 0 {
			t.Fatalf("raw value %d mapped to %s, want within [-256, 256]", raw, got)
		}
	}
}

func positiveStoreIntExtMod(v, mod int64) int64 {
	v %= mod
	if v < 0 {
		v += mod
	}
	return v
}
