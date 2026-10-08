package cell

import (
	"bytes"
	"testing"
)

var merkleUpdateCapacitySink *Cell

func TestPreparedMerkleUpdateClassicCapacityHints(t *testing.T) {
	tc := newMerkleUpdateLargeDictCase(t, 1024, 20, 2026082502)
	tracedUpdate := NewReadSet(tc.update).Root()
	prepared, err := PrepareMerkleUpdatePlanned(tracedUpdate)
	if err != nil {
		t.Fatalf("prepare traced update: %v", err)
	}
	if prepared.Planned() {
		t.Fatal("traced update unexpectedly took the replay-plan path")
	}
	if prepared.sourceHints.known <= 32 || prepared.sourceHints.seen <= 32 {
		t.Fatalf("fixture did not outgrow the classic defaults: hints=%+v", prepared.sourceHints)
	}

	index := newMerkleUpdateSourceIndex(prepared.sourceHints)
	if err = index.walkProof(tc.from, prepared.updateFrom, 0); err != nil {
		t.Fatalf("walk source with verdict hints: %v", err)
	}
	if got := len(index.known.entries); got != prepared.sourceHints.known {
		t.Fatalf("known hint is not exact: got %d entries, hint %d", got, prepared.sourceHints.known)
	}
	if got := len(index.seen.entries); got != prepared.sourceHints.seen {
		t.Fatalf("seen hint is not exact: got %d entries, hint %d", got, prepared.sourceHints.seen)
	}

	classic, err := ApplyMerkleUpdate(tc.from, tracedUpdate)
	if err != nil {
		t.Fatalf("classic apply: %v", err)
	}
	hinted, err := prepared.ApplyTo(tc.from)
	if err != nil {
		t.Fatalf("prepared classic apply: %v", err)
	}
	if classic.HashKey() != hinted.HashKey() || !bytes.Equal(classic.ToBOC(), hinted.ToBOC()) {
		t.Fatal("capacity hints changed the applied Merkle update")
	}

	defaults := merkleUpdateSourceIndexHints{known: 32, seen: 32}
	var applyErr error
	defaultAllocs := testing.AllocsPerRun(5, func() {
		merkleUpdateCapacitySink, applyErr = applyMerkleUpdateWithSourceIndex(
			tc.from, prepared.updateFrom, prepared.updateTo, defaults,
		)
	})
	if applyErr != nil {
		t.Fatalf("default-capacity apply: %v", applyErr)
	}
	hintedAllocs := testing.AllocsPerRun(5, func() {
		merkleUpdateCapacitySink, applyErr = applyMerkleUpdateWithSourceIndex(
			tc.from, prepared.updateFrom, prepared.updateTo, prepared.sourceHints,
		)
	})
	if applyErr != nil {
		t.Fatalf("hinted-capacity apply: %v", applyErr)
	}
	if hintedAllocs >= defaultAllocs {
		t.Fatalf("capacity hints did not reduce allocations: default %.0f, hinted %.0f", defaultAllocs, hintedAllocs)
	}
	t.Logf("classic source index allocations: default %.0f, hinted %.0f", defaultAllocs, hintedAllocs)
}

func BenchmarkMerkleUpdateClassicSourceIndexCapacityHints(b *testing.B) {
	tc := newMerkleUpdateLargeDictCase(b, 2048, 20, 2026082503)
	prepared, err := PrepareMerkleUpdatePlanned(NewReadSet(tc.update).Root())
	if err != nil {
		b.Fatalf("prepare traced update: %v", err)
	}
	if prepared.Planned() {
		b.Fatal("traced update unexpectedly took the replay-plan path")
	}

	bench := func(b *testing.B, hints merkleUpdateSourceIndexHints) {
		b.Helper()
		b.ReportAllocs()
		for b.Loop() {
			merkleUpdateCapacitySink, err = applyMerkleUpdateWithSourceIndex(
				tc.from, prepared.updateFrom, prepared.updateTo, hints,
			)
			if err != nil {
				b.Fatal(err)
			}
		}
	}

	b.Run("default_32", func(b *testing.B) {
		bench(b, merkleUpdateSourceIndexHints{known: 32, seen: 32})
	})
	b.Run("verdict_hints", func(b *testing.B) {
		bench(b, prepared.sourceHints)
	})
}
