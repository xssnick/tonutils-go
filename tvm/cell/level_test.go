package cell

import "testing"

// TestLevelMaskLevelClamping pins the convention shared with Cell.Hash,
// Cell.Depth and BOCCellMeta.HashAtLevel: a negative level means the top level,
// and a level above the maximum cannot introduce new significant bits.
func TestLevelMaskLevelClamping(t *testing.T) {
	cases := []struct {
		mask            byte
		level           int
		wantApply       byte
		wantSignificant bool
	}{
		{mask: 0b000, level: -1, wantApply: 0b000, wantSignificant: false},
		{mask: 0b000, level: 0, wantApply: 0b000, wantSignificant: true},
		{mask: 0b000, level: 1, wantApply: 0b000, wantSignificant: false},
		{mask: 0b000, level: 3, wantApply: 0b000, wantSignificant: false},
		{mask: 0b000, level: 5, wantApply: 0b000, wantSignificant: false},

		{mask: 0b010, level: -1, wantApply: 0b010, wantSignificant: false},
		{mask: 0b010, level: 0, wantApply: 0b000, wantSignificant: true},
		{mask: 0b010, level: 1, wantApply: 0b000, wantSignificant: false},
		{mask: 0b010, level: 3, wantApply: 0b010, wantSignificant: false},
		{mask: 0b010, level: 5, wantApply: 0b010, wantSignificant: false},

		{mask: 0b101, level: -1, wantApply: 0b101, wantSignificant: true},
		{mask: 0b101, level: 0, wantApply: 0b000, wantSignificant: true},
		{mask: 0b101, level: 1, wantApply: 0b001, wantSignificant: true},
		{mask: 0b101, level: 3, wantApply: 0b101, wantSignificant: true},
		{mask: 0b101, level: 5, wantApply: 0b101, wantSignificant: false},

		{mask: 0b111, level: -1, wantApply: 0b111, wantSignificant: true},
		{mask: 0b111, level: 0, wantApply: 0b000, wantSignificant: true},
		{mask: 0b111, level: 1, wantApply: 0b001, wantSignificant: true},
		{mask: 0b111, level: 3, wantApply: 0b111, wantSignificant: true},
		{mask: 0b111, level: 5, wantApply: 0b111, wantSignificant: false},
	}

	for _, tc := range cases {
		mask := LevelMask{Mask: tc.mask}
		if got := mask.Apply(tc.level); got.Mask != tc.wantApply {
			t.Errorf("LevelMask{%03b}.Apply(%d) = %03b, want %03b", tc.mask, tc.level, got.Mask, tc.wantApply)
		}
		if got := mask.IsSignificant(tc.level); got != tc.wantSignificant {
			t.Errorf("LevelMask{%03b}.IsSignificant(%d) = %t, want %t", tc.mask, tc.level, got, tc.wantSignificant)
		}
	}
}

// TestLevelMaskNegativeLevelMatchesMaxLevel states the clamp itself, so the
// convention survives a change of _DataCellMaxLevel.
func TestLevelMaskNegativeLevelMatchesMaxLevel(t *testing.T) {
	for raw := 0; raw <= 0b111; raw++ {
		mask := LevelMask{Mask: byte(raw)}
		for _, level := range []int{-1, -3, -100} {
			if got, want := mask.Apply(level), mask.Apply(_DataCellMaxLevel); got != want {
				t.Errorf("LevelMask{%03b}.Apply(%d) = %03b, want top level %03b", raw, level, got.Mask, want.Mask)
			}
			if got, want := mask.IsSignificant(level), mask.IsSignificant(_DataCellMaxLevel); got != want {
				t.Errorf("LevelMask{%03b}.IsSignificant(%d) = %t, want top level %t", raw, level, got, want)
			}
		}
	}
}
