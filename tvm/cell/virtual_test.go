package cell

import "testing"

func buildVirtualizedProofBody(t *testing.T) (*Cell, *Cell, *Cell) {
	t.Helper()

	branch := BeginCell().
		MustStoreUInt(0xBEEF, 16).
		MustStoreRef(BeginCell().MustStoreUInt(1, 1).EndCell()).
		EndCell()
	root := BeginCell().
		MustStoreUInt(0, 1).
		MustStoreRef(branch).
		EndCell()

	proof, err := root.CreateProof(CreateProofSkeleton())
	if err != nil {
		t.Fatalf("create proof: %v", err)
	}

	body, err := UnwrapProofVirtualized(proof, root.Hash())
	if err != nil {
		t.Fatalf("unwrap virtualized proof: %v", err)
	}

	pruned, err := body.PeekRef(0)
	if err != nil {
		t.Fatalf("peek virtualized child: %v", err)
	}

	return root, body, pruned
}

func TestUnwrapProofVirtualizedCreatesVisibleVirtualizedView(t *testing.T) {
	root, body, pruned := buildVirtualizedProofBody(t)

	if !body.IsVirtualized() {
		t.Fatal("expected proof body to be virtualized")
	}
	if body.EffectiveLevel() != 0 {
		t.Fatalf("unexpected effective level: %d", body.EffectiveLevel())
	}
	if body.ActualLevel() <= body.EffectiveLevel() {
		t.Fatalf("expected actual level to exceed effective level, got actual=%d effective=%d", body.ActualLevel(), body.EffectiveLevel())
	}

	rawBody, err := root.CreateProof(CreateProofSkeleton())
	if err != nil {
		t.Fatalf("recreate proof: %v", err)
	}
	unwrappedRaw, err := UnwrapProof(rawBody, root.Hash())
	if err != nil {
		t.Fatalf("unwrap raw proof: %v", err)
	}
	rawChild, err := unwrappedRaw.PeekRef(0)
	if err != nil {
		t.Fatalf("peek raw child: %v", err)
	}
	if rawChild.IsVirtualized() {
		t.Fatal("unexpected virtualization on raw proof child")
	}

	if !pruned.IsVirtualized() {
		t.Fatal("expected child reference from virtualized body to stay virtualized")
	}
	if pruned.GetType() != PrunedCellType {
		t.Fatalf("expected pruned child, got %v", pruned.GetType())
	}
	if pruned.EffectiveLevel() != 0 {
		t.Fatalf("unexpected child effective level: %d", pruned.EffectiveLevel())
	}
	if pruned.ActualLevel() <= pruned.EffectiveLevel() {
		t.Fatalf("expected child actual level to exceed effective level, got actual=%d effective=%d", pruned.ActualLevel(), pruned.EffectiveLevel())
	}

	rawView := body.rawCell()
	if rawView.HashKey() != unwrappedRaw.HashKey() {
		t.Fatal("raw cell lookup should devirtualize back to the underlying raw graph")
	}
	if rawView.refsCount() != 1 || rawView.ref(0).IsVirtualized() {
		t.Fatal("raw cell lookup should preserve the underlying raw graph")
	}

	serialized := body.ToBOC()
	decoded, err := FromBOC(serialized)
	if err != nil {
		t.Fatalf("serialize virtualized body: %v", err)
	}
	if decoded.IsVirtualized() {
		t.Fatal("serialized form should round-trip to an ordinary cell graph")
	}
	if decoded.HashKey() != unwrappedRaw.HashKey() {
		t.Fatal("serializing a virtualized body should devirtualize back to the underlying raw graph")
	}
}

func TestVirtualizedHashesDepthAndLevelsClampLikeCppVirtualCell(t *testing.T) {
	root, body, pruned := buildVirtualizedProofBody(t)

	proof, err := root.CreateProof(CreateProofSkeleton())
	if err != nil {
		t.Fatalf("recreate proof: %v", err)
	}
	rawBody, err := UnwrapProof(proof, root.Hash())
	if err != nil {
		t.Fatalf("unwrap raw proof: %v", err)
	}
	rawPruned, err := rawBody.PeekRef(0)
	if err != nil {
		t.Fatalf("peek raw child: %v", err)
	}

	tests := []struct {
		name string
		raw  *Cell
		virt *Cell
	}{
		{name: "body", raw: rawBody, virt: body},
		{name: "pruned", raw: rawPruned, virt: pruned},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantMask := tt.raw.LevelMask().Apply(tt.virt.EffectiveLevel())
			if got := tt.virt.LevelMask(); got != wantMask {
				t.Fatalf("unexpected level mask: got=%08b want=%08b", got.Mask, wantMask.Mask)
			}
			if got := tt.virt.Level(); got != wantMask.GetLevel() {
				t.Fatalf("unexpected level: got=%d want=%d", got, wantMask.GetLevel())
			}
			if got := tt.virt.ActualLevel(); got != tt.raw.Level() {
				t.Fatalf("unexpected actual level: got=%d want=%d", got, tt.raw.Level())
			}

			effective := tt.virt.EffectiveLevel()
			if got := tt.virt.HashKey(); got != tt.raw.HashKey(effective) {
				t.Fatalf("unexpected default hash at effective level %d", effective)
			}
			if got := tt.virt.Depth(); got != tt.raw.Depth(effective) {
				t.Fatalf("unexpected default depth at effective level %d: got=%d want=%d", effective, got, tt.raw.Depth(effective))
			}

			for level := 0; level <= _DataCellMaxLevel; level++ {
				wantLevel := min(level, effective)
				if got := tt.virt.HashKey(level); got != tt.raw.HashKey(wantLevel) {
					t.Fatalf("unexpected hash at level %d (clamped to %d)", level, wantLevel)
				}
				if got := tt.virt.Depth(level); got != tt.raw.Depth(wantLevel) {
					t.Fatalf("unexpected depth at level %d (clamped to %d): got=%d want=%d", level, wantLevel, got, tt.raw.Depth(wantLevel))
				}
			}
		})
	}
}

func TestVirtualizedSlicesExposeVisibleRefsAndDepth(t *testing.T) {
	_, body, pruned := buildVirtualizedProofBody(t)

	sl := body.BeginParse()
	if got, want := sl.Depth(), uint16(pruned.Depth()+1); got != want {
		t.Fatalf("unexpected slice depth: got=%d want=%d", got, want)
	}

	peeked, err := sl.PeekRefCell()
	if err != nil {
		t.Fatalf("peek ref: %v", err)
	}
	if !peeked.IsVirtualized() {
		t.Fatal("peeked ref should stay virtualized")
	}
	if peeked.HashKey() != pruned.HashKey() || peeked.LevelMask() != pruned.LevelMask() {
		t.Fatal("peeked ref should match visible virtualized child")
	}

	at0, err := sl.PeekRefCellAt(0)
	if err != nil {
		t.Fatalf("peek ref at 0: %v", err)
	}
	if !at0.IsVirtualized() {
		t.Fatal("indexed peeked ref should stay virtualized")
	}
	if at0.HashKey() != pruned.HashKey() || at0.LevelMask() != pruned.LevelMask() {
		t.Fatal("indexed peeked ref should match visible virtualized child")
	}

	preloaded, err := sl.PreloadRefCell()
	if err != nil {
		t.Fatalf("preload ref: %v", err)
	}
	if !preloaded.IsVirtualized() {
		t.Fatal("preloaded ref should stay virtualized")
	}

	loaded, err := sl.LoadRefCell()
	if err != nil {
		t.Fatalf("load ref: %v", err)
	}
	if !loaded.IsVirtualized() {
		t.Fatal("loaded ref should stay virtualized")
	}
	if sl.RefsNum() != 0 {
		t.Fatalf("expected slice refs to advance after load, got %d", sl.RefsNum())
	}
}
