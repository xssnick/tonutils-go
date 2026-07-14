package cell

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"testing"
)

func TestDictIteratorParityResetAndSignedOrder(t *testing.T) {
	dict := NewDict(8)
	for _, key := range []uint64{0x00, 0x10, 0x7f, 0x80, 0x81, 0xf0, 0xff} {
		if err := dict.Set(mustDictKey(t, key, 8), mustDictKey(t, key^0xff, 8)); err != nil {
			t.Fatal(err)
		}
	}

	for _, rev := range []bool{false, true} {
		for _, signed := range []bool{false, true} {
			t.Run(fmt.Sprintf("rev=%v/signed=%v", rev, signed), func(t *testing.T) {
				want, err := dict.Range(rev, signed)
				if err != nil {
					t.Fatal(err)
				}
				it, err := dict.Iterator(rev, signed)
				if err != nil {
					t.Fatal(err)
				}

				collect := func() []uint64 {
					keys := make([]uint64, 0, len(want))
					for it.Next() {
						keys = append(keys, it.Key().MustBeginParse().MustLoadUInt(8))
					}
					if err := it.Err(); err != nil {
						t.Fatal(err)
					}
					return keys
				}

				wantKeys := mustCollectDictKeys(t, want, 8)
				if got := collect(); !equalUint64Slices(got, wantKeys) {
					t.Fatalf("iterator order %v, want %v", got, wantKeys)
				}
				if it.Key() == nil || it.Key().MustBeginParse().MustLoadUInt(8) != wantKeys[len(wantKeys)-1] {
					t.Fatal("iterator did not preserve its last item after exhaustion")
				}
				it.Reset()
				if got := collect(); !equalUint64Slices(got, wantKeys) {
					t.Fatalf("iterator order after reset %v, want %v", got, wantKeys)
				}
			})
		}
	}
}

func TestDictionaryStructuralFilterActionsAndAtomicity(t *testing.T) {
	tests := []struct {
		name      string
		action    func(uint64) DictFilterAction
		wantKeys  []uint64
		wantCalls int
		changes   int
	}{
		{
			name: "keep",
			action: func(uint64) DictFilterAction {
				return DictFilterKeep
			},
			wantKeys: []uint64{0, 1, 2, 3, 4, 5, 6, 7}, wantCalls: 8,
		},
		{
			name: "remove one",
			action: func(key uint64) DictFilterAction {
				if key == 3 {
					return DictFilterRemove
				}
				return DictFilterKeep
			},
			wantKeys: []uint64{0, 1, 2, 4, 5, 6, 7}, wantCalls: 8, changes: 1,
		},
		{
			name: "keep rest",
			action: func(key uint64) DictFilterAction {
				if key == 3 {
					return DictFilterKeepRest
				}
				return DictFilterKeep
			},
			wantKeys: []uint64{0, 1, 2, 3, 4, 5, 6, 7}, wantCalls: 4,
		},
		{
			name: "remove rest",
			action: func(key uint64) DictFilterAction {
				if key == 3 {
					return DictFilterRemoveRest
				}
				return DictFilterKeep
			},
			wantKeys: []uint64{0, 1, 2}, wantCalls: 4, changes: 5,
		},
		{
			name: "remove then keep rest",
			action: func(key uint64) DictFilterAction {
				switch key {
				case 1:
					return DictFilterRemove
				case 3:
					return DictFilterKeepRest
				default:
					return DictFilterKeep
				}
			},
			wantKeys: []uint64{0, 2, 3, 4, 5, 6, 7}, wantCalls: 4, changes: 1,
		},
		{
			name: "remove then remove rest",
			action: func(key uint64) DictFilterAction {
				switch key {
				case 1:
					return DictFilterRemove
				case 3:
					return DictFilterRemoveRest
				default:
					return DictFilterKeep
				}
			},
			wantKeys: []uint64{0, 2}, wantCalls: 4, changes: 6,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dict := newSequentialTestDict(t, 8)
			calls := 0
			changes, err := dict.Filter(func(_ *Slice, key *Cell) (DictFilterAction, error) {
				calls++
				return tc.action(key.MustBeginParse().MustLoadUInt(8)), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if changes != tc.changes || calls != tc.wantCalls {
				t.Fatalf("changes=%d calls=%d, want %d/%d", changes, calls, tc.changes, tc.wantCalls)
			}
			items, err := dict.Range(false, false)
			if err != nil {
				t.Fatal(err)
			}
			if got := mustCollectDictKeys(t, items, 8); !equalUint64Slices(got, tc.wantKeys) {
				t.Fatalf("keys=%v, want %v", got, tc.wantKeys)
			}
			if !dict.ValidateAll() {
				t.Fatal("filtered dictionary is invalid")
			}
		})
	}

	dict := newSequentialTestDict(t, 8)
	original := dict.root
	originalHash := original.HashKey()
	wantErr := errors.New("stop")
	_, err := dict.Filter(func(_ *Slice, key *Cell) (DictFilterAction, error) {
		if key.MustBeginParse().MustLoadUInt(8) == 2 {
			return DictFilterKeep, wantErr
		}
		return DictFilterRemove, nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected callback error, got %v", err)
	}
	if dict.root != original || dict.root.HashKey() != originalHash {
		t.Fatal("filter callback error mutated dictionary")
	}
}

func TestDictionaryStructuralFilterSharesUntouchedBranch(t *testing.T) {
	dict := NewDict(8)
	for _, key := range []uint64{0x10, 0x11, 0x80, 0x81} {
		if err := dict.Set(mustDictKey(t, key, 8), mustDictKey(t, key, 8)); err != nil {
			t.Fatal(err)
		}
	}
	originalRight, err := dict.root.PeekRef(1)
	if err != nil {
		t.Fatal(err)
	}

	changes, err := dict.Filter(func(_ *Slice, key *Cell) (DictFilterAction, error) {
		if key.MustBeginParse().MustLoadUInt(8) == 0x10 {
			return DictFilterRemove, nil
		}
		return DictFilterKeep, nil
	})
	if err != nil || changes != 1 {
		t.Fatalf("filter failed: changes=%d err=%v", changes, err)
	}
	newRight, err := dict.root.PeekRef(1)
	if err != nil {
		t.Fatal(err)
	}
	if newRight != originalRight {
		t.Fatal("untouched right subtree was rebuilt")
	}
}

func TestAugmentedDictionaryStructuralFilterActionsAndRefExtra(t *testing.T) {
	aug := refCountAugmentation{}
	dict, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}
	for key := uint64(0); key < 8; key++ {
		if err = dict.SetIntKey(new(big.Int).SetUint64(key), mustDictKey(t, key+10, 8)); err != nil {
			t.Fatal(err)
		}
	}

	calls := 0
	changes, err := dict.Filter(func(value, extra *Slice, key *Cell) (DictFilterAction, error) {
		calls++
		extraCopy := *extra
		if got := extraCopy.MustLoadUInt(8); got != 1 {
			t.Fatalf("leaf extra count=%d", got)
		}
		if _, err := extraCopy.LoadRefCell(); err != nil {
			t.Fatalf("leaf extra ref: %v", err)
		}
		keyValue := key.MustBeginParse().MustLoadUInt(8)
		switch keyValue {
		case 1:
			return DictFilterRemove, nil
		case 3:
			return DictFilterKeepRest, nil
		default:
			return DictFilterKeep, nil
		}
	})
	if err != nil || changes != 1 || calls != 4 {
		t.Fatalf("filter failed: changes=%d calls=%d err=%v", changes, calls, err)
	}
	if !dict.ValidateAll() {
		t.Fatal("filtered augmented dictionary is invalid")
	}
	items, err := dict.RangeExtra(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCollectAugKeys(t, items, 8); !equalUint64Slices(got, []uint64{0, 2, 3, 4, 5, 6, 7}) {
		t.Fatalf("keys=%v", got)
	}
	rootExtra, err := dict.LoadRootExtra()
	if err != nil {
		t.Fatal(err)
	}
	if got := rootExtra.MustLoadUInt(8); got != 7 {
		t.Fatalf("root extra count=%d, want 7", got)
	}
	if _, err = rootExtra.LoadRefCell(); err != nil {
		t.Fatalf("root extra ref: %v", err)
	}

	original := dict.root
	wantErr := errors.New("stop")
	if _, err = dict.Filter(func(_ *Slice, _ *Slice, key *Cell) (DictFilterAction, error) {
		if key.MustBeginParse().MustLoadUInt(8) == 2 {
			return DictFilterKeep, wantErr
		}
		return DictFilterRemove, nil
	}); !errors.Is(err, wantErr) {
		t.Fatalf("expected callback error, got %v", err)
	}
	if dict.root != original {
		t.Fatal("augmented filter callback error mutated dictionary")
	}
}

func TestAugmentedDictionaryIteratorAndFilterAllActions(t *testing.T) {
	for _, rev := range []bool{false, true} {
		for _, signed := range []bool{false, true} {
			dict := newSequentialAugDict(t, 8)
			want, err := dict.RangeExtra(rev, signed)
			if err != nil {
				t.Fatal(err)
			}
			it, err := dict.IteratorExtra(rev, signed)
			if err != nil {
				t.Fatal(err)
			}
			var got []uint64
			for it.Next() {
				got = append(got, it.Key().MustBeginParse().MustLoadUInt(8))
				if it.Value() == nil || it.Extra() == nil {
					t.Fatal("iterator returned incomplete augmented item")
				}
			}
			if err = it.Err(); err != nil {
				t.Fatal(err)
			}
			if expected := mustCollectAugKeys(t, want, 8); !equalUint64Slices(got, expected) {
				t.Fatalf("rev=%v signed=%v got=%v want=%v", rev, signed, got, expected)
			}
			it.Reset()
			if !it.Next() || it.Key().MustBeginParse().MustLoadUInt(8) != mustCollectAugKeys(t, want, 8)[0] {
				t.Fatal("augmented iterator reset failed")
			}
		}
	}

	tests := []struct {
		name     string
		action   DictFilterAction
		wantKeys []uint64
		changes  int
		calls    int
	}{
		{name: "keep", action: DictFilterKeep, wantKeys: []uint64{0, 1, 2, 3, 4, 5, 6, 7}, calls: 8},
		{name: "remove", action: DictFilterRemove, wantKeys: []uint64{0, 1, 2, 4, 5, 6, 7}, changes: 1, calls: 8},
		{name: "keep rest", action: DictFilterKeepRest, wantKeys: []uint64{0, 1, 2, 3, 4, 5, 6, 7}, calls: 4},
		{name: "remove rest", action: DictFilterRemoveRest, wantKeys: []uint64{0, 1, 2}, changes: 5, calls: 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dict := newSequentialAugDict(t, 8)
			originalRight, err := dict.root.PeekRef(1)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			changes, err := dict.Filter(func(_ *Slice, _ *Slice, key *Cell) (DictFilterAction, error) {
				calls++
				if key.MustBeginParse().MustLoadUInt(8) == 3 {
					return tc.action, nil
				}
				return DictFilterKeep, nil
			})
			if err != nil || changes != tc.changes || calls != tc.calls {
				t.Fatalf("changes=%d calls=%d err=%v", changes, calls, err)
			}
			items, err := dict.RangeExtra(false, false)
			if err != nil {
				t.Fatal(err)
			}
			if got := mustCollectAugKeys(t, items, 8); !equalUint64Slices(got, tc.wantKeys) {
				t.Fatalf("keys=%v want=%v", got, tc.wantKeys)
			}
			if !dict.ValidateAll() {
				t.Fatal("filtered augmented dictionary is invalid")
			}
			if tc.action == DictFilterRemove {
				newRight, err := dict.root.PeekRef(1)
				if err != nil {
					t.Fatal(err)
				}
				if newRight != originalRight {
					t.Fatal("untouched augmented subtree was rebuilt")
				}
			}
		})
	}
}

func TestDictionaryLoadMinMaxAndDeleteAllOrders(t *testing.T) {
	for _, fetchMax := range []bool{false, true} {
		for _, invertFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("max=%v/invert=%v", fetchMax, invertFirst), func(t *testing.T) {
				dict := NewDict(8)
				for _, key := range []uint64{0x01, 0x20, 0x7f, 0x80, 0xa0, 0xff} {
					if err := dict.Set(mustDictKey(t, key, 8), mustDictKey(t, key^0xff, 8)); err != nil {
						t.Fatal(err)
					}
				}
				want, err := dict.Range(fetchMax, invertFirst)
				if err != nil {
					t.Fatal(err)
				}
				for i, item := range want {
					key, value, err := dict.LoadMinMaxAndDelete(fetchMax, invertFirst)
					if err != nil {
						t.Fatalf("delete %d: %v", i, err)
					}
					if compareKeyCells(key, item.Key, false) != 0 {
						t.Fatalf("delete %d key mismatch", i)
					}
					wantValue := key.MustBeginParse().MustLoadUInt(8) ^ 0xff
					if got := value.MustLoadUInt(8); got != wantValue {
						t.Fatalf("delete %d value=%x want=%x", i, got, wantValue)
					}
				}
				if !dict.IsEmpty() {
					t.Fatal("dictionary is not empty")
				}
				if _, _, err = dict.LoadMinMaxAndDelete(fetchMax, invertFirst); !errors.Is(err, ErrNoSuchKeyInDict) {
					t.Fatalf("empty delete error=%v", err)
				}
			})
		}
	}
}

func TestDictionaryLookupNearestWideKeys(t *testing.T) {
	for _, keyBits := range []uint{256, 1023} {
		t.Run(fmt.Sprintf("bits=%d", keyBits), func(t *testing.T) {
			dict := NewDict(keyBits)
			keys := make([]*Cell, 0, 16)
			positions := []uint{keyBits, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14}
			for i, position := range positions {
				key := wideTransitionKey(position, keyBits)
				keys = append(keys, key)
				if err := dict.Set(key, mustDictKey(t, uint64(i+1), 8)); err != nil {
					t.Fatal(err)
				}
			}

			queries := append([]*Cell(nil), keys...)
			for _, position := range []uint{15, 16, 31, 63} {
				queries = append(queries, wideTransitionKey(position, keyBits))
			}
			for _, invertFirst := range []bool{false, true} {
				ordered := append([]*Cell(nil), keys...)
				sort.Slice(ordered, func(i, j int) bool {
					return compareKeyCells(ordered[i], ordered[j], invertFirst) < 0
				})
				for _, query := range queries {
					for _, fetchNext := range []bool{false, true} {
						for _, allowEq := range []bool{false, true} {
							want := nearestByScan(ordered, query, fetchNext, allowEq, invertFirst)
							got, _, err := dict.LookupNearestKey(query, fetchNext, allowEq, invertFirst)
							if want == nil {
								if !errors.Is(err, ErrNoSuchKeyInDict) {
									t.Fatalf("expected miss, got %v", err)
								}
								continue
							}
							if err != nil || compareKeyCells(got, want, false) != 0 {
								t.Fatalf("nearest mismatch: next=%v eq=%v signed=%v err=%v", fetchNext, allowEq, invertFirst, err)
							}
						}
					}
				}
			}
		})
	}
}

func TestStoreDictLabelUnalignedFormsConsumeInput(t *testing.T) {
	tests := []struct {
		name   string
		bits   string
		keyLen uint
	}{
		{name: "empty", keyLen: 32},
		{name: "short", bits: "101", keyLen: 32},
		{name: "same", bits: "11111111111111111111", keyLen: 64},
		{name: "long", bits: "10100101101001011010", keyLen: 1023},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			carrier := BeginCell().MustStoreUInt(0b101, 3)
			for _, bit := range tc.bits {
				carrier.MustStoreBoolBit(bit == '1')
			}
			label := carrier.ToSlice()
			if err := label.SkipBits(3); err != nil {
				t.Fatal(err)
			}
			out := BeginCell()
			if err := storeDictLabel(out, label, tc.keyLen); err != nil {
				t.Fatal(err)
			}
			if label.BitsLeft() != 0 {
				t.Fatalf("label left %d bits", label.BitsLeft())
			}
			loader := out.EndCell().MustBeginParse()
			length, got, err := readLabelView(tc.keyLen, loader)
			if err != nil {
				t.Fatal(err)
			}
			if length != uint(len(tc.bits)) || sliceBitString(got) != tc.bits {
				t.Fatalf("decoded %q/%d, want %q", sliceBitString(got), length, tc.bits)
			}
		})
	}
}

func TestBuilderSnakeAtomicBoundariesAndPrefixIntKeys(t *testing.T) {
	for _, prefixBits := range []uint{0, 3, 1015} {
		first := int((1023 - prefixBits) / 8)
		for _, size := range []int{0, first, first + 1, first + 127, first + 128, first + 400} {
			data := bytes.Repeat([]byte{byte(size + 1)}, size)
			builder := BeginCell()
			if err := builder.StoreSameBit(false, prefixBits); err != nil {
				t.Fatal(err)
			}
			if err := builder.StoreBinarySnake(data); err != nil {
				t.Fatalf("prefix=%d size=%d: %v", prefixBits, size, err)
			}
			loader := builder.EndCell().MustBeginParse()
			if err := loader.SkipBits(prefixBits); err != nil {
				t.Fatal(err)
			}
			got, err := loader.LoadBinarySnake()
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("prefix=%d size=%d: got %d bytes err=%v", prefixBits, size, len(got), err)
			}
		}
	}

	fullRefs := BeginCell()
	for i := 0; i < 4; i++ {
		fullRefs.MustStoreRef(BeginCell().EndCell())
	}
	before := *fullRefs
	if err := fullRefs.StoreBinarySnake(make([]byte, 128)); !errors.Is(err, ErrTooMuchRefs) {
		t.Fatalf("expected ErrTooMuchRefs, got %v", err)
	}
	if *fullRefs != before {
		t.Fatal("failed snake store mutated builder")
	}

	prefix := NewPrefixDict(16)
	if err := prefix.SetIntKey(big.NewInt(7), mustDictKey(t, 0xaa, 8)); err != nil {
		t.Fatal(err)
	}
	value, err := prefix.LoadValueByIntKey(big.NewInt(7))
	if err != nil || value.MustLoadUInt(8) != 0xaa {
		t.Fatalf("prefix int-key load failed: %v", err)
	}
	if err = prefix.DeleteIntKey(big.NewInt(7)); err != nil {
		t.Fatal(err)
	}
}

func FuzzDictionaryStructuralFilterParity(f *testing.F) {
	f.Add([]byte{1, 2, 3, 4, 5}, byte(2), byte(DictFilterRemove))
	f.Add([]byte{0, 127, 128, 255}, byte(128), byte(DictFilterRemoveRest))
	f.Fuzz(func(t *testing.T, input []byte, pivot byte, rawAction byte) {
		if len(input) > 64 {
			input = input[:64]
		}
		keysMap := make(map[byte]struct{}, len(input))
		for _, key := range input {
			keysMap[key] = struct{}{}
		}
		keys := make([]int, 0, len(keysMap))
		for key := range keysMap {
			keys = append(keys, int(key))
		}
		sort.Ints(keys)

		dict := NewDict(8)
		augDict, err := NewAugDict(8, testMetricAugmentation{})
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range keys {
			cellKey := mustDictKey(t, uint64(key), 8)
			value := mustDictKey(t, uint64(key), 8)
			if err = dict.Set(cellKey, value); err != nil {
				t.Fatal(err)
			}
			if err = augDict.Set(cellKey, value); err != nil {
				t.Fatal(err)
			}
		}

		action := DictFilterAction(rawAction % 4)
		model := append([]int(nil), keys...)
		for i, key := range model {
			if key != int(pivot) {
				continue
			}
			switch action {
			case DictFilterRemove:
				model = append(model[:i], model[i+1:]...)
			case DictFilterRemoveRest:
				model = model[:i]
			}
			break
		}
		predicate := func(key *Cell) DictFilterAction {
			if key.MustBeginParse().MustLoadUInt(8) == uint64(pivot) {
				return action
			}
			return DictFilterKeep
		}
		if _, err = dict.Filter(func(_ *Slice, key *Cell) (DictFilterAction, error) { return predicate(key), nil }); err != nil {
			t.Fatal(err)
		}
		if _, err = augDict.Filter(func(_ *Slice, _ *Slice, key *Cell) (DictFilterAction, error) { return predicate(key), nil }); err != nil {
			t.Fatal(err)
		}
		want := make([]uint64, len(model))
		for i := range model {
			want[i] = uint64(model[i])
		}
		plainItems, err := dict.Range(false, false)
		if err != nil {
			t.Fatal(err)
		}
		augItems, err := augDict.Range(false, false)
		if err != nil {
			t.Fatal(err)
		}
		if got := mustCollectDictKeys(t, plainItems, 8); !equalUint64Slices(got, want) {
			t.Fatalf("plain keys=%v want=%v", got, want)
		}
		if got := mustCollectDictKeys(t, augItems, 8); !equalUint64Slices(got, want) {
			t.Fatalf("aug keys=%v want=%v", got, want)
		}
		if !dict.ValidateAll() || !augDict.ValidateAll() {
			t.Fatal("filtered dictionary is invalid")
		}
	})
}

func FuzzAugmentedDictionaryCombineParity(f *testing.F) {
	f.Add([]byte{0, 2, 4}, []byte{1, 3, 5})
	f.Add([]byte{1, 2}, []byte{2, 3})
	f.Fuzz(func(t *testing.T, leftInput, rightInput []byte) {
		if len(leftInput) > 32 {
			leftInput = leftInput[:32]
		}
		if len(rightInput) > 32 {
			rightInput = rightInput[:32]
		}
		aug := testMetricAugmentation{}
		left, err := NewAugDict(8, aug)
		if err != nil {
			t.Fatal(err)
		}
		right, err := NewAugDict(8, aug)
		if err != nil {
			t.Fatal(err)
		}
		leftSet := map[byte]struct{}{}
		conflict := false
		for _, key := range leftInput {
			leftSet[key] = struct{}{}
			if err = left.Set(mustDictKey(t, uint64(key), 8), mustDictKey(t, uint64(key), 8)); err != nil {
				t.Fatal(err)
			}
		}
		for _, key := range rightInput {
			if _, ok := leftSet[key]; ok {
				conflict = true
			}
			if err = right.Set(mustDictKey(t, uint64(key), 8), mustDictKey(t, uint64(key), 8)); err != nil {
				t.Fatal(err)
			}
		}
		ok, err := left.CombineWith(right)
		if err != nil {
			t.Fatal(err)
		}
		if ok == conflict {
			t.Fatalf("combine ok=%v conflict=%v", ok, conflict)
		}
		if ok && !left.ValidateAll() {
			t.Fatal("combined dictionary is invalid")
		}
	})
}

type refCountAugmentation struct{}

func (refCountAugmentation) SkipExtra(loader *Slice) error {
	if _, err := loader.LoadUInt(8); err != nil {
		return err
	}
	_, err := loader.LoadRefCell()
	return err
}

func (refCountAugmentation) EmptyExtra() (*Cell, error) {
	return refCountExtra(0), nil
}

func (refCountAugmentation) LeafExtra(*Slice) (*Cell, error) {
	return refCountExtra(1), nil
}

func (refCountAugmentation) CombineExtra(left, right *Slice) (*Cell, error) {
	leftCount, err := left.LoadUInt(8)
	if err != nil {
		return nil, err
	}
	if _, err = left.LoadRefCell(); err != nil {
		return nil, err
	}
	rightCount, err := right.LoadUInt(8)
	if err != nil {
		return nil, err
	}
	if _, err = right.LoadRefCell(); err != nil {
		return nil, err
	}
	return refCountExtra(leftCount + rightCount), nil
}

func refCountExtra(count uint64) *Cell {
	return BeginCell().MustStoreUInt(count, 8).MustStoreRef(BeginCell().MustStoreUInt(0xaa, 8).EndCell()).EndCell()
}

func newSequentialTestDict(t *testing.T, count int) *Dictionary {
	t.Helper()
	dict := NewDict(8)
	for key := 0; key < count; key++ {
		if err := dict.Set(mustDictKey(t, uint64(key), 8), mustDictKey(t, uint64(key+10), 8)); err != nil {
			t.Fatal(err)
		}
	}
	return dict
}

func newSequentialAugDict(t *testing.T, count int) *AugmentedDictionary {
	t.Helper()
	dict, err := NewAugDict(8, testMetricAugmentation{})
	if err != nil {
		t.Fatal(err)
	}
	for key := 0; key < count; key++ {
		if err = dict.Set(mustDictKey(t, uint64(key), 8), mustDictKey(t, uint64(key+10), 8)); err != nil {
			t.Fatal(err)
		}
	}
	return dict
}

func wideTransitionKey(position, bits uint) *Cell {
	position = min(position, bits)
	builder := BeginCell()
	if err := builder.StoreSameBit(false, position); err != nil {
		panic(err)
	}
	if err := builder.StoreSameBit(true, bits-position); err != nil {
		panic(err)
	}
	return builder.EndCell()
}

func sliceBitString(slice Slice) string {
	var out bytes.Buffer
	for slice.BitsLeft() > 0 {
		if slice.MustLoadBoolBit() {
			out.WriteByte('1')
		} else {
			out.WriteByte('0')
		}
	}
	return out.String()
}
