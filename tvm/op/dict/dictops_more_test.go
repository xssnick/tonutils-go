package dict

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func mustRefDictRoot(t *testing.T, keyBits uint, items map[uint64]*cell.Cell) *cell.Cell {
	t.Helper()
	dict := cell.NewDict(keyBits)
	for key, value := range items {
		if _, err := dict.SetBuilderWithMode(mustDictKeyCell(t, key, keyBits), cell.BeginCell().MustStoreRef(value), cell.DictSetModeSet); err != nil {
			t.Fatalf("seed ref dict: %v", err)
		}
	}
	return dict.AsCell()
}

func mustSignedDictRoot(t *testing.T, keyBits uint, items map[int64]uint64, valueBits uint) *cell.Cell {
	t.Helper()
	dict := cell.NewDict(keyBits)
	for key, value := range items {
		if err := dict.SetIntKey(big.NewInt(key), cell.BeginCell().MustStoreUInt(value, valueBits).EndCell()); err != nil {
			t.Fatalf("seed signed dict: %v", err)
		}
	}
	return dict.AsCell()
}

func TestDictGetAdditionalBranches(t *testing.T) {
	refValue := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	refRoot := mustRefDictRoot(t, 8, map[uint64]*cell.Cell{0x22: refValue})

	state := newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(0x22)); err != nil {
		t.Fatalf("push getref key: %v", err)
	}
	if err := state.Stack.PushCell(refRoot); err != nil {
		t.Fatalf("push getref root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push getref bits: %v", err)
	}
	if err := execDictGet(dictValueVariant{kind: dictKeyUnsignedInt, byRef: true})(state); err != nil {
		t.Fatalf("dict get ref failed: %v", err)
	}
	ok, err := state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop getref ok: %v", err)
	}
	gotRef, err := state.Stack.PopCell()
	if err != nil {
		t.Fatalf("pop getref value: %v", err)
	}
	if !ok || string(gotRef.Hash()) != string(refValue.Hash()) {
		t.Fatalf("unexpected getref result")
	}

	state = newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(0x99)); err != nil {
		t.Fatalf("push getref miss key: %v", err)
	}
	if err := state.Stack.PushCell(refRoot); err != nil {
		t.Fatalf("push getref miss root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push getref miss bits: %v", err)
	}
	if err := execDictGet(dictValueVariant{kind: dictKeyUnsignedInt, byRef: true})(state); err != nil {
		t.Fatalf("dict get ref miss failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop getref miss ok: %v", err)
	}
	if ok || state.Stack.Len() != 0 {
		t.Fatalf("expected getref miss to push only false")
	}

	root := mustPlainDictRoot(t, 8, map[uint64]uint64{0x12: 0x34}, 8)
	state = newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0x99, 8)); err != nil {
		t.Fatalf("push miss key: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push miss root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push miss bits: %v", err)
	}
	if err := execDictGet(dictValueVariant{kind: dictKeySlice})(state); err != nil {
		t.Fatalf("dict get miss failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop miss ok: %v", err)
	}
	if ok || state.Stack.Len() != 0 {
		t.Fatalf("expected miss to push only false")
	}

	state = newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(-1)); err != nil {
		t.Fatalf("push invalid unsigned key: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push invalid root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push invalid bits: %v", err)
	}
	if err := execDictGet(dictValueVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("dict get invalid key should be relaxed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop invalid ok: %v", err)
	}
	if ok || state.Stack.Len() != 0 {
		t.Fatalf("expected invalid unsigned key to behave like miss")
	}
}

func TestDictSetAdditionalBranches(t *testing.T) {
	refValue := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()

	state := newDictTestState()
	if err := state.Stack.PushCell(refValue); err != nil {
		t.Fatalf("push addref value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(0x21)); err != nil {
		t.Fatalf("push addref key: %v", err)
	}
	if err := state.Stack.PushCell(nil); err != nil {
		t.Fatalf("push addref root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push addref bits: %v", err)
	}
	if err := execDictSet(cell.DictSetModeAdd)(dictValueVariant{kind: dictKeyUnsignedInt, byRef: true})(state); err != nil {
		t.Fatalf("dict add ref failed: %v", err)
	}
	ok, err := state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop addref ok: %v", err)
	}
	newRoot, err := state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop addref root: %v", err)
	}
	gotRefValue, err := newRoot.AsDict(8).LoadValueByIntKey(big.NewInt(0x21))
	if err != nil {
		t.Fatalf("unexpected addref result: %v err=%v", nil, err)
	}
	gotRef, err := gotRefValue.LoadRefCell()
	if err != nil || string(gotRef.Hash()) != string(refValue.Hash()) || !ok {
		t.Fatalf("unexpected addref result: %v err=%v", gotRef, err)
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0x55, 8).EndCell().BeginParse()); err != nil {
		t.Fatalf("push replace-miss value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(0x33)); err != nil {
		t.Fatalf("push replace-miss key: %v", err)
	}
	if err := state.Stack.PushCell(nil); err != nil {
		t.Fatalf("push replace-miss root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push replace-miss bits: %v", err)
	}
	if err := execDictSet(cell.DictSetModeReplace)(dictValueVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("dict replace miss failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop replace-miss ok: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop replace-miss root: %v", err)
	}
	if ok || newRoot != nil {
		t.Fatalf("expected replace miss to keep empty dict and return false")
	}

	root := mustPlainDictRoot(t, 8, map[uint64]uint64{0x12: 0x34}, 8)
	state = newDictTestState()
	if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0x99, 8).EndCell().BeginParse()); err != nil {
		t.Fatalf("push add-existing value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(0x12)); err != nil {
		t.Fatalf("push add-existing key: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push add-existing root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push add-existing bits: %v", err)
	}
	if err := execDictSet(cell.DictSetModeAdd)(dictValueVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("dict add existing failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop add-existing ok: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop add-existing root: %v", err)
	}
	got, err := newRoot.AsDict(8).LoadValueByIntKey(big.NewInt(0x12))
	if err != nil || got.MustLoadUInt(8) != 0x34 || ok {
		t.Fatalf("expected add-existing to leave old value untouched: %v err=%v", got, err)
	}
}

func TestDictSetGetAdditionalBranches(t *testing.T) {
	oldRef := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	newRef := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	refRoot := mustRefDictRoot(t, 8, map[uint64]*cell.Cell{0x12: oldRef})

	state := newDictTestState()
	if err := state.Stack.PushCell(newRef); err != nil {
		t.Fatalf("push setget-ref value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(0x12)); err != nil {
		t.Fatalf("push setget-ref key: %v", err)
	}
	if err := state.Stack.PushCell(refRoot); err != nil {
		t.Fatalf("push setget-ref root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push setget-ref bits: %v", err)
	}
	if err := execDictSetGet(cell.DictSetModeReplace)(dictValueVariant{kind: dictKeyUnsignedInt, byRef: true})(state); err != nil {
		t.Fatalf("dict setget ref replace failed: %v", err)
	}
	ok, err := state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop setget-ref ok: %v", err)
	}
	gotOldRef, err := state.Stack.PopCell()
	if err != nil {
		t.Fatalf("pop setget-ref old: %v", err)
	}
	newRoot, err := state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop setget-ref root: %v", err)
	}
	gotNewRefValue, err := newRoot.AsDict(8).LoadValueByIntKey(big.NewInt(0x12))
	if err != nil {
		t.Fatalf("unexpected setget-ref replace result")
	}
	gotNewRef, err := gotNewRefValue.LoadRefCell()
	if err != nil || !ok || string(gotOldRef.Hash()) != string(oldRef.Hash()) || string(gotNewRef.Hash()) != string(newRef.Hash()) {
		t.Fatalf("unexpected setget-ref replace result")
	}

	state = newDictTestState()
	if err := state.Stack.PushCell(newRef); err != nil {
		t.Fatalf("push setget-add-ref value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(0x44)); err != nil {
		t.Fatalf("push setget-add-ref key: %v", err)
	}
	if err := state.Stack.PushCell(nil); err != nil {
		t.Fatalf("push setget-add-ref root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push setget-add-ref bits: %v", err)
	}
	if err := execDictSetGet(cell.DictSetModeAdd)(dictValueVariant{kind: dictKeyUnsignedInt, byRef: true})(state); err != nil {
		t.Fatalf("dict setget add ref failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop setget-add-ref ok: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop setget-add-ref root: %v", err)
	}
	gotNewRefValue, err = newRoot.AsDict(8).LoadValueByIntKey(big.NewInt(0x44))
	if err != nil {
		t.Fatalf("unexpected setget add ref result")
	}
	gotNewRef, err = gotNewRefValue.LoadRefCell()
	if err != nil || !ok || string(gotNewRef.Hash()) != string(newRef.Hash()) || state.Stack.Len() != 0 {
		t.Fatalf("unexpected setget add ref result")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0x66, 8).EndCell().BeginParse()); err != nil {
		t.Fatalf("push setget-replace-miss value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(0x44)); err != nil {
		t.Fatalf("push setget-replace-miss key: %v", err)
	}
	if err := state.Stack.PushCell(nil); err != nil {
		t.Fatalf("push setget-replace-miss root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push setget-replace-miss bits: %v", err)
	}
	if err := execDictSetGet(cell.DictSetModeReplace)(dictValueVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("dict setget replace miss failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop setget-replace-miss ok: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop setget-replace-miss root: %v", err)
	}
	if ok || newRoot != nil || state.Stack.Len() != 0 {
		t.Fatalf("expected replace miss to return only false and empty root")
	}

	root := mustPlainDictRoot(t, 8, map[uint64]uint64{0x12: 0x34}, 8)
	state = newDictTestState()
	if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0x77, 8).EndCell().BeginParse()); err != nil {
		t.Fatalf("push setget-add-existing value: %v", err)
	}
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0x12, 8)); err != nil {
		t.Fatalf("push setget-add-existing key: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push setget-add-existing root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push setget-add-existing bits: %v", err)
	}
	if err := execDictSetGet(cell.DictSetModeAdd)(dictValueVariant{kind: dictKeySlice})(state); err != nil {
		t.Fatalf("dict setget add existing failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop setget-add-existing ok: %v", err)
	}
	oldValue, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop setget-add-existing old value: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop setget-add-existing root: %v", err)
	}
	got, err := newRoot.AsDict(8).LoadValueByIntKey(big.NewInt(0x12))
	if err != nil || ok || oldValue.MustLoadUInt(8) != 0x34 || got.MustLoadUInt(8) != 0x34 {
		t.Fatalf("expected add-existing to report old value and keep dict intact")
	}
}

func TestDictDeleteGetAdditionalBranches(t *testing.T) {
	refValue := cell.BeginCell().MustStoreUInt(0xCA, 8).EndCell()
	refRoot := mustRefDictRoot(t, 8, map[uint64]*cell.Cell{0x12: refValue})

	state := newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(0x12)); err != nil {
		t.Fatalf("push delget-ref key: %v", err)
	}
	if err := state.Stack.PushCell(refRoot); err != nil {
		t.Fatalf("push delget-ref root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push delget-ref bits: %v", err)
	}
	if err := execDictDeleteGet(dictValueVariant{kind: dictKeyUnsignedInt, byRef: true})(state); err != nil {
		t.Fatalf("dict delget ref failed: %v", err)
	}
	ok, err := state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop delget-ref ok: %v", err)
	}
	gotRef, err := state.Stack.PopCell()
	if err != nil {
		t.Fatalf("pop delget-ref value: %v", err)
	}
	newRoot, err := state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop delget-ref root: %v", err)
	}
	if !ok || string(gotRef.Hash()) != string(refValue.Hash()) || newRoot != nil {
		t.Fatalf("unexpected delget-ref success result")
	}

	state = newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(0x77)); err != nil {
		t.Fatalf("push delget-miss key: %v", err)
	}
	if err := state.Stack.PushCell(refRoot); err != nil {
		t.Fatalf("push delget-miss root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push delget-miss bits: %v", err)
	}
	if err := execDictDeleteGet(dictValueVariant{kind: dictKeyUnsignedInt, byRef: true})(state); err != nil {
		t.Fatalf("dict delget miss failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop delget-miss ok: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop delget-miss root: %v", err)
	}
	if ok || newRoot == nil || string(newRoot.Hash()) != string(refRoot.Hash()) {
		t.Fatalf("expected missing key to keep original root")
	}

	root := mustPlainDictRoot(t, 8, map[uint64]uint64{0x12: 0x34}, 8)
	state = newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(-1)); err != nil {
		t.Fatalf("push delget-invalid key: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push delget-invalid root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push delget-invalid bits: %v", err)
	}
	if err := execDictDeleteGet(dictValueVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("dict delget invalid key should be relaxed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop delget-invalid ok: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop delget-invalid root: %v", err)
	}
	if ok || newRoot == nil || string(newRoot.Hash()) != string(root.Hash()) || state.Stack.Len() != 0 {
		t.Fatalf("expected invalid key to behave like a miss")
	}
}

func TestDictMinMaxAdditionalBranches(t *testing.T) {
	state := newDictTestState()
	if err := state.Stack.PushCell(nil); err != nil {
		t.Fatalf("push empty minmax root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push empty minmax bits: %v", err)
	}
	if err := execDictMinMax(false, false)(dictValueVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("dict min miss failed: %v", err)
	}
	ok, err := state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop empty minmax ok: %v", err)
	}
	if ok || state.Stack.Len() != 0 {
		t.Fatalf("expected empty minmax to push only false")
	}

	state = newDictTestState()
	if err := state.Stack.PushCell(nil); err != nil {
		t.Fatalf("push empty remmin root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push empty remmin bits: %v", err)
	}
	if err := execDictMinMax(false, true)(dictValueVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("dict remmin miss failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop empty remmin ok: %v", err)
	}
	newRoot, err := state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop empty remmin root: %v", err)
	}
	if ok || newRoot != nil {
		t.Fatalf("expected empty remmin to return nil root and false")
	}

	ref1 := cell.BeginCell().MustStoreUInt(0xA1, 8).EndCell()
	ref2 := cell.BeginCell().MustStoreUInt(0xB2, 8).EndCell()
	refRoot := mustRefDictRoot(t, 8, map[uint64]*cell.Cell{
		0x11: ref1,
		0x22: ref2,
	})

	state = newDictTestState()
	if err := state.Stack.PushCell(refRoot); err != nil {
		t.Fatalf("push minref root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push minref bits: %v", err)
	}
	if err := execDictMinMax(false, false)(dictValueVariant{kind: dictKeyUnsignedInt, byRef: true})(state); err != nil {
		t.Fatalf("dict min ref failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop minref ok: %v", err)
	}
	minKey, err := state.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop minref key: %v", err)
	}
	gotRef, err := state.Stack.PopCell()
	if err != nil {
		t.Fatalf("pop minref value: %v", err)
	}
	if !ok || minKey.Uint64() != 0x11 || string(gotRef.Hash()) != string(ref1.Hash()) {
		t.Fatalf("unexpected minref result")
	}

	signedRoot := mustSignedDictRoot(t, 8, map[int64]uint64{
		-2: 0xC1,
		1:  0xD2,
	}, 8)
	state = newDictTestState()
	if err := state.Stack.PushCell(signedRoot); err != nil {
		t.Fatalf("push signed min root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push signed min bits: %v", err)
	}
	if err := execDictMinMax(false, false)(dictValueVariant{kind: dictKeySignedInt})(state); err != nil {
		t.Fatalf("dict signed min failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop signed min ok: %v", err)
	}
	signedKey, err := state.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop signed min key: %v", err)
	}
	signedValue, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop signed min value: %v", err)
	}
	if !ok || signedKey.Int64() != -2 || signedValue.MustLoadUInt(8) != 0xC1 {
		t.Fatalf("unexpected signed min result")
	}
}

func TestPrefixDictGetAdditionalModes(t *testing.T) {
	pfxRoot := mustPrefixDictRoot(t, 4, map[uint64]dictPrefixEntry{
		0b10: {bits: 2, value: cell.BeginCell().MustStoreUInt(0xD, 4).EndCell()},
	})

	state := newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0b1011, 4)); err != nil {
		t.Fatalf("push pfx get input: %v", err)
	}
	if err := state.Stack.PushCell(pfxRoot); err != nil {
		t.Fatalf("push pfx get root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push pfx get bits: %v", err)
	}
	if err := execPfxDictGet(1)(state); err != nil {
		t.Fatalf("pfx dict get op1 failed: %v", err)
	}
	input, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop pfx op1 input: %v", err)
	}
	value, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop pfx op1 value: %v", err)
	}
	prefix, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop pfx op1 prefix: %v", err)
	}
	if input.MustLoadUInt(2) != 0b11 || value.MustLoadUInt(4) != 0xD || prefix.MustLoadUInt(2) != 0b10 {
		t.Fatalf("unexpected pfx get op1 result")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0b0111, 4)); err != nil {
		t.Fatalf("push pfx miss input: %v", err)
	}
	if err := state.Stack.PushCell(pfxRoot); err != nil {
		t.Fatalf("push pfx miss root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push pfx miss bits: %v", err)
	}
	err = execPfxDictGet(1)(state)
	if err == nil {
		t.Fatal("expected pfx get op1 miss to fail")
	}
	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeCellUnderflow {
		t.Fatalf("expected cell underflow on op1 miss, got %v", err)
	}

	codeRoot := mustPrefixDictRoot(t, 4, map[uint64]dictPrefixEntry{
		0b10: {bits: 2, value: cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell()},
	})

	state = newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0b1011, 4)); err != nil {
		t.Fatalf("push pfx jmp input: %v", err)
	}
	if err := state.Stack.PushCell(codeRoot); err != nil {
		t.Fatalf("push pfx jmp root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push pfx jmp bits: %v", err)
	}
	if err := execPfxDictGet(2)(state); err != nil {
		t.Fatalf("pfx dict get jump failed: %v", err)
	}
	input, err = state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop pfx jmp input: %v", err)
	}
	prefix, err = state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop pfx jmp prefix: %v", err)
	}
	if input.MustLoadUInt(2) != 0b11 || prefix.MustLoadUInt(2) != 0b10 || state.CurrentCode.MustLoadUInt(8) != 0xCD {
		t.Fatalf("unexpected pfx get jump result")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0b1011, 4)); err != nil {
		t.Fatalf("push pfx exec input: %v", err)
	}
	if err := state.Stack.PushCell(codeRoot); err != nil {
		t.Fatalf("push pfx exec root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push pfx exec bits: %v", err)
	}
	if err := execPfxDictGet(3)(state); err != nil {
		t.Fatalf("pfx dict get exec failed: %v", err)
	}
	if state.CurrentCode.MustLoadUInt(8) != 0xCD {
		t.Fatalf("expected pfx exec to switch current code")
	}
	if _, ok := state.Reg.C[0].(*vm.OrdinaryContinuation); !ok {
		t.Fatalf("expected pfx exec to install return continuation, got %T", state.Reg.C[0])
	}
}

func TestDictGetNearAdditionalBranches(t *testing.T) {
	root := mustPlainDictRoot(t, 8, map[uint64]uint64{
		0x10: 0xA1,
		0x20: 0xB2,
		0xF0: 0xD4,
	}, 8)

	state := newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0x05, 8)); err != nil {
		t.Fatalf("push get-near miss key: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push get-near miss root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push get-near miss bits: %v", err)
	}
	if err := execDictGetNear(dictNearVariant{kind: dictKeySlice, fetchNext: false, allowEq: false})(state); err != nil {
		t.Fatalf("dict get-near slice miss failed: %v", err)
	}
	ok, err := state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop get-near miss ok: %v", err)
	}
	if ok || state.Stack.Len() != 0 {
		t.Fatalf("expected slice miss to push only false")
	}

	state = newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(0x1FF)); err != nil {
		t.Fatalf("push get-near overflow key: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push get-near overflow root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push get-near overflow bits: %v", err)
	}
	if err := execDictGetNear(dictNearVariant{kind: dictKeyUnsignedInt, fetchNext: false, allowEq: false})(state); err != nil {
		t.Fatalf("dict get-near overflow failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop get-near overflow ok: %v", err)
	}
	maxKey, err := state.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop get-near overflow key: %v", err)
	}
	maxValue, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop get-near overflow value: %v", err)
	}
	if !ok || maxKey.Uint64() != 0xF0 || maxValue.MustLoadUInt(8) != 0xD4 {
		t.Fatalf("unexpected overflow fallback result")
	}

	signedRoot := mustSignedDictRoot(t, 8, map[int64]uint64{
		-2: 0xC1,
		1:  0xD2,
	}, 8)
	state = newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("push signed near key: %v", err)
	}
	if err := state.Stack.PushCell(signedRoot); err != nil {
		t.Fatalf("push signed near root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push signed near bits: %v", err)
	}
	if err := execDictGetNear(dictNearVariant{kind: dictKeySignedInt, fetchNext: false, allowEq: false})(state); err != nil {
		t.Fatalf("dict get-near signed failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop signed near ok: %v", err)
	}
	signedKey, err := state.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop signed near key: %v", err)
	}
	signedValue, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop signed near value: %v", err)
	}
	if !ok || signedKey.Int64() != -2 || signedValue.MustLoadUInt(8) != 0xC1 {
		t.Fatalf("unexpected signed near result")
	}
}

func TestPopSubdictPrefixUnsignedAndErrorPaths(t *testing.T) {
	state := newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(3)); err != nil {
		t.Fatalf("push unsigned prefix value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("push unsigned prefix bits: %v", err)
	}
	bits, prefix, err := popSubdictPrefix(state, 8, dictKeyUnsignedInt)
	if err != nil {
		t.Fatalf("popSubdictPrefix unsigned failed: %v", err)
	}
	if bits != 2 || prefix.BeginParse().MustLoadUInt(2) != 0b11 {
		t.Fatalf("unexpected unsigned prefix result")
	}

	state = newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push bad unsigned prefix value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("push bad unsigned prefix bits: %v", err)
	}
	_, _, err = popSubdictPrefix(state, 8, dictKeyUnsignedInt)
	if err == nil {
		t.Fatal("expected unsigned prefix overflow")
	}
	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeCellUnderflow {
		t.Fatalf("expected cell underflow for oversized unsigned prefix, got %v", err)
	}
}

func TestDictOptRefDeleteAndBuilderBranches(t *testing.T) {
	refValue := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	refRoot := mustRefDictRoot(t, 8, map[uint64]*cell.Cell{0x11: refValue})

	state := newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(-1)); err != nil {
		t.Fatalf("push getoptref invalid key: %v", err)
	}
	if err := state.Stack.PushCell(refRoot); err != nil {
		t.Fatalf("push getoptref root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push getoptref bits: %v", err)
	}
	if err := execDictGetOptRef(dictScalarVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("dict getoptref invalid failed: %v", err)
	}
	gotMaybe, err := state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop getoptref invalid result: %v", err)
	}
	if gotMaybe != nil {
		t.Fatalf("expected invalid getoptref to return nil")
	}

	state = newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(0x77)); err != nil {
		t.Fatalf("push getoptref miss key: %v", err)
	}
	if err := state.Stack.PushCell(refRoot); err != nil {
		t.Fatalf("push getoptref miss root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push getoptref miss bits: %v", err)
	}
	if err := execDictGetOptRef(dictScalarVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("dict getoptref miss failed: %v", err)
	}
	gotMaybe, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop getoptref miss result: %v", err)
	}
	if gotMaybe != nil {
		t.Fatalf("expected missing getoptref to return nil")
	}

	newRef := cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell()
	state = newDictTestState()
	if err := state.Stack.PushCell(newRef); err != nil {
		t.Fatalf("push setgetoptref value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(0x11)); err != nil {
		t.Fatalf("push setgetoptref key: %v", err)
	}
	if err := state.Stack.PushCell(refRoot); err != nil {
		t.Fatalf("push setgetoptref root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push setgetoptref bits: %v", err)
	}
	if err := execDictSetGetOptRef(dictScalarVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("dict setgetoptref replace failed: %v", err)
	}
	oldRef, err := state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop setgetoptref old ref: %v", err)
	}
	newRoot, err := state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop setgetoptref new root: %v", err)
	}
	gotRefValue, err := newRoot.AsDict(8).LoadValueByIntKey(big.NewInt(0x11))
	if err != nil {
		t.Fatalf("unexpected setgetoptref replace result")
	}
	gotRef, err := gotRefValue.LoadRefCell()
	if err != nil || string(oldRef.Hash()) != string(refValue.Hash()) || string(gotRef.Hash()) != string(newRef.Hash()) {
		t.Fatalf("unexpected setgetoptref replace result")
	}

	state = newDictTestState()
	if err := state.Stack.PushCell(nil); err != nil {
		t.Fatalf("push setgetoptref delete value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(0x11)); err != nil {
		t.Fatalf("push setgetoptref delete key: %v", err)
	}
	if err := state.Stack.PushCell(refRoot); err != nil {
		t.Fatalf("push setgetoptref delete root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push setgetoptref delete bits: %v", err)
	}
	if err := execDictSetGetOptRef(dictScalarVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("dict setgetoptref delete failed: %v", err)
	}
	oldRef, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop setgetoptref delete old ref: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop setgetoptref delete root: %v", err)
	}
	if string(oldRef.Hash()) != string(refValue.Hash()) || newRoot != nil {
		t.Fatalf("unexpected setgetoptref delete result")
	}

	root := mustPlainDictRoot(t, 8, map[uint64]uint64{0x12: 0x34}, 8)
	state = newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(0x99)); err != nil {
		t.Fatalf("push delete miss key: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push delete miss root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push delete miss bits: %v", err)
	}
	if err := execDictDelete(dictScalarVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("dict delete miss failed: %v", err)
	}
	ok, err := state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop delete miss ok: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop delete miss root: %v", err)
	}
	got, err := newRoot.AsDict(8).LoadValueByIntKey(big.NewInt(0x12))
	if err != nil || got.MustLoadUInt(8) != 0x34 {
		t.Fatalf("expected delete miss to keep original value, got %v err=%v", got, err)
	}

	state = newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(-1)); err != nil {
		t.Fatalf("push delete invalid key: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push delete invalid root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push delete invalid bits: %v", err)
	}
	if err := execDictDelete(dictScalarVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("dict delete invalid key failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop delete invalid ok: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop delete invalid root: %v", err)
	}
	got, err = newRoot.AsDict(8).LoadValueByIntKey(big.NewInt(0x12))
	if err != nil || ok || got.MustLoadUInt(8) != 0x34 {
		t.Fatalf("expected invalid delete key to return false and keep dict")
	}

	state = newDictTestState()
	if err := state.Stack.PushBuilder(cell.BeginCell().MustStoreUInt(0x77, 8)); err != nil {
		t.Fatalf("push setbuilder miss value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(0x44)); err != nil {
		t.Fatalf("push setbuilder miss key: %v", err)
	}
	if err := state.Stack.PushCell(nil); err != nil {
		t.Fatalf("push setbuilder miss root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push setbuilder miss bits: %v", err)
	}
	if err := execDictSetBuilder(cell.DictSetModeReplace)(dictScalarVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("dict setbuilder replace miss failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop setbuilder miss ok: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop setbuilder miss root: %v", err)
	}
	if ok || newRoot != nil {
		t.Fatalf("expected setbuilder replace miss to keep empty dict")
	}

	state = newDictTestState()
	if err := state.Stack.PushBuilder(cell.BeginCell().MustStoreUInt(0x88, 8)); err != nil {
		t.Fatalf("push setgetbuilder add value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(0x12)); err != nil {
		t.Fatalf("push setgetbuilder add key: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push setgetbuilder add root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push setgetbuilder add bits: %v", err)
	}
	if err := execDictSetGetBuilder(cell.DictSetModeAdd)(dictScalarVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("dict setgetbuilder add existing failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop setgetbuilder add ok: %v", err)
	}
	oldValue, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop setgetbuilder add old value: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop setgetbuilder add root: %v", err)
	}
	currentValue, err := newRoot.AsDict(8).LoadValueByIntKey(big.NewInt(0x12))
	if err != nil || ok || oldValue.MustLoadUInt(8) != 0x34 || currentValue.MustLoadUInt(8) != 0x34 {
		t.Fatalf("unexpected setgetbuilder add existing result")
	}
}

func TestPrefixMutationMissBranches(t *testing.T) {
	pfxRoot := mustPrefixDictRoot(t, 4, map[uint64]dictPrefixEntry{
		0b10: {bits: 2, value: cell.BeginCell().MustStoreUInt(0xD, 4).EndCell()},
	})

	state := newDictTestState()
	if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xE, 4).EndCell().BeginParse()); err != nil {
		t.Fatalf("push pfx replace miss value: %v", err)
	}
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0b11, 2)); err != nil {
		t.Fatalf("push pfx replace miss key: %v", err)
	}
	if err := state.Stack.PushCell(nil); err != nil {
		t.Fatalf("push pfx replace miss root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push pfx replace miss bits: %v", err)
	}
	if err := execPfxDictSet(cell.DictSetModeReplace)(state); err != nil {
		t.Fatalf("pfx replace miss failed: %v", err)
	}
	ok, err := state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop pfx replace miss ok: %v", err)
	}
	newRoot, err := state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop pfx replace miss root: %v", err)
	}
	if ok || newRoot != nil {
		t.Fatalf("expected pfx replace miss to keep empty root")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xE, 4).EndCell().BeginParse()); err != nil {
		t.Fatalf("push pfx add existing value: %v", err)
	}
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0b10, 2)); err != nil {
		t.Fatalf("push pfx add existing key: %v", err)
	}
	if err := state.Stack.PushCell(pfxRoot); err != nil {
		t.Fatalf("push pfx add existing root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push pfx add existing bits: %v", err)
	}
	if err := execPfxDictSet(cell.DictSetModeAdd)(state); err != nil {
		t.Fatalf("pfx add existing failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop pfx add existing ok: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop pfx add existing root: %v", err)
	}
	if ok || newRoot == nil || string(newRoot.Hash()) != string(pfxRoot.Hash()) {
		t.Fatalf("expected pfx add existing to keep original root")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0b01, 2)); err != nil {
		t.Fatalf("push pfx delete miss key: %v", err)
	}
	if err := state.Stack.PushCell(pfxRoot); err != nil {
		t.Fatalf("push pfx delete miss root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push pfx delete miss bits: %v", err)
	}
	if err := execPfxDictDelete(state); err != nil {
		t.Fatalf("pfx delete miss failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop pfx delete miss ok: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop pfx delete miss root: %v", err)
	}
	gotValue, err := newRoot.AsPrefixDict(4).LoadValue(mustDictKeyCell(t, 0b10, 2))
	if err != nil || gotValue.MustLoadUInt(4) != 0xD {
		t.Fatalf("expected pfx delete miss to keep original prefix entry: %v err=%v", gotValue, err)
	}
}

func TestDictLoadAndPrefixMissTailBranches(t *testing.T) {
	root := mustPlainDictRoot(t, 8, map[uint64]uint64{0x12: 0x34}, 8)
	serialized := cell.BeginCell().MustStoreMaybeRef(root).ToSlice()

	state := newDictTestState()
	if err := state.Stack.PushSlice(serialized.Copy()); err != nil {
		t.Fatalf("push preload dict slice: %v", err)
	}
	if err := execLoadDictSlice(true, false)(state); err != nil {
		t.Fatalf("preload dict slice failed: %v", err)
	}
	dictSlice, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop preload dict slice: %v", err)
	}
	if dictSlice.RefsNum() != 1 || state.Stack.Len() != 0 {
		t.Fatalf("unexpected preload dict slice result")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(serialized.Copy()); err != nil {
		t.Fatalf("push quiet preload dict slice: %v", err)
	}
	if err := execLoadDictSlice(true, true)(state); err != nil {
		t.Fatalf("quiet preload dict slice failed: %v", err)
	}
	ok, err := state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop quiet preload dict slice ok: %v", err)
	}
	dictSlice, err = state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop quiet preload dict slice result: %v", err)
	}
	if !ok || dictSlice.RefsNum() != 1 {
		t.Fatalf("unexpected quiet preload dict slice result")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(1, 1).ToSlice()); err != nil {
		t.Fatalf("push invalid load-dict-slice: %v", err)
	}
	if err := execLoadDictSlice(false, false)(state); err == nil {
		t.Fatal("expected non-quiet load dict slice to fail on invalid serialization")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(serialized.Copy()); err != nil {
		t.Fatalf("push preload dict: %v", err)
	}
	if err := execLoadDict(true, false)(state); err != nil {
		t.Fatalf("preload dict failed: %v", err)
	}
	gotRoot, err := state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop preload dict root: %v", err)
	}
	if gotRoot == nil || string(gotRoot.Hash()) != string(root.Hash()) || state.Stack.Len() != 0 {
		t.Fatalf("unexpected preload dict result")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(serialized.Copy()); err != nil {
		t.Fatalf("push load-dict rest slice: %v", err)
	}
	if err := execLoadDict(false, false)(state); err != nil {
		t.Fatalf("load dict with remainder failed: %v", err)
	}
	restWithDict, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop load-dict remainder: %v", err)
	}
	gotRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop load-dict root: %v", err)
	}
	if gotRoot == nil || string(gotRoot.Hash()) != string(root.Hash()) || restWithDict.BitsLeft() != 0 || restWithDict.RefsNum() != 0 {
		t.Fatalf("unexpected load-dict remainder result")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(1, 1).ToSlice()); err != nil {
		t.Fatalf("push invalid load-dict: %v", err)
	}
	if err := execLoadDict(false, false)(state); err == nil {
		t.Fatal("expected non-quiet load dict to fail on invalid serialization")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(1, 1).ToSlice()); err != nil {
		t.Fatalf("push invalid quiet preload dict: %v", err)
	}
	if err := execLoadDict(true, true)(state); err != nil {
		t.Fatalf("quiet preload invalid dict should not fail: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop quiet preload invalid dict ok: %v", err)
	}
	if ok || state.Stack.Len() != 0 {
		t.Fatalf("expected quiet preload invalid dict to return only false")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(1, 1).ToSlice()); err != nil {
		t.Fatalf("push invalid quiet dict: %v", err)
	}
	if err := execLoadDict(false, true)(state); err != nil {
		t.Fatalf("quiet invalid dict with remainder should not fail: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop quiet invalid dict ok: %v", err)
	}
	rest, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop quiet invalid dict remainder: %v", err)
	}
	if ok || rest.MustLoadUInt(1) != 1 {
		t.Fatalf("expected quiet invalid dict to keep original slice and return false")
	}

	pfxRoot := mustPrefixDictRoot(t, 4, map[uint64]dictPrefixEntry{
		0b10: {bits: 2, value: cell.BeginCell().MustStoreUInt(0xD, 4).EndCell()},
	})
	state = newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0b0111, 4)); err != nil {
		t.Fatalf("push pfx jmp miss input: %v", err)
	}
	if err := state.Stack.PushCell(pfxRoot); err != nil {
		t.Fatalf("push pfx jmp miss root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push pfx jmp miss bits: %v", err)
	}
	if err := execPfxDictGet(2)(state); err != nil {
		t.Fatalf("pfx dict get jump miss failed: %v", err)
	}
	rest, err = state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop pfx jmp miss rest: %v", err)
	}
	if rest.MustLoadUInt(4) != 0b0111 || state.Stack.Len() != 0 {
		t.Fatalf("expected jump miss to leave input unchanged")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xE, 4).EndCell().BeginParse()); err != nil {
		t.Fatalf("push invalid pfx set value: %v", err)
	}
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0b11, 2)); err != nil {
		t.Fatalf("push invalid pfx set key: %v", err)
	}
	if err := state.Stack.PushCell(pfxRoot); err != nil {
		t.Fatalf("push invalid pfx set root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push invalid pfx set bits: %v", err)
	}
	if err := execPfxDictSet(cell.DictSetModeSet)(state); err == nil {
		t.Fatal("expected oversized prefix key to fail for pfx set")
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0b11, 2)); err != nil {
		t.Fatalf("push invalid pfx delete key: %v", err)
	}
	if err := state.Stack.PushCell(pfxRoot); err != nil {
		t.Fatalf("push invalid pfx delete root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push invalid pfx delete bits: %v", err)
	}
	if err := execPfxDictDelete(state); err == nil {
		t.Fatal("expected oversized prefix key to fail for pfx delete")
	}
}

func TestDictSetGetAndDeleteGetTailBranches(t *testing.T) {
	refValue := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	refRoot := mustRefDictRoot(t, 8, map[uint64]*cell.Cell{0x12: refValue})

	state := newDictTestState()
	if err := state.Stack.PushCell(cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()); err != nil {
		t.Fatalf("push add-existing ref value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(0x12)); err != nil {
		t.Fatalf("push add-existing ref key: %v", err)
	}
	if err := state.Stack.PushCell(refRoot); err != nil {
		t.Fatalf("push add-existing ref root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push add-existing ref bits: %v", err)
	}
	if err := execDictSetGet(cell.DictSetModeAdd)(dictValueVariant{kind: dictKeyUnsignedInt, byRef: true})(state); err != nil {
		t.Fatalf("dict setget add existing ref failed: %v", err)
	}
	ok, err := state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop add-existing ref ok: %v", err)
	}
	oldRef, err := state.Stack.PopCell()
	if err != nil {
		t.Fatalf("pop add-existing ref old value: %v", err)
	}
	newRoot, err := state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop add-existing ref root: %v", err)
	}
	gotRefValue, err := newRoot.AsDict(8).LoadValueByIntKey(big.NewInt(0x12))
	if err != nil {
		t.Fatalf("expected add-existing ref to return old value and keep dict intact")
	}
	gotRef, err := gotRefValue.LoadRefCell()
	if err != nil || ok || string(oldRef.Hash()) != string(refValue.Hash()) || string(gotRef.Hash()) != string(refValue.Hash()) {
		t.Fatalf("expected add-existing ref to return old value and keep dict intact")
	}

	root := mustPlainDictRoot(t, 8, map[uint64]uint64{0x12: 0x34}, 8)
	state = newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0x99, 8)); err != nil {
		t.Fatalf("push deleteget miss key: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push deleteget miss root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push deleteget miss bits: %v", err)
	}
	if err := execDictDeleteGet(dictValueVariant{kind: dictKeySlice})(state); err != nil {
		t.Fatalf("dict deleteget miss failed: %v", err)
	}
	ok, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop deleteget miss ok: %v", err)
	}
	newRoot, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop deleteget miss root: %v", err)
	}
	gotValue, err := newRoot.AsDict(8).LoadValueByIntKey(big.NewInt(0x12))
	if err != nil || ok || gotValue.MustLoadUInt(8) != 0x34 || state.Stack.Len() != 0 {
		t.Fatalf("expected deleteget miss to keep original dict and return false")
	}
}
