package cell

import (
	"errors"
	"fmt"
	"math/rand"
	"testing"
)

func TestSetManyWithDiffContract(t *testing.T) {
	dict, err := NewAugDict(8, testMetricAugmentation{})
	if err != nil {
		t.Fatal(err)
	}

	empty, err := dict.SetManyWithDiff(nil)
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil {
		t.Fatal("empty mutation returned a nil receipt")
	}
	if err = empty.Replay(); err != nil {
		t.Fatalf("empty structural replay: %v", err)
	}

	rootBefore := dict.RootCell()
	wrapperBefore := dict.AsCell().HashKey()
	failed, err := dict.SetManyWithDiff([]AugmentedEntry{{
		Key:   mustTestAugKey(t, 1),
		Value: mustTestAugValue(t, 1, 8),
		Mode:  DictSetModeReplace,
	}})
	if err == nil {
		t.Fatal("replace of an absent key succeeded")
	}
	if failed != nil {
		t.Fatal("failed mutation returned a receipt")
	}
	if dict.RootCell() != rootBefore || dict.AsCell().HashKey() != wrapperBefore {
		t.Fatal("failed mutation changed the dictionary root")
	}

	diff, err := dict.SetManyWithDiff([]AugmentedEntry{{
		Key:   mustTestAugKey(t, 1),
		Value: mustTestAugValue(t, 1, 8),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err = diff.Replay(); err != nil {
		t.Fatal(err)
	}
}

func TestSetManyWithDiffMatchesScanDiff(t *testing.T) {
	for _, workers := range []int{1, 8} {
		t.Run(fmt.Sprintf("parallelism_%d", workers), func(t *testing.T) {
			rnd := rand.New(rand.NewSource(0x51d1ff + int64(workers)))
			for round := 0; round < 100; round++ {
				values := make(map[uint64]uint64)
				for len(values) < 40 {
					values[uint64(rnd.Intn(256))] = uint64(rnd.Intn(256))
				}
				old := mustDiffDict(t, testMetricAugmentation{}, values)

				entries := make([]AugmentedEntry, 0, 32)
				used := make(map[uint64]struct{}, cap(entries))
				for len(entries) < cap(entries) {
					key := uint64(rnd.Intn(256))
					if _, exists := used[key]; exists {
						continue
					}
					used[key] = struct{}{}
					value := uint64(rnd.Intn(256))
					if previous, exists := values[key]; exists && rnd.Intn(4) == 0 {
						value = previous
					}
					entries = append(entries, AugmentedEntry{
						Key:   mustTestAugKey(t, key),
						Value: mustTestAugValue(t, value, 8),
					})
				}
				rnd.Shuffle(len(entries), func(i, j int) {
					entries[i], entries[j] = entries[j], entries[i]
				})

				wantDict := old.Copy()
				if err := wantDict.SetMany(entries, workers); err != nil {
					t.Fatalf("round %d SetMany: %v", round, err)
				}
				if err := old.ScanDiff(wantDict, true, func(*Cell, *Slice, *Slice) error { return nil }); err != nil {
					t.Fatalf("round %d ScanDiff: %v", round, err)
				}

				gotDict := old.Copy()
				diff, err := gotDict.SetManyWithDiff(entries, workers)
				if err != nil {
					t.Fatalf("round %d SetManyWithDiff: %v", round, err)
				}
				if err = diff.Replay(); err != nil {
					t.Fatalf("round %d Replay: %v", round, err)
				}

				if gotDict.RootCell().HashKey() != wantDict.RootCell().HashKey() {
					t.Fatalf("round %d root differs", round)
				}
			}
		})
	}
}

func TestSetManyWithDiffReplaysRelabeledLeafClosure(t *testing.T) {
	build := func(t *testing.T) (*AugmentedDictionary, *Cell) {
		t.Helper()

		aug := &diffReferencedValueAugmentation{}
		dict, err := NewAugDict(8, aug)
		if err != nil {
			t.Fatal(err)
		}
		payload := BeginCell().MustStoreUInt(0xa0, 8).EndCell()
		value := BeginCell().MustStoreRef(payload).EndCell()
		if err = dict.Set(mustTestAugKey(t, 0x00), value); err != nil {
			t.Fatal(err)
		}
		return dict, payload
	}
	entry := AugmentedEntry{
		Key:   mustTestAugKey(t, 0x40),
		Value: BeginCell().MustStoreRef(BeginCell().MustStoreUInt(0xb0, 8).EndCell()).EndCell(),
	}

	baseline, baselinePayload := build(t)
	baselineRead := NewReadSet(baseline.RootCell())
	baselineOld := baseline.Copy().SetTrace(baselineRead.Trace())
	baselineNew := baselineOld.Copy()
	if err := baselineNew.SetMany([]AugmentedEntry{entry}); err != nil {
		t.Fatal(err)
	}
	if _, loaded := baselineRead.Contains(baselinePayload.HashKey()); loaded {
		t.Fatal("ordinary mutation loaded the retained payload")
	}
	if err := baselineOld.ScanDiff(baselineNew, true, func(*Cell, *Slice, *Slice) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, loaded := baselineRead.Contains(baselinePayload.HashKey()); !loaded {
		t.Fatal("ScanDiff did not load the retained payload")
	}
	baselineProof, err := baselineRead.Proof()
	if err != nil {
		t.Fatal(err)
	}

	withReceipt, receiptPayload := build(t)
	receiptRead := NewReadSet(withReceipt.RootCell())
	receiptOld := withReceipt.Copy().SetTrace(receiptRead.Trace())
	receiptNew := receiptOld.Copy()
	diff, err := receiptNew.SetManyWithDiff([]AugmentedEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	if _, loaded := receiptRead.Contains(receiptPayload.HashKey()); loaded {
		t.Fatal("receipt mutation loaded the retained payload before Replay")
	}
	if err = diff.Replay(); err != nil {
		t.Fatal(err)
	}
	if _, loaded := receiptRead.Contains(receiptPayload.HashKey()); !loaded {
		t.Fatal("Replay did not load the retained payload")
	}
	receiptProof, err := receiptRead.Proof()
	if err != nil {
		t.Fatal(err)
	}

	if receiptProof.HashKey() != baselineProof.HashKey() {
		t.Fatalf("receipt proof = %x, ScanDiff proof = %x", receiptProof.Hash(), baselineProof.Hash())
	}
}

func TestSetManyWithDiffReplaysChangedLeafWithFinalTrace(t *testing.T) {
	aug := &diffReferencedValueAugmentation{}
	base, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}
	reusedPayload := BeginCell().
		MustStoreUInt(0xa0, 8).
		MustStoreRef(BeginCell().MustStoreUInt(0xa1, 8).EndCell()).
		EndCell()
	oldPayload := BeginCell().MustStoreUInt(0xb0, 8).EndCell()
	if err = base.Set(mustTestAugKey(t, 0x00), BeginCell().MustStoreRef(reusedPayload).EndCell()); err != nil {
		t.Fatal(err)
	}
	if err = base.Set(mustTestAugKey(t, 0x80), BeginCell().MustStoreRef(oldPayload).EndCell()); err != nil {
		t.Fatal(err)
	}
	entry := AugmentedEntry{
		Key:   mustTestAugKey(t, 0x80),
		Value: BeginCell().MustStoreRef(reusedPayload).EndCell(),
	}

	baselineRead := NewReadSet(base.RootCell())
	baselineOld := base.Copy().SetTrace(baselineRead.Trace())
	baselineNew := baselineOld.Copy()
	if err = baselineNew.SetMany([]AugmentedEntry{entry}); err != nil {
		t.Fatal(err)
	}
	if _, loaded := baselineRead.Contains(reusedPayload.HashKey()); loaded {
		t.Fatal("mutation recorded the untraced reused payload before ScanDiff")
	}
	if err = baselineOld.ScanDiff(baselineNew, true, func(*Cell, *Slice, *Slice) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, loaded := baselineRead.Contains(reusedPayload.HashKey()); !loaded {
		t.Fatal("ScanDiff did not record the reused payload through the final candidate path")
	}
	baselineProof, err := baselineRead.Proof()
	if err != nil {
		t.Fatal(err)
	}

	receiptRead := NewReadSet(base.RootCell())
	receiptOld := base.Copy().SetTrace(receiptRead.Trace())
	receiptNew := receiptOld.Copy()
	diff, err := receiptNew.SetManyWithDiff([]AugmentedEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	if _, loaded := receiptRead.Contains(reusedPayload.HashKey()); loaded {
		t.Fatal("receipt mutation recorded the untraced reused payload before Replay")
	}
	update, err := receiptRead.CreateMerkleUpdate(receiptNew.RootCell())
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateMerkleUpdate(update); err != nil {
		t.Fatal(err)
	}
	applied, err := ApplyMerkleUpdate(base.RootCell(), update)
	if err != nil {
		t.Fatal(err)
	}
	if applied.HashKey() != receiptNew.RootCell().HashKey() {
		t.Fatal("pre-Replay Merkle update produced the wrong state root")
	}
	if err = diff.Replay(); err != nil {
		t.Fatal(err)
	}
	if _, loaded := receiptRead.Contains(reusedPayload.HashKey()); !loaded {
		t.Fatal("Replay did not record the reused payload through the final candidate path")
	}
	receiptProof, err := receiptRead.Proof()
	if err != nil {
		t.Fatal(err)
	}
	if receiptProof.HashKey() != baselineProof.HashKey() {
		t.Fatalf("receipt proof = %x, ScanDiff proof = %x", receiptProof.Hash(), baselineProof.Hash())
	}
	assertSourceReadSetsEqual(t, base.RootCell(), baselineRead, receiptRead)

	provenOld, err := UnwrapProofVirtualized(receiptProof, base.RootCell().Hash())
	if err != nil {
		t.Fatal(err)
	}
	provenNew, err := ApplyMerkleUpdate(provenOld, update)
	if err != nil {
		t.Fatal(err)
	}
	if err = provenOld.AsAugDict(8, aug).ScanDiff(
		provenNew.AsAugDict(8, aug),
		true,
		func(*Cell, *Slice, *Slice) error { return nil },
	); err != nil {
		t.Fatalf("validator-style ScanDiff over proof and pre-Replay update: %v", err)
	}
}

func TestSetManyWithDiffSkipsEqualLeafBesideChange(t *testing.T) {
	aug := &diffReferencedValueAugmentation{}
	base, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}
	unchangedPayload := BeginCell().MustStoreUInt(0xa0, 8).EndCell()
	oldPayload := BeginCell().MustStoreUInt(0xb0, 8).EndCell()
	newPayload := BeginCell().MustStoreUInt(0xc0, 8).EndCell()
	if err = base.Set(mustTestAugKey(t, 0x00), BeginCell().MustStoreRef(unchangedPayload).EndCell()); err != nil {
		t.Fatal(err)
	}
	if err = base.Set(mustTestAugKey(t, 0x80), BeginCell().MustStoreRef(oldPayload).EndCell()); err != nil {
		t.Fatal(err)
	}
	entries := []AugmentedEntry{
		{Key: mustTestAugKey(t, 0x00), Value: BeginCell().MustStoreRef(unchangedPayload).EndCell()},
		{Key: mustTestAugKey(t, 0x80), Value: BeginCell().MustStoreRef(newPayload).EndCell()},
	}

	baselineRead := NewReadSet(base.RootCell())
	baselineOld := base.Copy().SetTrace(baselineRead.Trace())
	baselineNew := baselineOld.Copy()
	if err = baselineNew.SetMany(entries); err != nil {
		t.Fatal(err)
	}
	if err = baselineOld.ScanDiff(baselineNew, true, func(*Cell, *Slice, *Slice) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, loaded := baselineRead.Contains(unchangedPayload.HashKey()); loaded {
		t.Fatal("ScanDiff loaded the payload of a hash-equal leaf")
	}

	receiptRead := NewReadSet(base.RootCell())
	receiptOld := base.Copy().SetTrace(receiptRead.Trace())
	receiptNew := receiptOld.Copy()
	diff, err := receiptNew.SetManyWithDiff(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err = diff.Replay(); err != nil {
		t.Fatal(err)
	}
	if _, loaded := receiptRead.Contains(unchangedPayload.HashKey()); loaded {
		t.Fatal("Replay loaded the payload of a hash-equal leaf")
	}
	baselineProof, err := baselineRead.Proof()
	if err != nil {
		t.Fatal(err)
	}
	receiptProof, err := receiptRead.Proof()
	if err != nil {
		t.Fatal(err)
	}
	if receiptProof.HashKey() != baselineProof.HashKey() {
		t.Fatalf("receipt proof = %x, ScanDiff proof = %x", receiptProof.Hash(), baselineProof.Hash())
	}
}

func TestSetManyWithDiffReplaysRelabeledForkClosure(t *testing.T) {
	aug := &diffReferencedValueAugmentation{}
	base, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []uint64{0x40, 0x60} {
		if err = base.Set(mustTestAugKey(t, key), referencedDiffValue(key)); err != nil {
			t.Fatal(err)
		}
	}
	rootNode, err := parseFixedDictNode(base.RootCell(), 8)
	if err != nil {
		t.Fatal(err)
	}
	children := make([]Hash, 2)
	for bit := range children {
		child, err := rootNode.ref(bit)
		if err != nil {
			t.Fatal(err)
		}
		children[bit] = child.HashKey()
	}
	entry := AugmentedEntry{Key: mustTestAugKey(t, 0xc0), Value: referencedDiffValue(0xc0)}

	baselineRead := NewReadSet(base.RootCell())
	baselineOld := base.Copy().SetTrace(baselineRead.Trace())
	baselineNew := baselineOld.Copy()
	if err = baselineNew.SetMany([]AugmentedEntry{entry}); err != nil {
		t.Fatal(err)
	}
	if err = baselineOld.ScanDiff(baselineNew, true, func(*Cell, *Slice, *Slice) error { return nil }); err != nil {
		t.Fatal(err)
	}
	baselineProof, err := baselineRead.Proof()
	if err != nil {
		t.Fatal(err)
	}

	receiptRead := NewReadSet(base.RootCell())
	receiptOld := base.Copy().SetTrace(receiptRead.Trace())
	receiptNew := receiptOld.Copy()
	diff, err := receiptNew.SetManyWithDiff([]AugmentedEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	for _, child := range children {
		if _, loaded := receiptRead.Contains(child); loaded {
			t.Fatal("mutation loaded a child of the retained fork before Replay")
		}
	}
	if err = diff.Replay(); err != nil {
		t.Fatal(err)
	}
	for _, child := range children {
		if _, loaded := receiptRead.Contains(child); !loaded {
			t.Fatal("Replay did not load a child of the retained fork")
		}
	}
	receiptProof, err := receiptRead.Proof()
	if err != nil {
		t.Fatal(err)
	}
	if receiptProof.HashKey() != baselineProof.HashKey() {
		t.Fatalf("receipt proof = %x, ScanDiff proof = %x", receiptProof.Hash(), baselineProof.Hash())
	}
}

func TestSetManyWithDiffMatchesScanDiffProofRandomized(t *testing.T) {
	aug := &diffReferencedValueAugmentation{}
	rnd := rand.New(rand.NewSource(0x5e7d1ff))
	for _, workers := range []int{1, 8} {
		for round := 0; round < 30; round++ {
			base, err := NewAugDict(8, aug)
			if err != nil {
				t.Fatal(err)
			}
			keys := rnd.Perm(256)
			for _, key := range keys[:64] {
				if err = base.Set(mustTestAugKey(t, uint64(key)), referencedDiffValue(uint64(key))); err != nil {
					t.Fatal(err)
				}
			}

			entries := make([]AugmentedEntry, 0, 48)
			for i, key := range keys[32:80] {
				entries = append(entries, AugmentedEntry{
					Key:   mustTestAugKey(t, uint64(key)),
					Value: referencedDiffValue(uint64(round*64 + i + 256)),
				})
			}
			rnd.Shuffle(len(entries), func(i, j int) {
				entries[i], entries[j] = entries[j], entries[i]
			})

			baselineRead := NewReadSet(base.RootCell())
			baselineOld := base.Copy().SetTrace(baselineRead.Trace())
			baselineNew := baselineOld.Copy()
			if err = baselineNew.SetMany(entries, workers); err != nil {
				t.Fatal(err)
			}
			if err = baselineOld.ScanDiff(baselineNew, true, func(*Cell, *Slice, *Slice) error { return nil }); err != nil {
				t.Fatal(err)
			}
			baselineProof, err := baselineRead.Proof()
			if err != nil {
				t.Fatal(err)
			}

			receiptRead := NewReadSet(base.RootCell())
			receiptOld := base.Copy().SetTrace(receiptRead.Trace())
			receiptNew := receiptOld.Copy()
			diff, err := receiptNew.SetManyWithDiff(entries, workers)
			if err != nil {
				t.Fatal(err)
			}
			if err = diff.Replay(); err != nil {
				t.Fatal(err)
			}
			receiptProof, err := receiptRead.Proof()
			if err != nil {
				t.Fatal(err)
			}

			if receiptProof.HashKey() != baselineProof.HashKey() {
				t.Fatalf("parallelism %d round %d proof differs", workers, round)
			}
			assertSourceReadSetsEqual(t, base.RootCell(), baselineRead, receiptRead)
		}
	}
}

func TestSetManyWithLoadedPathsAndDiff(t *testing.T) {
	const keyBits = 8
	base := bulkLoadedDenseDict(t, keyBits, 128)
	entries := make([]AugmentedEntry, 0, 32)
	for i := 0; i < 16; i++ {
		key := bulkLoadedKey(uint64(i*7), keyBits)
		entries = append(entries, AugmentedEntry{
			Key:   key,
			Value: bulkValue(key, uint64(100+i)),
			Mode:  DictSetModeReplace,
		})
	}
	for i := 0; i < 16; i++ {
		key := bulkLoadedKey(uint64(160+i), keyBits)
		entries = append(entries, AugmentedEntry{
			Key:   key,
			Value: bulkValue(key, uint64(200+i)),
			Mode:  DictSetModeAdd,
		})
	}

	want := base.Copy()
	if err := want.SetMany(entries, 8); err != nil {
		t.Fatal(err)
	}

	prepare := func() (*AugmentedDictionary, *AugmentedDictionary, *ReadSet, [][]*Cell) {
		read := NewReadSet(base.RootCell())
		dict, _, recorder := bulkLoadedLazyDict(t, base, read.Trace())
		paths := make([][]*Cell, 0, len(entries))
		for _, entry := range entries {
			path, lookupErr := bulkLoadedLookupPath(dict, recorder, entry.Key)
			if entry.Mode == DictSetModeReplace {
				if lookupErr != nil {
					t.Fatalf("load replace path: %v", lookupErr)
				}
			} else if !errors.Is(lookupErr, ErrNoSuchKeyInDict) {
				t.Fatalf("load insert path: got %v, want ErrNoSuchKeyInDict", lookupErr)
			}
			paths = append(paths, path)
		}
		old := dict.Copy()
		recorder.trace.DetachListener()
		return old, dict, read, paths
	}

	baselineOld, baselineNew, baselineRead, baselinePaths := prepare()
	if err := baselineNew.SetManyWithLoadedPaths(entries, baselinePaths, 8); err != nil {
		t.Fatal(err)
	}
	if err := baselineOld.ScanDiff(baselineNew, true, func(*Cell, *Slice, *Slice) error { return nil }); err != nil {
		t.Fatal(err)
	}
	baselineProof, err := baselineRead.Proof()
	if err != nil {
		t.Fatal(err)
	}

	_, receiptNew, receiptRead, receiptPaths := prepare()
	diff, err := receiptNew.SetManyWithLoadedPathsAndDiff(entries, receiptPaths, 8)
	if err != nil {
		t.Fatal(err)
	}
	update, err := receiptRead.CreateMerkleUpdate(receiptNew.RootCell())
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateMerkleUpdate(update); err != nil {
		t.Fatal(err)
	}
	if err = diff.Replay(); err != nil {
		t.Fatal(err)
	}
	if receiptNew.RootCell().HashKey() != want.RootCell().HashKey() {
		t.Fatal("loaded-path receipt changed the resulting dictionary")
	}
	receiptProof, err := receiptRead.Proof()
	if err != nil {
		t.Fatal(err)
	}
	if receiptProof.HashKey() != baselineProof.HashKey() {
		t.Fatalf("receipt proof = %x, lazy ScanDiff proof = %x", receiptProof.Hash(), baselineProof.Hash())
	}
	assertSourceReadSetsEqual(t, base.RootCell(), baselineRead, receiptRead)

	provenOld, err := UnwrapProofVirtualized(receiptProof, base.RootCell().Hash())
	if err != nil {
		t.Fatal(err)
	}
	provenNew, err := ApplyMerkleUpdate(provenOld, update)
	if err != nil {
		t.Fatal(err)
	}
	if err = provenOld.AsAugDict(keyBits, base.aug).ScanDiff(
		provenNew.AsAugDict(keyBits, base.aug),
		true,
		func(*Cell, *Slice, *Slice) error { return nil },
	); err != nil {
		t.Fatalf("validator-style ScanDiff over loaded-path receipt: %v", err)
	}
}

func referencedDiffValue(value uint64) *Cell {
	return BeginCell().MustStoreRef(BeginCell().MustStoreUInt(value, 16).EndCell()).EndCell()
}

func assertSourceReadSetsEqual(t *testing.T, source *Cell, want, got *ReadSet) {
	t.Helper()

	sourceHashes := make(map[Hash]struct{})
	var collectSource func(*Cell)
	collectSource = func(cell *Cell) {
		hash := cell.HashKey()
		if _, exists := sourceHashes[hash]; exists {
			return
		}
		sourceHashes[hash] = struct{}{}
		for i := 0; i < cell.refsCount(); i++ {
			collectSource(cell.refs[i])
		}
	}
	collectSource(source)

	filter := func(read *ReadSet) map[Hash]struct{} {
		filtered := make(map[Hash]struct{})
		for _, hash := range read.Hashes() {
			if _, exists := sourceHashes[hash]; exists {
				filtered[hash] = struct{}{}
			}
		}
		return filtered
	}
	wantSet := filter(want)
	gotSet := filter(got)
	if len(gotSet) != len(wantSet) {
		t.Fatalf("source read-set size = %d, want %d", len(gotSet), len(wantSet))
	}
	for hash := range gotSet {
		if _, exists := wantSet[hash]; !exists {
			t.Fatalf("unexpected source read-set cell %x", hash)
		}
	}
}

var benchmarkAugMutationDiffRootSink *Cell

func BenchmarkAugDictSetManyMutationDiff(b *testing.B) {
	const (
		keyBits      = 256
		existingKeys = 4096
		updateKeys   = 80
		parallelism  = 8
	)

	rnd := rand.New(rand.NewSource(2026082001))
	keys := randomBulkKeys(b, rnd, keyBits, existingKeys, nil)
	payloads := make([]*Cell, 256)
	for i := range payloads {
		payloads[i] = BeginCell().
			MustStoreUInt(uint64(i), 8).
			MustStoreRef(BeginCell().MustStoreUInt(uint64(i*257), 24).EndCell()).
			EndCell()
	}

	base, err := NewAugDict(keyBits, &diffReferencedValueAugmentation{})
	if err != nil {
		b.Fatal(err)
	}
	baseEntries := make([]AugmentedEntry, len(keys))
	for i, key := range keys {
		baseEntries[i] = AugmentedEntry{
			Key: key,
			Value: BeginCell().
				MustStoreUInt(uint64(i), 16).
				MustStoreRef(payloads[i%len(payloads)]).
				EndCell(),
		}
	}
	if err = base.SetMany(baseEntries, parallelism); err != nil {
		b.Fatal(err)
	}

	entries := make([]AugmentedEntry, 0, updateKeys)
	for i := 0; i < updateKeys/2; i++ {
		key := keys[i*47%len(keys)]
		entries = append(entries, AugmentedEntry{
			Key:  key,
			Mode: DictSetModeReplace,
			Value: BeginCell().
				MustStoreUInt(uint64(existingKeys+i), 16).
				MustStoreRef(payloads[(i*29+1)%len(payloads)]).
				EndCell(),
		})
	}
	freshKeys := randomBulkKeys(b, rnd, keyBits, updateKeys/2, keys)
	for i, key := range freshKeys {
		entries = append(entries, AugmentedEntry{
			Key:  key,
			Mode: DictSetModeAdd,
			Value: BeginCell().
				MustStoreUInt(uint64(existingKeys+updateKeys+i), 16).
				MustStoreRef(payloads[(i*31+3)%len(payloads)]).
				EndCell(),
		})
	}
	rnd.Shuffle(len(entries), func(i, j int) {
		entries[i], entries[j] = entries[j], entries[i]
	})

	canonical := base.Copy()
	if err = canonical.SetMany(entries, parallelism); err != nil {
		b.Fatal(err)
	}
	if err = base.ScanDiff(canonical, true, func(*Cell, *Slice, *Slice) error { return nil }); err != nil {
		b.Fatal(err)
	}
	receipt := base.Copy()
	diff, err := receipt.SetManyWithDiff(entries, parallelism)
	if err != nil {
		b.Fatal(err)
	}
	if err = diff.Replay(); err != nil {
		b.Fatal(err)
	}
	if canonical.RootCell().HashKey() != receipt.RootCell().HashKey() {
		b.Fatal("mutation receipt changed the resulting dictionary")
	}

	noop := func(*Cell, *Slice, *Slice) error { return nil }
	b.Run("set_many_scan_diff", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			next := base.Copy()
			if err := next.SetMany(entries, parallelism); err != nil {
				b.Fatal(err)
			}
			if err := base.ScanDiff(next, true, noop); err != nil {
				b.Fatal(err)
			}
			benchmarkAugMutationDiffRootSink = next.RootCell()
		}
	})
	b.Run("set_many_with_diff_replay", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			next := base.Copy()
			diff, err := next.SetManyWithDiff(entries, parallelism)
			if err != nil {
				b.Fatal(err)
			}
			if err = diff.Replay(); err != nil {
				b.Fatal(err)
			}
			benchmarkAugMutationDiffRootSink = next.RootCell()
		}
	})
}

// The parallel replay must record exactly the reads the sequential one
// records: that set is what the closure exists to put into the collated proof.
// The dictionary is wide enough that the frontier actually splits — a closure
// with one task is the sequential walk under another name, and would prove
// nothing about the split.
func TestReplayParallelRecordsTheSequentialReads(t *testing.T) {
	rnd := rand.New(rand.NewSource(0x7a11e1))
	for round := 0; round < 40; round++ {
		values := make(map[uint64]uint64)
		for len(values) < 600 {
			values[uint64(rnd.Intn(1<<16))] = uint64(rnd.Intn(256))
		}
		entries := make([]AugmentedEntry, 0, 300)
		used := make(map[uint64]struct{}, cap(entries))
		for len(entries) < cap(entries) {
			key := uint64(rnd.Intn(1 << 16))
			if _, exists := used[key]; exists {
				continue
			}
			used[key] = struct{}{}
			entries = append(entries, AugmentedEntry{
				Key:   mustTestAugKey16(t, key),
				Value: mustTestAugValue(t, uint64(rnd.Intn(256)), 8),
			})
		}

		record := func(workers int) (map[Hash]struct{}, int) {
			old := mustDiffDict16(t, testMetricAugmentation{}, values)
			usage := NewReadSet(old.RootCell())
			traced := old.Copy().SetTrace(usage.Trace())
			diff, err := traced.SetManyWithDiff(entries, 4)
			if err != nil {
				t.Fatalf("round %d SetManyWithDiff: %v", round, err)
			}
			before := usage.Size()
			var tasks []augDiffReplayTask
			if workers > 1 {
				var top augmentedNodeChecker
				if err = replayAugDiffNodeTo(diff.replay, diff.replay.cell.Trace(), diff.aug, &top, replayFrontierDepth, &tasks); err != nil {
					t.Fatalf("round %d frontier: %v", round, err)
				}
				// Re-record from scratch: the frontier walk above already
				// recorded the top, so the real run starts on a fresh set.
				old = mustDiffDict16(t, testMetricAugmentation{}, values)
				usage = NewReadSet(old.RootCell())
				traced = old.Copy().SetTrace(usage.Trace())
				if diff, err = traced.SetManyWithDiff(entries, 4); err != nil {
					t.Fatal(err)
				}
				before = usage.Size()
				err = diff.ReplayParallel(workers)
			} else {
				err = diff.Replay()
			}
			if err != nil {
				t.Fatalf("round %d replay(%d): %v", round, workers, err)
			}
			got := make(map[Hash]struct{}, usage.Size()-before)
			for _, c := range usage.Cells() {
				got[c.HashKey()] = struct{}{}
			}
			return got, len(tasks)
		}
		sequential, _ := record(1)
		parallel, tasks := record(16)
		if tasks < 4 {
			t.Fatalf("round %d: the frontier produced %d tasks; the split is vacuous", round, tasks)
		}
		if len(parallel) != len(sequential) {
			t.Fatalf("round %d: parallel replay recorded %d cells, sequential %d", round, len(parallel), len(sequential))
		}
		for h := range sequential {
			if _, ok := parallel[h]; !ok {
				t.Fatalf("round %d: parallel replay missed cell %x", round, h[:4])
			}
		}
	}
}

func mustTestAugKey16(t *testing.T, value uint64) *Cell {
	t.Helper()
	return BeginCell().MustStoreUInt(value, 16).EndCell()
}

func mustDiffDict16(t *testing.T, aug Augmentation, values map[uint64]uint64) *AugmentedDictionary {
	t.Helper()
	dict, err := NewAugDict(16, aug)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range values {
		if err = dict.Set(mustTestAugKey16(t, key), mustTestAugValue(t, value, 8)); err != nil {
			t.Fatal(err)
		}
	}
	return dict
}

// ScanDiffParallel must record the reads ScanDiff records, and must actually
// split — a pair of dictionaries that differ under one prefix would be scanned
// by one task and prove nothing about the workers.
func TestScanDiffParallelRecordsTheSequentialReads(t *testing.T) {
	rnd := rand.New(rand.NewSource(0x5ca1d1f))
	for round := 0; round < 30; round++ {
		base := make(map[uint64]uint64)
		for len(base) < 800 {
			base[uint64(rnd.Intn(1<<16))] = uint64(rnd.Intn(256))
		}
		changed := make([]AugmentedEntry, 0, 200)
		used := map[uint64]struct{}{}
		for len(changed) < cap(changed) {
			key := uint64(rnd.Intn(1 << 16))
			if _, dup := used[key]; dup {
				continue
			}
			used[key] = struct{}{}
			changed = append(changed, AugmentedEntry{Key: mustTestAugKey16(t, key), Value: mustTestAugValue(t, uint64(rnd.Intn(256)), 8)})
		}
		deleted := make([]*Cell, 0, 60)
		for key := range base {
			if len(deleted) == cap(deleted) {
				break
			}
			if _, touched := used[key]; !touched {
				deleted = append(deleted, mustTestAugKey16(t, key))
			}
		}

		record := func(parallel bool) (map[Hash]struct{}, int) {
			old := mustDiffDict16(t, testMetricAugmentation{}, base)
			usage := NewReadSet(old.RootCell())
			tracedOld := old.Copy().SetTrace(usage.Trace())
			next := old.Copy()
			if err := next.SetMany(changed, 4); err != nil {
				t.Fatal(err)
			}
			if err := next.DeleteMany(deleted, 4); err != nil {
				t.Fatal(err)
			}
			noop := func(*Cell, *Slice, *Slice) error { return nil }
			tasks := 0
			if parallel {
				var collected []augDictDiffTask
				probe := augDictDiffWalk{keySz: 16, newAug: next.aug, checkNew: true, fn: noop, frontierBits: 6, tasks: &collected}
				if err := probe.node(old.Copy().root, next.root, 16, 0, 0); err != nil {
					t.Fatal(err)
				}
				tasks = len(collected)
				if err := tracedOld.scanDiffParallelAt(next, true, noop, 8, 6); err != nil {
					t.Fatalf("round %d parallel: %v", round, err)
				}
			} else if err := tracedOld.ScanDiff(next, true, noop); err != nil {
				t.Fatalf("round %d sequential: %v", round, err)
			}
			got := make(map[Hash]struct{}, usage.Size())
			for _, c := range usage.Cells() {
				got[c.HashKey()] = struct{}{}
			}
			return got, tasks
		}
		sequential, _ := record(false)
		parallel, tasks := record(true)
		if tasks < 4 {
			t.Fatalf("round %d: %d tasks; the split is vacuous", round, tasks)
		}
		if len(parallel) != len(sequential) {
			t.Fatalf("round %d: parallel scan recorded %d cells, sequential %d", round, len(parallel), len(sequential))
		}
		for h := range sequential {
			if _, ok := parallel[h]; !ok {
				t.Fatalf("round %d: parallel scan missed %x", round, h[:4])
			}
		}
	}
}
