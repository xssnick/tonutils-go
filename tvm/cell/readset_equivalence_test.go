package cell

import (
	"bytes"
	"fmt"
	"testing"
)

// viewEquivalenceDict builds the state-shaped fixture every test below reads: a
// tag, a dictionary of keys entries whose values carry a reference of their own,
// and a tail subtree no read ever reaches. It is round-tripped through a BOC so
// reads go through freshly deserialized cells rather than the builder's.
func viewEquivalenceDict(tb testing.TB, keys int) *Cell {
	tb.Helper()

	dict := NewDict(32)
	for i := 0; i < keys; i++ {
		value := BeginCell().
			MustStoreUInt(uint64(i)+0xfeed, 64).
			MustStoreRef(BeginCell().MustStoreUInt(uint64(i), 32).EndCell()).
			EndCell()
		if err := dict.Set(BeginCell().MustStoreUInt(uint64(i), 32).EndCell(), value); err != nil {
			tb.Fatalf("seed key %d: %v", i, err)
		}
	}
	tail := BeginCell().
		MustStoreUInt(0x7A11, 32).
		MustStoreRef(BeginCell().MustStoreUInt(0x99, 8).EndCell()).
		EndCell()
	root := BeginCell().
		MustStoreUInt(0x57415645, 32).
		MustStoreRef(dict.AsCell()).
		MustStoreRef(tail).
		EndCell()

	reloaded, err := FromBOC(root.ToBOC())
	if err != nil {
		tb.Fatalf("reload source dict: %v", err)
	}
	return reloaded
}

// viewEquivalenceReadKeys is one read pass over that fixture: open the root, look
// up each key and open the value's own reference, which is what makes a value a
// subtree the proof has to decide about rather than a handful of bits.
func viewEquivalenceReadKeys(tb testing.TB, root *Cell, keySz uint, keys []uint64) {
	tb.Helper()

	slice, err := root.BeginParse()
	if err != nil {
		tb.Fatalf("parse root: %v", err)
	}
	if _, err = slice.LoadUInt(32); err != nil {
		tb.Fatalf("load tag: %v", err)
	}
	dictRoot, err := slice.LoadRefCell()
	if err != nil {
		tb.Fatalf("load dict: %v", err)
	}

	dict := dictRoot.AsDict(keySz)
	for _, key := range keys {
		value, err := dict.LoadValue(BeginCell().MustStoreUInt(key, keySz).EndCell())
		if err != nil {
			tb.Fatalf("read %d: %v", key, err)
		}
		if value.RefsNum() == 0 {
			continue
		}
		ref, err := value.LoadRefCell()
		if err != nil {
			tb.Fatalf("read %d ref: %v", key, err)
		}
		if _, err = ref.BeginParse(); err != nil {
			tb.Fatalf("parse %d ref: %v", key, err)
		}
	}
}

// TestReadSetProofMatchesHashUsageProofBytes pins that the proof contains exactly
// the cells that were read and nothing else. CreateHashUsageProof serializes the
// same selection from a predicate the caller supplies instead of reading it off
// the recorder, so it is the one production path that can answer the question
// independently — and the answer is consensus-visible, hence bytes, not shapes.
func TestReadSetProofMatchesHashUsageProofBytes(t *testing.T) {
	for _, keys := range []int{1, 4, 64, 512} {
		t.Run(fmt.Sprintf("keys=%d", keys), func(t *testing.T) {
			root := viewEquivalenceDict(t, keys)
			readKeys := []uint64{0}
			for i := 1; i < keys; i += max(1, keys/5) {
				readKeys = append(readKeys, uint64(i))
			}

			rs := NewReadSet(root)
			viewEquivalenceReadKeys(t, rs.Root(), 32, readKeys)
			got, err := rs.Proof()
			if err != nil {
				t.Fatalf("create read set proof: %v", err)
			}

			want, err := root.CreateHashUsageProof(func(hash Hash) bool {
				_, recorded := rs.Contains(hash)
				return recorded
			})
			if err != nil {
				t.Fatalf("create hash usage proof: %v", err)
			}

			if got.HashKey() != want.HashKey() {
				t.Fatalf("proof root hash differs:\n set  %x\n hash %x", got.HashKey(), want.HashKey())
			}
			if !bytes.Equal(ToBOCWithFlags([]*Cell{got}, false), ToBOCWithFlags([]*Cell{want}, false)) {
				t.Fatal("proof bytes differ")
			}
		})
	}
}

// TestReadSetMerkleUpdateOnDictionaryApplies covers the collator's actual shape:
// read some keys, write some, emit the update. Most of the dictionary is never
// read, so the update's value depends entirely on where pruning stops — and the
// update is consensus-visible, so a different but valid pruning choice is a
// broken block, not a slower one. What holds it in place is that it must apply to
// the untouched source and rebuild the destination the reader assembled.
func TestReadSetMerkleUpdateOnDictionaryApplies(t *testing.T) {
	for _, keys := range []int{16, 128, 512} {
		t.Run(fmt.Sprintf("keys=%d", keys), func(t *testing.T) {
			source := viewEquivalenceDict(t, keys)
			reads := []uint64{0, 2, uint64(keys / 3)}
			writes := []uint64{1, uint64(keys / 2), uint64(keys - 1)}

			rs := NewReadSet(source)
			rsTo, err := readSetDictTransition(rs.Root(), reads, writes)
			if err != nil {
				t.Fatalf("build destination through the read set: %v", err)
			}
			sizeBefore := rs.Size()
			rsUpdate, rsApplied, err := rs.CreateMerkleUpdateApplied(rsTo)
			if err != nil {
				t.Fatalf("read set merkle update: %v", err)
			}
			if rs.Size() != sizeBefore {
				t.Fatalf("building the update widened the record from %d to %d", sizeBefore, rs.Size())
			}

			if err = ValidateMerkleUpdate(rsUpdate); err != nil {
				t.Fatalf("validate: %v", err)
			}
			applied, err := ApplyMerkleUpdate(source, rsUpdate)
			if err != nil {
				t.Fatalf("apply to the real source: %v", err)
			}
			if applied.HashKey() != rsTo.HashKey() {
				t.Fatal("applying the update rebuilt a different destination")
			}
			if rsApplied.HashKey() != applied.HashKey() {
				t.Fatal("the applied root returned with the update differs from applying it")
			}
			if shared := countSharedCells(rsApplied, source); shared == 0 {
				t.Fatal("the applied root shares no cells with the source state")
			}
		})
	}
}

// readSetDictTransition is one collation-shaped pass: read keys, write keys, and
// reassemble the state-shaped root around the new dictionary.
func readSetDictTransition(root *Cell, reads, writes []uint64) (*Cell, error) {
	slice, err := root.BeginParse()
	if err != nil {
		return nil, err
	}
	tag, err := slice.LoadUInt(32)
	if err != nil {
		return nil, err
	}
	dictRoot, err := slice.LoadRefCell()
	if err != nil {
		return nil, err
	}
	tail, err := slice.LoadRefCell()
	if err != nil {
		return nil, err
	}
	dict := dictRoot.AsDict(32)
	for _, key := range reads {
		if _, err = dict.LoadValue(BeginCell().MustStoreUInt(key, 32).EndCell()); err != nil {
			return nil, fmt.Errorf("read %d: %w", key, err)
		}
	}
	for _, key := range writes {
		value := BeginCell().
			MustStoreUInt(key+0xfeed, 64).
			MustStoreRef(BeginCell().MustStoreUInt(key, 32).EndCell()).
			EndCell()
		if err = dict.Set(BeginCell().MustStoreUInt(key, 32).EndCell(), value); err != nil {
			return nil, fmt.Errorf("write %d: %w", key, err)
		}
	}
	nextDict, err := dict.ToCell()
	if err != nil {
		return nil, err
	}
	return BeginCell().
		MustStoreUInt(tag, 32).
		MustStoreRef(nextDict).
		MustStoreRef(tail).
		EndCell(), nil
}

// TestReadSetIgnoreReadsKeepsBoundary pins the scope dictionary readers need: they
// parse a root to validate its shape, and a proof taken afterwards must still keep
// that root pruned.
func TestReadSetIgnoreReadsKeepsBoundary(t *testing.T) {
	inner := BeginCell().MustStoreUInt(0x55, 8).EndCell()
	validated := BeginCell().MustStoreUInt(0x1234, 16).MustStoreRef(inner).EndCell()
	root := BeginCell().MustStoreUInt(1, 1).MustStoreRef(validated).EndCell()

	rs := NewReadSet(root)
	slice, err := rs.Root().BeginParse()
	if err != nil {
		t.Fatalf("parse root: %v", err)
	}
	ref, err := slice.PeekRefCellAt(0)
	if err != nil {
		t.Fatalf("peek ref: %v", err)
	}

	rs.IgnoreReads(true)
	rs.IgnoreReads(true)
	if _, err = ref.BeginParse(); err != nil {
		t.Fatalf("validate ref: %v", err)
	}
	rs.IgnoreReads(false)
	if _, ok := rs.Contains(validated.HashKey()); ok {
		t.Fatal("a read inside the outer ignore scope was recorded")
	}
	rs.IgnoreReads(false)

	proof, err := rs.Proof()
	if err != nil {
		t.Fatalf("proof: %v", err)
	}
	body, err := UnwrapProof(proof, root.Hash())
	if err != nil {
		t.Fatalf("unwrap: %v", err)
	}
	boundary, err := body.PeekRef(0)
	if err != nil {
		t.Fatalf("peek boundary: %v", err)
	}
	if boundary.GetType() != PrunedCellType {
		t.Fatalf("ignored read should have stayed a boundary, got %v", boundary.GetType())
	}

	if _, err = ref.BeginParse(); err != nil {
		t.Fatalf("re-read ref: %v", err)
	}
	if _, ok := rs.Contains(validated.HashKey()); !ok {
		t.Fatal("a read outside the ignore scope was not recorded")
	}
}

// TestReadSetKeepsUnloadedOrdinaryLeafRef pins the C++ rule the proof builders
// already implement: an unread childless reference stays ordinary, because a
// boundary would be larger than the cell it hides.
func TestReadSetKeepsUnloadedOrdinaryLeafRef(t *testing.T) {
	child := BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	root := BeginCell().MustStoreUInt(0, 1).MustStoreRef(child).EndCell()

	rs := NewReadSet(root)
	if _, err := rs.Root().BeginParse(); err != nil {
		t.Fatalf("parse root: %v", err)
	}
	proof, err := rs.Proof()
	if err != nil {
		t.Fatalf("proof: %v", err)
	}
	body, err := UnwrapProof(proof, root.Hash())
	if err != nil {
		t.Fatalf("unwrap: %v", err)
	}
	kept, err := body.PeekRef(0)
	if err != nil {
		t.Fatalf("peek leaf: %v", err)
	}
	if kept.IsSpecial() {
		t.Fatalf("unread ordinary leaf should stay ordinary like C++, got %v", kept.GetType())
	}
	if !bytes.Equal(kept.Hash(0), child.Hash()) {
		t.Fatal("kept leaf lost its hash")
	}
}

// TestReadSetConcurrentRecording pins that lanes running over one set agree on the
// record. Collation and replay walk one predecessor from GOMAXPROCS goroutines.
func TestReadSetConcurrentRecording(t *testing.T) {
	source := viewEquivalenceDict(t, 256)

	sequential := NewReadSet(source)
	for w := 0; w < 8; w++ {
		keys := make([]uint64, 0, 8)
		for i := 0; i < 8; i++ {
			keys = append(keys, uint64((w*8+i)%256))
		}
		viewEquivalenceReadKeys(t, sequential.Root(), 32, keys)
	}
	want, err := sequential.Proof()
	if err != nil {
		t.Fatalf("sequential proof: %v", err)
	}

	concurrent := NewReadSet(source)
	done := make(chan struct{}, 8)
	for w := 0; w < 8; w++ {
		go func(seed int) {
			defer func() { done <- struct{}{} }()
			keys := make([]uint64, 0, 8)
			for i := 0; i < 8; i++ {
				keys = append(keys, uint64((seed*8+i)%256))
			}
			viewEquivalenceReadKeys(t, concurrent.Root(), 32, keys)
		}(w)
	}
	for w := 0; w < 8; w++ {
		<-done
	}
	got, err := concurrent.Proof()
	if err != nil {
		t.Fatalf("concurrent proof: %v", err)
	}

	if concurrent.Size() != sequential.Size() {
		t.Fatalf("record size differs: concurrent %d, sequential %d", concurrent.Size(), sequential.Size())
	}
	if !bytes.Equal(ToBOCWithFlags([]*Cell{got}, false), ToBOCWithFlags([]*Cell{want}, false)) {
		t.Fatal("concurrent reads produced a different proof than the same reads run sequentially")
	}
}

// BenchmarkReadSetDictProof is the read pass plus proof serialization, which is
// what a proof-serving call site pays. The read pass alone and the read pass plus
// state update are covered by BenchmarkReadSetRecordedWalk and
// BenchmarkCreateMerkleUpdateRecordedDictRewrite.
func BenchmarkReadSetDictProof(b *testing.B) {
	for _, keys := range []int{128, 2048} {
		source := viewEquivalenceDict(b, keys)
		var read []uint64
		for i := 0; i < keys; i += max(1, keys/16) {
			read = append(read, uint64(i))
		}

		b.Run(fmt.Sprintf("keys=%d", keys), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				rs := NewReadSet(source)
				viewEquivalenceReadKeys(b, rs.Root(), 32, read)
				if _, err := rs.Proof(); err != nil {
					b.Fatalf("read set proof: %v", err)
				}
			}
		})
	}
}

// TestRecordRecursiveDescendsPastAlreadyRecordedCells pins the distinction that
// makes RecordRecursive usable for execution proofs: it must stop on what this
// walk has already visited, not on what the read set already holds. A cell the
// execution read is in the record while the references it never opened are not,
// and those are precisely what the walk is called to add.
func TestRecordRecursiveDescendsPastAlreadyRecordedCells(t *testing.T) {
	deep := BeginCell().MustStoreUInt(0xdd, 8).EndCell()
	unopened := BeginCell().MustStoreUInt(0xcc, 8).MustStoreRef(deep).EndCell()
	read := BeginCell().MustStoreUInt(0xbb, 8).MustStoreRef(unopened).EndCell()
	root := BeginCell().MustStoreUInt(0xaa, 8).MustStoreRef(read).EndCell()

	rs := NewReadSet(root)
	slice, err := rs.Root().BeginParse()
	if err != nil {
		t.Fatalf("parse root: %v", err)
	}
	// Read one level down and stop: the child is recorded, its own subtree is not.
	child, err := slice.LoadRefCell()
	if err != nil {
		t.Fatalf("load child: %v", err)
	}
	if _, err = child.BeginParse(); err != nil {
		t.Fatalf("parse child: %v", err)
	}
	if _, recorded := rs.Contains(read.HashKey()); !recorded {
		t.Fatal("the child read was not recorded")
	}
	if _, recorded := rs.Contains(unopened.HashKey()); recorded {
		t.Fatal("an unopened reference was recorded")
	}

	if err = rs.RecordRecursive(child); err != nil {
		t.Fatalf("record recursive: %v", err)
	}
	for name, c := range map[string]*Cell{"unopened": unopened, "deep": deep} {
		if _, recorded := rs.Contains(c.HashKey()); !recorded {
			t.Fatalf("%s cell below an already-recorded cell was not reached", name)
		}
	}
}
