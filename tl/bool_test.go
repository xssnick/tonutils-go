package tl

import (
	"encoding/binary"
	"testing"
)

func TestAppendBool(t *testing.T) {
	tests := []struct {
		name   string
		value  bool
		schema string
	}{
		{name: "true", value: true, schema: "boolTrue = Bool"},
		{name: "false", value: false, schema: "boolFalse = Bool"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prefix := []byte{1, 2, 3}
			encoded := AppendBool(prefix, test.value)
			if len(encoded) != len(prefix)+4 {
				t.Fatalf("encoded length = %d, want %d", len(encoded), len(prefix)+4)
			}

			if actual := binary.LittleEndian.Uint32(encoded[len(prefix):]); actual != CRC(test.schema) {
				t.Fatalf("constructor id = 0x%08x, want 0x%08x", actual, CRC(test.schema))
			}
		})
	}
}
