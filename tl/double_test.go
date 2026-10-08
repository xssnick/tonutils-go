package tl

import (
	"bytes"
	"encoding/hex"
	"math"
	"testing"
)

const doubleFieldsSchema = "test.doubleFields value:double values:(vector double) = test.DoubleFields"

type DoubleFields struct {
	Value  float64   `tl:"double"`
	Values []float64 `tl:"vector double"`
}

func init() {
	Register(DoubleFields{}, doubleFieldsSchema)
}

func TestDoubleRoundTripPreservesBits(t *testing.T) {
	t.Parallel()

	valueBits := uint64(0x41d954fc40012345)
	vectorBits := []uint64{
		0,
		1 << 63,
		0x7ff0000000000000,
		0xfff0000000000000,
		0x7ff8000000000042,
	}
	value := DoubleFields{
		Value:  math.Float64frombits(valueBits),
		Values: make([]float64, len(vectorBits)),
	}
	for i, bits := range vectorBits {
		value.Values[i] = math.Float64frombits(bits)
	}

	expected, err := hex.DecodeString(
		"389f7918" +
			"45230140fc54d941" +
			"05000000" +
			"0000000000000000" +
			"0000000000000080" +
			"000000000000f07f" +
			"000000000000f0ff" +
			"420000000000f87f",
	)
	if err != nil {
		t.Fatalf("decode C++ wire vector: %v", err)
	}

	appended, err := Append(nil, value, true)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if !bytes.Equal(appended, expected) {
		t.Fatalf("append bytes = %x, want %x", appended, expected)
	}

	var buf bytes.Buffer
	serialized, err := Serialize(value, true, &buf)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if !bytes.Equal(serialized, expected) {
		t.Fatalf("serialize bytes = %x, want %x", serialized, expected)
	}

	for _, parse := range []struct {
		name string
		run  func(*DoubleFields, []byte) ([]byte, error)
	}{
		{name: "copy", run: func(dst *DoubleFields, data []byte) ([]byte, error) {
			return Parse(dst, data, true)
		}},
		{name: "no copy", run: func(dst *DoubleFields, data []byte) ([]byte, error) {
			return ParseNoCopy(dst, data, true)
		}},
	} {
		t.Run(parse.name, func(t *testing.T) {
			t.Parallel()

			var decoded DoubleFields
			rest, err := parse.run(&decoded, expected)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(rest) != 0 {
				t.Fatalf("trailing bytes = %d", len(rest))
			}
			if actual := math.Float64bits(decoded.Value); actual != valueBits {
				t.Fatalf("value bits = %#016x, want %#016x", actual, valueBits)
			}
			if len(decoded.Values) != len(vectorBits) {
				t.Fatalf("vector length = %d, want %d", len(decoded.Values), len(vectorBits))
			}
			for i, expectedBits := range vectorBits {
				if actual := math.Float64bits(decoded.Values[i]); actual != expectedBits {
					t.Fatalf("value %d bits = %#016x, want %#016x", i, actual, expectedBits)
				}
			}
		})
	}
}

func TestDoubleRejectsTruncatedInput(t *testing.T) {
	t.Parallel()

	boxed, err := Serialize(DoubleFields{
		Value:  1.25,
		Values: []float64{2.5},
	}, true)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	for _, size := range []int{4, 11, len(boxed) - 1} {
		var decoded DoubleFields
		if _, err := ParseNoCopy(&decoded, boxed[:size], true); err == nil {
			t.Fatalf("accepted truncated input of %d bytes", size)
		}
	}
}
