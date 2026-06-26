package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestLoadFamilyDynamicMetadataAndErrorEdges(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   interface {
			Deserialize(*cell.Slice) error
			SerializeText() string
		}
		src      func() *cell.Builder
		wantText string
	}{
		{name: "LDIXReadsPLDUXQMode", op: LDIX(), src: PLDUXQ().Serialize, wantText: "LDIX"},
		{name: "PLDUXQReadsLDIXMode", op: PLDUXQ(), src: LDIX().Serialize, wantText: "PLDUXQ"},
		{name: "PLDSLICEXReadsLDSLICEXQMode", op: PLDSLICEX(), src: LDSLICEXQ().Serialize, wantText: "PLDSLICEX"},
		{name: "PLDSLICEXQReadsPLDSLICEXMode", op: PLDSLICEXQ(), src: PLDSLICEX().Serialize, wantText: "PLDSLICEXQ"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.op.Deserialize(tt.src().EndCell().MustBeginParse()); err != nil {
				t.Fatalf("%s deserialize: %v", tt.name, err)
			}
			if got := tt.op.SerializeText(); got != tt.wantText {
				t.Fatalf("%s text = %q, want %q", tt.name, got, tt.wantText)
			}
		})
	}

	if err := LDIX().Deserialize(cell.BeginCell().MustStoreUInt(0xD700>>3, 13).EndCell().MustBeginParse()); err == nil {
		t.Fatal("LDIX short suffix deserialize unexpectedly succeeded")
	}
	if err := PLDSLICEX().Deserialize(cell.BeginCell().MustStoreUInt(0xD718>>2, 14).EndCell().MustBeginParse()); err == nil {
		t.Fatal("PLDSLICEX short suffix deserialize unexpectedly succeeded")
	}
	if err := LDSLICE(1).Deserialize(cell.BeginCell().MustStoreUInt(0xD6, 8).EndCell().MustBeginParse()); err == nil {
		t.Fatal("LDSLICE short suffix deserialize unexpectedly succeeded")
	}
	ldslice := LDSLICE(1)
	if err := ldslice.Deserialize(LDSLICE(5).Serialize().EndCell().MustBeginParse()); err != nil {
		t.Fatalf("LDSLICE deserialize: %v", err)
	}
	if got := ldslice.SerializeText(); got != "LDSLICE 5" {
		t.Fatalf("LDSLICE text after deserialize = %q, want LDSLICE 5", got)
	}

	plduz := PLDUZ(32)
	if got := plduz.SerializeText(); got != "PLDUZ 32" {
		t.Fatalf("PLDUZ text = %q, want PLDUZ 32", got)
	}
	if err := plduz.Deserialize(PLDUZ(256).Serialize().EndCell().MustBeginParse()); err != nil {
		t.Fatalf("PLDUZ deserialize: %v", err)
	}
	if got := plduz.SerializeText(); got != "PLDUZ 256" {
		t.Fatalf("PLDUZ text after deserialize = %q, want PLDUZ 256", got)
	}
	if err := PLDUZ(32).Deserialize(cell.BeginCell().MustStoreUInt(0xD710>>3, 13).EndCell().MustBeginParse()); err == nil {
		t.Fatal("PLDUZ short suffix deserialize unexpectedly succeeded")
	}
}

func TestLoadFamilyDynamicActionEdges(t *testing.T) {
	t.Run("PLDUZStackTypeAndEmptySlice", func(t *testing.T) {
		assertCellSliceVMErrorCode(t, PLDUZ(32).Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)

		st := newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, PLDUZ(32).Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().ToSlice())
		if err := PLDUZ(256).Interpret(st); err != nil {
			t.Fatalf("PLDUZ empty slice failed: %v", err)
		}
		if got := popCellSliceInt(t, st); got != 0 {
			t.Fatalf("PLDUZ empty value = %d, want 0", got)
		}
		if rest := popCellSliceSlice(t, st); rest.BitsLeft() != 0 || rest.RefsNum() != 0 {
			t.Fatalf("PLDUZ empty rest=(%d,%d), want empty", rest.BitsLeft(), rest.RefsNum())
		}
	})

	t.Run("LoadIntPopSliceType", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceInt(t, st, 8)
		assertCellSliceVMErrorCode(t, LDIX().Interpret(st), vmerr.CodeTypeCheck)
	})

	t.Run("LoadIntWidthType", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 8, 0xAB))
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, LDUX().Interpret(st), vmerr.CodeTypeCheck)
	})

	t.Run("LoadIntRangeAndUnderflowOrder", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceInt(t, st, 258)
		assertCellSliceVMErrorCode(t, LDIX().Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 257, 0x80))
		pushCellSliceInt(t, st, 258)
		assertCellSliceVMErrorCode(t, LDIX().Interpret(st), vmerr.CodeRangeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 256, 0x80))
		pushCellSliceInt(t, st, 257)
		assertCellSliceVMErrorCode(t, LDUX().Interpret(st), vmerr.CodeRangeCheck)
	})

	t.Run("LoadIntNonQuietUnderflow", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 4, 0xF0))
		pushCellSliceInt(t, st, 5)
		assertCellSliceVMErrorCode(t, LDIX().Interpret(st), vmerr.CodeCellUnderflow)
	})

	t.Run("LoadIntQuietSuccessAndFailureShapes", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 8, 0xAB))
		pushCellSliceInt(t, st, 4)
		if err := LDIXQ().Interpret(st); err != nil {
			t.Fatalf("LDIXQ success: %v", err)
		}
		if ok := popCellSliceBool(t, st); !ok {
			t.Fatal("LDIXQ success flag = false")
		}
		if rest := popCellSliceSlice(t, st); rest.BitsLeft() != 4 || rest.MustLoadUInt(4) != 0xB {
			t.Fatal("LDIXQ success remainder mismatch")
		}
		if got := popCellSliceInt(t, st); got != -6 {
			t.Fatalf("LDIXQ signed value = %d, want -6", got)
		}

		st = newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 4, 0xF0))
		pushCellSliceInt(t, st, 5)
		if err := PLDIXQ().Interpret(st); err != nil {
			t.Fatalf("PLDIXQ failure: %v", err)
		}
		if ok := popCellSliceBool(t, st); ok {
			t.Fatal("PLDIXQ failure flag = true")
		}
		if st.Stack.Len() != 0 {
			t.Fatalf("PLDIXQ failure stack depth = %d, want 0", st.Stack.Len())
		}
	})

	t.Run("LoadIntQuietSuccessFlagOverflowLeavesValueAndRest", func(t *testing.T) {
		st := newCellSliceState()
		fillLoadSliceStack(t, st, 2)
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 8, 0xAB))
		pushCellSliceInt(t, st, 4)
		assertCellSliceVMErrorCode(t, LDIXQ().Interpret(st), vmerr.CodeStackOverflow)

		rest := popCellSliceSlice(t, st)
		if rest.BitsLeft() != 4 || rest.MustLoadUInt(4) != 0xB {
			t.Fatal("partial LDIXQ rest mismatch")
		}
		if got := popCellSliceInt(t, st); got != -6 {
			t.Fatalf("partial LDIXQ value = %d, want -6", got)
		}
	})

	t.Run("LoadSlicePopSliceTypeAndWidthType", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceInt(t, st, 3)
		assertCellSliceVMErrorCode(t, PLDSLICEX().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 8, 0xAB))
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, LDSLICEXQ().Interpret(st), vmerr.CodeTypeCheck)
	})

	t.Run("LoadSliceRangeAndUnderflow", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceInt(t, st, 1024)
		assertCellSliceVMErrorCode(t, PLDSLICEX().Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 8, 0xAB))
		pushCellSliceInt(t, st, 1024)
		assertCellSliceVMErrorCode(t, PLDSLICEX().Interpret(st), vmerr.CodeRangeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 4, 0xF0))
		pushCellSliceInt(t, st, 5)
		assertCellSliceVMErrorCode(t, PLDSLICEX().Interpret(st), vmerr.CodeCellUnderflow)
	})

	t.Run("LoadSliceQuietSuccessAndFailureShapes", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 8, 0xAB))
		pushCellSliceInt(t, st, 4)
		if err := LDSLICEXQ().Interpret(st); err != nil {
			t.Fatalf("LDSLICEXQ success: %v", err)
		}
		if ok := popCellSliceBool(t, st); !ok {
			t.Fatal("LDSLICEXQ success flag = false")
		}
		if rest := popCellSliceSlice(t, st); rest.BitsLeft() != 4 || rest.MustLoadUInt(4) != 0xB {
			t.Fatal("LDSLICEXQ success remainder mismatch")
		}
		if part := popCellSliceSlice(t, st); part.BitsLeft() != 4 || part.MustLoadUInt(4) != 0xA {
			t.Fatal("LDSLICEXQ success part mismatch")
		}

		st = newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 4, 0xF0))
		if err := LDSLICE(5).Interpret(st); err == nil {
			t.Fatal("LDSLICE non-quiet underflow unexpectedly succeeded")
		} else {
			assertCellSliceVMErrorCode(t, err, vmerr.CodeCellUnderflow)
		}
	})

	t.Run("FixedLoadSliceQuietShapes", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 8, 0xAB))
		if err := LDSLICEFIX(4, true, false).Interpret(st); err != nil {
			t.Fatalf("LDSLICEQ success: %v", err)
		}
		if ok := popCellSliceBool(t, st); !ok {
			t.Fatal("LDSLICEQ success flag = false")
		}
		if rest := popCellSliceSlice(t, st); rest.BitsLeft() != 4 || rest.MustLoadUInt(4) != 0xB {
			t.Fatal("LDSLICEQ success remainder mismatch")
		}
		if part := popCellSliceSlice(t, st); part.BitsLeft() != 4 || part.MustLoadUInt(4) != 0xA {
			t.Fatal("LDSLICEQ success part mismatch")
		}

		st = newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 4, 0xF0))
		if err := LDSLICEFIX(5, true, false).Interpret(st); err != nil {
			t.Fatalf("LDSLICEQ underflow should be quiet: %v", err)
		}
		if ok := popCellSliceBool(t, st); ok {
			t.Fatal("LDSLICEQ underflow flag = true")
		}
		if preserved := popCellSliceSlice(t, st); preserved.BitsLeft() != 4 || preserved.MustLoadUInt(4) != 0xF {
			t.Fatal("LDSLICEQ underflow should preserve source")
		}

		st = newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 8, 0xAB))
		if err := PLDSLICEFIX(4, true, true).Interpret(st); err != nil {
			t.Fatalf("PLDSLICEQ success: %v", err)
		}
		if ok := popCellSliceBool(t, st); !ok {
			t.Fatal("PLDSLICEQ success flag = false")
		}
		if part := popCellSliceSlice(t, st); part.BitsLeft() != 4 || part.MustLoadUInt(4) != 0xA {
			t.Fatal("PLDSLICEQ success part mismatch")
		}

		st = newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 4, 0xF0))
		if err := PLDSLICEFIX(5, true, true).Interpret(st); err != nil {
			t.Fatalf("PLDSLICEQ underflow should be quiet: %v", err)
		}
		if ok := popCellSliceBool(t, st); ok {
			t.Fatal("PLDSLICEQ underflow flag = true")
		}
		if st.Stack.Len() != 0 {
			t.Fatalf("PLDSLICEQ underflow stack depth = %d, want 0", st.Stack.Len())
		}
	})
}

func TestLoadSliceCommonStackOverflowBranches(t *testing.T) {
	t.Run("QuietUnderflowPreserveThenFlagOverflow", func(t *testing.T) {
		st := newCellSliceState()
		fillLoadSliceStack(t, st, 1)
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 4, 0xA0))
		assertCellSliceVMErrorCode(t, loadSliceCommon(st, 5, false, true), vmerr.CodeStackOverflow)
		if preserved := popCellSliceSlice(t, st); preserved.BitsLeft() != 4 || preserved.MustLoadUInt(4) != 0xA {
			t.Fatal("quiet underflow partial preserved slice mismatch")
		}
	})

	t.Run("SuccessRemainderOverflowLeavesPart", func(t *testing.T) {
		st := newCellSliceState()
		fillLoadSliceStack(t, st, 1)
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 8, 0xAB))
		assertCellSliceVMErrorCode(t, loadSliceCommon(st, 4, false, false), vmerr.CodeStackOverflow)
		if part := popCellSliceSlice(t, st); part.BitsLeft() != 4 || part.MustLoadUInt(4) != 0xA {
			t.Fatal("success partial loaded slice mismatch")
		}
	})

	t.Run("QuietPreloadSuccessFlagOverflowLeavesPart", func(t *testing.T) {
		st := newCellSliceState()
		fillLoadSliceStack(t, st, 1)
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 8, 0xAB))
		assertCellSliceVMErrorCode(t, loadSliceCommon(st, 4, true, true), vmerr.CodeStackOverflow)
		if part := popCellSliceSlice(t, st); part.BitsLeft() != 4 || part.MustLoadUInt(4) != 0xA {
			t.Fatal("quiet preload partial loaded slice mismatch")
		}
	})
}

func FuzzTVMLoadFamilyDynamicRules(f *testing.F) {
	for _, seed := range []struct {
		group, mode byte
		have, want  uint16
	}{
		{group: 0, mode: 0, have: 8, want: 4},
		{group: 0, mode: 1, have: 8, want: 8},
		{group: 0, mode: 4, have: 4, want: 5},
		{group: 0, mode: 7, have: 4, want: 5},
		{group: 1, mode: 0, have: 8, want: 4},
		{group: 1, mode: 2, have: 4, want: 5},
		{group: 1, mode: 3, have: 4, want: 5},
		{group: 1, mode: 1, have: 8, want: 1024},
	} {
		f.Add(seed.group, seed.mode, seed.have, seed.want)
	}

	f.Fuzz(func(t *testing.T, rawGroup, rawMode byte, rawHave, rawWant uint16) {
		group := rawGroup % 2
		have := uint(rawHave % 1024)
		want := int64(rawWant % 1030)
		st := newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, have, 0xAA))
		pushCellSliceInt(t, st, want)

		if group == 0 {
			mode := rawMode % 8
			err := loadIntXOp("fuzz", uint64(mode)).Interpret(st)
			maxBits := int64(256)
			if mode&1 == 0 {
				maxBits = 257
			}
			if want > maxBits {
				assertCellSliceVMErrorCode(t, err, vmerr.CodeRangeCheck)
				return
			}
			loadFamilyCheckDynamicResult(t, st, err, have, uint(want), mode&2 != 0, mode&4 != 0, false)
			return
		}

		mode := rawMode % 4
		err := loadSliceXOp("fuzz", uint64(mode)).Interpret(st)
		if want > 1023 {
			assertCellSliceVMErrorCode(t, err, vmerr.CodeRangeCheck)
			return
		}
		loadFamilyCheckDynamicResult(t, st, err, have, uint(want), mode&1 != 0, mode&2 != 0, true)
	})
}

func loadFamilyCheckDynamicResult(t *testing.T, st *vm.State, err error, have, want uint, preload, quiet, sliceValue bool) {
	t.Helper()
	if want > have {
		if quiet {
			if err != nil {
				t.Fatalf("quiet dynamic load failed with error: %v", err)
			}
			if ok := popCellSliceBool(t, st); ok {
				t.Fatal("quiet underflow flag = true, want false")
			}
			if !preload {
				if rest := popCellSliceSlice(t, st); rest.BitsLeft() != have {
					t.Fatalf("quiet underflow preserved bits = %d, want %d", rest.BitsLeft(), have)
				}
			}
			if st.Stack.Len() != 0 {
				t.Fatalf("quiet underflow stack depth = %d, want 0", st.Stack.Len())
			}
			return
		}
		assertCellSliceVMErrorCode(t, err, vmerr.CodeCellUnderflow)
		return
	}

	if err != nil {
		t.Fatalf("dynamic load unexpectedly failed: %v", err)
	}
	if quiet {
		if ok := popCellSliceBool(t, st); !ok {
			t.Fatal("quiet success flag = false")
		}
	}
	if !preload {
		if rest := popCellSliceSlice(t, st); rest.BitsLeft() != have-want {
			t.Fatalf("success remainder bits = %d, want %d", rest.BitsLeft(), have-want)
		}
	}
	if sliceValue {
		if part := popCellSliceSlice(t, st); part.BitsLeft() != want {
			t.Fatalf("loaded slice bits = %d, want %d", part.BitsLeft(), want)
		}
	} else {
		if _, popErr := st.Stack.PopIntFinite(); popErr != nil {
			t.Fatalf("pop loaded int: %v", popErr)
		}
	}
	if st.Stack.Len() != 0 {
		t.Fatalf("success stack depth = %d, want 0", st.Stack.Len())
	}
}

func fillLoadSliceStack(t *testing.T, st *vm.State, spare int) {
	t.Helper()

	for {
		err := st.Stack.PushSmallInt(0)
		if err != nil {
			assertCellSliceVMErrorCode(t, err, vmerr.CodeStackOverflow)
			break
		}
	}
	for i := 0; i < spare; i++ {
		if _, err := st.Stack.PopAny(); err != nil {
			t.Fatalf("failed to free stack slot: %v", err)
		}
	}
}

func loadFamilyBitsSlice(t *testing.T, bits uint, fill byte) *cell.Slice {
	t.Helper()
	buf := make([]byte, (bits+7)/8)
	for i := range buf {
		buf[i] = fill
	}
	return cell.BeginCell().MustStoreSlice(buf, bits).EndCell().MustBeginParse()
}
