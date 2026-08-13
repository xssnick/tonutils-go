package cell

import (
	"bytes"
	"testing"
)

func TestCreatePrunedBranchSparseLevelMask(t *testing.T) {
	branch := BeginCell().MustStoreUInt(0xA5, 8).
		MustStoreRef(BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()).
		EndCell()
	maskTwo, err := CreatePrunedBranch(branch, 2, 3)
	if err != nil {
		t.Fatalf("create mask-010 branch: %v", err)
	}
	source := BeginCell().MustStoreRef(maskTwo).EndCell()
	pruned, err := CreatePrunedBranch(source, 3, 3)
	if err != nil {
		t.Fatalf("create mask-110 branch: %v", err)
	}
	if got := pruned.LevelMask().Mask; got != 0b110 {
		t.Fatalf("level mask = %03b, want 110", got)
	}

	for _, level := range []int{0, 2} {
		if got, want := pruned.Hash(level), source.Hash(level); !bytes.Equal(got, want) {
			t.Fatalf("level-%d stored hash mismatch", level)
		}
		if got, want := pruned.Depth(level), source.Depth(level); got != want {
			t.Fatalf("level-%d stored depth = %d, want %d", level, got, want)
		}
	}

	wrapped := pruned
	wrappers := 0
	for wrapped.Level() > 0 {
		wrapped, err = CreateMerkleProof(wrapped)
		if err != nil {
			t.Fatalf("wrap mask-110 branch: %v", err)
		}
		wrappers++
	}
	boc, err := wrapped.ToBOCWithOptionsErr(BOCSerializeOptions{WithCRC32C: true})
	if err != nil {
		t.Fatalf("serialize wrapped mask-110 branch: %v", err)
	}
	for _, tc := range []struct {
		name string
		opts BOCParseOptions
	}{
		{name: "eager"},
		{name: "lazy", opts: BOCParseOptions{Lazy: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decoded, err := FromBOCWithOptions(boc, tc.opts)
			if err != nil {
				t.Fatalf("parse wrapped mask-110 branch: %v", err)
			}
			for range wrappers {
				decoded, err = decoded.load()
				if err != nil {
					t.Fatalf("materialize decoded Merkle wrapper: %v", err)
				}
				decoded, err = decoded.PeekRef(0)
				if err != nil {
					t.Fatalf("unwrap decoded mask-110 branch: %v", err)
				}
			}
			decoded, err = decoded.load()
			if err != nil {
				t.Fatalf("materialize decoded mask-110 branch: %v", err)
			}
			if got := decoded.LevelMask().Mask; got != 0b110 {
				t.Fatalf("decoded level mask = %03b, want 110", got)
			}
		})
	}
}
