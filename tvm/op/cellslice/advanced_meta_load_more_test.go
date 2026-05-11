package cellslice

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestAdvancedMetaOps(t *testing.T) {
	t.Run("SDBeginsXVariants", func(t *testing.T) {
		state := newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xA, 4).ToSlice())
		if err := SDBEGINSX().Interpret(state); err != nil {
			t.Fatalf("SDBEGINSX failed: %v", err)
		}
		if sl := popCellSliceSlice(t, state); sl.BitsLeft() != 4 || sl.MustLoadUInt(4) != 0xB {
			t.Fatal("unexpected SDBEGINSX remainder")
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xC, 4).ToSlice())
		if err := SDBEGINSXQ().Interpret(state); err != nil {
			t.Fatalf("SDBEGINSXQ failed: %v", err)
		}
		if popCellSliceBool(t, state) {
			t.Fatal("expected SDBEGINSXQ mismatch flag")
		}
		if sl := popCellSliceSlice(t, state); sl.BitsLeft() != 8 {
			t.Fatal("expected SDBEGINSXQ to preserve source")
		}
	})

	t.Run("HashAndDepthIndexedOps", func(t *testing.T) {
		cl := cell.BeginCell().MustStoreRef(cell.BeginCell().EndCell()).EndCell()

		hashOp := CHASHI(0)
		hashDecoded := CHASHI(1)
		if err := hashDecoded.Deserialize(hashOp.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("CHASHI deserialize failed: %v", err)
		}
		state := newCellSliceState()
		pushCellSliceCell(t, state, cl)
		if err := hashDecoded.Interpret(state); err != nil {
			t.Fatalf("CHASHI failed: %v", err)
		}
		hashVal, err := state.Stack.PopInt()
		if err != nil {
			t.Fatalf("failed to pop CHASHI value: %v", err)
		}
		if hashVal.Cmp(new(big.Int).SetBytes(cl.Hash(0))) != 0 {
			t.Fatal("unexpected CHASHI value")
		}

		depthOp := CDEPTHI(0)
		depthDecoded := CDEPTHI(1)
		if err := depthDecoded.Deserialize(depthOp.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("CDEPTHI deserialize failed: %v", err)
		}
		state = newCellSliceState()
		pushCellSliceCell(t, state, cl)
		if err := depthDecoded.Interpret(state); err != nil {
			t.Fatalf("CDEPTHI failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 1 {
			t.Fatalf("unexpected CDEPTHI depth: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceCell(t, state, cl)
		pushCellSliceInt(t, state, 0)
		if err := CHASHIX().Interpret(state); err != nil {
			t.Fatalf("CHASHIX failed: %v", err)
		}
		hashVal, err = state.Stack.PopInt()
		if err != nil {
			t.Fatalf("failed to pop CHASHIX value: %v", err)
		}
		if hashVal.Cmp(new(big.Int).SetBytes(cl.Hash(0))) != 0 {
			t.Fatal("unexpected CHASHIX value")
		}

		state = newCellSliceState()
		pushCellSliceCell(t, state, cl)
		pushCellSliceInt(t, state, 0)
		if err := CDEPTHIX().Interpret(state); err != nil {
			t.Fatalf("CDEPTHIX failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 1 {
			t.Fatalf("unexpected CDEPTHIX depth: %d", got)
		}
	})
}

func TestLittleEndianOpVariants(t *testing.T) {
	if loadLEModeName(15) != "PLDULE8Q" {
		t.Fatalf("unexpected loadLEModeName result: %s", loadLEModeName(15))
	}
	if storeLEModeName(3) != "STULE8" {
		t.Fatalf("unexpected storeLEModeName result: %s", storeLEModeName(3))
	}

	t.Run("WrapperConstructors", func(t *testing.T) {
		ops := []interface{}{
			LDILE4(), LDULE4(), LDILE8(), LDULE8(),
			PLDILE4(), PLDULE4(), PLDILE8(), PLDULE8(),
			LDILE4Q(), LDULE4Q(), LDILE8Q(), LDULE8Q(),
			PLDILE4Q(), PLDULE4Q(), PLDILE8Q(), PLDULE8Q(),
			STILE4(), STULE4(), STILE8(), STULE8(),
		}
		if len(ops) != 20 {
			t.Fatalf("unexpected op count: %d", len(ops))
		}
	})

	t.Run("LoadAndStoreBranches", func(t *testing.T) {
		state := newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreSlice([]byte{0x78, 0x56, 0x34, 0x12}, 32).ToSlice())
		if err := LDULE4().Interpret(state); err != nil {
			t.Fatalf("LDULE4 failed: %v", err)
		}
		if sl := popCellSliceSlice(t, state); sl.BitsLeft() != 0 {
			t.Fatalf("expected LDULE4 to consume slice, got %d bits", sl.BitsLeft())
		}
		if got := popCellSliceInt(t, state); got != 0x12345678 {
			t.Fatalf("unexpected LDULE4 value: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreSlice([]byte{0x11, 0x22, 0x33, 0x44}, 32).ToSlice())
		if err := PLDULE4().Interpret(state); err != nil {
			t.Fatalf("PLDULE4 failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 0x44332211 {
			t.Fatalf("unexpected PLDULE4 value: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreSlice([]byte{0xFF, 0xFF, 0xFF, 0xFF}, 32).ToSlice())
		if err := LDILE8Q().Interpret(state); err != nil {
			t.Fatalf("LDILE8Q failed: %v", err)
		}
		if popCellSliceBool(t, state) {
			t.Fatal("expected LDILE8Q failure flag")
		}
		if sl := popCellSliceSlice(t, state); sl.BitsLeft() != 32 {
			t.Fatal("expected LDILE8Q failure to preserve source")
		}

		state = newCellSliceState()
		pushCellSliceInt(t, state, -2)
		pushCellSliceBuilder(t, state, cell.BeginCell())
		if err := STILE4().Interpret(state); err != nil {
			t.Fatalf("STILE4 failed: %v", err)
		}
		if got := decodeLEInt(popCellSliceBuilder(t, state).EndCell().BeginParse().MustLoadSlice(32), false).Int64(); got != -2 {
			t.Fatalf("unexpected STILE4 value: %d", got)
		}
	})

	t.Run("RoundTripMetadata", func(t *testing.T) {
		loadDecoded := LDILE4()
		if err := loadDecoded.Deserialize(LDULE8Q().Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("LDULE8Q deserialize failed: %v", err)
		}
		if got := loadDecoded.SerializeText(); got != "LDULE8Q" {
			t.Fatalf("unexpected LE load text: %q", got)
		}
		if got := loadDecoded.InstructionBits(); got != 16 {
			t.Fatalf("unexpected LE load bits: %d", got)
		}

		storeDecoded := STILE4()
		if err := storeDecoded.Deserialize(STULE8().Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("STULE8 deserialize failed: %v", err)
		}
		if got := storeDecoded.SerializeText(); got != "STULE8" {
			t.Fatalf("unexpected LE store text: %q", got)
		}
		if got := storeDecoded.InstructionBits(); got != 16 {
			t.Fatalf("unexpected LE store bits: %d", got)
		}

		state := newCellSliceState()
		pushCellSliceInt(t, state, -1)
		pushCellSliceBuilder(t, state, cell.BeginCell())
		if err := STULE8().Interpret(state); err == nil {
			t.Fatal("expected STULE8 to reject negative unsigned values")
		}
	})
}

func TestLoadFamilyVariants(t *testing.T) {
	t.Run("WrapperConstructors", func(t *testing.T) {
		ops := []interface{}{
			LDIX(), LDUX(), PLDIX(), PLDUX(), LDIXQ(), LDUXQ(), PLDIXQ(), PLDUXQ(),
			LDIFIX(8, false, false, false), LDUFIX(8, false, false, true),
			PLDIFIX(8, false, true, false), PLDUFIX(8, true, true, true),
			PLDSLICEX(), LDSLICEXQ(), PLDSLICEXQ(),
			LDSLICEFIX(8, false, false), PLDSLICEFIX(8, true, true),
		}
		if len(ops) != 17 {
			t.Fatalf("unexpected op count: %d", len(ops))
		}
	})

	t.Run("DynamicAndQuietBranches", func(t *testing.T) {
		state := newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		pushCellSliceInt(t, state, 8)
		if err := LDUX().Interpret(state); err != nil {
			t.Fatalf("LDUX failed: %v", err)
		}
		if sl := popCellSliceSlice(t, state); sl.BitsLeft() != 0 {
			t.Fatalf("expected LDUX to consume slice, got %d bits", sl.BitsLeft())
		}
		if got := popCellSliceInt(t, state); got != 0xAB {
			t.Fatalf("unexpected LDUX value: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(1, 1).ToSlice())
		pushCellSliceInt(t, state, 1)
		if err := PLDIX().Interpret(state); err != nil {
			t.Fatalf("PLDIX failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != -1 {
			t.Fatalf("unexpected PLDIX value: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		pushCellSliceInt(t, state, 16)
		if err := LDUXQ().Interpret(state); err != nil {
			t.Fatalf("LDUXQ failed: %v", err)
		}
		if popCellSliceBool(t, state) {
			t.Fatal("expected LDUXQ failure flag")
		}
		if sl := popCellSliceSlice(t, state); sl.BitsLeft() != 8 {
			t.Fatal("expected LDUXQ failure to preserve source")
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		pushCellSliceInt(t, state, 16)
		if err := PLDUXQ().Interpret(state); err != nil {
			t.Fatalf("PLDUXQ failed: %v", err)
		}
		if popCellSliceBool(t, state) {
			t.Fatal("expected PLDUXQ failure flag")
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xCD, 8).ToSlice())
		if err := PLDUFIX(8, false, true, true).Interpret(state); err != nil {
			t.Fatalf("PLDUFIX failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 0xCD {
			t.Fatalf("unexpected PLDUFIX value: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		pushCellSliceInt(t, state, 4)
		if err := PLDSLICEX().Interpret(state); err != nil {
			t.Fatalf("PLDSLICEX failed: %v", err)
		}
		if sl := popCellSliceSlice(t, state); sl.BitsLeft() != 4 || sl.MustLoadUInt(4) != 0xA {
			t.Fatal("unexpected PLDSLICEX result")
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		pushCellSliceInt(t, state, 16)
		if err := LDSLICEXQ().Interpret(state); err != nil {
			t.Fatalf("LDSLICEXQ failed: %v", err)
		}
		if popCellSliceBool(t, state) {
			t.Fatal("expected LDSLICEXQ failure flag")
		}
		if sl := popCellSliceSlice(t, state); sl.BitsLeft() != 8 {
			t.Fatal("expected LDSLICEXQ failure to preserve source")
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		if err := PLDSLICEFIX(4, true, true).Interpret(state); err != nil {
			t.Fatalf("PLDSLICEFIX failed: %v", err)
		}
		if popCellSliceBool(t, state) != true {
			t.Fatal("expected PLDSLICEFIX quiet success flag")
		}
		if sl := popCellSliceSlice(t, state); sl.BitsLeft() != 4 || sl.MustLoadUInt(4) != 0xA {
			t.Fatal("unexpected PLDSLICEFIX part")
		}
	})

	t.Run("RoundTripMetadata", func(t *testing.T) {
		intDecoded := LDIFIX(1, false, false, false)
		if err := intDecoded.Deserialize(LDUFIX(32, true, false, true).Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("LDUFIX deserialize failed: %v", err)
		}
		if got := intDecoded.SerializeText(); got != "LDUQ 32" {
			t.Fatalf("unexpected fixed int text: %q", got)
		}
		if got := intDecoded.InstructionBits(); got != 24 {
			t.Fatalf("unexpected fixed int bits: %d", got)
		}

		sliceDecoded := LDSLICEFIX(1, false, false)
		if err := sliceDecoded.Deserialize(PLDSLICEFIX(16, true, true).Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("PLDSLICEFIX deserialize failed: %v", err)
		}
		if got := sliceDecoded.SerializeText(); got != "PLDSLICEQ 16" {
			t.Fatalf("unexpected fixed slice text: %q", got)
		}
		if got := sliceDecoded.InstructionBits(); got != 24 {
			t.Fatalf("unexpected fixed slice bits: %d", got)
		}

		plduzDecoded := PLDUZ(32)
		if err := plduzDecoded.Deserialize(PLDUZ(64).Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("PLDUZ deserialize failed: %v", err)
		}
		if got := plduzDecoded.InstructionBits(); got != 16 {
			t.Fatalf("unexpected PLDUZ bits: %d", got)
		}

		state := newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0x1122334455667788, 64).ToSlice())
		if err := plduzDecoded.Interpret(state); err != nil {
			t.Fatalf("deserialized PLDUZ failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 0x1122334455667788 {
			t.Fatalf("unexpected PLDUZ value after deserialize: %x", got)
		}
	})
}
