package cell

import (
	"bytes"
	"errors"
	"testing"
)

// These tests replay the production incident where a validator mutates an
// account storage-stat dictionary bound from another producer's Merkle proof.
// The producer's proof retains exactly the cells its own update walk loaded;
// a delete executed here in a different order can then need a sibling cell
// the producer never touched — present only as a pruned branch. On testnet
// the fabricated node surfaced three to four forks deep on the next Set's
// path as "failed to dive into N ref of branch: ... invalid dictionary fork
// node"; the minimal shape below puts it at the root, the mechanism is the
// same.
//
// The delete descent is guarded (a pruned node on the path reports
// ErrDictHasSpecialCells), and the surviving-sibling merge must hold the same
// line: parsing a pruned branch as label+payload silently fabricates a
// garbage node, which either corrupts the committed root hash (a wrong
// REJECT vote downstream) or explodes on the next Set.

func prunedSiblingStatValue(refCount uint64) *Builder {
	return BeginCell().MustStoreUInt(refCount, 32).MustStoreUInt(1, 2)
}

// prunedSiblingKey returns a 256-bit key whose two leading bits are set by
// prefix, giving the dictionary a fixed shape: key 0b00 forms a lone leaf
// under the root fork, keys 0b10 and 0b11 form a two-leaf subtree beside it.
func prunedSiblingKey(prefix byte) []byte {
	key := make([]byte, 32)
	key[0] = prefix << 6
	key[31] = prefix // keep the keys distinct beyond the fork bits
	return key
}

func buildPrunedSiblingDict(t *testing.T) (*Cell, [3][]byte) {
	t.Helper()
	keys := [3][]byte{
		prunedSiblingKey(0b00),
		prunedSiblingKey(0b10),
		prunedSiblingKey(0b11),
	}
	dict := NewDict(256)
	for i, key := range keys {
		if err := dict.SetBuilderByBytesKey(key, prunedSiblingStatValue(uint64(i+1))); err != nil {
			t.Fatalf("set key %d: %v", i, err)
		}
	}
	return dict.AsCell(), keys
}

// virtualizedDictProof builds a Merkle proof of root retaining exactly the
// cells a walk over walkKeys loads, and returns the virtualized dictionary
// view — the same shape bindInitialStorageStat consumes from collated data.
func virtualizedDictProof(t *testing.T, root *Cell, walkKeys ...[]byte) *Dictionary {
	t.Helper()
	builder := NewMerkleProofBuilder(root)
	walked := builder.Root().AsDict(256)
	for _, key := range walkKeys {
		if _, err := walked.LoadValueByBytesKey(key); err != nil {
			t.Fatalf("walk key %x: %v", key, err)
		}
	}
	proof, err := builder.CreateProof()
	if err != nil {
		t.Fatalf("create proof: %v", err)
	}
	virtual, err := UnwrapProofVirtualized(proof, root.Hash())
	if err != nil {
		t.Fatalf("virtualize proof: %v", err)
	}
	return virtual.AsDict(256)
}

// TestDictDeletePrunedSiblingMergeIsRejected pins the fix: deleting a retained
// leaf whose merge sibling is a pruned subtree must surface the classified
// special-cells error, never fabricate a node from pruned-branch bytes.
//
// Before the guard this delete returned nil: the merge parsed the pruned
// branch as an empty label plus payload, committed a root whose hash differs
// from the true post-delete dictionary, and the next Set on the surviving
// half died with ErrInvalidDictForkNode — the testnet abstain.
func TestDictDeletePrunedSiblingMergeIsRejected(t *testing.T) {
	root, keys := buildPrunedSiblingDict(t)

	// The walk loads only the lone leaf's path; the sibling subtree holding
	// keys[1] and keys[2] stays pruned.
	dict := virtualizedDictProof(t, root, keys[0])

	// The retained leaf reads back, and a pruned path is already classified.
	if _, err := dict.LoadValueByBytesKey(keys[0]); err != nil {
		t.Fatalf("load retained key: %v", err)
	}
	if _, err := dict.LoadValueByBytesKey(keys[1]); !errors.Is(err, ErrDictHasSpecialCells) {
		t.Fatalf("load across pruned path: got %v, want ErrDictHasSpecialCells", err)
	}

	preDelete := dict.AsCell().Hash()

	err := dict.DeleteByBytesKey(keys[0])
	if !errors.Is(err, ErrDictHasSpecialCells) {
		// Document the corruption the guard prevents before failing.
		truth := root.AsDict(256)
		if truthErr := truth.DeleteByBytesKey(keys[0]); truthErr != nil {
			t.Fatalf("ground-truth delete: %v", truthErr)
		}
		if err == nil && !bytes.Equal(dict.AsCell().Hash(), truth.AsCell().Hash()) {
			setErr := dict.SetBuilderByBytesKey(keys[1], prunedSiblingStatValue(9))
			t.Fatalf("delete over pruned sibling merged garbage silently: root %x, want %x; follow-up set: %v",
				dict.AsCell().Hash(), truth.AsCell().Hash(), setErr)
		}
		t.Fatalf("delete over pruned sibling: got %v, want ErrDictHasSpecialCells", err)
	}

	// The failed delete must not have moved the root.
	if !bytes.Equal(dict.AsCell().Hash(), preDelete) {
		t.Fatalf("failed delete moved the root: %x -> %x", preDelete, dict.AsCell().Hash())
	}
}

// TestDictDeleteRetainedSiblingMergeSucceeds is the control arm: when the
// proof covers the sibling subtree's root (the producer's walk loaded it),
// the merge must keep working and reproduce the true post-delete root hash —
// even though the subtree's own leaves stay pruned, the merge only needs the
// sibling node itself.
func TestDictDeleteRetainedSiblingMergeSucceeds(t *testing.T) {
	root, keys := buildPrunedSiblingDict(t)

	// Walking keys[1] loads the sibling subtree's fork node (and one of its
	// leaves); keys[2]'s leaf stays pruned behind it.
	dict := virtualizedDictProof(t, root, keys[0], keys[1])
	if err := dict.DeleteByBytesKey(keys[0]); err != nil {
		t.Fatalf("delete with retained sibling: %v", err)
	}

	truth := root.AsDict(256)
	if err := truth.DeleteByBytesKey(keys[0]); err != nil {
		t.Fatalf("ground-truth delete: %v", err)
	}
	if !bytes.Equal(dict.AsCell().Hash(), truth.AsCell().Hash()) {
		t.Fatalf("post-delete root diverged from ground truth: %x, want %x",
			dict.AsCell().Hash(), truth.AsCell().Hash())
	}

	if value, err := dict.LoadValueByBytesKey(keys[1]); err != nil {
		t.Fatalf("surviving sibling unreadable after merge: %v", err)
	} else if got := value.MustLoadUInt(32); got != 2 {
		t.Fatalf("surviving sibling value: got %d, want 2", got)
	}
}
