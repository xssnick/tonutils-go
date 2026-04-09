package dict

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestPFXDICTSWITCHEdgeCases(t *testing.T) {
	op := PFXDICTSWITCH(nil, 4)
	if got := op.SerializeText(); got != "PFXDICTSWITCH 4 (<nil>)" {
		t.Fatalf("unexpected nil-root text form: %q", got)
	}

	decoded := PFXDICTSWITCH(mustPlainDictRoot(t, 4, map[uint64]uint64{0b1010: 0x11}, 8))
	if err := decoded.Deserialize(op.Serialize().EndCell().BeginParse()); err != nil {
		t.Fatalf("deserialize nil-root opcode failed: %v", err)
	}
	if decoded.bits != 4 || decoded.root != nil {
		t.Fatalf("unexpected decoded nil-root state: bits=%d root=%v", decoded.bits, decoded.root)
	}

	if err := decoded.Deserialize(cell.BeginCell().ToSlice()); err == nil {
		t.Fatal("expected deserialize to reject truncated opcode cell")
	}

	state := newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0b1010, 4)); err != nil {
		t.Fatalf("push switch input: %v", err)
	}
	if err := op.Interpret(state); err != nil {
		t.Fatalf("interpret nil-root switch failed: %v", err)
	}
	rest, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop switch rest: %v", err)
	}
	if rest.MustLoadUInt(4) != 0b1010 || state.Stack.Len() != 0 {
		t.Fatalf("expected nil-root switch to leave only the input slice on the stack")
	}

	state = newDictTestState()
	if err := op.Interpret(state); err == nil {
		t.Fatal("expected switch interpret to fail on empty stack")
	}
}

func TestPrefixDictGetAndSubdictEdgeBranches(t *testing.T) {
	pfxRoot := mustPrefixDictRoot(t, 4, map[uint64]dictPrefixEntry{
		0b10: {bits: 2, value: cell.BeginCell().MustStoreUInt(0xD, 4).EndCell()},
	})

	state := newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0b0111, 4)); err != nil {
		t.Fatalf("push pfx miss key: %v", err)
	}
	if err := state.Stack.PushCell(pfxRoot); err != nil {
		t.Fatalf("push pfx miss root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push pfx miss bits: %v", err)
	}
	if err := execPfxDictGet(0)(state); err != nil {
		t.Fatalf("quiet pfx miss should not fail: %v", err)
	}
	ok, err := state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop quiet pfx miss ok: %v", err)
	}
	rest, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop quiet pfx miss rest: %v", err)
	}
	if ok || rest.MustLoadUInt(4) != 0b0111 || state.Stack.Len() != 0 {
		t.Fatalf("expected quiet pfx miss to return false and keep the input slice")
	}

	subdictRoot := mustPlainDictRoot(t, 4, map[uint64]uint64{
		0b1000: 0x11,
		0b1010: 0x22,
		0b0011: 0x33,
	}, 8)

	state = newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(0b10)); err != nil {
		t.Fatalf("push subdict prefix value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("push subdict prefix bits: %v", err)
	}
	if err := state.Stack.PushCell(subdictRoot); err != nil {
		t.Fatalf("push subdict root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push subdict key bits: %v", err)
	}
	if err := execSubdict(false)(dictScalarVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("unsigned subdict without prefix removal failed: %v", err)
	}
	keptRoot, err := state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop kept subdict root: %v", err)
	}
	if keptRoot == nil {
		t.Fatal("expected non-empty kept-prefix subdict")
	}
	keptDict := keptRoot.AsDict(4)
	value, err := keptDict.LoadValueByIntKey(big.NewInt(0b1000))
	if err != nil || value.MustLoadUInt(8) != 0x11 {
		t.Fatalf("unexpected preserved key 1000: value=%v err=%v", value, err)
	}
	value, err = keptDict.LoadValueByIntKey(big.NewInt(0b1010))
	if err != nil || value.MustLoadUInt(8) != 0x22 {
		t.Fatalf("unexpected preserved key 1010: value=%v err=%v", value, err)
	}
	if _, err = keptDict.LoadValueByIntKey(big.NewInt(0b0011)); !errors.Is(err, cell.ErrNoSuchKeyInDict) {
		t.Fatalf("expected unrelated key to be absent from preserved subdict, got %v", err)
	}

	state = newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("push empty subdict prefix value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("push empty subdict prefix bits: %v", err)
	}
	if err := pushMaybeCell(state.Stack, nil); err != nil {
		t.Fatalf("push empty subdict root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push empty subdict key bits: %v", err)
	}
	if err := execSubdict(false)(dictScalarVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("empty subdict extraction should not fail: %v", err)
	}
	emptyRoot, err := state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop empty subdict root: %v", err)
	}
	if emptyRoot != nil {
		t.Fatalf("expected empty subdict root, got %v", emptyRoot)
	}
}

func TestDictHelperEdgeBranches(t *testing.T) {
	state := newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push signed prefix value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push signed prefix bits: %v", err)
	}
	if _, _, err := popSubdictPrefix(state, 8, dictKeySignedInt); err == nil {
		t.Fatal("expected signed prefix overflow to fail")
	} else {
		var vmErr vmerr.VMError
		if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeCellUnderflow {
			t.Fatalf("expected cell underflow for signed prefix overflow, got %v", err)
		}
	}

	state = newDictTestState()
	if err := pushMaybeCell(state.Stack, nil); err != nil {
		t.Fatalf("push nil dict root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push nil dict key bits: %v", err)
	}
	bits, root, err := popDictRootAndLen(state)
	if err != nil || bits != 4 || root != nil {
		t.Fatalf("unexpected nil-root popDictRootAndLen result: bits=%d root=%v err=%v", bits, root, err)
	}

	state = newDictTestState()
	if _, _, err := popDictRootAndLen(state); err == nil {
		t.Fatal("expected popDictRootAndLen to fail on empty stack")
	}

	emptySerialized := cell.BeginCell().MustStoreMaybeRef(nil).ToSlice()

	state = newDictTestState()
	if err := state.Stack.PushSlice(emptySerialized.Copy()); err != nil {
		t.Fatalf("push empty dict serialization: %v", err)
	}
	if err := execLoadDict(false, false)(state); err != nil {
		t.Fatalf("load empty dict failed: %v", err)
	}
	rest, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop empty dict rest: %v", err)
	}
	root, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop empty dict root: %v", err)
	}
	if root != nil || rest.BitsLeft() != 0 || rest.RefsNum() != 0 {
		t.Fatalf("unexpected empty dict load result: root=%v restBits=%d restRefs=%d", root, rest.BitsLeft(), rest.RefsNum())
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(emptySerialized.Copy()); err != nil {
		t.Fatalf("push preload empty dict serialization: %v", err)
	}
	if err := execLoadDict(true, false)(state); err != nil {
		t.Fatalf("preload empty dict failed: %v", err)
	}
	root, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop preload empty dict root: %v", err)
	}
	if root != nil || state.Stack.Len() != 0 {
		t.Fatalf("unexpected preload empty dict result: root=%v stack=%d", root, state.Stack.Len())
	}

	state = newDictTestState()
	if err := state.Stack.PushSlice(emptySerialized.Copy()); err != nil {
		t.Fatalf("push quiet preload empty dict serialization: %v", err)
	}
	if err := execLoadDict(true, true)(state); err != nil {
		t.Fatalf("quiet preload empty dict failed: %v", err)
	}
	ok, err := state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop quiet preload empty dict ok: %v", err)
	}
	root, err = state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop quiet preload empty dict root: %v", err)
	}
	if !ok || root != nil || state.Stack.Len() != 0 {
		t.Fatalf("unexpected quiet preload empty dict result: ok=%v root=%v stack=%d", ok, root, state.Stack.Len())
	}
}
