package dict

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func fuzzDictVersion(raw int64) int {
	version := int(raw % int64(vm.MaxSupportedGlobalVersion+1))
	if version < 0 {
		version = -version
	}
	return version
}

func TestFuzzDictVersionCoversDefaultRange(t *testing.T) {
	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		if got := fuzzDictVersion(int64(version)); got != version {
			t.Fatalf("seed version %d mapped to %d", version, got)
		}
	}
	if got := fuzzDictVersion(-int64(vm.MaxSupportedGlobalVersion)); got != vm.MaxSupportedGlobalVersion {
		t.Fatalf("negative default version mapped to %d, want %d", got, vm.MaxSupportedGlobalVersion)
	}
	for _, raw := range []int64{
		-1,
		-int64(vm.MaxSupportedGlobalVersion) - 1,
		-int64(vm.MaxSupportedGlobalVersion) - 2,
		-123456789,
		123456789,
	} {
		got := fuzzDictVersion(raw)
		if got < 0 || got > vm.MaxSupportedGlobalVersion {
			t.Fatalf("raw version %d mapped to %d, want within [0, %d]", raw, got, vm.MaxSupportedGlobalVersion)
		}
	}
}

func FuzzTVMVersionedPrefixDictUnderflowPrecheck(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		f.Add(version, uint8(0))
		f.Add(version, uint8(1))
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawOp uint8) {
		version := fuzzDictVersion(rawVersion)
		opKind := rawOp % 2
		state := &vm.State{
			GlobalVersion: version,

			Stack: vm.NewStack(),
		}

		var (
			err         error
			legacyLen   int
			precheckLen int
		)
		switch opKind {
		case 0:
			if err = state.Stack.PushInt(big.NewInt(4)); err != nil {
				t.Fatalf("push key bits: %v", err)
			}
			err = execPfxDictDelete(state)
			legacyLen = 0
			precheckLen = 1
		default:
			if err = state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0b10, 2).ToSlice()); err != nil {
				t.Fatalf("push key: %v", err)
			}
			if err = state.Stack.PushAny(nil); err != nil {
				t.Fatalf("push root: %v", err)
			}
			if err = state.Stack.PushInt(big.NewInt(4)); err != nil {
				t.Fatalf("push key bits: %v", err)
			}
			err = execPfxDictSet(cell.DictSetModeSet)(state)
			legacyLen = 0
			precheckLen = 3
		}

		assertDictVMErrorCode(t, err, vmerr.CodeStackUnderflow)
		wantLen := legacyLen
		if version >= 9 {
			wantLen = precheckLen
		}
		if state.Stack.Len() != wantLen {
			t.Fatalf("version=%d op=%d stack len = %d, want %d", version, opKind, state.Stack.Len(), wantLen)
		}
	})
}

func pushPfxDictShortStackArgs(t *testing.T, state *vm.State, length int) {
	t.Helper()

	if length >= 3 {
		if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0b10, 2).ToSlice()); err != nil {
			t.Fatalf("push key: %v", err)
		}
	}
	if length >= 2 {
		if err := state.Stack.PushAny(nil); err != nil {
			t.Fatalf("push root: %v", err)
		}
	}
	if length >= 1 {
		if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
			t.Fatalf("push key bits: %v", err)
		}
	}
}

func FuzzTVMVersionedPrefixDictShortStackPrecheckMatrix(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for op := uint8(0); op < 4; op++ {
			for length := uint8(0); length <= 3; length++ {
				f.Add(version, op, length)
			}
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawOp, rawLen uint8) {
		version := fuzzDictVersion(rawVersion)
		op := rawOp % 4
		set := op < 3
		maxShortLen := uint8(3)
		if !set {
			maxShortLen = 2
		}
		length := int(rawLen % (maxShortLen + 1))

		state := &vm.State{
			GlobalVersion: version,

			Stack: vm.NewStack(),
		}
		pushPfxDictShortStackArgs(t, state, length)

		var err error
		switch op {
		case 0:
			err = execPfxDictSet(cell.DictSetModeSet)(state)
		case 1:
			err = execPfxDictSet(cell.DictSetModeReplace)(state)
		case 2:
			err = execPfxDictSet(cell.DictSetModeAdd)(state)
		default:
			err = execPfxDictDelete(state)
		}

		assertDictVMErrorCode(t, err, vmerr.CodeStackUnderflow)
		wantLen := 0
		if version >= 9 {
			wantLen = length
		}
		if state.Stack.Len() != wantLen {
			t.Fatalf("version=%d op=%d shortLen=%d stack len = %d, want %d", version, op, length, state.Stack.Len(), wantLen)
		}
	})
}

func FuzzTVMVersionedPrefixDictGetNilRootMiss(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for op := uint8(0); op < 4; op++ {
			f.Add(version, op)
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawOp uint8) {
		version := fuzzDictVersion(rawVersion)
		op := int(rawOp % 4)
		state := &vm.State{
			GlobalVersion: version,

			Stack: vm.NewStack(),
		}
		if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0b1011, 4).ToSlice()); err != nil {
			t.Fatalf("push input: %v", err)
		}
		if err := state.Stack.PushAny(nil); err != nil {
			t.Fatalf("push root: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
			t.Fatalf("push key bits: %v", err)
		}

		err := execPfxDictGet(op)(state)
		if op&1 != 0 {
			assertDictVMErrorCode(t, err, vmerr.CodeCellUnderflow)
			if state.Stack.Len() != 0 {
				t.Fatalf("PFXDICTGET nil root miss version=%d op=%d left %d stack values", version, op, state.Stack.Len())
			}
			return
		}
		if err != nil {
			t.Fatalf("PFXDICTGET nil root miss version=%d op=%d failed: %v", version, op, err)
		}
		if op == 0 {
			got, popErr := state.Stack.PopBool()
			if popErr != nil || got {
				t.Fatalf("PFXDICTGETQ nil root result = (%v, %v), want false", got, popErr)
			}
		}
		input, popErr := state.Stack.PopSlice()
		if popErr != nil {
			t.Fatalf("pop preserved input: %v", popErr)
		}
		if input.BitsLeft() != 4 {
			t.Fatalf("preserved input bits = %d, want 4", input.BitsLeft())
		}
		if state.Stack.Len() != 0 {
			t.Fatalf("PFXDICTGET nil root miss version=%d op=%d left %d stack values", version, op, state.Stack.Len())
		}
	})
}

func FuzzTVMVersionedPrefixDictOversizedKeyMiss(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for op := uint8(0); op < 4; op++ {
			f.Add(version, op, false)
			f.Add(version, op, true)
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawOp uint8, nilRoot bool) {
		version := fuzzDictVersion(rawVersion)
		op := rawOp % 4
		root := (*cell.Cell)(nil)
		if !nilRoot {
			dict := cell.NewPrefixDict(4)
			value := cell.BeginCell().MustStoreUInt(0xA, 4).EndCell()
			if _, err := dict.SetWithMode(cell.BeginCell().MustStoreUInt(0b10, 2).EndCell(), value, cell.DictSetModeSet); err != nil {
				t.Fatalf("seed prefix dict: %v", err)
			}
			root = dict.AsCell()
		}

		state := &vm.State{
			GlobalVersion: version,

			Stack: vm.NewStack(),
		}
		key := cell.BeginCell().MustStoreUInt(0b11, 2).ToSlice()
		var err error
		switch op {
		case 0:
			if err = state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xE, 4).ToSlice()); err != nil {
				t.Fatalf("push set value: %v", err)
			}
			pushPrefixDictOversizedKeyMissArgs(t, state, key, root)
			err = execPfxDictSet(cell.DictSetModeSet)(state)
		case 1:
			if err = state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xE, 4).ToSlice()); err != nil {
				t.Fatalf("push replace value: %v", err)
			}
			pushPrefixDictOversizedKeyMissArgs(t, state, key, root)
			err = execPfxDictSet(cell.DictSetModeReplace)(state)
		case 2:
			if err = state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xE, 4).ToSlice()); err != nil {
				t.Fatalf("push add value: %v", err)
			}
			pushPrefixDictOversizedKeyMissArgs(t, state, key, root)
			err = execPfxDictSet(cell.DictSetModeAdd)(state)
		default:
			pushPrefixDictOversizedKeyMissArgs(t, state, key, root)
			err = execPfxDictDelete(state)
		}
		if err != nil {
			t.Fatalf("oversized prefix key version=%d op=%d nilRoot=%t failed: %v", version, op, nilRoot, err)
		}
		ok, popErr := state.Stack.PopBool()
		if popErr != nil {
			t.Fatalf("pop result: %v", popErr)
		}
		gotRoot, popErr := state.Stack.PopMaybeCell()
		if popErr != nil {
			t.Fatalf("pop root: %v", popErr)
		}
		if ok || !sameMaybeCell(gotRoot, root) || state.Stack.Len() != 0 {
			t.Fatalf("oversized prefix key version=%d op=%d nilRoot=%t result=%t rootChanged=%t stack=%d", version, op, nilRoot, ok, !sameMaybeCell(gotRoot, root), state.Stack.Len())
		}
	})
}

func pushPrefixDictOversizedKeyMissArgs(t *testing.T, state *vm.State, key *cell.Slice, root *cell.Cell) {
	t.Helper()

	if err := state.Stack.PushSlice(key.Copy()); err != nil {
		t.Fatalf("push key: %v", err)
	}
	if err := pushMaybeCell(state.Stack, root); err != nil {
		t.Fatalf("push root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push key bits: %v", err)
	}
}

func FuzzTVMVersionedPfxDictSwitchNilRootFlagWithRef(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		f.Add(version, uint16(4), uint16(0b1011), uint8(4))
		f.Add(version, uint16(0), uint16(0), uint8(0))
		f.Add(version, uint16(1023), uint16(0xff), uint8(8))
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawBits uint16, rawInput uint16, rawInputBits uint8) {
		version := fuzzDictVersion(rawVersion)
		bits := uint64(rawBits & 1023)
		inputBits := uint(rawInputBits % 9)
		inputValue := uint64(rawInput)
		if inputBits < 16 {
			inputValue &= (uint64(1) << inputBits) - 1
		}

		op := PFXDICTSWITCH(cell.BeginCell().EndCell(), 1)
		code := cell.BeginCell().
			MustStoreSlice([]byte{0xF4, 0xAC}, 13).
			MustStoreBoolBit(false).
			MustStoreRef(cell.BeginCell().EndCell()).
			MustStoreUInt(bits, 10).
			EndCell()
		if err := op.Deserialize(code.MustBeginParse()); err != nil {
			t.Fatalf("deserialize nil-root switch version=%d bits=%d: %v", version, bits, err)
		}
		if !op.rootLoaded || op.root != nil || op.bits != bits {
			t.Fatalf("decoded switch version=%d rootLoaded=%t root=%v bits=%d want %d", version, op.rootLoaded, op.root, op.bits, bits)
		}

		state := &vm.State{
			GlobalVersion: version,

			Stack: vm.NewStack(),
		}
		input := cell.BeginCell().MustStoreUInt(inputValue, inputBits).ToSlice()
		if err := state.Stack.PushSlice(input.Copy()); err != nil {
			t.Fatalf("push input: %v", err)
		}
		if err := op.Interpret(state); err != nil {
			t.Fatalf("interpret nil-root switch version=%d bits=%d inputBits=%d: %v", version, bits, inputBits, err)
		}
		got, err := state.Stack.PopSlice()
		if err != nil {
			t.Fatalf("pop preserved input: %v", err)
		}
		if got.BitsLeft() != inputBits {
			t.Fatalf("preserved input bits version=%d got %d want %d", version, got.BitsLeft(), inputBits)
		}
		if inputBits > 0 && got.MustLoadUInt(inputBits) != inputValue {
			t.Fatalf("preserved input value mismatch")
		}
		if state.Stack.Len() != 0 {
			t.Fatalf("nil-root switch version=%d left %d stack values", version, state.Stack.Len())
		}
	})
}

func FuzzTVMVersionedSubdictPrefixErrors(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for kind := uint8(0); kind < 3; kind++ {
			f.Add(version, kind, false, false, uint8(0))
			f.Add(version, kind, true, false, uint8(1))
			f.Add(version, kind, false, true, uint8(2))
			f.Add(version, kind, true, true, uint8(3))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind uint8, removePrefix bool, rangeErr bool, rawBits uint8) {
		version := fuzzDictVersion(rawVersion)
		kind := dictKeyKind(rawKind % 3)
		state := &vm.State{
			GlobalVersion: version,

			Stack: vm.NewStack(),
		}

		keyBits := int64(8)
		prefixBits := keyBits + 1 + int64(rawBits%4)
		wantCode := int64(vmerr.CodeRangeCheck)
		wantLen := 1
		if !rangeErr {
			prefixBits = 1 + int64(rawBits%8)
			wantCode = int64(vmerr.CodeCellUnderflow)
			wantLen = 0
		}

		switch kind {
		case dictKeySlice:
			sliceBits := uint(keyBits)
			if !rangeErr {
				sliceBits = uint(prefixBits - 1)
			}
			if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0, sliceBits).ToSlice()); err != nil {
				t.Fatalf("push prefix slice: %v", err)
			}
		case dictKeySignedInt:
			value := big.NewInt(0)
			if !rangeErr {
				value = new(big.Int).Lsh(big.NewInt(1), uint(prefixBits))
			}
			if err := state.Stack.PushInt(value); err != nil {
				t.Fatalf("push signed prefix value: %v", err)
			}
		default:
			value := big.NewInt(0)
			if !rangeErr {
				value = new(big.Int).Lsh(big.NewInt(1), uint(prefixBits))
			}
			if err := state.Stack.PushInt(value); err != nil {
				t.Fatalf("push unsigned prefix value: %v", err)
			}
		}
		if err := state.Stack.PushInt(big.NewInt(prefixBits)); err != nil {
			t.Fatalf("push prefix bits: %v", err)
		}
		if err := state.Stack.PushAny(nil); err != nil {
			t.Fatalf("push root: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(keyBits)); err != nil {
			t.Fatalf("push key bits: %v", err)
		}

		err := execSubdict(removePrefix)(dictScalarVariant{kind: kind})(state)
		assertDictVMErrorCode(t, err, wantCode)
		if state.Stack.Len() != wantLen {
			t.Fatalf("subdict prefix error version=%d kind=%d remove=%t rangeErr=%t left %d stack values, want %d", version, kind, removePrefix, rangeErr, state.Stack.Len(), wantLen)
		}
	})
}

func FuzzTVMVersionedSignedDeleteGetRangeErrors(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		f.Add(version, false, false, uint16(0))
		f.Add(version, true, false, uint16(1))
		f.Add(version, false, true, uint16(2))
		f.Add(version, true, true, uint16(3))
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, byRef bool, below bool, delta uint16) {
		version := fuzzDictVersion(rawVersion)
		state := &vm.State{
			GlobalVersion: version,

			Stack: vm.NewStack(),
		}

		key := big.NewInt(128 + int64(delta%32))
		if below {
			key = big.NewInt(-129 - int64(delta%32))
		}
		if err := state.Stack.PushInt(key); err != nil {
			t.Fatalf("push signed key: %v", err)
		}
		if err := state.Stack.PushAny(nil); err != nil {
			t.Fatalf("push root: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
			t.Fatalf("push key bits: %v", err)
		}

		err := execDictDeleteGet(dictValueVariant{kind: dictKeySignedInt, byRef: byRef})(state)
		assertDictVMErrorCode(t, err, vmerr.CodeRangeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("signed DELGET range error left %d stack values", state.Stack.Len())
		}
	})
}

func FuzzTVMVersionedSignedGetRangeMisses(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		f.Add(version, false, false, uint16(0))
		f.Add(version, true, false, uint16(1))
		f.Add(version, false, true, uint16(2))
		f.Add(version, true, true, uint16(3))
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, byRef bool, below bool, delta uint16) {
		version := fuzzDictVersion(rawVersion)
		state := &vm.State{
			GlobalVersion: version,

			Stack: vm.NewStack(),
		}

		key := big.NewInt(128 + int64(delta%32))
		if below {
			key = big.NewInt(-129 - int64(delta%32))
		}
		if err := state.Stack.PushInt(key); err != nil {
			t.Fatalf("push signed key: %v", err)
		}
		if err := state.Stack.PushAny(nil); err != nil {
			t.Fatalf("push root: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
			t.Fatalf("push key bits: %v", err)
		}

		if err := execDictGet(dictValueVariant{kind: dictKeySignedInt, byRef: byRef})(state); err != nil {
			t.Fatalf("signed GET range miss version=%d byRef=%t below=%t failed: %v", version, byRef, below, err)
		}
		ok, err := state.Stack.PopBool()
		if err != nil {
			t.Fatalf("pop miss flag: %v", err)
		}
		if ok || state.Stack.Len() != 0 {
			t.Fatalf("signed GET range miss version=%d byRef=%t below=%t = ok %v stack %d", version, byRef, below, ok, state.Stack.Len())
		}
	})
}

func FuzzTVMVersionedSignedGetOptRefRangeNulls(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		f.Add(version, false, uint16(0))
		f.Add(version, true, uint16(1))
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, below bool, delta uint16) {
		version := fuzzDictVersion(rawVersion)
		state := &vm.State{
			GlobalVersion: version,

			Stack: vm.NewStack(),
		}

		key := big.NewInt(128 + int64(delta%32))
		if below {
			key = big.NewInt(-129 - int64(delta%32))
		}
		if err := state.Stack.PushInt(key); err != nil {
			t.Fatalf("push signed key: %v", err)
		}
		if err := state.Stack.PushAny(nil); err != nil {
			t.Fatalf("push root: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
			t.Fatalf("push key bits: %v", err)
		}

		if err := execDictGetOptRef(dictScalarVariant{kind: dictKeySignedInt})(state); err != nil {
			t.Fatalf("signed GETOPTREF range null version=%d below=%t failed: %v", version, below, err)
		}
		got, err := state.Stack.PopMaybeCell()
		if err != nil {
			t.Fatalf("pop maybe cell: %v", err)
		}
		if got != nil || state.Stack.Len() != 0 {
			t.Fatalf("signed GETOPTREF range null version=%d below=%t = cell %v stack %d", version, below, got, state.Stack.Len())
		}
	})
}

func FuzzTVMVersionedSignedSetGetOptRefRangeErrors(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		f.Add(version, false, false, uint16(0))
		f.Add(version, true, false, uint16(1))
		f.Add(version, false, true, uint16(2))
		f.Add(version, true, true, uint16(3))
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, below bool, delete bool, delta uint16) {
		version := fuzzDictVersion(rawVersion)
		state := &vm.State{
			GlobalVersion: version,

			Stack: vm.NewStack(),
		}

		if delete {
			if err := state.Stack.PushAny(nil); err != nil {
				t.Fatalf("push nil value: %v", err)
			}
		} else {
			ref := cell.BeginCell().MustStoreUInt(0x55, 8).EndCell()
			if err := state.Stack.PushCell(ref); err != nil {
				t.Fatalf("push ref value: %v", err)
			}
		}
		key := big.NewInt(128 + int64(delta%32))
		if below {
			key = big.NewInt(-129 - int64(delta%32))
		}
		if err := state.Stack.PushInt(key); err != nil {
			t.Fatalf("push signed key: %v", err)
		}
		if err := state.Stack.PushAny(nil); err != nil {
			t.Fatalf("push root: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
			t.Fatalf("push key bits: %v", err)
		}

		err := execDictSetGetOptRef(dictScalarVariant{kind: dictKeySignedInt})(state)
		assertDictVMErrorCode(t, err, vmerr.CodeRangeCheck)
		got, popErr := state.Stack.PopMaybeCell()
		if popErr != nil {
			t.Fatalf("pop preserved maybe cell: %v", popErr)
		}
		if (got == nil) != delete || state.Stack.Len() != 0 {
			t.Fatalf("signed SETGETOPTREF range error version=%d below=%t delete=%t left cell %v stack %d", version, below, delete, got, state.Stack.Len())
		}
	})
}

func FuzzTVMVersionedUnsignedSetGetOptRefRangeErrors(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		f.Add(version, false, false, uint16(0))
		f.Add(version, true, false, uint16(1))
		f.Add(version, false, true, uint16(2))
		f.Add(version, true, true, uint16(3))
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, below bool, delete bool, delta uint16) {
		version := fuzzDictVersion(rawVersion)
		state := &vm.State{
			GlobalVersion: version,

			Stack: vm.NewStack(),
		}

		if delete {
			if err := state.Stack.PushAny(nil); err != nil {
				t.Fatalf("push nil value: %v", err)
			}
		} else {
			ref := cell.BeginCell().MustStoreUInt(0x66, 8).EndCell()
			if err := state.Stack.PushCell(ref); err != nil {
				t.Fatalf("push ref value: %v", err)
			}
		}
		key := big.NewInt(256 + int64(delta%32))
		if below {
			key = big.NewInt(-1 - int64(delta%32))
		}
		if err := state.Stack.PushInt(key); err != nil {
			t.Fatalf("push unsigned key: %v", err)
		}
		if err := state.Stack.PushAny(nil); err != nil {
			t.Fatalf("push root: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
			t.Fatalf("push key bits: %v", err)
		}

		err := execDictSetGetOptRef(dictScalarVariant{kind: dictKeyUnsignedInt})(state)
		assertDictVMErrorCode(t, err, vmerr.CodeRangeCheck)
		got, popErr := state.Stack.PopMaybeCell()
		if popErr != nil {
			t.Fatalf("pop preserved maybe cell: %v", popErr)
		}
		if (got == nil) != delete || state.Stack.Len() != 0 {
			t.Fatalf("unsigned SETGETOPTREF range error version=%d below=%t delete=%t left cell %v stack %d", version, below, delete, got, state.Stack.Len())
		}
	})
}

func FuzzTVMVersionedSetGetOptRefShortSliceKey(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		f.Add(version, false, uint8(0))
		f.Add(version, true, uint8(1))
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, delete bool, rawBits uint8) {
		version := fuzzDictVersion(rawVersion)
		state := &vm.State{
			GlobalVersion: version,

			Stack: vm.NewStack(),
		}

		if delete {
			if err := state.Stack.PushAny(nil); err != nil {
				t.Fatalf("push nil value: %v", err)
			}
		} else {
			ref := cell.BeginCell().MustStoreUInt(0x77, 8).EndCell()
			if err := state.Stack.PushCell(ref); err != nil {
				t.Fatalf("push ref value: %v", err)
			}
		}
		bits := uint(rawBits % 8)
		if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0, bits).ToSlice()); err != nil {
			t.Fatalf("push short key: %v", err)
		}
		if err := state.Stack.PushAny(nil); err != nil {
			t.Fatalf("push root: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
			t.Fatalf("push key bits: %v", err)
		}

		err := execDictSetGetOptRef(dictScalarVariant{kind: dictKeySlice})(state)
		assertDictVMErrorCode(t, err, vmerr.CodeCellUnderflow)
		if state.Stack.Len() != 0 {
			t.Fatalf("SETGETOPTREF short key version=%d delete=%t left %d stack values", version, delete, state.Stack.Len())
		}
	})
}

func FuzzTVMVersionedSetGetOptRefPlainOldValueErrors(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		f.Add(version, false, uint16(0))
		f.Add(version, true, uint16(1))
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, delete bool, rawValue uint16) {
		version := fuzzDictVersion(rawVersion)
		key := cell.BeginCell().MustStoreUInt(0x12, 8).EndCell()
		dict := cell.NewDict(8)
		value := cell.BeginCell().MustStoreUInt(uint64(rawValue&0xff), 8)
		if _, err := dict.SetBuilderWithMode(key, value, cell.DictSetModeSet); err != nil {
			t.Fatalf("seed plain dict: %v", err)
		}

		state := &vm.State{
			GlobalVersion: version,

			Stack: vm.NewStack(),
		}
		if delete {
			if err := state.Stack.PushAny(nil); err != nil {
				t.Fatalf("push nil value: %v", err)
			}
		} else {
			ref := cell.BeginCell().MustStoreUInt(0x88, 8).EndCell()
			if err := state.Stack.PushCell(ref); err != nil {
				t.Fatalf("push ref value: %v", err)
			}
		}
		if err := state.Stack.PushSlice(key.MustBeginParse()); err != nil {
			t.Fatalf("push key: %v", err)
		}
		if err := state.Stack.PushCell(dict.AsCell()); err != nil {
			t.Fatalf("push root: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
			t.Fatalf("push key bits: %v", err)
		}

		err := execDictSetGetOptRef(dictScalarVariant{kind: dictKeySlice})(state)
		assertDictVMErrorCode(t, err, vmerr.CodeDict)
		if state.Stack.Len() != 0 {
			t.Fatalf("SETGETOPTREF plain old value version=%d delete=%t left %d stack values", version, delete, state.Stack.Len())
		}
	})
}

func FuzzTVMVersionedMinMaxRefPlainValueErrors(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		f.Add(version, false, false, uint8(0), uint16(0))
		f.Add(version, true, false, uint8(1), uint16(1))
		f.Add(version, false, true, uint8(2), uint16(2))
		f.Add(version, true, true, uint8(0), uint16(3))
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, fetchMax bool, remove bool, rawKind uint8, rawValue uint16) {
		version := fuzzDictVersion(rawVersion)
		key := cell.BeginCell().MustStoreUInt(0x12, 8).EndCell()
		dict := cell.NewDict(8)
		value := cell.BeginCell().MustStoreUInt(uint64(rawValue&0xff), 8)
		if _, err := dict.SetBuilderWithMode(key, value, cell.DictSetModeSet); err != nil {
			t.Fatalf("seed plain dict: %v", err)
		}

		state := &vm.State{
			GlobalVersion: version,

			Stack: vm.NewStack(),
		}
		if err := state.Stack.PushCell(dict.AsCell()); err != nil {
			t.Fatalf("push root: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
			t.Fatalf("push key bits: %v", err)
		}

		kind := dictKeyKind(rawKind % 3)
		err := execDictMinMax(fetchMax, remove)(dictValueVariant{kind: kind, byRef: true})(state)
		assertDictVMErrorCode(t, err, vmerr.CodeDict)
		if state.Stack.Len() != 0 {
			t.Fatalf("MINMAXREF plain value version=%d kind=%d fetchMax=%t remove=%t left %d stack values", version, kind, fetchMax, remove, state.Stack.Len())
		}
	})
}

func FuzzTVMVersionedMinMaxKeyBitsRangeErrors(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		f.Add(version, false, false, uint8(0), uint16(0))
		f.Add(version, true, false, uint8(1), uint16(1))
		f.Add(version, false, true, uint8(2), uint16(2))
		f.Add(version, true, true, uint8(0), uint16(3))
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, fetchMax bool, remove bool, rawKind uint8, delta uint16) {
		version := fuzzDictVersion(rawVersion)
		kind := dictKeyKind(rawKind % 3)
		maxBits := int64(1023)
		switch kind {
		case dictKeySignedInt:
			maxBits = 257
		case dictKeyUnsignedInt:
			maxBits = 256
		}
		keyBits := maxBits + 1 + int64(delta%16)

		root := cell.BeginCell().MustStoreUInt(1, 1).EndCell()
		state := &vm.State{
			GlobalVersion: version,

			Stack: vm.NewStack(),
		}
		if err := state.Stack.PushCell(root); err != nil {
			t.Fatalf("push root: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(keyBits)); err != nil {
			t.Fatalf("push key bits: %v", err)
		}

		err := execDictMinMax(fetchMax, remove)(dictValueVariant{kind: kind, byRef: delta%2 == 0})(state)
		assertDictVMErrorCode(t, err, vmerr.CodeRangeCheck)
		gotRoot, popErr := state.Stack.PopMaybeCell()
		if popErr != nil {
			t.Fatalf("pop preserved root: %v", popErr)
		}
		if !sameMaybeCell(gotRoot, root) || state.Stack.Len() != 0 {
			t.Fatalf("MINMAX key bits range version=%d kind=%d fetchMax=%t remove=%t left root %v stack %d", version, kind, fetchMax, remove, gotRoot, state.Stack.Len())
		}
	})
}

func FuzzTVMVersionedNearKeyBitsRangeErrors(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		f.Add(version, false, false, uint8(0), uint16(0))
		f.Add(version, true, false, uint8(1), uint16(1))
		f.Add(version, false, true, uint8(2), uint16(2))
		f.Add(version, true, true, uint8(0), uint16(3))
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, fetchNext bool, allowEq bool, rawKind uint8, delta uint16) {
		version := fuzzDictVersion(rawVersion)
		kind := dictKeyKind(rawKind % 3)
		maxBits := int64(1023)
		switch kind {
		case dictKeySignedInt:
			maxBits = 257
		case dictKeyUnsignedInt:
			maxBits = 256
		}
		keyBits := maxBits + 1 + int64(delta%16)

		root := cell.BeginCell().MustStoreUInt(1, 1).EndCell()
		state := &vm.State{
			GlobalVersion: version,

			Stack: vm.NewStack(),
		}
		if kind == dictKeySlice {
			if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0, 1).ToSlice()); err != nil {
				t.Fatalf("push key: %v", err)
			}
		} else {
			if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
				t.Fatalf("push key: %v", err)
			}
		}
		if err := state.Stack.PushCell(root); err != nil {
			t.Fatalf("push root: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(keyBits)); err != nil {
			t.Fatalf("push key bits: %v", err)
		}

		err := execDictGetNear(dictNearVariant{kind: kind, fetchNext: fetchNext, allowEq: allowEq})(state)
		assertDictVMErrorCode(t, err, vmerr.CodeRangeCheck)
		gotRoot, popErr := state.Stack.PopMaybeCell()
		if popErr != nil {
			t.Fatalf("pop preserved root: %v", popErr)
		}
		if !sameMaybeCell(gotRoot, root) {
			t.Fatalf("NEAR key bits range version=%d kind=%d root changed", version, kind)
		}
		if kind == dictKeySlice {
			if _, popErr = state.Stack.PopSlice(); popErr != nil {
				t.Fatalf("pop preserved key slice: %v", popErr)
			}
		} else {
			gotKey, popErr := state.Stack.PopInt()
			if popErr != nil {
				t.Fatalf("pop preserved key int: %v", popErr)
			}
			if gotKey.Cmp(big.NewInt(1)) != 0 {
				t.Fatalf("preserved key = %s, want 1", gotKey)
			}
		}
		if state.Stack.Len() != 0 {
			t.Fatalf("NEAR key bits range version=%d kind=%d fetchNext=%t allowEq=%t left %d stack values", version, kind, fetchNext, allowEq, state.Stack.Len())
		}
	})
}

func FuzzTVMVersionedNearShortSliceKeyUnderflow(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		f.Add(version, false, false, uint8(0))
		f.Add(version, true, false, uint8(1))
		f.Add(version, false, true, uint8(2))
		f.Add(version, true, true, uint8(3))
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, fetchNext bool, allowEq bool, rawBits uint8) {
		version := fuzzDictVersion(rawVersion)
		keyBits := uint(8)
		shortBits := uint(rawBits % uint8(keyBits))

		state := &vm.State{
			GlobalVersion: version,

			Stack: vm.NewStack(),
		}
		if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0, shortBits).ToSlice()); err != nil {
			t.Fatalf("push key: %v", err)
		}
		if err := state.Stack.PushAny(nil); err != nil {
			t.Fatalf("push root: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(int64(keyBits))); err != nil {
			t.Fatalf("push key bits: %v", err)
		}

		err := execDictGetNear(dictNearVariant{kind: dictKeySlice, fetchNext: fetchNext, allowEq: allowEq})(state)
		assertDictVMErrorCode(t, err, vmerr.CodeCellUnderflow)
		if state.Stack.Len() != 0 {
			t.Fatalf("NEAR short slice key version=%d fetchNext=%t allowEq=%t shortBits=%d left %d stack values", version, fetchNext, allowEq, shortBits, state.Stack.Len())
		}
	})
}

func FuzzTVMVersionedSignedDeleteRangeErrors(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		f.Add(version, false, uint16(0))
		f.Add(version, true, uint16(1))
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, below bool, delta uint16) {
		version := fuzzDictVersion(rawVersion)
		state := &vm.State{
			GlobalVersion: version,

			Stack: vm.NewStack(),
		}

		key := big.NewInt(128 + int64(delta%32))
		if below {
			key = big.NewInt(-129 - int64(delta%32))
		}
		if err := state.Stack.PushInt(key); err != nil {
			t.Fatalf("push signed key: %v", err)
		}
		if err := state.Stack.PushAny(nil); err != nil {
			t.Fatalf("push root: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
			t.Fatalf("push key bits: %v", err)
		}

		err := execDictDelete(dictScalarVariant{kind: dictKeySignedInt})(state)
		assertDictVMErrorCode(t, err, vmerr.CodeRangeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("signed DEL range error left %d stack values", state.Stack.Len())
		}
	})
}
