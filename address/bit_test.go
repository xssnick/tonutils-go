package address

import "testing"

func TestClearBit(t *testing.T) {
	type args struct {
		n   *byte
		pos uint
	}
	tests := []struct {
		name string
		args args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearBit(tt.args.n, tt.args.pos)
		})
	}
}

func TestHasBit(t *testing.T) {
	type args struct {
		n   byte
		pos uint
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{"0", args{0b00000001, 0}, true},
		{"1", args{0b00000010, 1}, true},
		{"2", args{0b00000100, 2}, true},
		{"3", args{0b00001000, 3}, true},
		{"4", args{0b00010000, 4}, true},
		{"5", args{0b00100000, 5}, true},
		{"6", args{0b01000000, 6}, true},
		{"7", args{0b10000000, 7}, true},
		{"0 - false", args{0b00000000, 0}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasBit(tt.args.n, tt.args.pos); got != tt.want {
				t.Errorf("HasBit() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetBit(t *testing.T) {
	type args struct {
		n   *byte
		pos uint
	}
	tests := []struct {
		name string
		args args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setBit(tt.args.n, tt.args.pos)
		})
	}
}
