package cell

import "testing"

var optionalDictSink *Dictionary

func TestSliceLoadOptionalDict(t *testing.T) {
	emptyCell := BeginCell().MustStoreBoolBit(false).EndCell()
	empty := emptyCell.MustBeginParse()
	dict, err := empty.LoadOptionalDict(32)
	if err != nil {
		t.Fatal(err)
	}
	if dict != nil {
		t.Fatal("empty optional dictionary is non-nil")
	}

	legacy, err := emptyCell.MustBeginParse().LoadDict(32)
	if err != nil {
		t.Fatal(err)
	}
	if legacy == nil || !legacy.IsEmpty() {
		t.Fatal("LoadDict no longer returns its historical allocated empty dictionary")
	}

	want := NewDict(32)
	if err = want.SetBuilderByBytesKey([]byte{0, 0, 0, 7}, BeginCell().MustStoreUInt(0xAB, 8)); err != nil {
		t.Fatal(err)
	}
	encoded := BeginCell().MustStoreDict(want).EndCell()
	got, err := encoded.MustBeginParse().LoadOptionalDict(32)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.AsCell().HashKey() != want.AsCell().HashKey() {
		t.Fatal("present optional dictionary changed root")
	}

	invalid := emptyCell.MustBeginParse()
	bits, refs := invalid.BitsLeft(), invalid.RefsNum()
	if _, err = invalid.LoadOptionalDict(1024); err == nil {
		t.Fatal("invalid optional dictionary key size was accepted")
	}
	if invalid.BitsLeft() != bits || invalid.RefsNum() != refs {
		t.Fatal("invalid key size advanced the source")
	}
}

func BenchmarkSliceLoadEmptyDictionary(b *testing.B) {
	base := *BeginCell().MustStoreBoolBit(false).EndCell().MustBeginParse()

	b.Run("Allocated", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s := base
			dict, err := s.LoadDict(32)
			if err != nil {
				b.Fatal(err)
			}
			optionalDictSink = dict
		}
	})
	b.Run("Optional", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s := base
			dict, err := s.LoadOptionalDict(32)
			if err != nil {
				b.Fatal(err)
			}
			optionalDictSink = dict
		}
	})
}
