package cell

import (
	"strings"
	"testing"
)

// The parent contract of PreparedMerkleUpdate.ApplyTo, in whichever build is
// running.
//
// The contract is that from carries no Trace: reads recorded during an apply
// widen a ReadSet, a ReadSet selects the cells a collated proof ships, and so a
// trace arriving here by accident changes produced block bytes on one node and
// nowhere else. Under -tags cellassert that is refused; without the tag it is
// allowed, because a traced parent is a legitimate choice of a caller that
// wants those reads and ApplyTo cannot tell the two apart.
//
// The test asks the assertion itself which build this is rather than reading a
// build tag, so it also pins the thing that could silently rot: that ApplyTo
// consults the assertion at all.
func TestPreparedMerkleUpdateTracedParentContract(t *testing.T) {
	from := buildBinaryTree([]uint16{1, 2, 3, 4})
	to := buildBinaryTree([]uint16{9, 2, 3, 4})
	updateFrom, updateTo, err := buildOrdinaryMerkleUpdateBodiesFromDiffAt(from, to, true)
	if err != nil {
		t.Fatalf("build update bodies: %v", err)
	}
	update, err := CreateMerkleUpdate(updateFrom, updateTo)
	if err != nil {
		t.Fatalf("create update: %v", err)
	}
	prepared, err := PrepareMerkleUpdatePlanned(update)
	if err != nil {
		t.Fatalf("prepare update: %v", err)
	}
	traced := NewReadSet(from).Root()
	if traced.Trace() == nil {
		t.Fatal("the fixture parent carries no trace, so nothing about the contract is testable")
	}
	if traced.HashKeyAt(0) != from.HashKeyAt(0) {
		t.Fatal("tracing the parent changed its hash")
	}

	// The untraced parent applies in every build; it is the control that says a
	// rejection below is about the trace and not about this fixture.
	want, err := prepared.ApplyTo(traced.WithoutTrace())
	if err != nil {
		t.Fatalf("applying to the untraced parent: %v", err)
	}

	got, err := prepared.ApplyTo(traced)
	if assertUntracedApplyParent(traced) != nil {
		if err == nil || !strings.Contains(err.Error(), "parent carries a trace") {
			t.Fatalf("applying to a traced parent under the assertion = %v, want the parent-trace rejection", err)
		}
		if got != nil {
			t.Fatal("a refused apply returned a root")
		}
		return
	}
	if err != nil {
		t.Fatalf("applying to a traced parent in a build without the assertion: %v", err)
	}
	if got.HashKeyAt(0) != want.HashKeyAt(0) {
		t.Fatal("the traced parent produced another successor")
	}
}
