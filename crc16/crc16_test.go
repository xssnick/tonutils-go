package crc16

import (
	"testing"
)

func TestChecksum(t *testing.T) {
	tests := []struct {
		input    string
		expected uint16
	}{
		{"", 0x0000},
		{"123456789", 0x31C3},
		{"hello", 0xC362},
	}

	for _, tt := range tests {
		got := ChecksumXMODEM([]byte(tt.input))
		if got != tt.expected {
			t.Errorf("ChecksumXMODEM(%q) = 0x%04X, want 0x%04X", tt.input, got, tt.expected)
		}
	}
}
