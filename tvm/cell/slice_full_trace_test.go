package cell

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func TestSliceToCellFullTracePreservesContentsAndEvents(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(5, 3).EndCell()
	pruned, err := createPrunedBranchFromCell(leaf, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		ref  *Cell
	}{{"ordinary", leaf}, {"multilevel", pruned}} {
		t.Run(tc.name, func(t *testing.T) {
			root := BeginCell().MustStoreUInt(0b101, 3).MustStoreRef(tc.ref).MustStoreRef(tc.ref).EndCell()
			// Parsing a BoC leaves the top-up bit in the backing payload.
			root, err := FromBOC(root.ToBOC())
			if err != nil {
				t.Fatal(err)
			}
			var events []int
			children := [2]*Trace{
				NewTrace(TraceHooks{OnLoad: func(*Cell) { events = append(events, 10) }}),
				NewTrace(TraceHooks{OnLoad: func(*Cell) { events = append(events, 11) }}),
			}
			trace := NewTrace(TraceHooks{
				OnChild:      func(i int) *Trace { events = append(events, i); return children[i] },
				OnCreate:     func() { events = append(events, 2) },
				PendingError: func() error { events = append(events, 3); return nil },
			})
			s := root.MustBeginParse().SetTrace(trace)
			out, err := s.ToCell()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(events, []int{0, 1, 2, 3}) {
				t.Fatalf("creation events = %v", events)
			}
			if out == root || out.Trace() != nil || root.MustPeekRef(0).Trace() != nil {
				t.Fatal("creation must keep source and output trace ownership separate")
			}
			for level := 0; level <= 3; level++ {
				if out.HashKeyAt(level) != root.HashKeyAt(level) || out.Depth(level) != root.Depth(level) {
					t.Fatalf("hash/depth changed at level %d", level)
				}
			}
			if !bytes.Equal(out.ToBOC(), root.ToBOC()) {
				t.Fatal("traced copy changed BoC encoding")
			}
			for i := range children {
				ref := out.MustPeekRef(i)
				if ref.Trace() != children[i] {
					t.Fatalf("child %d trace changed", i)
				}
				ref.MustBeginParse()
			}
			if !reflect.DeepEqual(events, []int{0, 1, 2, 3, 10, 11}) {
				t.Fatalf("child events = %v", events)
			}
		})
	}
}

func TestSliceToCellFullTraceRejectsCreation(t *testing.T) {
	want := errors.New("creation rejected")
	creates := 0
	trace := NewTrace(TraceHooks{
		OnCreate:     func() { creates++ },
		PendingError: func() error { return want },
	})
	s := BeginCell().MustStoreUInt(1, 1).EndCell().MustBeginParse().SetTrace(trace)
	got, err := s.ToCell()
	if !errors.Is(err, want) || got != nil || creates != 1 {
		t.Fatalf("ToCell = %v, %v; creates = %d", got, err, creates)
	}
}

func TestSliceToCellFullTraceCopiesBorrowedPayload(t *testing.T) {
	data := []byte{0xA0}
	raw := Cell{data: data, bitsSz: 3}
	s := Slice{cell: &raw, bitEnd: 3, forceCopyOnToCell: true}
	s.SetTrace(NewTrace(TraceHooks{OnCreate: func() {}}))
	out, err := s.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	data[0] = 0
	if out.MustBeginParse().MustLoadUInt(3) != 5 {
		t.Fatal("traced cell retained borrowed payload")
	}
}

func TestSliceToCellTracedSpecialAndVirtualized(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(7, 3).EndCell()
	pruned, err := createPrunedBranchFromCell(leaf, 1)
	if err != nil {
		t.Fatal(err)
	}
	root := BeginCell().MustStoreUInt(3, 2).MustStoreRef(pruned).EndCell()
	proof, err := CreateMerkleProof(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		cell *Cell
	}{{"virtualized", root.Virtualize(0)}, {"proof", proof}, {"pruned", pruned}} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.cell.MustBeginParse().SetTrace(NewTrace(TraceHooks{OnCreate: func() {}}))
			out, err := s.ToCell()
			if err != nil {
				t.Fatal(err)
			}
			want, err := s.ToBuilder().EndCellSpecial(tc.cell.IsSpecial())
			if err != nil {
				t.Fatal(err)
			}
			if out.IsVirtualized() || out.HashKey() != want.HashKey() || !bytes.Equal(out.ToBOC(), want.ToBOC()) {
				t.Fatal("traced slice differs from explicitly rebuilt cell")
			}
		})
	}
}
