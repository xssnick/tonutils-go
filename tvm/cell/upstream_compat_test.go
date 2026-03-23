package cell

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"math/rand"
	"testing"
)

func bocOptionsFromMode(mode int) BOCOptions {
	return BOCOptions{
		WithIndex:     mode&1 != 0,
		WithCRC32C:    mode&2 != 0,
		WithTopHash:   mode&4 != 0,
		WithIntHashes: mode&8 != 0,
		WithCacheBits: mode&16 != 0,
	}
}

func validBOCModes() []int {
	var modes []int
	for mode := 0; mode < 32; mode++ {
		if mode&16 != 0 && mode&1 == 0 {
			continue
		}
		modes = append(modes, mode)
	}
	return modes
}

func buildUpstreamCompatRoots(tb testing.TB) []*Cell {
	tb.Helper()

	leaf := BeginCell().MustStoreUInt(0xABCD, 16).EndCell()
	shared := BeginCell().MustStoreUInt(0x42, 8).MustStoreRef(leaf).EndCell()
	ordinaryRoot := BeginCell().MustStoreUInt(0x99, 8).MustStoreRef(shared).MustStoreRef(leaf).EndCell()

	source := BeginCell().MustStoreSlice([]byte("abcd"), 32).MustStoreRef(shared).EndCell()
	pruned, err := createPrunedBranchFromCell(source, _DataCellMaxLevel)
	if err != nil {
		tb.Fatalf("failed to create pruned branch: %v", err)
	}

	specialRoot := BeginCell().MustStoreUInt(0b101, 3).MustStoreRef(pruned).EndCell()
	return []*Cell{ordinaryRoot, specialRoot}
}

func requireSameRootHashes(tb testing.TB, got, want []*Cell) {
	tb.Helper()

	if len(got) != len(want) {
		tb.Fatalf("unexpected roots count, got %d want %d", len(got), len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i].Hash(), want[i].Hash()) {
			tb.Fatalf("root %d hash mismatch, got %x want %x", i, got[i].Hash(), want[i].Hash())
		}
	}
}

// Upstream TonDb.BocFuzz also contains one long payload that C++ std_boc_deserialize
// currently accepts, but it does not satisfy the generic BOC header invariants from
// boc.cpp::Info::parse_serialized_header. We keep the malformed generic cases here and
// intentionally skip legacy/ambiguous compatibility vectors.
func TestBOCUpstreamMalformedFuzzCases(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		wantErr bool
	}{
		{
			name:    "invalid-small-boc",
			payload: "te6ccgEBAQEAAgAoAAA=",
			wantErr: true,
		},
		{
			name:    "invalid-magic",
			payload: "SEkh/w==",
			wantErr: true,
		},
		{
			name:    "invalid-header",
			payload: "te6ccqwBMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMAKCEAAAAgAQ==",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := base64.StdEncoding.DecodeString(tc.payload)
			if err != nil {
				t.Fatalf("failed to decode test payload: %v", err)
			}

			roots, err := FromBOCMultiRoot(data)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected malformed BOC to fail")
				}
				return
			}
			if err != nil {
				t.Fatalf("expected BOC to parse, got error: %v", err)
			}
			if len(roots) == 0 {
				t.Fatal("expected at least one root")
			}
		})
	}
}

func buildRandomOrdinaryRoots(tb testing.TB, rnd *rand.Rand, rootsCount, size int) []*Cell {
	tb.Helper()

	if rootsCount < 1 {
		tb.Fatal("roots count must be positive")
	}
	if size < rootsCount {
		tb.Fatal("graph size must be at least roots count")
	}

	nodes := make([]*Cell, 0, size)
	for len(nodes) < size {
		builder := BeginCell()

		bits := rnd.Intn(257)
		if bits > 0 {
			data := make([]byte, (bits+7)/8)
			if _, err := rnd.Read(data); err != nil {
				tb.Fatalf("failed to read random data: %v", err)
			}
			if err := builder.StoreSlice(data, uint(bits)); err != nil {
				tb.Fatalf("failed to store random bits: %v", err)
			}
		}

		refsLimit := len(nodes)
		if refsLimit > 4 {
			refsLimit = 4
		}
		refsNum := 0
		if refsLimit > 0 {
			refsNum = rnd.Intn(refsLimit + 1)
			for _, idx := range rnd.Perm(len(nodes))[:refsNum] {
				if err := builder.StoreRef(nodes[idx]); err != nil {
					tb.Fatalf("failed to store random ref: %v", err)
				}
			}
		}

		nodes = append(nodes, builder.EndCell())
	}

	roots := make([]*Cell, rootsCount)
	for i, idx := range rnd.Perm(len(nodes))[:rootsCount] {
		roots[i] = nodes[idx]
	}
	return roots
}

func TestBOCUpstreamRejectsTruncatedPrefixes(t *testing.T) {
	root := buildUpstreamCompatRoots(t)[1]
	boc := root.ToBOCWithOptions(mode31Options())
	if len(boc) == 0 {
		t.Fatal("expected non-empty boc")
	}

	for i := 0; i < len(boc); i++ {
		if _, err := FromBOC(boc[:i]); err == nil {
			t.Fatalf("expected prefix of length %d to fail parsing", i)
		}
	}

	if _, err := FromBOC(boc); err != nil {
		t.Fatalf("full boc should parse: %v", err)
	}
}

func TestBOCUpstreamRoundTripAllValidModes(t *testing.T) {
	roots := buildUpstreamCompatRoots(t)

	for _, mode := range validBOCModes() {
		t.Run(fmt.Sprintf("mode_%d", mode), func(t *testing.T) {
			opts := bocOptionsFromMode(mode)
			boc := ToBOCWithOptions(roots, opts)
			if len(boc) == 0 {
				t.Fatal("expected non-empty boc")
			}

			parsedRoots, err := FromBOCMultiRoot(boc)
			if err != nil {
				t.Fatalf("failed to parse serialized boc: %v", err)
			}
			requireSameRootHashes(t, parsedRoots, roots)

			reboc := ToBOCWithOptions(parsedRoots, opts)
			if !bytes.Equal(boc, reboc) {
				t.Fatalf("mode=%d roundtrip serialization mismatch", mode)
			}
		})
	}
}

func TestBOCUpstreamRandomSingleRootRoundTrip(t *testing.T) {
	rnd := rand.New(rand.NewSource(123))
	modes := validBOCModes()

	for i := 0; i < 64; i++ {
		mode := modes[rnd.Intn(len(modes))]
		root := buildRandomOrdinaryRoots(t, rnd, 1, rnd.Intn(40)+1)[0]
		boc := root.ToBOCWithOptions(bocOptionsFromMode(mode))

		parsed, err := FromBOC(boc)
		if err != nil {
			t.Fatalf("iteration %d mode=%d failed to parse serialized boc: %v", i, mode, err)
		}
		if !bytes.Equal(parsed.Hash(), root.Hash()) {
			t.Fatalf("iteration %d mode=%d root hash mismatch", i, mode)
		}

		reboc := parsed.ToBOCWithOptions(bocOptionsFromMode(mode))
		if !bytes.Equal(boc, reboc) {
			t.Fatalf("iteration %d mode=%d roundtrip serialization mismatch", i, mode)
		}
	}
}

func TestBOCUpstreamRandomMultiRootRoundTrip(t *testing.T) {
	rnd := rand.New(rand.NewSource(321))
	modes := validBOCModes()

	for i := 0; i < 32; i++ {
		rootsCount := rnd.Intn(6) + 1
		roots := buildRandomOrdinaryRoots(t, rnd, rootsCount, rootsCount+rnd.Intn(40))
		mode := modes[rnd.Intn(len(modes))]
		boc := ToBOCWithOptions(roots, bocOptionsFromMode(mode))

		parsedRoots, err := FromBOCMultiRoot(boc)
		if err != nil {
			t.Fatalf("iteration %d mode=%d failed to parse serialized boc: %v", i, mode, err)
		}
		requireSameRootHashes(t, parsedRoots, roots)

		reboc := ToBOCWithOptions(parsedRoots, bocOptionsFromMode(mode))
		if !bytes.Equal(boc, reboc) {
			t.Fatalf("iteration %d mode=%d multi-root roundtrip serialization mismatch", i, mode)
		}
	}
}

func TestBOCUpstreamDuplicateRootsRoundTrip(t *testing.T) {
	root := buildUpstreamCompatRoots(t)[1]
	roots := []*Cell{root, root}
	boc := ToBOCWithOptions(roots, mode31Options())
	if len(boc) == 0 {
		t.Fatal("expected non-empty boc")
	}

	parsedRoots, err := FromBOCMultiRoot(boc)
	if err != nil {
		t.Fatalf("failed to parse duplicate-roots boc: %v", err)
	}
	requireSameRootHashes(t, parsedRoots, roots)

	reboc := ToBOCWithOptions(parsedRoots, mode31Options())
	if !bytes.Equal(boc, reboc) {
		t.Fatal("duplicate-roots boc roundtrip mismatch")
	}
}
