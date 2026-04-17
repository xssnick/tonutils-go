package cell

import (
	"bytes"
	"errors"
	"testing"
)

func makeTestDictKey(bits uint, value uint64) *Cell {
	return BeginCell().MustStoreUInt(value, bits).EndCell()
}

func makeTestDictValue(value uint64) *Cell {
	return BeginCell().MustStoreUInt(value, 8).EndCell()
}

func mustCreateProof(t testing.TB, root *Cell, sk *ProofSkeleton) *Cell {
	t.Helper()

	proof, err := root.CreateProof(sk)
	if err != nil {
		t.Fatalf("create proof: %v", err)
	}
	return proof
}

func assertSameCellTree(t testing.TB, got, want *Cell) {
	t.Helper()

	if got == nil || want == nil {
		if got != want {
			t.Fatalf("nil mismatch: got %v want %v", got, want)
		}
		return
	}
	if got.bitsSz != want.bitsSz {
		t.Fatalf("bits size mismatch: got %d want %d", got.bitsSz, want.bitsSz)
	}
	if got.flags != want.flags {
		t.Fatalf("flags mismatch: got %08b want %08b", got.flags, want.flags)
	}
	if !bytes.Equal(got.data, want.data) {
		t.Fatalf("data mismatch: got %x want %x", got.data, want.data)
	}
	if got.refsCount() != want.refsCount() {
		t.Fatalf("refs count mismatch: got %d want %d", got.refsCount(), want.refsCount())
	}
	for i := 0; i < got.refsCount(); i++ {
		assertSameCellTree(t, got.ref(i), want.ref(i))
	}
}

func TestProofTraceAppendUsageToReturnsLeafSkeleton(t *testing.T) {
	trace := NewProofTrace()
	first := trace.OnRef(0, 1)
	leafNode := trace.OnRef(first, 0)

	dst := CreateProofSkeleton()
	leaf := trace.AppendUsageTo(dst, leafNode)
	if leaf == nil {
		t.Fatal("expected non-nil leaf skeleton")
	}
	if leaf != dst.branches[1].branches[0] {
		t.Fatal("unexpected leaf skeleton pointer")
	}

	leaf.SetRecursive()
	if !dst.branches[1].branches[0].recursive {
		t.Fatal("expected recursive flag on leaf subtree")
	}
	if dst.recursive || dst.branches[1].recursive {
		t.Fatal("unexpected recursive flag outside leaf subtree")
	}
}

func TestDictionaryProofTraceBuildsProofForLoadedKey(t *testing.T) {
	dict := NewDict(4)
	if err := dict.Set(makeTestDictKey(4, 0x1), makeTestDictValue(0x11)); err != nil {
		t.Fatalf("set first key: %v", err)
	}
	if err := dict.Set(makeTestDictKey(4, 0x9), makeTestDictValue(0x22)); err != nil {
		t.Fatalf("set second key: %v", err)
	}

	key := makeTestDictKey(4, 0x1)
	root := dict.AsCell()

	trace := NewProofTrace()
	observed := root.AsDict(4).SetObserver(trace)
	value, err := observed.LoadValue(key)
	if err != nil {
		t.Fatalf("load value with trace: %v", err)
	}
	if value == nil {
		t.Fatal("expected traced value")
	}

	proofBody, err := UnwrapProof(mustCreateProof(t, root, trace.Skeleton()), root.Hash())
	if err != nil {
		t.Fatalf("unwrap proof: %v", err)
	}
	if _, err = proofBody.AsDict(4).LoadValue(key); err != nil {
		t.Fatalf("proof lookup failed: %v", err)
	}
}

func TestDictionaryProofTraceBuildsProofForMissingKey(t *testing.T) {
	dict := NewDict(4)
	if err := dict.Set(makeTestDictKey(4, 0x1), makeTestDictValue(0x11)); err != nil {
		t.Fatalf("set first key: %v", err)
	}
	if err := dict.Set(makeTestDictKey(4, 0x9), makeTestDictValue(0x22)); err != nil {
		t.Fatalf("set second key: %v", err)
	}

	key := makeTestDictKey(4, 0x5)
	root := dict.AsCell()

	trace := NewProofTrace()
	observed := root.AsDict(4).SetObserver(trace)
	if _, err := observed.LoadValue(key); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("expected traced missing-key error, got %v", err)
	}

	proofBody, err := UnwrapProof(mustCreateProof(t, root, trace.Skeleton()), root.Hash())
	if err != nil {
		t.Fatalf("unwrap proof: %v", err)
	}
	if _, err = proofBody.AsDict(4).LoadValue(key); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("expected missing-key proof lookup error, got %v", err)
	}
}

func TestNestedDictionaryProofTraceContinuesThroughLoadDict(t *testing.T) {
	inner := NewDict(4)
	if err := inner.Set(makeTestDictKey(4, 0x3), makeTestDictValue(0x33)); err != nil {
		t.Fatalf("set inner key: %v", err)
	}

	outer := NewDict(4)
	outerValue := BeginCell().MustStoreMaybeRef(inner.AsCell()).EndCell()
	if err := outer.Set(makeTestDictKey(4, 0x7), outerValue); err != nil {
		t.Fatalf("set outer key: %v", err)
	}

	root := outer.AsCell()
	outerKey := makeTestDictKey(4, 0x7)
	innerKey := makeTestDictKey(4, 0x3)

	trace := NewProofTrace()
	observedOuter := root.AsDict(4).SetObserver(trace)
	observedOuterValue, err := observedOuter.LoadValue(outerKey)
	if err != nil {
		t.Fatalf("load outer value: %v", err)
	}
	observedInner, err := observedOuterValue.LoadDict(4)
	if err != nil {
		t.Fatalf("load inner dict: %v", err)
	}
	if _, err = observedInner.LoadValue(innerKey); err != nil {
		t.Fatalf("load inner value: %v", err)
	}

	proofBody, err := UnwrapProof(mustCreateProof(t, root, trace.Skeleton()), root.Hash())
	if err != nil {
		t.Fatalf("unwrap proof: %v", err)
	}
	proofOuterValue, err := proofBody.AsDict(4).LoadValue(outerKey)
	if err != nil {
		t.Fatalf("proof outer lookup: %v", err)
	}
	proofInner, err := proofOuterValue.LoadDict(4)
	if err != nil {
		t.Fatalf("proof inner dict: %v", err)
	}
	proofInnerValue, err := proofInner.LoadValue(innerKey)
	if err != nil {
		t.Fatalf("proof inner lookup: %v", err)
	}
	if proofInnerValue.MustLoadUInt(8) != 0x33 {
		t.Fatal("unexpected inner proof value")
	}
}

func TestNestedDictionaryProofTraceExplicitNodeAPIMatchesLoadDict(t *testing.T) {
	inner := NewDict(4)
	if err := inner.Set(makeTestDictKey(4, 0x4), makeTestDictValue(0x44)); err != nil {
		t.Fatalf("set inner key: %v", err)
	}

	outer := NewDict(4)
	outerValue := BeginCell().MustStoreRef(inner.AsCell()).EndCell()
	if err := outer.Set(makeTestDictKey(4, 0x8), outerValue); err != nil {
		t.Fatalf("set outer key: %v", err)
	}

	root := outer.AsCell()
	outerKey := makeTestDictKey(4, 0x8)
	innerKey := makeTestDictKey(4, 0x4)

	autoTrace := NewProofTrace()
	observedOuter := root.AsDict(4).SetObserver(autoTrace)
	observedOuterValue, err := observedOuter.LoadValue(outerKey)
	if err != nil {
		t.Fatalf("auto load outer value: %v", err)
	}
	observedInnerRoot, err := observedOuterValue.LoadRef()
	if err != nil {
		t.Fatalf("auto load inner ref: %v", err)
	}
	observedInner, err := observedInnerRoot.ToDict(4)
	if err != nil {
		t.Fatalf("auto load inner dict: %v", err)
	}
	if _, err = observedInner.LoadValue(innerKey); err != nil {
		t.Fatalf("auto load inner value: %v", err)
	}
	autoProof := mustCreateProof(t, root, autoTrace.Skeleton())

	trace := NewProofTrace()
	observedOuter = root.AsDict(4).SetObserver(trace)
	observedOuterValue, err = observedOuter.LoadValue(outerKey)
	if err != nil {
		t.Fatalf("load outer value: %v", err)
	}
	innerRoot, node, err := observedOuterValue.LoadRefCellWithNode()
	if err != nil {
		t.Fatalf("load inner root with node: %v", err)
	}
	observedInner = innerRoot.AsDict(4).SetObserverNode(trace, node)
	if _, err = observedInner.LoadValue(innerKey); err != nil {
		t.Fatalf("load inner value: %v", err)
	}

	assertSameCellTree(t, mustCreateProof(t, root, trace.Skeleton()), autoProof)
}
