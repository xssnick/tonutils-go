package dict

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type dictPrefixEntry struct {
	bits  uint
	value *cell.Cell
}

func newDictTestState() *vm.State {
	st := vm.NewExecutionState(vm.DefaultGlobalVersion, vm.NewGas(), nil, tuple.Tuple{}, vm.NewStack())
	st.CurrentCode = cell.BeginCell().EndCell().BeginParse()
	st.InitForExecution()
	return st
}

func mustDictKeyCell(t *testing.T, value uint64, bits uint) *cell.Cell {
	t.Helper()
	return cell.BeginCell().MustStoreUInt(value, bits).EndCell()
}

func mustDictKeySlice(t *testing.T, value uint64, bits uint) *cell.Slice {
	t.Helper()
	return mustDictKeyCell(t, value, bits).BeginParse()
}

func mustPlainDictRoot(t *testing.T, keyBits uint, items map[uint64]uint64, valueBits uint) *cell.Cell {
	t.Helper()
	dict := cell.NewDict(keyBits)
	for key, value := range items {
		if _, err := dict.SetWithMode(
			mustDictKeyCell(t, key, keyBits),
			cell.BeginCell().MustStoreUInt(value, valueBits).EndCell(),
			cell.DictSetModeSet,
		); err != nil {
			t.Fatalf("seed dict: %v", err)
		}
	}
	return dict.AsCell()
}

func mustPrefixDictRoot(t *testing.T, keyBits uint, items map[uint64]dictPrefixEntry) *cell.Cell {
	t.Helper()
	dict := cell.NewPrefixDict(keyBits)
	for key, item := range items {
		if _, err := dict.SetWithMode(
			mustDictKeyCell(t, key, item.bits),
			item.value,
			cell.DictSetModeSet,
		); err != nil {
			t.Fatalf("seed prefix dict: %v", err)
		}
	}
	return dict.AsCell()
}

func TestDictNamingAndRegistrationHelpers(t *testing.T) {
	if got := dictValueName(dictValueVariant{kind: dictKeySlice}, "GET"); got != "DICTGET" {
		t.Fatalf("unexpected dict value name: %q", got)
	}
	if got := dictValueName(dictValueVariant{kind: dictKeySignedInt, byRef: true}, "SET"); got != "DICTISETREF" {
		t.Fatalf("unexpected signed dict value name: %q", got)
	}
	if got := dictScalarName(dictScalarVariant{kind: dictKeyUnsignedInt}, "DEL"); got != "DICTUDEL" {
		t.Fatalf("unexpected dict scalar name: %q", got)
	}
	if got := dictNearName(dictNearVariant{kind: dictKeyUnsignedInt, fetchNext: false, allowEq: true}); got != "DICTUGETPREVEQ" {
		t.Fatalf("unexpected dict near name: %q", got)
	}

	before := len(vm.List)
	registerSimpleExact(0xF000, "TESTDICT", func(*vm.State) error { return nil })
	registerDictValueFamily(0xF100, "VAL", func(dictValueVariant) func(*vm.State) error {
		return func(*vm.State) error { return nil }
	})
	registerDictScalarFamily(0xF200, "SC", func(dictScalarVariant) func(*vm.State) error {
		return func(*vm.State) error { return nil }
	})
	registerDictNearFamily(0xF300, func(dictNearVariant) func(*vm.State) error {
		return func(*vm.State) error { return nil }
	})

	expected := before + 1 + len(dictValueVariants) + len(dictScalarVariants) + len(dictNearVariants)
	if len(vm.List) != expected {
		t.Fatalf("unexpected vm list growth: got %d want %d", len(vm.List), expected)
	}

	if got := vm.List[before]().Serialize().EndCell().BeginParse().MustLoadUInt(16); got != 0xF000 {
		t.Fatalf("unexpected registered opcode: %#x", got)
	}
}

func TestDictHelpers(t *testing.T) {
	stack := vm.NewStack()
	ref := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()

	if err := pushMaybeCell(stack, nil); err != nil {
		t.Fatalf("push maybe nil cell: %v", err)
	}
	if value, err := stack.PopMaybeCell(); err != nil || value != nil {
		t.Fatalf("expected nil maybe cell, got %v err=%v", value, err)
	}
	if err := pushMaybeCell(stack, ref); err != nil {
		t.Fatalf("push maybe cell: %v", err)
	}
	if value, err := stack.PopMaybeCell(); err != nil || string(value.Hash()) != string(ref.Hash()) {
		t.Fatalf("unexpected maybe cell result: %v err=%v", value, err)
	}

	if dictNonEmpty(cell.BeginCell().ToSlice()) != -1 {
		t.Fatal("empty slice should be invalid as dict serialization")
	}
	if dictNonEmpty(cell.BeginCell().MustStoreUInt(1, 1).ToSlice()) != -1 {
		t.Fatal("missing ref should be invalid dict serialization")
	}
	if dictNonEmpty(cell.BeginCell().MustStoreMaybeRef(nil).ToSlice()) != 0 {
		t.Fatal("nil maybe-ref should be empty dict")
	}
	if dictNonEmpty(cell.BeginCell().MustStoreMaybeRef(ref).ToSlice()) != 1 {
		t.Fatal("ref maybe-ref should be non-empty dict")
	}

	if bits, ok := encodeDictIntBits(big.NewInt(5), 4, false); !ok || len(bits) == 0 {
		t.Fatalf("encode unsigned key failed")
	}
	if _, ok := encodeDictIntBits(big.NewInt(-1), 4, false); ok {
		t.Fatal("negative unsigned key should not encode")
	}
	if key, ok := encodeDictIntKey(big.NewInt(-1), 4, true); !ok || key == nil {
		t.Fatal("signed key should encode")
	}
	if _, ok := encodeDictIntKey(big.NewInt(16), 4, false); ok {
		t.Fatal("out-of-range unsigned key should fail")
	}

	data := []byte{0x12, 0x34}
	shiftSliceLeft(data, 4)
	if data[0] != 0x23 || data[1] != 0x40 {
		t.Fatalf("unexpected shifted bytes: %#v", data)
	}
	if minUint(3, 7) != 3 || minUint(9, 7) != 7 {
		t.Fatal("unexpected minUint result")
	}

	if cellUnderflowError(nil) != nil {
		t.Fatal("nil underflow should stay nil")
	}
	if cellOverflowError(nil) != nil {
		t.Fatal("nil overflow should stay nil")
	}

	var vmErr vmerr.VMError
	if err := mapDictError(cell.ErrNoSuchKeyInDict); !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeDict {
		t.Fatalf("expected dict error for missing key, got %v", err)
	}
	if err := mapDictError(cell.ErrNoMoreRefs); !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeCellUnderflow {
		t.Fatalf("expected cell underflow for ref underflow, got %v", err)
	}
	if err := mapDictError(cell.ErrTooMuchRefs); !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeCellOverflow {
		t.Fatalf("expected cell overflow for too many refs, got %v", err)
	}
	if err := mapDictError(errors.New("not enough data in reader")); !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeCellUnderflow {
		t.Fatalf("expected reader underflow mapping, got %v", err)
	}
	origErr := vmerr.Error(vmerr.CodeRangeCheck)
	if mapDictError(origErr) != origErr {
		t.Fatal("vm errors should be returned as-is")
	}

	sl := mustDictKeySlice(t, 0xA, 4)
	keyCell, err := sliceKeyCell(sl, 4)
	if err != nil {
		t.Fatalf("sliceKeyCell failed: %v", err)
	}
	if got := keyCell.BeginParse().MustLoadUInt(4); got != 0xA {
		t.Fatalf("unexpected key cell: %#x", got)
	}
	if _, err = sliceKeyCell(mustDictKeySlice(t, 0x1, 1), 4); err == nil {
		t.Fatal("expected sliceKeyCell underflow")
	}

	state := newDictTestState()
	if err = state.Stack.PushSlice(mustDictKeySlice(t, 0xA, 4)); err != nil {
		t.Fatalf("push slice key: %v", err)
	}
	if key, ok, err := popDictKey(state, 4, dictKeySlice, false); err != nil || !ok || key.BeginParse().MustLoadUInt(4) != 0xA {
		t.Fatalf("unexpected slice pop result: key=%v ok=%v err=%v", key, ok, err)
	}

	if err = state.Stack.PushInt(big.NewInt(-1)); err != nil {
		t.Fatalf("push signed key: %v", err)
	}
	if key, ok, err := popDictKey(state, 4, dictKeySignedInt, true); err != nil || !ok || key == nil {
		t.Fatalf("unexpected signed pop result: key=%v ok=%v err=%v", key, ok, err)
	}

	if err = state.Stack.PushInt(big.NewInt(-1)); err != nil {
		t.Fatalf("push invalid unsigned key: %v", err)
	}
	if key, ok, err := popDictKey(state, 4, dictKeyUnsignedInt, false); err != nil || ok || key != nil {
		t.Fatalf("unexpected relaxed unsigned result: key=%v ok=%v err=%v", key, ok, err)
	}

	if err = state.Stack.PushInt(big.NewInt(-1)); err != nil {
		t.Fatalf("push invalid strict unsigned key: %v", err)
	}
	if _, _, err := popDictKey(state, 4, dictKeyUnsignedInt, true); err == nil {
		t.Fatal("expected strict unsigned range check")
	}

	if err = state.Stack.PushCell(ref); err != nil {
		t.Fatalf("push root: %v", err)
	}
	if err = state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push bits: %v", err)
	}
	if bits, root, err := popDictRootAndLen(state); err != nil || bits != 8 || string(root.Hash()) != string(ref.Hash()) {
		t.Fatalf("unexpected popDictRootAndLen result: bits=%d root=%v err=%v", bits, root, err)
	}

	if err = state.Stack.PushCell(ref); err != nil {
		t.Fatalf("push root: %v", err)
	}
	if err = state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push bits: %v", err)
	}
	if bits, root, err := popDictMinMaxRootAndLen(state, dictKeyUnsignedInt); err != nil || bits != 8 || string(root.Hash()) != string(ref.Hash()) {
		t.Fatalf("unexpected popDictMinMaxRootAndLen result: bits=%d root=%v err=%v", bits, root, err)
	}

	if err = state.Stack.PushSlice(mustDictKeySlice(t, 0b10, 2)); err != nil {
		t.Fatalf("push slice prefix: %v", err)
	}
	if err = state.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("push prefix bits: %v", err)
	}
	if bits, prefix, err := popSubdictPrefix(state, 4, dictKeySlice); err != nil || bits != 2 || prefix.BeginParse().MustLoadUInt(2) != 0b10 {
		t.Fatalf("unexpected slice prefix result: bits=%d prefix=%v err=%v", bits, prefix, err)
	}

	if err = state.Stack.PushInt(big.NewInt(-1)); err != nil {
		t.Fatalf("push signed prefix val: %v", err)
	}
	if err = state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push signed prefix bits: %v", err)
	}
	if bits, prefix, err := popSubdictPrefix(state, 8, dictKeySignedInt); err != nil || bits != 4 || prefix == nil {
		t.Fatalf("unexpected signed prefix result: bits=%d prefix=%v err=%v", bits, prefix, err)
	}

	if err = pushDictKeyValue(state, mustDictKeyCell(t, 0xA, 4), dictKeySlice); err != nil {
		t.Fatalf("push slice dict key value: %v", err)
	}
	if got, err := state.Stack.PopSlice(); err != nil || got.MustLoadUInt(4) != 0xA {
		t.Fatalf("unexpected pushed slice dict key value: %v err=%v", got, err)
	}
	if err = pushDictKeyValue(state, cell.BeginCell().MustStoreBigInt(big.NewInt(-1), 4).EndCell(), dictKeySignedInt); err != nil {
		t.Fatalf("push signed dict key value: %v", err)
	}
	if got, err := state.Stack.PopIntFinite(); err != nil || got.Int64() != -1 {
		t.Fatalf("unexpected pushed signed dict key value: %v err=%v", got, err)
	}
	if err = pushDictKeyValue(state, mustDictKeyCell(t, 0xF, 4), dictKeyUnsignedInt); err != nil {
		t.Fatalf("push unsigned dict key value: %v", err)
	}
	if got, err := state.Stack.PopIntFinite(); err != nil || got.Uint64() != 0xF {
		t.Fatalf("unexpected pushed unsigned dict key value: %v err=%v", got, err)
	}

	cont := newOrdContinuation(cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().BeginParse(), 7)
	if cont.Data.CP != 7 || cont.Code.MustLoadUInt(8) != 0xAA {
		t.Fatalf("unexpected ordinary continuation contents")
	}
}

func TestPFXDICTSWITCHLifecycleAndInterpret(t *testing.T) {
	root := mustPrefixDictRoot(t, 4, map[uint64]dictPrefixEntry{
		0b10: {bits: 2, value: cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()},
	})

	op := PFXDICTSWITCH(root, 4)
	if op.InstructionBits() != 24 {
		t.Fatalf("unexpected instruction bits: %d", op.InstructionBits())
	}
	if op.SerializeText() == "" {
		t.Fatal("serialize text should not be empty")
	}

	decoded := PFXDICTSWITCH(nil)
	if err := decoded.Deserialize(op.Serialize().EndCell().BeginParse()); err != nil {
		t.Fatalf("deserialize failed: %v", err)
	}
	if decoded.bits != 4 || decoded.root == nil {
		t.Fatalf("unexpected decoded opcode state: bits=%d root=%v", decoded.bits, decoded.root)
	}
	if decoded.SerializeText() == "PFXDICTSWITCH 4 (<nil>)" {
		t.Fatal("expected decoded root to be reflected in text form")
	}

	state := newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0b1011, 4)); err != nil {
		t.Fatalf("push input: %v", err)
	}
	if err := op.Interpret(state); err != nil {
		t.Fatalf("interpret hit failed: %v", err)
	}
	input, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop input: %v", err)
	}
	prefix, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop prefix: %v", err)
	}
	if input.MustLoadUInt(2) != 0b11 || prefix.MustLoadUInt(2) != 0b10 {
		t.Fatalf("unexpected switch stack result")
	}
	if state.CurrentCode.MustLoadUInt(8) != 0xAB {
		t.Fatalf("unexpected jumped code")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0b0111, 4)); err != nil {
		t.Fatalf("push miss input: %v", err)
	}
	if err := op.Interpret(state); err != nil {
		t.Fatalf("interpret miss failed: %v", err)
	}
	rest, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop miss rest: %v", err)
	}
	if rest.MustLoadUInt(4) != 0b0111 || state.Stack.Len() != 0 {
		t.Fatalf("unexpected miss result")
	}
}

func TestDictBasicExecOps(t *testing.T) {
	root := mustPlainDictRoot(t, 8, map[uint64]uint64{0x12: 0x34}, 8)
	serialized := cell.BeginCell().MustStoreMaybeRef(root).ToSlice()

	state := newDictTestState()
	builder := cell.BeginCell().MustStoreUInt(0xA, 4)
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push dict root: %v", err)
	}
	if err := state.Stack.PushBuilder(builder); err != nil {
		t.Fatalf("push builder: %v", err)
	}
	if err := execStoreDict(state); err != nil {
		t.Fatalf("store dict failed: %v", err)
	}
	stored, err := state.Stack.PopBuilder()
	if err != nil {
		t.Fatalf("pop stored builder: %v", err)
	}
	storedSlice := stored.ToSlice()
	if storedSlice.MustLoadUInt(5) != 0b10101 {
		t.Fatalf("unexpected serialized builder contents")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(serialized.Copy()); err != nil {
		t.Fatalf("push dict slice: %v", err)
	}
	if err := execSkipDict(state); err != nil {
		t.Fatalf("skip dict failed: %v", err)
	}
	rest, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop skipped remainder: %v", err)
	}
	if rest.BitsLeft() != 0 || rest.RefsNum() != 0 {
		t.Fatalf("unexpected remainder after skip")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(1, 1).ToSlice()); err != nil {
		t.Fatalf("push invalid dict slice: %v", err)
	}
	if err := execSkipDict(state); err == nil {
		t.Fatal("expected invalid dict serialization error")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(serialized.Copy()); err != nil {
		t.Fatalf("push dict slice: %v", err)
	}
	if err := execLoadDictSlice(false, false)(state); err != nil {
		t.Fatalf("load dict slice failed: %v", err)
	}
	rest, err = state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop remainder: %v", err)
	}
	dictSlice, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop dict slice: %v", err)
	}
	if rest.BitsLeft() != 0 || rest.RefsNum() != 0 || dictSlice.RefsNum() != 1 {
		t.Fatalf("unexpected load dict slice result")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(cell.BeginCell().ToSlice()); err != nil {
		t.Fatalf("push invalid quiet slice: %v", err)
	}
	if err := execLoadDictSlice(false, true)(state); err != nil {
		t.Fatalf("quiet load dict slice should not fail: %v", err)
	}
	ok, err := state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop quiet ok: %v", err)
	}
	if ok {
		t.Fatal("expected false on invalid quiet dict slice")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(serialized.Copy()); err != nil {
		t.Fatalf("push dict slice: %v", err)
	}
	if err := execLoadDict(false, true)(state); err != nil {
		t.Fatalf("load dict failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop load dict ok: %v", err)
	}
	rest, err = state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop dict rest: %v", err)
	}
	loadedRoot, err := state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop loaded root: %v", err)
	}
	if !ok || loadedRoot == nil || string(loadedRoot.Hash()) != string(root.Hash()) || rest.BitsLeft() != 0 {
		t.Fatalf("unexpected load dict result")
	}

	state = newDictTestState()
	if err := pushSetGetResultSlice(state, cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell().BeginParse(), cell.DictSetModeReplace); err != nil {
		t.Fatalf("push set/get slice result: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop slice bool: %v", err)
	}
	sliceValue, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop slice value: %v", err)
	}
	if !ok || sliceValue.MustLoadUInt(8) != 0xAB {
		t.Fatalf("unexpected set/get slice result")
	}

	state = newDictTestState()
	ref := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	if err := pushSetGetResultRef(state, ref, cell.DictSetModeAdd); err != nil {
		t.Fatalf("push set/get ref result: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop ref bool: %v", err)
	}
	gotRef, err := state.Stack.PopCell()
	if err != nil {
		t.Fatalf("pop ref value: %v", err)
	}
	if ok || string(gotRef.Hash()) != string(ref.Hash()) {
		t.Fatalf("unexpected set/get ref result")
	}
}

func TestDictMutationExecOps(t *testing.T) {
	root := mustPlainDictRoot(t, 8, map[uint64]uint64{0x12: 0x34}, 8)

	state := newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0x12, 8)); err != nil {
		t.Fatalf("push key: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push bits: %v", err)
	}
	if err := execDictGet(dictValueVariant{kind: dictKeySlice})(state); err != nil {
		t.Fatalf("dict get failed: %v", err)
	}
	ok, err := state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop dict get ok: %v", err)
	}
	value, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop dict get value: %v", err)
	}
	if !ok || value.MustLoadUInt(8) != 0x34 {
		t.Fatalf("unexpected dict get result")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0x99, 8).EndCell().BeginParse()); err != nil {
		t.Fatalf("push new value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(0x12)); err != nil {
		t.Fatalf("push key: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push bits: %v", err)
	}
	if err := execDictSet(cell.DictSetModeSet)(dictValueVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("dict set failed: %v", err)
	}
	newRoot, err := state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop new root: %v", err)
	}
	got, err := newRoot.AsDict(8).LoadValueByIntKey(big.NewInt(0x12))
	if err != nil || got.MustLoadUInt(8) != 0x99 {
		t.Fatalf("unexpected dict set contents: %v err=%v", got, err)
	}

	refValue := cell.BeginCell().MustStoreUInt(0x55, 8).EndCell()
	state = newDictTestState()
	if err := state.Stack.PushCell(refValue); err != nil {
		t.Fatalf("push new ref value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(0x12)); err != nil {
		t.Fatalf("push key: %v", err)
	}
	if err := state.Stack.PushCell(nil); err != nil {
		t.Fatalf("push empty root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push bits: %v", err)
	}
	if err := execDictSetGetOptRef(dictScalarVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("dict set/get opt ref failed: %v", err)
	}
	oldValue, err := state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop old maybe cell: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop new opt-ref root: %v", err)
	}
	if oldValue != nil || newRoot == nil {
		t.Fatalf("unexpected set/get opt ref result")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0x99, 8).EndCell().BeginParse()); err != nil {
		t.Fatalf("push value: %v", err)
	}
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0x56, 8)); err != nil {
		t.Fatalf("push key: %v", err)
	}
	if err := state.Stack.PushCell(nil); err != nil {
		t.Fatalf("push empty root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push bits: %v", err)
	}
	if err := execDictSetGet(cell.DictSetModeAdd)(dictValueVariant{kind: dictKeySlice})(state); err != nil {
		t.Fatalf("dict add/get failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop add/get ok: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop add/get root: %v", err)
	}
	if !ok || newRoot == nil {
		t.Fatalf("unexpected add/get result")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0x12, 8)); err != nil {
		t.Fatalf("push delete key: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push delete root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push delete bits: %v", err)
	}
	if err := execDictDelete(dictScalarVariant{kind: dictKeySlice})(state); err != nil {
		t.Fatalf("dict delete failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop delete ok: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop delete root: %v", err)
	}
	if !ok || newRoot != nil {
		t.Fatalf("unexpected delete result")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0x12, 8)); err != nil {
		t.Fatalf("push delete/get key: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push delete/get root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push delete/get bits: %v", err)
	}
	if err := execDictDeleteGet(dictValueVariant{kind: dictKeySlice})(state); err != nil {
		t.Fatalf("dict delete/get failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop delete/get ok: %v", err)
	}
	value, err = state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop delete/get value: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop delete/get root: %v", err)
	}
	if !ok || value.MustLoadUInt(8) != 0x34 || newRoot != nil {
		t.Fatalf("unexpected delete/get result")
	}
}

func TestAdvancedDictExecOps(t *testing.T) {
	root := mustPlainDictRoot(t, 8, map[uint64]uint64{
		0x10: 0xA1,
		0x7F: 0xB2,
		0xF0: 0xD4,
	}, 8)

	refDict := cell.NewDict(8)
	refValue := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	if _, err := refDict.SetRefWithMode(mustDictKeyCell(t, 0x22, 8), refValue, cell.DictSetModeSet); err != nil {
		t.Fatalf("seed ref dict: %v", err)
	}

	state := newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(0x22)); err != nil {
		t.Fatalf("push optref key: %v", err)
	}
	if err := state.Stack.PushCell(refDict.AsCell()); err != nil {
		t.Fatalf("push optref root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push optref bits: %v", err)
	}
	if err := execDictGetOptRef(dictScalarVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("dict get opt ref failed: %v", err)
	}
	gotRef, err := state.Stack.PopMaybeCell()
	if err != nil || gotRef == nil || string(gotRef.Hash()) != string(refValue.Hash()) {
		t.Fatalf("unexpected dict get opt ref result: %v err=%v", gotRef, err)
	}

	state = newDictTestState()
	if err := state.Stack.PushBuilder(cell.BeginCell().MustStoreUInt(0x44, 8)); err != nil {
		t.Fatalf("push builder value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(0x33)); err != nil {
		t.Fatalf("push builder key: %v", err)
	}
	if err := state.Stack.PushCell(nil); err != nil {
		t.Fatalf("push builder root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push builder bits: %v", err)
	}
	if err := execDictSetBuilder(cell.DictSetModeSet)(dictScalarVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("dict set builder failed: %v", err)
	}
	newRoot, err := state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop builder root: %v", err)
	}
	gotSlice, err := newRoot.AsDict(8).LoadValueByIntKey(big.NewInt(0x33))
	if err != nil || gotSlice.MustLoadUInt(8) != 0x44 {
		t.Fatalf("unexpected builder-set contents: %v err=%v", gotSlice, err)
	}

	state = newDictTestState()
	if err := state.Stack.PushBuilder(cell.BeginCell().MustStoreUInt(0x55, 8)); err != nil {
		t.Fatalf("push replace builder: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(0x10)); err != nil {
		t.Fatalf("push replace key: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push replace root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push replace bits: %v", err)
	}
	if err := execDictSetGetBuilder(cell.DictSetModeReplace)(dictScalarVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("dict set/get builder failed: %v", err)
	}
	ok, err := state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop set/get builder ok: %v", err)
	}
	oldValue, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop set/get builder old value: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop set/get builder root: %v", err)
	}
	if !ok || oldValue.MustLoadUInt(8) != 0xA1 || newRoot == nil {
		t.Fatalf("unexpected set/get builder result")
	}

	state = newDictTestState()
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push minmax root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push minmax bits: %v", err)
	}
	if err := execDictMinMax(true, true)(dictValueVariant{kind: dictKeySlice})(state); err != nil {
		t.Fatalf("dict remmax failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop remmax ok: %v", err)
	}
	maxKey, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop remmax key: %v", err)
	}
	maxValue, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop remmax value: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop remmax root: %v", err)
	}
	if !ok || maxKey.MustLoadUInt(8) != 0xF0 || maxValue.MustLoadUInt(8) != 0xD4 || newRoot == nil {
		t.Fatalf("unexpected remmax result")
	}

	pfxRoot := mustPrefixDictRoot(t, 4, map[uint64]dictPrefixEntry{
		0b10: {bits: 2, value: cell.BeginCell().MustStoreUInt(0xD, 4).EndCell()},
	})

	state = newDictTestState()
	if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xE, 4).EndCell().BeginParse()); err != nil {
		t.Fatalf("push pfx set value: %v", err)
	}
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0b11, 2)); err != nil {
		t.Fatalf("push pfx set key: %v", err)
	}
	if err := state.Stack.PushCell(pfxRoot); err != nil {
		t.Fatalf("push pfx set root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push pfx set bits: %v", err)
	}
	if err := execPfxDictSet(cell.DictSetModeSet)(state); err != nil {
		t.Fatalf("pfx dict set failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop pfx set ok: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop pfx set root: %v", err)
	}
	if !ok || newRoot == nil {
		t.Fatalf("unexpected pfx set result")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0b1011, 4)); err != nil {
		t.Fatalf("push pfx get input: %v", err)
	}
	if err := state.Stack.PushCell(pfxRoot); err != nil {
		t.Fatalf("push pfx get root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push pfx get bits: %v", err)
	}
	if err := execPfxDictGet(0)(state); err != nil {
		t.Fatalf("pfx dict get failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop pfx get ok: %v", err)
	}
	rest, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop pfx get rest: %v", err)
	}
	pfxValue, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop pfx get value: %v", err)
	}
	prefix, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop pfx get prefix: %v", err)
	}
	if !ok || rest.MustLoadUInt(2) != 0b11 || pfxValue.MustLoadUInt(4) != 0xD || prefix.MustLoadUInt(2) != 0b10 {
		t.Fatalf("unexpected pfx get result")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0b10, 2)); err != nil {
		t.Fatalf("push pfx delete key: %v", err)
	}
	if err := state.Stack.PushCell(pfxRoot); err != nil {
		t.Fatalf("push pfx delete root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push pfx delete bits: %v", err)
	}
	if err := execPfxDictDelete(state); err != nil {
		t.Fatalf("pfx dict delete failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop pfx delete ok: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop pfx delete root: %v", err)
	}
	if !ok || newRoot != nil {
		t.Fatalf("unexpected pfx delete result")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0x7F, 8)); err != nil {
		t.Fatalf("push get-near slice key: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push get-near root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push get-near bits: %v", err)
	}
	if err := execDictGetNear(dictNearVariant{kind: dictKeySlice, fetchNext: true, allowEq: true})(state); err != nil {
		t.Fatalf("dict get near slice failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop get-near ok: %v", err)
	}
	nearKey, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop get-near key: %v", err)
	}
	nearValue, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop get-near value: %v", err)
	}
	if !ok || nearKey.MustLoadUInt(8) != 0x7F || nearValue.MustLoadUInt(8) != 0xB2 {
		t.Fatalf("unexpected slice get-near result")
	}

	state = newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(-1)); err != nil {
		t.Fatalf("push get-near int key: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push get-near int root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push get-near int bits: %v", err)
	}
	if err := execDictGetNear(dictNearVariant{kind: dictKeyUnsignedInt, fetchNext: true, allowEq: false})(state); err != nil {
		t.Fatalf("dict get near int failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop get-near int ok: %v", err)
	}
	nearIntKey, err := state.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop get-near int key: %v", err)
	}
	nearValue, err = state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop get-near int value: %v", err)
	}
	if !ok || nearIntKey.Uint64() != 0x10 || nearValue.MustLoadUInt(8) != 0xA1 {
		t.Fatalf("unexpected int get-near result")
	}

	codeDict := cell.NewDict(8)
	codeCell := cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell()
	if _, err := codeDict.SetWithMode(mustDictKeyCell(t, 0x2A, 8), codeCell, cell.DictSetModeSet); err != nil {
		t.Fatalf("seed code dict: %v", err)
	}

	state = newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(0x2A)); err != nil {
		t.Fatalf("push exec key: %v", err)
	}
	if err := state.Stack.PushCell(codeDict.AsCell()); err != nil {
		t.Fatalf("push exec root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push exec bits: %v", err)
	}
	if err := execDictGetExec(true, false, false)(state); err != nil {
		t.Fatalf("dict get exec failed: %v", err)
	}
	if state.CurrentCode.MustLoadUInt(8) != 0xCD {
		t.Fatalf("unexpected dict-exec code")
	}

	state = newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(0x99)); err != nil {
		t.Fatalf("push miss exec key: %v", err)
	}
	if err := state.Stack.PushCell(codeDict.AsCell()); err != nil {
		t.Fatalf("push miss exec root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push miss exec bits: %v", err)
	}
	if err := execDictGetExec(true, false, true)(state); err != nil {
		t.Fatalf("dict get exec miss failed: %v", err)
	}
	missedKey, err := state.Stack.PopIntFinite()
	if err != nil || missedKey.Uint64() != 0x99 {
		t.Fatalf("unexpected kept miss key: %v err=%v", missedKey, err)
	}

	subdictRoot := mustPlainDictRoot(t, 4, map[uint64]uint64{
		0b1000: 0x11,
		0b1010: 0x22,
	}, 8)
	state = newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0b10, 2)); err != nil {
		t.Fatalf("push subdict prefix: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("push subdict prefix bits: %v", err)
	}
	if err := state.Stack.PushCell(subdictRoot); err != nil {
		t.Fatalf("push subdict root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push subdict bits: %v", err)
	}
	if err := execSubdict(true)(dictScalarVariant{kind: dictKeySlice})(state); err != nil {
		t.Fatalf("subdict failed: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop subdict root: %v", err)
	}
	subdictValue, err := newRoot.AsDict(2).LoadValue(mustDictKeyCell(t, 0b00, 2))
	if err != nil || subdictValue.MustLoadUInt(8) != 0x11 {
		t.Fatalf("unexpected subdict contents: %v err=%v", subdictValue, err)
	}
}
