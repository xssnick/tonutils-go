package cell

import "testing"

func buildTestLabelSlice(t *testing.T, value uint64, bits uint) *Slice {
	t.Helper()
	return BeginCell().MustStoreUInt(value, bits).ToSlice()
}

func TestMatchLabelPrefix(t *testing.T) {
	tests := []struct {
		name         string
		maxLen       uint
		labelValue   uint64
		labelBits    uint
		keyValue     uint64
		keyBits      uint
		wantLen      uint
		wantMatched  uint
		wantPayload  bool
		checkPayload bool
	}{
		{
			name:         "ShortExactLeavesPayload",
			maxLen:       8,
			labelValue:   0b1,
			labelBits:    1,
			keyValue:     0b1,
			keyBits:      1,
			wantLen:      1,
			wantMatched:  1,
			wantPayload:  true,
			checkPayload: true,
		},
		{
			name:         "LongExactLeavesPayload",
			maxLen:       8,
			labelValue:   0b1010110,
			labelBits:    7,
			keyValue:     0b1010110,
			keyBits:      7,
			wantLen:      7,
			wantMatched:  7,
			wantPayload:  true,
			checkPayload: true,
		},
		{
			name:         "SameExactLeavesPayload",
			maxLen:       5,
			labelValue:   0b11111,
			labelBits:    5,
			keyValue:     0b11111,
			keyBits:      5,
			wantLen:      5,
			wantMatched:  5,
			wantPayload:  true,
			checkPayload: true,
		},
		{
			name:        "SameMismatchInsideLabel",
			maxLen:      5,
			labelValue:  0b11111,
			labelBits:   5,
			keyValue:    0b11110,
			keyBits:     5,
			wantLen:     5,
			wantMatched: 4,
		},
		{
			name:        "SameKeyShorterThanLabel",
			maxLen:      5,
			labelValue:  0b00000,
			labelBits:   5,
			keyValue:    0b0000,
			keyBits:     4,
			wantLen:     5,
			wantMatched: 4,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			builder := BeginCell()
			if err := storeDictLabel(builder, buildTestLabelSlice(t, tc.labelValue, tc.labelBits), tc.maxLen); err != nil {
				t.Fatal(err)
			}
			builder.MustStoreBoolBit(tc.wantPayload)

			loader := builder.EndCell().BeginParse()
			key := BeginCell().MustStoreUInt(tc.keyValue, tc.keyBits).ToSlice()

			gotLen, gotMatched, err := matchLabelPrefix(tc.maxLen, loader, key)
			if err != nil {
				t.Fatal(err)
			}
			if gotLen != tc.wantLen {
				t.Fatalf("unexpected label length: got=%d want=%d", gotLen, tc.wantLen)
			}
			if gotMatched != tc.wantMatched {
				t.Fatalf("unexpected matched length: got=%d want=%d", gotMatched, tc.wantMatched)
			}
			if tc.checkPayload {
				if got := loader.MustLoadBoolBit(); got != tc.wantPayload {
					t.Fatalf("payload bit was not left at loader boundary: got=%v want=%v", got, tc.wantPayload)
				}
			}
		})
	}
}

func TestDictionary_LoadValueSameBitLabel(t *testing.T) {
	dict := NewDict(5)
	key := BeginCell().MustStoreUInt(0b11111, 5).EndCell()
	value := BeginCell().MustStoreUInt(0xAB, 8).EndCell()

	if err := dict.Set(key, value); err != nil {
		t.Fatal(err)
	}

	got, err := dict.LoadValue(key)
	if err != nil {
		t.Fatal(err)
	}
	if got.MustLoadUInt(8) != 0xAB {
		t.Fatalf("unexpected value for same-bit label lookup")
	}

	if _, err = dict.LoadValue(BeginCell().MustStoreUInt(0b11110, 5).EndCell()); err == nil {
		t.Fatal("expected lookup miss on mismatched same-bit label")
	}
}
