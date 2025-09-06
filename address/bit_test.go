package address

import "testing"

func TestClearBit(t *testing.T) {
	type args struct {
		n   *byte
		pos uint
	}
	bytePtr := func(v byte) *byte { b := v; return &b }
	tests := []struct {
		name string
		args args
	}{
		{"0", args{bytePtr(0b00000001), 0}},
		{"1", args{bytePtr(0b00000010), 1}},
		{"2", args{bytePtr(0b00000100), 2}},
		{"3", args{bytePtr(0b00001000), 3}},
		{"4", args{bytePtr(0b00010000), 4}},
		{"5", args{bytePtr(0b00100000), 5}},
		{"6", args{bytePtr(0b01000000), 6}},
		{"7", args{bytePtr(0b10000000), 7}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearBit(tt.args.n, tt.args.pos)
			if *tt.args.n != 0 {
				t.Errorf("ClearBit() = %v, n = %v", tt.name, *tt.args.n)
			}
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
	bytePtr := func(v byte) *byte { b := v; return &b }
	tests := []struct {
		name string
		args args
	}{
		{"0", args{bytePtr(0b00000000), 0}},
		{"1", args{bytePtr(0b00000000), 1}},
		{"2", args{bytePtr(0b00000000), 2}},
		{"3", args{bytePtr(0b00000000), 3}},
		{"4", args{bytePtr(0b00000000), 4}},
		{"5", args{bytePtr(0b00000000), 5}},
		{"6", args{bytePtr(0b00000000), 6}},
		{"7", args{bytePtr(0b00000000), 7}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setBit(tt.args.n, tt.args.pos)
			if *tt.args.n == 0 {
				t.Errorf("ClearBit() = %v, n = %v", tt.name, *tt.args.n)
			}
		})
	}
}
