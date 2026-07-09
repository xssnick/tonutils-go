package vm

import (
	"bytes"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func makeLibraryRoot(t *testing.T, lib *cell.Cell) *cell.Cell {
	t.Helper()

	dict := cell.NewDict(256)
	key := cell.BeginCell().MustStoreSlice(lib.Hash(), 256).EndCell()
	value := cell.BeginCell().MustStoreRef(lib).EndCell()
	if err := dict.Set(key, value); err != nil {
		t.Fatalf("set library dict value: %v", err)
	}
	root, err := dict.ToCell()
	if err != nil {
		t.Fatalf("dict to cell: %v", err)
	}
	return root
}

func makeLazyLibraryRoot(t *testing.T, root *cell.Cell) *cell.Cell {
	t.Helper()

	roots, _, err := cell.FromBOCMultiRootReader(cell.NewBOCNoCopyReader(root.ToBOCWithOptions(cell.BOCSerializeOptions{
		WithTopHash:   true,
		WithIntHashes: true,
	})), cell.BOCParseOptions{
		Lazy:          true,
		TrustedHashes: true,
	})
	if err != nil {
		t.Fatalf("parse lazy library root: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("unexpected lazy roots count: got %d want 1", len(roots))
	}
	return roots[0]
}

func TestLibrariesAndResolveXLoadCell(t *testing.T) {
	libChild := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	lib := cell.BeginCell().MustStoreUInt(0xAA, 8).MustStoreRef(libChild).EndCell()
	root := makeLibraryRoot(t, lib)

	st := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
	st.InitForExecution()
	st.SetLibraries(root)
	if len(st.Libraries) != 1 || st.Libraries[0] != root {
		t.Fatal("set libraries should replace the libraries slice")
	}

	if got, err := st.LoadLibraryByHash(lib.Hash()); err != nil || got == nil || got.HashKey() != lib.HashKey() {
		t.Fatalf("load library by hash = (%v, %v), want (%x, nil)", got, err, lib.Hash())
	}
	st.SetLibraries(makeLazyLibraryRoot(t, root))
	got, err := st.LoadLibraryByHash(lib.Hash())
	if err != nil {
		t.Fatalf("load lazy library by hash: %v", err)
	}
	if got == nil || got.HashKey() != lib.HashKey() {
		t.Fatalf("load lazy library by hash = %v, want %x", got, lib.Hash())
	}
	child, err := got.MustBeginParse().LoadRefCell()
	if err != nil {
		t.Fatalf("load library child: %v", err)
	}
	if child.HashKey() != libChild.HashKey() {
		t.Fatalf("library child = %v, want %x", child, libChild.Hash())
	}
	st.SetLibraries(root)
	if got, err := st.LoadLibraryByHash([]byte{1, 2, 3}); err != nil || got != nil {
		t.Fatalf("load library by invalid hash len = (%v, %v), want (nil, nil)", got, err)
	}

	if got, err := st.ResolveLibraryCell(lib); err != nil || got != lib {
		t.Fatalf("resolve ordinary cell = (%v, %v), want (%v, nil)", got, err, lib)
	}
	if _, err := st.ResolveLibraryCell(nil); err == nil {
		t.Fatal("expected nil xload cell to fail")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeCellUnderflow)
	}

	libraryCell := mustLibraryCellForHash(t, lib.Hash())
	if got, err := st.ResolveLibraryCell(libraryCell); err != nil || got == nil || got.HashKey() != lib.HashKey() {
		t.Fatalf("resolve library cell = (%v, %v), want (%x, nil)", got, err, lib.Hash())
	}
	lazyLibraryCell := makeLazyLibraryRoot(t, libraryCell)
	if got, err := st.ResolveLibraryCell(lazyLibraryCell); err != nil || got == nil || got.HashKey() != lib.HashKey() {
		t.Fatalf("resolve lazy library cell = (%v, %v), want (%x, nil)", got, err, lib.Hash())
	}

	pruned := mustPrunedCell(t)
	if _, err := st.ResolveLibraryCell(pruned); err == nil {
		t.Fatal("expected pruned cell resolution to fail")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeCellUnderflow)
	}
}

func TestStackStringDoesNotConsumeCellTraceGas(t *testing.T) {
	root := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	lazy := makeLazyLibraryRoot(t, root)

	st := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
	st.InitForExecution()
	traced := lazy.WithTrace(st.Cells.Trace())

	stack := NewStack()
	if err := stack.PushCell(traced); err != nil {
		t.Fatalf("push cell: %v", err)
	}

	before := st.Gas
	if text := stack.String(); text == "" {
		t.Fatal("expected non-empty stack string")
	}
	if st.Gas != before {
		t.Fatalf("stack string changed gas: got=%+v want=%+v", st.Gas, before)
	}
	if err := st.Cells.PendingError(); err != nil {
		t.Fatalf("stack string left pending cell trace error: %v", err)
	}
}

func TestCellManagerHelpers(t *testing.T) {
	st := &State{
		Gas:   GasWithLimit(10_000),
		Stack: NewStack(),
	}
	st.InitForExecution()

	root := cell.BeginCell().MustStoreUInt(0x10, 8).MustStoreRef(cell.BeginCell().MustStoreUInt(0x20, 8).EndCell()).EndCell()
	child := root.MustPeekRef(0)

	if err := st.Cells.RegisterCellLoad(nil); err != nil {
		t.Fatalf("register nil cell load: %v", err)
	}

	rootHash := root.HashKey()
	if err := st.Cells.RegisterCellLoadKey(rootHash); err != nil {
		t.Fatalf("unexpected error after first load: %v", err)
	}
	if err := st.Cells.RegisterCellLoadKey(rootHash); err != nil {
		t.Fatalf("unexpected error after reload: %v", err)
	}
	if err := st.Cells.RegisterCellCreate(); err != nil {
		t.Fatalf("unexpected error after create: %v", err)
	}

	parsed, err := st.Cells.BeginParse(root)
	if err != nil {
		t.Fatalf("begin parse: %v", err)
	}
	if v, err := parsed.LoadUInt(8); err != nil || v != 0x10 {
		t.Fatalf("load parsed bits = (%d, %v), want (16, nil)", v, err)
	}

	refSlice, err := st.Cells.LoadRef(parsed)
	if err != nil {
		t.Fatalf("load ref slice: %v", err)
	}
	if v, err := refSlice.LoadUInt(8); err != nil || v != 0x20 {
		t.Fatalf("load ref slice bits = (%d, %v), want (32, nil)", v, err)
	}

	parsed = root.MustBeginParse()
	if _, err = parsed.LoadUInt(8); err != nil {
		t.Fatalf("load root prefix: %v", err)
	}
	refCell, err := st.Cells.LoadRefCell(parsed)
	if err != nil {
		t.Fatalf("load ref cell: %v", err)
	}
	if refCell != child {
		t.Fatal("unexpected ref cell returned")
	}

	lowGas := &State{
		Gas:           GasWithLimit(50),
		Stack:         NewStack(),
		GlobalVersion: MaxSupportedGlobalVersion,
	}
	lowGas.InitForExecution()
	_ = lowGas.Cells.Trace().NotifyCreate()
	if err := lowGas.Cells.PendingError(); err == nil {
		t.Fatal("expected pending error when cell creation exceeds available gas")
	}

	keepErr := errors.New("keep me")
	lowGas.Cells.pendingErr = keepErr
	lowGas.Cells.Trace().NotifyLoad(root)
	if !errors.Is(lowGas.Cells.PendingError(), keepErr) {
		t.Fatalf("pending error should be preserved, got %v", lowGas.Cells.PendingError())
	}
}

func TestCellManagerVirtualizationSemantics(t *testing.T) {
	st := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(10_000), nil, tuple.Tuple{}, NewStack())
	st.InitForExecution()

	pruned := mustPrunedCell(t)
	virtualized := pruned.Virtualize(0)
	if !virtualized.IsVirtualized() {
		t.Fatal("expected virtualized pruned cell")
	}

	if _, err := st.Cells.BeginParse(pruned); err == nil {
		t.Fatal("expected raw pruned parse to fail")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeCellUnderflow)
	}

	sl, special, err := st.Cells.BeginParseSpecial(pruned)
	if err != nil {
		t.Fatalf("begin parse special pruned: %v", err)
	}
	if !special || sl == nil || !sl.IsSpecial() {
		t.Fatal("expected begin parse special to preserve raw pruned cell")
	}

	if _, err := st.Cells.BeginParse(virtualized); err == nil {
		t.Fatal("expected virtualized pruned parse to fail")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeVirtualization)
	}

	if _, _, err := st.Cells.BeginParseSpecial(virtualized); err == nil {
		t.Fatal("expected virtualized pruned special parse to fail")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeVirtualization)
	}

	if _, err := st.ResolveLibraryCell(virtualized); err == nil {
		t.Fatal("expected xload on virtualized pruned cell to stay underflow")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeCellUnderflow)
	}
}

func TestStackWrappersAndHelpers(t *testing.T) {
	s := NewStack()
	if err := s.PushBool(true); err != nil {
		t.Fatalf("push true: %v", err)
	}
	if err := s.PushBool(false); err != nil {
		t.Fatalf("push false: %v", err)
	}
	if got, err := s.PopBool(); err != nil || got {
		t.Fatalf("pop false bool = (%v, %v), want (false, nil)", got, err)
	}
	if got, err := s.PopBool(); err != nil || !got {
		t.Fatalf("pop true bool = (%v, %v), want (true, nil)", got, err)
	}

	builder := cell.BeginCell().MustStoreUInt(0xAA, 8)
	slice := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell().MustBeginParse()
	cl := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	cont := &QuitContinuation{ExitCode: 9}

	if err := s.PushBuilder(builder); err != nil {
		t.Fatalf("push builder: %v", err)
	}
	if err := s.PushSlice(slice); err != nil {
		t.Fatalf("push slice: %v", err)
	}
	if err := s.PushCell(cl); err != nil {
		t.Fatalf("push cell: %v", err)
	}
	if err := s.PushContinuation(cont); err != nil {
		t.Fatalf("push continuation: %v", err)
	}

	if got, err := s.PopContinuation(); err != nil || got == nil {
		t.Fatalf("pop continuation = (%v, %v), want continuation", got, err)
	}
	if got, err := s.PopCell(); err != nil || got != cl {
		t.Fatalf("pop cell = (%v, %v), want original cell", got, err)
	}
	if got, err := s.PopSlice(); err != nil || got.BitsLeft() != 8 {
		t.Fatalf("pop slice = (%v, %v), want 8-bit slice", got, err)
	}
	if got, err := s.PopBuilder(); err != nil || got.BitsUsed() != 8 {
		t.Fatalf("pop builder = (%v, %v), want 8-bit builder", got, err)
	}

	if err := s.PushAny(nil); err != nil {
		t.Fatalf("push nil maybe cell: %v", err)
	}
	if got, err := s.PopMaybeCell(); err != nil || got != nil {
		t.Fatalf("pop maybe nil cell = (%v, %v), want (nil, nil)", got, err)
	}
	if err := s.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push int for maybe cell: %v", err)
	}
	if _, err := s.PopMaybeCell(); err == nil {
		t.Fatal("expected pop maybe cell type check failure")
	}

	maybeTuple := tuple.NewTuple("x", "y")
	if err := s.PushMaybeTuple(nil); err != nil {
		t.Fatalf("push nil maybe tuple: %v", err)
	}
	gotNilTuple, err := s.PopMaybeTupleRange(3)
	if err != nil || gotNilTuple != nil {
		t.Fatalf("pop nil maybe tuple = (%v, %v), want (nil, nil)", gotNilTuple, err)
	}
	if err := s.PushMaybeTuple(maybeTuple); err != nil {
		t.Fatalf("push maybe tuple: %v", err)
	}
	gotTuple, err := s.PopMaybeTupleRange(3)
	if err != nil || gotTuple.Len() != 2 {
		t.Fatalf("pop maybe tuple = (%v, %v), want len 2", gotTuple, err)
	}
	if err := s.PushTuple(tuple.NewTupleValue("x", "y")); err != nil {
		t.Fatalf("push tuple: %v", err)
	}
	if _, err := s.PopTupleRange(1); err == nil {
		t.Fatal("expected tuple range validation to fail")
	}

	pushInts(t, s, 1, 2, 3)
	if err := s.PushAt(1); err != nil {
		t.Fatalf("push at: %v", err)
	}
	if got := mustPopInt64(t, s); got != 2 {
		t.Fatalf("push at copied value = %d, want 2", got)
	}

	s = NewStack()
	pushInts(t, s, 1, 2, 3, 4, 5)
	if err := s.DropMany(2, 1); err != nil {
		t.Fatalf("drop many: %v", err)
	}
	assertPopInts(t, s, 5, 2, 1)

	s = NewStack()
	pushInts(t, s, 1, 2, 3)
	if err := s.DropAfter(2); err != nil {
		t.Fatalf("drop after: %v", err)
	}
	assertPopInts(t, s, 3, 2)

	s = NewStack()
	pushInts(t, s, 1, 2, 3)
	if err := s.PopSwapAt(1); err != nil {
		t.Fatalf("pop swap at: %v", err)
	}
	assertPopInts(t, s, 3, 1)

	s = NewStack()
	pushInts(t, s, 1, 2, 3)
	if err := s.Exchange(0, 2); err != nil {
		t.Fatalf("exchange: %v", err)
	}
	assertPopInts(t, s, 1, 2, 3)

	s = NewStack()
	pushInts(t, s, 1, 2, 3)
	if idx, err := s.FromTop(1); err != nil || idx != 1 {
		t.Fatalf("from top = (%d, %v), want (1, nil)", idx, err)
	}
	if _, err := s.FromTop(5); err == nil {
		t.Fatal("expected from top to fail for too large offset")
	}

	display := NewStack()
	if err := display.PushAny(NaN{}); err != nil {
		t.Fatalf("push nan: %v", err)
	}
	if err := display.PushAny(nil); err != nil {
		t.Fatalf("push nil: %v", err)
	}
	if err := display.PushCell(cl); err != nil {
		t.Fatalf("push cell for string: %v", err)
	}
	if err := display.PushSlice(slice); err != nil {
		t.Fatalf("push slice for string: %v", err)
	}
	if err := display.PushBuilder(builder); err != nil {
		t.Fatalf("push builder for string: %v", err)
	}
	if err := display.PushInt(big.NewInt(99)); err != nil {
		t.Fatalf("push int for string: %v", err)
	}
	out := display.String()
	if !strings.Contains(out, "int") || !strings.Contains(out, "cell") || !strings.Contains(out, "builder") || !strings.Contains(out, "slice") {
		t.Fatalf("unexpected stack string: %q", out)
	}
	display.Clear()
	if display.Len() != 0 {
		t.Fatalf("stack len after clear = %d, want 0", display.Len())
	}

	if err := display.PushCell(cl); err != nil {
		t.Fatalf("push cell after clear: %v", err)
	}
	gotCell, err := display.Get(0)
	if err != nil {
		t.Fatalf("get top item: %v", err)
	}
	if gotCell.(*cell.Cell) != cl {
		t.Fatal("unexpected item returned by Get")
	}
	if _, err = display.Get(2); err == nil {
		t.Fatal("expected Get to fail for out-of-range index")
	}
}

func TestLoadLibraryByHashReturnsNilForMissingEntry(t *testing.T) {
	lib := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	st := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
	got, err := st.LoadLibraryByHash(bytes.Repeat([]byte{0x11}, 32))
	if err != nil || got != nil {
		t.Fatalf("load missing library = (%v, %v), want (nil, nil)", got, err)
	}

	root := makeLibraryRoot(t, lib)
	st.SetLibraries(root, nil)
	got, err = st.LoadLibraryByHash(bytes.Repeat([]byte{0x22}, 32))
	if err != nil || got != nil {
		t.Fatalf("load other missing library = (%v, %v), want (nil, nil)", got, err)
	}
}

func FuzzLoadLibraryByHashVersionedLookupGas(f *testing.F) {
	for version := uint8(0); version <= uint8(MaxSupportedGlobalVersion); version++ {
		f.Add(version, uint8(0), uint8(1), uint16(version))
		f.Add(version, uint8(1), uint8(2), uint16(version)<<8|1)
		f.Add(version, uint8(2), uint8(3), uint16(version)<<8|2)
		f.Add(version, uint8(3), uint8(2), uint16(version)<<8|3)
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawCase, rawLookups uint8, rawSeed uint16) {
		version := int(rawVersion % uint8(MaxSupportedGlobalVersion+1))
		lookups := int(rawLookups%3) + 1
		lib := cell.BeginCell().
			MustStoreUInt(uint64(rawSeed), 16).
			MustStoreUInt(uint64(rawCase), 8).
			EndCell()

		var hash []byte
		var roots []*cell.Cell
		found := false
		switch rawCase % 4 {
		case 0:
			hash = []byte{byte(rawSeed), byte(rawSeed >> 8)}
		case 1:
			hash = bytes.Repeat([]byte{byte(rawSeed)}, 32)
			roots = []*cell.Cell{nil, makeLibraryRoot(t, lib)}
		case 2:
			hash = lib.Hash()
			roots = []*cell.Cell{makeLibraryRoot(t, lib)}
			found = true
		default:
			hash = lib.Hash()
			roots = []*cell.Cell{nil, makeLibraryRoot(t, lib), nil}
			found = true
		}

		st := NewExecutionState(version, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
		st.InitForExecution()
		st.SetLibraries(roots...)

		var got *cell.Cell
		var err error
		for i := 0; i < lookups; i++ {
			got, err = st.LoadLibraryByHash(hash)
			if err != nil {
				t.Fatalf("v%d case=%d lookup=%d load library: %v", version, rawCase%4, i, err)
			}
			if found && (got == nil || got.HashKey() != lib.HashKey()) {
				t.Fatalf("v%d case=%d lookup=%d got library %v, want %x", version, rawCase%4, i, got, lib.Hash())
			}
			if !found && got != nil {
				t.Fatalf("v%d case=%d lookup=%d got library %x, want nil", version, rawCase%4, i, got.Hash())
			}
		}

		wantGas := expectedVersionedLibraryLookupGas(version, found, lookups)
		if gotGas := st.Gas.Used(); gotGas != wantGas {
			t.Fatalf("v%d case=%d found=%v lookups=%d gas = %d, want %d", version, rawCase%4, found, lookups, gotGas, wantGas)
		}
	})
}

func expectedVersionedLibraryLookupGas(version int, found bool, lookups int) int64 {
	if !found || version >= 5 {
		return 0
	}

	if version >= 4 {
		return CellLoadGasPrice + int64(lookups-1)*CellReloadGasPrice
	}
	return 2*CellLoadGasPrice + int64(lookups-1)*2*CellReloadGasPrice
}
