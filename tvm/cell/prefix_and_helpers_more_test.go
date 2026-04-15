package cell

import (
	"errors"
	"math/big"
	"testing"
)

type testCellObserver struct {
	loads   int
	creates int
}

func (o *testCellObserver) OnCellLoad([]byte) { o.loads++ }
func (o *testCellObserver) OnCellCreate()     { o.creates++ }

type testHashObserver struct {
	testCellObserver
	keys int
}

func (o *testHashObserver) OnCellLoadKey([32]byte) { o.keys++ }

func TestPrefixDictionary_WrappersCompatibilityAndObservers(t *testing.T) {
	var nilDict *PrefixDictionary
	if !nilDict.IsEmpty() {
		t.Fatal("nil prefix dict should be empty")
	}
	if nilDict.AsCell() != nil {
		t.Fatal("nil prefix dict should not expose a root")
	}
	if c, err := nilDict.ToCell(); err != nil || c != nil {
		t.Fatalf("unexpected nil prefix dict ToCell result: cell=%v err=%v", c, err)
	}

	dict := NewPrefixDict(4)
	if dict.GetKeySize() != 4 || !dict.IsEmpty() {
		t.Fatalf("unexpected fresh prefix dict state: keySz=%d empty=%v", dict.GetKeySize(), dict.IsEmpty())
	}

	keyA := mustPrefixKey(t, 0b10, 2)
	keyB := mustPrefixKey(t, 0b011, 3)
	if err := dict.SetBuilder(keyA, BeginCell().MustStoreUInt(0xaa, 8)); err != nil {
		t.Fatal(err)
	}
	if err := dict.Set(keyB, mustPrefixValue(t, 0xbb, 8)); err != nil {
		t.Fatal(err)
	}
	if dict.IsEmpty() {
		t.Fatal("prefix dict with values should not be empty")
	}

	if got := mustUIntFromCell(t, dict.Get(keyA), 8); got != 0xaa {
		t.Fatalf("unexpected prefix Get value: %x", got)
	}

	copyDict := dict.Copy()
	if err := copyDict.Set(mustPrefixKey(t, 0b1110, 4), mustPrefixValue(t, 0xcc, 8)); err != nil {
		t.Fatal(err)
	}
	if _, err := dict.LoadValue(mustPrefixKey(t, 0b1110, 4)); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("copy mutation should not affect original dict, got %v", err)
	}

	root := dict.AsCell()
	if root == nil || dict.MustToCell() != root {
		t.Fatal("prefix dict should expose root cell through wrappers")
	}
	if c, err := dict.ToCell(); err != nil || c != root {
		t.Fatalf("unexpected ToCell result: cell=%v err=%v", c, err)
	}

	asDict := root.AsPrefixDict(4)
	if got, err := asDict.LoadValue(keyA); err != nil || mustLoadTestValue(t, got, 8) != 0xaa {
		t.Fatalf("unexpected AsPrefixDict load: err=%v", err)
	}

	inlineLoaded := root.BeginParse().MustToPrefixDict(4)
	if got, err := inlineLoaded.LoadValue(keyB); err != nil || mustLoadTestValue(t, got, 8) != 0xbb {
		t.Fatalf("unexpected MustToPrefixDict load: err=%v", err)
	}

	maybeLoaded := BeginCell().MustStoreMaybeRef(root).EndCell().BeginParse().MustLoadPrefixDict(4)
	if got, err := maybeLoaded.LoadValue(keyA); err != nil || mustLoadTestValue(t, got, 8) != 0xaa {
		t.Fatalf("unexpected MustLoadPrefixDict load: err=%v", err)
	}

	if err := dict.Set(keyA, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := dict.LoadValue(keyA); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("Set(key, nil) should delete the key, got %v", err)
	}
	if err := dict.Delete(keyA); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("unexpected delete error for missing prefix key: %v", err)
	}

	obs := &testCellObserver{}
	observed := copyDict.Copy().SetObserver(obs)
	observed.skipRootLoad = true
	_ = observed.beginParse(observed.root)
	if obs.loads != 0 {
		t.Fatalf("skipRootLoad should suppress first observer charge, got %d", obs.loads)
	}
	_ = observed.beginParse(observed.root)
	if obs.loads != 1 {
		t.Fatalf("second beginParse should charge observer once, got %d", obs.loads)
	}

	pruned := makeManualCellForTest(true, LevelMask{}, 288, append([]byte{byte(PrunedCellType), 0x01}, make([]byte, 34)...), nil)
	if err := validatePrefixDictRoot(pruned, 4); err != nil {
		t.Fatalf("pruned cell should be accepted in prefix dict validation, got %v", err)
	}

	lib := makeManualCellForTest(true, LevelMask{}, 8, []byte{byte(LibraryCellType)}, nil)
	if err := validatePrefixDictRoot(lib, 4); err == nil {
		t.Fatal("unsupported special prefix dict cell should fail validation")
	}
}

func TestDictFixedSliceValidateAndCompatHelpers(t *testing.T) {
	dict := NewDict(8)
	for _, pair := range []struct {
		key uint64
		val uint64
	}{
		{0x10, 0xa0},
		{0x20, 0xb0},
		{0x30, 0xc0},
	} {
		if err := dict.Set(mustDictKey(t, pair.key, 8), mustDictKey(t, pair.val, 8)); err != nil {
			t.Fatal(err)
		}
	}

	all := dict.All()
	if len(all) != 3 {
		t.Fatalf("unexpected compatibility All size: %d", len(all))
	}
	if got := mustUIntFromCell(t, all[0].Value, 8); got == 0 {
		t.Fatal("compatibility All should expose value cells")
	}

	items, err := dict.Range(false, false)
	if err != nil {
		t.Fatal(err)
	}
	key, value := fixedDictNearest(items, mustDictKey(t, 0x20, 8), false, true, false)
	if mustUIntFromCell(t, key, 8) != 0x20 || mustLoadTestValue(t, value, 8) != 0xb0 {
		t.Fatal("fixedDictNearest should match equal key when allowed")
	}
	key, value = fixedDictNearest(items, mustDictKey(t, 0x05, 8), false, false, false)
	if key != nil || value != nil {
		t.Fatal("fixedDictNearest should miss before the first key")
	}
	key, value = fixedDictNearest(items, mustDictKey(t, 0x40, 8), true, false, false)
	if key != nil || value != nil {
		t.Fatal("fixedDictNearest should miss after the last key when fetching next")
	}

	if compareKeyCells(nil, mustDictKey(t, 1, 1), false) >= 0 {
		t.Fatal("nil key should compare before non-nil key")
	}
	if compareKeyCells(mustDictKey(t, 1, 1), nil, false) <= 0 {
		t.Fatal("non-nil key should compare after nil key")
	}
	if compareKeyCells(mustDictKey(t, 0, 1), mustDictKey(t, 1, 1), true) <= 0 {
		t.Fatal("invertFirst should invert first-bit ordering")
	}

	if _, err := cellPrefix(nil, 1); err == nil {
		t.Fatal("cellPrefix should reject nil key")
	}
	if _, err := cellPrefix(mustDictKey(t, 0xaa, 8), 9); err == nil {
		t.Fatal("cellPrefix should reject oversized prefix length")
	}
	pfx, err := cellPrefix(mustDictKey(t, 0xaa, 8), 4)
	if err != nil {
		t.Fatal(err)
	}
	if pfx.BeginParse().MustLoadUInt(4) != 0x0a {
		t.Fatalf("unexpected cellPrefix value: %s", pfx.Dump())
	}

	if ok, err := fixedDictCheckForEach(items, nil, false); err != nil || !ok {
		t.Fatalf("fixedDictCheckForEach should accept nil callback: ok=%v err=%v", ok, err)
	}
	var seen int
	ok, err := fixedDictCheckForEach(append([]DictItem(nil), items...), func(value *Slice, key *Cell) (bool, error) {
		seen++
		return value != nil && key != nil, nil
	}, true)
	if err != nil || !ok || seen != len(items) {
		t.Fatalf("unexpected shuffled foreach result: ok=%v err=%v seen=%d", ok, err, seen)
	}

	filtered, changes, err := fixedDictFilterItems(items, nil)
	if err != nil || changes != 0 || len(filtered) != len(items) {
		t.Fatalf("unexpected nil-filter result: len=%d changes=%d err=%v", len(filtered), changes, err)
	}
	filtered, changes, err = fixedDictFilterItems(items, func(_ *Slice, key *Cell) (DictFilterAction, error) {
		if key.BeginParse().MustLoadUInt(8) == 0x10 {
			return DictFilterKeepRest, nil
		}
		return DictFilterRemove, nil
	})
	if err != nil || changes != 0 || len(filtered) != len(items) {
		t.Fatalf("unexpected keep-rest filter result: len=%d changes=%d err=%v", len(filtered), changes, err)
	}
	filtered, changes, err = fixedDictFilterItems(items, func(_ *Slice, key *Cell) (DictFilterAction, error) {
		if key.BeginParse().MustLoadUInt(8) == 0x20 {
			return DictFilterRemoveRest, nil
		}
		return DictFilterKeep, nil
	})
	if err != nil || changes != 2 || len(filtered) != 1 {
		t.Fatalf("unexpected remove-rest filter result: len=%d changes=%d err=%v", len(filtered), changes, err)
	}
	if _, _, err = fixedDictFilterItems(items, func(_ *Slice, _ *Cell) (DictFilterAction, error) {
		return DictFilterAction(255), nil
	}); err == nil {
		t.Fatal("fixedDictFilterItems should reject unknown actions")
	}

	refA := BeginCell().MustStoreUInt(0xa, 4).EndCell()
	refB := BeginCell().MustStoreUInt(0xb, 4).EndCell()
	sl := mustBitSlice(t, "101100", refA, refB)
	if bit, err := sl.BitAt(2); err != nil || bit != 1 {
		t.Fatalf("unexpected BitAt result: bit=%d err=%v", bit, err)
	}
	if _, err := sl.BitAt(8); err == nil {
		t.Fatal("BitAt should reject out-of-range offsets")
	}

	fetchSrc := mustBitSlice(t, "101100", refA, refB)
	fetched, err := fetchSrc.FetchSubslice(3, 1)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.BitsLeft() != 3 || fetched.RefsNum() != 1 || fetchSrc.BitsLeft() != 3 || fetchSrc.RefsNum() != 1 {
		t.Fatalf("unexpected FetchSubslice shapes: fetched=(%d,%d) src=(%d,%d)", fetched.BitsLeft(), fetched.RefsNum(), fetchSrc.BitsLeft(), fetchSrc.RefsNum())
	}

	preloadSrc := mustBitSlice(t, "101100", refA, refB)
	beforeBits, beforeRefs := preloadSrc.BitsLeft(), preloadSrc.RefsNum()
	preloaded, err := preloadSrc.PreloadSubslice(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if preloaded.BitsLeft() != 2 || preloaded.RefsNum() != 1 {
		t.Fatalf("unexpected PreloadSubslice result: bits=%d refs=%d", preloaded.BitsLeft(), preloaded.RefsNum())
	}
	if preloadSrc.BitsLeft() != beforeBits || preloadSrc.RefsNum() != beforeRefs {
		t.Fatalf("PreloadSubslice should not advance source: before=(%d,%d) after=(%d,%d)", beforeBits, beforeRefs, preloadSrc.BitsLeft(), preloadSrc.RefsNum())
	}

	uintSrc := mustBitSlice(t, "10110000")
	if got, err := uintSrc.PreloadUInt(4); err != nil || got != 0b1011 {
		t.Fatalf("unexpected PreloadUInt result: got=%b err=%v", got, err)
	}
	if uintSrc.BitsLeft() != 8 {
		t.Fatalf("PreloadUInt should not advance source, got %d bits left", uintSrc.BitsLeft())
	}

	bigVal := new(big.Int).Lsh(big.NewInt(1), 72)
	bigVal.Add(bigVal, big.NewInt(0x55))
	bigSrc := BeginCell().MustStoreBigUInt(bigVal, 80).EndCell().BeginParse()
	if got, err := bigSrc.PreloadBigUInt(80); err != nil || got.Cmp(bigVal) != 0 {
		t.Fatalf("unexpected PreloadBigUInt result: got=%v err=%v", got, err)
	}
	if got, err := bigSrc.LoadBigUInt(80); err != nil || got.Cmp(bigVal) != 0 {
		t.Fatalf("unexpected LoadBigUInt result: got=%v err=%v", got, err)
	}

	unknown := makeManualCellForTest(true, LevelMask{}, 8, []byte{0xff}, nil)
	if err := validateLoadedCell(unknown); err == nil {
		t.Fatal("unknown special cell type should fail validation")
	}

	prunedWithRef := makeManualCellForTest(true, LevelMask{}, 16, []byte{byte(PrunedCellType), 0x01}, []*Cell{BeginCell().EndCell()})
	if err := validateLoadedCell(prunedWithRef); err == nil {
		t.Fatal("pruned special cell with refs should fail validation")
	}
}

func TestValidateLoadedCellAdditionalMerkleAndPrunedPaths(t *testing.T) {
	obs := &testHashObserver{}
	notifyCellLoad(obs, BeginCell().MustStoreUInt(1, 1).EndCell())
	if obs.keys != 1 || obs.loads != 0 {
		t.Fatalf("hash observer should be notified via key path, got keys=%d loads=%d", obs.keys, obs.loads)
	}

	validPruned := makeManualCellForTest(true, LevelMask{Mask: 1}, 288, append([]byte{byte(PrunedCellType), 0x01}, make([]byte, 34)...), nil)
	if err := validateLoadedCell(validPruned); err != nil {
		t.Fatalf("valid pruned branch should pass validation: %v", err)
	}

	shortPruned := makeManualCellForTest(true, LevelMask{Mask: 1}, 280, append([]byte{byte(PrunedCellType), 0x01}, make([]byte, 33)...), nil)
	if err := validateLoadedCell(shortPruned); err == nil {
		t.Fatal("short pruned branch should fail validation")
	}

	leaf := BeginCell().MustStoreUInt(0x1234, 16).EndCell()
	sk := CreateProofSkeleton()
	sk.SetRecursive()
	proof, err := leaf.CreateProof(sk)
	if err != nil {
		t.Fatal(err)
	}

	proofNoRefs := makeManualCellForTest(true, proof.LevelMask(), proof.BitsSize(), proof.data, nil)
	if err := validateLoadedCell(proofNoRefs); err == nil {
		t.Fatal("merkle proof without refs should fail")
	}

	proofBadHash := makeManualCellForTest(true, proof.LevelMask(), proof.BitsSize(), proof.data, proof.rawRefs())
	proofBadHash.data[1] ^= 0xff
	if err := validateLoadedCell(proofBadHash); err == nil {
		t.Fatal("merkle proof with bad hash should fail")
	}

	proofBadDepth := makeManualCellForTest(true, proof.LevelMask(), proof.BitsSize(), proof.data, proof.rawRefs())
	proofBadDepth.data[1+hashSize] ^= 0xff
	if err := validateLoadedCell(proofBadDepth); err == nil {
		t.Fatal("merkle proof with bad depth should fail")
	}

	left := BeginCell().MustStoreUInt(0xaa, 8).EndCell()
	right := BeginCell().MustStoreUInt(0xbb, 8).EndCell()
	update := mustMerkleUpdateCell(t, left, right)

	updateOneRef := makeManualCellForTest(true, update.LevelMask(), update.BitsSize(), update.data, []*Cell{left})
	if err := validateLoadedCell(updateOneRef); err == nil {
		t.Fatal("merkle update with wrong ref count should fail")
	}

	updateBadFirstHash := makeManualCellForTest(true, update.LevelMask(), update.BitsSize(), update.data, update.rawRefs())
	updateBadFirstHash.data[1] ^= 0xff
	if err := validateLoadedCell(updateBadFirstHash); err == nil {
		t.Fatal("merkle update with bad first hash should fail")
	}

	updateBadSecondDepth := makeManualCellForTest(true, update.LevelMask(), update.BitsSize(), update.data, update.rawRefs())
	secondDepthOff := 1 + hashSize*2 + depthSize
	updateBadSecondDepth.data[secondDepthOff] ^= 0xff
	if err := validateLoadedCell(updateBadSecondDepth); err == nil {
		t.Fatal("merkle update with bad second depth should fail")
	}
}

func TestDictionaryFixedAPI_NilAndErrorBranches(t *testing.T) {
	var nilDict *Dictionary

	if items, err := nilDict.Range(false, false); err != nil || len(items) != 0 {
		t.Fatalf("nil Range should be empty: len=%d err=%v", len(items), err)
	}
	it, err := nilDict.Iterator(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if it.Next() {
		t.Fatal("nil dict iterator should be empty")
	}
	if ok, err := nilDict.HasCommonPrefix(nil); err != nil || !ok {
		t.Fatalf("nil HasCommonPrefix should succeed: ok=%v err=%v", ok, err)
	}
	if pfx, err := nilDict.GetCommonPrefix(); err != nil || pfx.BitsSize() != 0 {
		t.Fatalf("nil GetCommonPrefix should be empty: pfx=%v err=%v", pfx, err)
	}
	if root, err := nilDict.ExtractPrefixSubdictRoot(nil, false); err != nil || root != nil {
		t.Fatalf("nil ExtractPrefixSubdictRoot should be empty: root=%v err=%v", root, err)
	}
	if ok, err := nilDict.CutPrefixSubdict(nil, false); err != nil || !ok {
		t.Fatalf("nil CutPrefixSubdict should succeed: ok=%v err=%v", ok, err)
	}
	if ok, err := nilDict.CheckForEach(nil, false, false); err != nil || !ok {
		t.Fatalf("nil CheckForEach should succeed: ok=%v err=%v", ok, err)
	}
	if ok, err := nilDict.ValidateCheck(nil, false); err != nil || !ok {
		t.Fatalf("nil ValidateCheck should succeed: ok=%v err=%v", ok, err)
	}
	if !nilDict.ValidateAll() {
		t.Fatal("nil ValidateAll should return true")
	}
	if changes, err := nilDict.Filter(nil); err != nil || changes != 0 {
		t.Fatalf("nil Filter should be a no-op: changes=%d err=%v", changes, err)
	}
	if _, _, err := nilDict.LookupNearestKey(mustDictKey(t, 1, 8), false, false, false); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("nil LookupNearestKey should fail with ErrNoSuchKeyInDict, got %v", err)
	}

	if _, err := rebuildPlainDict(8, []DictItem{{Key: mustDictKey(t, 1, 7), Value: mustBitSlice(t, "1")}}); err == nil {
		t.Fatal("rebuildPlainDict should reject invalid key size")
	}
	if _, err := rebuildPlainDict(8, []DictItem{{Key: mustDictKey(t, 1, 8), Value: nil}}); err == nil {
		t.Fatal("rebuildPlainDict should reject nil values")
	}

	root, err := rebuildPlainDict(8, []DictItem{
		{Key: mustDictKey(t, 1, 8), Value: mustPrefixValue(t, 0xaa, 8).BeginParse()},
		{Key: mustDictKey(t, 2, 8), Value: mustPrefixValue(t, 0xbb, 8).BeginParse()},
	})
	if err != nil || root == nil {
		t.Fatalf("rebuildPlainDict should rebuild a root: root=%v err=%v", root, err)
	}

	dict := NewDict(8)
	if err := dict.Set(mustDictKey(t, 1, 8), mustPrefixValue(t, 0xaa, 8)); err != nil {
		t.Fatal(err)
	}
	if changes, err := dict.Filter(func(_ *Slice, _ *Cell) (DictFilterAction, error) {
		return DictFilterKeep, nil
	}); err != nil || changes != 0 {
		t.Fatalf("keep-only Filter should be a no-op: changes=%d err=%v", changes, err)
	}
	if ok, err := dict.CutPrefixSubdict(BeginCell().MustStoreUInt(0, 9).EndCell(), true); err != nil || ok {
		t.Fatalf("too-long plain prefix cut should return false,nil: ok=%v err=%v", ok, err)
	}

	libCell, err := BeginCell().MustStoreUInt(uint64(LibraryCellType), 8).MustStoreSlice(make([]byte, 32), 256).EndCellSpecial(true)
	if err != nil {
		t.Fatal(err)
	}
	bad := &Dictionary{keySz: 8, root: libCell}
	if ok, err := bad.ValidateCheck(nil, false); err == nil || ok {
		t.Fatalf("ValidateCheck should fail on unsupported special plain dict roots: ok=%v err=%v", ok, err)
	}
	if bad.ValidateAll() {
		t.Fatal("ValidateAll should fail on unsupported special plain dict roots")
	}
}
