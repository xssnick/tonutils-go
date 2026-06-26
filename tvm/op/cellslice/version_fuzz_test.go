package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func fuzzCellSliceVersion(raw int64) int {
	version := int(raw % int64(vm.DefaultGlobalVersion+1))
	if version < 0 {
		version = -version
	}
	return version
}

func TestFuzzCellSliceVersionCoversDefaultRange(t *testing.T) {
	for version := 0; version <= vm.DefaultGlobalVersion; version++ {
		if got := fuzzCellSliceVersion(int64(version)); got != version {
			t.Fatalf("seed version %d mapped to %d", version, got)
		}
	}
	if got := fuzzCellSliceVersion(-int64(vm.DefaultGlobalVersion)); got != vm.DefaultGlobalVersion {
		t.Fatalf("negative default version mapped to %d, want %d", got, vm.DefaultGlobalVersion)
	}
	for _, raw := range []int64{
		-1,
		-int64(vm.DefaultGlobalVersion) - 1,
		-int64(vm.DefaultGlobalVersion) - 2,
		-123456789,
		123456789,
	} {
		got := fuzzCellSliceVersion(raw)
		if got < 0 || got > vm.DefaultGlobalVersion {
			t.Fatalf("raw version %d mapped to %d, want within [0, %d]", raw, got, vm.DefaultGlobalVersion)
		}
	}
}

func FuzzTVMVersionedCellSliceLoadAndCheckEdges(f *testing.F) {
	f.Add(int64(4), uint8(0), uint16(8), uint16(1))
	f.Add(int64(5), uint8(0), uint16(8), uint16(1))
	f.Add(int64(4), uint8(1), uint16(8), uint16(1))
	f.Add(int64(5), uint8(1), uint16(8), uint16(1))
	f.Add(int64(0), uint8(2), uint16(1023), uint16(0))
	f.Add(int64(0), uint8(2), uint16(1024), uint16(0))
	f.Add(int64(13), uint8(3), uint16(16), uint16(0))
	f.Add(int64(13), uint8(4), uint16(8), uint16(5))
	f.Add(int64(vm.DefaultGlobalVersion), uint8(0), uint16(8), uint16(1))
	f.Add(int64(vm.DefaultGlobalVersion), uint8(1), uint16(8), uint16(1))
	f.Add(int64(vm.DefaultGlobalVersion), uint8(2), uint16(1024), uint16(0))
	f.Add(int64(vm.DefaultGlobalVersion), uint8(3), uint16(16), uint16(0))
	f.Add(int64(vm.DefaultGlobalVersion), uint8(4), uint16(8), uint16(5))
	for version := int64(0); version <= int64(vm.DefaultGlobalVersion); version++ {
		for opKind := uint8(0); opKind < 5; opKind++ {
			f.Add(version, opKind, uint16(8), uint16(1))
			f.Add(version, opKind, uint16(1024), uint16(5))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawOp uint8, rawBits uint16, rawRefs uint16) {
		version := fuzzCellSliceVersion(rawVersion)
		opKind := rawOp % 5
		bits := int64(rawBits % 1100)
		refs := int64(rawRefs % 7)

		switch opKind {
		case 0:
			fuzzXLoadVersioned(t, version, false)
		case 1:
			fuzzXLoadVersioned(t, version, true)
		case 2:
			fuzzSChkBitsQVersioned(t, version, bits)
		case 3:
			fuzzSChkBitsVersioned(t, version, bits)
		default:
			fuzzSChkBitRefsQVersioned(t, version, bits, refs)
		}
	})
}

func fuzzXLoadVersioned(t *testing.T, version int, quiet bool) {
	t.Helper()

	state := newCellSliceState()
	state.GlobalVersion = version
	state.GlobalVersionConfigured = true
	lib := mustLibraryCell(t)
	pushCellSliceCell(t, state, lib)

	var err error
	if quiet {
		err = XLOADQ().Interpret(state)
	} else {
		err = XLOAD().Interpret(state)
	}

	if version < 5 {
		if err != nil {
			t.Fatalf("XLOAD version=%d quiet=%v failed: %v", version, quiet, err)
		}
		if quiet && !popCellSliceBool(t, state) {
			t.Fatal("XLOADQ before v5 should push true")
		}
		if !sameCellHash(popCellSliceCell(t, state), lib) {
			t.Fatal("XLOAD before v5 should keep the original special cell")
		}
		return
	}

	if quiet {
		if err != nil {
			t.Fatalf("XLOADQ version=%d failed: %v", version, err)
		}
		if popCellSliceBool(t, state) {
			t.Fatal("XLOADQ should report missing library as false from v5")
		}
		if state.Stack.Len() != 0 {
			t.Fatalf("XLOADQ version=%d stack len = %d, want 0", version, state.Stack.Len())
		}
		return
	}

	assertCellSliceVMErrorCode(t, err, vmerr.CodeCellUnderflow)
	if state.Stack.Len() != 0 {
		t.Fatalf("XLOAD version=%d stack len = %d, want 0", version, state.Stack.Len())
	}
}

func fuzzSChkBitsQVersioned(t *testing.T, version int, bits int64) {
	t.Helper()

	state := newCellSliceState()
	state.GlobalVersion = version
	state.GlobalVersionConfigured = true
	pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
	pushCellSliceInt(t, state, bits)

	err := SCHKBITSQ().Interpret(state)
	if bits > 1023 {
		assertCellSliceVMErrorCode(t, err, vmerr.CodeRangeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("SCHKBITSQ range stack len = %d, want 1", state.Stack.Len())
		}
		return
	}
	if err != nil {
		t.Fatalf("SCHKBITSQ version=%d bits=%d failed: %v", version, bits, err)
	}
	want := bits <= 8
	if got := popCellSliceBool(t, state); got != want {
		t.Fatalf("SCHKBITSQ version=%d bits=%d = %v, want %v", version, bits, got, want)
	}
}

func fuzzSChkBitsVersioned(t *testing.T, version int, bits int64) {
	t.Helper()

	state := newCellSliceState()
	state.GlobalVersion = version
	state.GlobalVersionConfigured = true
	pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
	pushCellSliceInt(t, state, bits)

	err := SCHKBITS().Interpret(state)
	if bits > 1023 {
		assertCellSliceVMErrorCode(t, err, vmerr.CodeRangeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("SCHKBITS range stack len = %d, want 1", state.Stack.Len())
		}
		return
	}
	if bits > 8 {
		assertCellSliceVMErrorCode(t, err, vmerr.CodeCellUnderflow)
		if state.Stack.Len() != 0 {
			t.Fatalf("SCHKBITS underflow stack len = %d, want 0", state.Stack.Len())
		}
		return
	}
	if err != nil {
		t.Fatalf("SCHKBITS version=%d bits=%d failed: %v", version, bits, err)
	}
	if state.Stack.Len() != 0 {
		t.Fatalf("SCHKBITS success stack len = %d, want 0", state.Stack.Len())
	}
}

func fuzzSChkBitRefsQVersioned(t *testing.T, version int, bits, refs int64) {
	t.Helper()

	state := newCellSliceState()
	state.GlobalVersion = version
	state.GlobalVersionConfigured = true
	ref := cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell()
	pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xAB, 8).MustStoreRef(ref).ToSlice())
	pushCellSliceInt(t, state, bits)
	pushCellSliceInt(t, state, refs)

	err := SCHKBITREFSQ().Interpret(state)
	if refs > 4 {
		assertCellSliceVMErrorCode(t, err, vmerr.CodeRangeCheck)
		if state.Stack.Len() != 2 {
			t.Fatalf("SCHKBITREFSQ refs range stack len = %d, want 2", state.Stack.Len())
		}
		return
	}
	if bits > 1023 {
		assertCellSliceVMErrorCode(t, err, vmerr.CodeRangeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("SCHKBITREFSQ bits range stack len = %d, want 1", state.Stack.Len())
		}
		return
	}
	if err != nil {
		t.Fatalf("SCHKBITREFSQ version=%d bits=%d refs=%d failed: %v", version, bits, refs, err)
	}
	want := bits <= 8 && refs <= 1
	if got := popCellSliceBool(t, state); got != want {
		t.Fatalf("SCHKBITREFSQ version=%d bits=%d refs=%d = %v, want %v", version, bits, refs, got, want)
	}
}

func fuzzCellSliceLibraryTarget(seed uint64, bits uint16, refs uint8) *cell.Cell {
	builder := cell.BeginCell()
	payloadBits := uint(bits % 128)
	if payloadBits > 0 {
		payload := make([]byte, (payloadBits+7)/8)
		for i := range payload {
			payload[i] = byte(seed >> (uint(i%8) * 8))
		}
		builder.MustStoreSlice(payload, payloadBits)
	}
	for i := uint8(0); i < refs%4; i++ {
		builder.MustStoreRef(cell.BeginCell().MustStoreUInt(uint64(seed)+uint64(i), 16).EndCell())
	}
	return builder.EndCell()
}

func fuzzCellSliceLibraryCellForHash(t *testing.T, hash []byte) *cell.Cell {
	t.Helper()

	cl, err := cell.BeginCell().
		MustStoreUInt(uint64(cell.LibraryCellType), 8).
		MustStoreSlice(hash, 256).
		EndCellSpecial(true)
	if err != nil {
		t.Fatalf("failed to build library cell: %v", err)
	}
	return cl
}

func fuzzCellSliceLibraryRoot(t *testing.T, target *cell.Cell) *cell.Cell {
	t.Helper()

	dict := cell.NewDict(256)
	key := cell.BeginCell().MustStoreSlice(target.Hash(), 256).EndCell()
	value := cell.BeginCell().MustStoreRef(target).EndCell()
	if err := dict.Set(key, value); err != nil {
		t.Fatalf("set library entry: %v", err)
	}
	root, err := dict.ToCell()
	if err != nil {
		t.Fatalf("library dict to cell: %v", err)
	}
	return root
}

func FuzzTVMVersionedXLoadLibraryResolution(f *testing.F) {
	for version := int64(0); version <= int64(vm.DefaultGlobalVersion); version++ {
		f.Add(version, false, uint64(version), uint16(0), uint8(0))
		f.Add(version, true, uint64(version+1), uint16(16), uint8(1))
		f.Add(version, false, uint64(version+2), uint16(127), uint8(3))
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, quiet bool, seed uint64, rawBits uint16, rawRefs uint8) {
		version := fuzzCellSliceVersion(rawVersion)
		target := fuzzCellSliceLibraryTarget(seed, rawBits, rawRefs)
		libraryCell := fuzzCellSliceLibraryCellForHash(t, target.Hash())

		state := newCellSliceState()
		state.GlobalVersion = version
		state.GlobalVersionConfigured = true
		state.SetLibraries(fuzzCellSliceLibraryRoot(t, target))
		pushCellSliceCell(t, state, libraryCell)

		var err error
		if quiet {
			err = XLOADQ().Interpret(state)
		} else {
			err = XLOAD().Interpret(state)
		}
		if err != nil {
			t.Fatalf("XLOAD version=%d quiet=%v failed: %v", version, quiet, err)
		}
		if quiet {
			if got := popCellSliceBool(t, state); !got {
				t.Fatalf("XLOADQ version=%d resolved library flag=false, want true", version)
			}
		}

		want := target
		if version < 5 {
			want = libraryCell
		}
		got := popCellSliceCell(t, state)
		if !sameCellHash(got, want) {
			t.Fatalf("XLOAD version=%d quiet=%v result hash=%x, want %x", version, quiet, got.Hash(), want.Hash())
		}
		if state.Stack.Len() != 0 {
			t.Fatalf("XLOAD version=%d quiet=%v left %d stack values, want 0", version, quiet, state.Stack.Len())
		}
	})
}
