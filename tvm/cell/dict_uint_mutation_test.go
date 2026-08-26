package cell

import (
	"errors"
	"testing"
)

func TestDictionaryUintMutationAPIsMatchCellKeys(t *testing.T) {
	const key = uint64(0x1234567890ABCDEF)
	byCell := NewDict(64)
	byUint := NewDict(64)

	value := BeginCell().MustStoreUInt(0xAABBCCDD, 32)
	if err := byCell.Set(BeginCell().MustStoreUInt(key, 64).EndCell(), value.EndCell()); err != nil {
		t.Fatal(err)
	}
	if inserted, err := byUint.SetBuilderByUintKeyWithMode(key, value, DictSetModeAdd); err != nil || !inserted {
		t.Fatalf("uint insert: inserted=%v err=%v", inserted, err)
	}
	if byUint.AsCell().HashKey() != byCell.AsCell().HashKey() {
		t.Fatal("uint-key set differs from cell-key set")
	}
	if inserted, err := byUint.SetBuilderByUintKeyWithMode(key, value, DictSetModeAdd); err != nil || inserted {
		t.Fatalf("duplicate add: inserted=%v err=%v", inserted, err)
	}

	var removed Slice
	if err := byUint.LoadValueAndDeleteByUintKeyInto(key, &removed); err != nil {
		t.Fatal(err)
	}
	if got := removed.MustLoadUInt(32); got != 0xAABBCCDD || !byUint.IsEmpty() {
		t.Fatalf("removed value=%x empty=%v", got, byUint.IsEmpty())
	}
	if err := byUint.LoadValueAndDeleteByUintKeyInto(key, &removed); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("missing uint delete error = %v", err)
	}
}

func TestDictionaryRawKeyLoadDeleteInto(t *testing.T) {
	const key = uint64(7)
	dict := NewDict(64)
	if err := dict.SetBuilderByUintKey(key, BeginCell().MustStoreUInt(0xAB, 8)); err != nil {
		t.Fatal(err)
	}

	keyCell := BeginCell().MustStoreUInt(0xF, 4).MustStoreUInt(key, 64).EndCell()
	keySlice := keyCell.MustBeginParse()
	if err := keySlice.SkipBits(4); err != nil {
		t.Fatal(err)
	}
	bitsBefore := keySlice.BitsLeft()
	var value Slice
	if err := dict.LoadValueAndDeleteBySliceKeyInto(keySlice, &value); err != nil {
		t.Fatal(err)
	}
	if keySlice.BitsLeft() != bitsBefore || value.MustLoadUInt(8) != 0xAB {
		t.Fatal("slice-key delete consumed the key or returned the wrong value")
	}

	if err := dict.SetBuilderByUintKey(key, BeginCell().MustStoreUInt(0xCD, 8)); err != nil {
		t.Fatal(err)
	}
	keyBytes := BeginCell().MustStoreUInt(key, 64).EndCell().MustBeginParse().MustLoadSlice(64)
	if err := dict.LoadValueAndDeleteByBytesKeyInto(keyBytes, &value); err != nil {
		t.Fatal(err)
	}
	if value.MustLoadUInt(8) != 0xCD || !dict.IsEmpty() {
		t.Fatal("byte-key delete returned the wrong value or kept the entry")
	}
}

func TestDictionaryUintMutationRejectsOverflow(t *testing.T) {
	dict := NewDict(4)
	if _, err := dict.SetBuilderByUintKeyWithMode(16, BeginCell(), DictSetModeSet); err == nil {
		t.Fatal("overflowing uint key was accepted")
	}
	if !dict.IsEmpty() {
		t.Fatal("failed uint-key insertion mutated the dictionary")
	}
}
