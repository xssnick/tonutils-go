package tlb

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

type testArrayPayload struct {
	ID    uint8  `tlb:"## 8"`
	Label string `tlb:"string"`
}

type testArrayEmptyPayload struct{}

type testScalarArrayTags struct {
	Numbers []uint16 `tlb:"array ## 16"`
	Flags   []bool   `tlb:"array bool"`
	Bytes   [][]byte `tlb:"array bits 8"`
	Labels  []string `tlb:"array string"`
}

type testNestedArrayTags struct {
	Nested [][]uint8          `tlb:"array array ## 8"`
	Inline []testArrayPayload `tlb:"array ."`
	Refs   []testArrayPayload `tlb:"array ^"`
}

type benchUint8Array struct {
	Values []uint8 `tlb:"array ## 8"`
}

func TestArrayTagRoundTrip(t *testing.T) {
	scalars := testScalarArrayTags{
		Numbers: []uint16{1, 2, 65535},
		Flags:   []bool{true, false, true},
		Bytes:   [][]byte{{0xAA}, {0x00}, {0xFF}},
		Labels:  []string{"alpha", "beta", "gamma", "delta", "epsilon"},
	}

	c, err := ToCell(scalars)
	if err != nil {
		t.Fatal(err)
	}

	var scalarDst testScalarArrayTags
	if err = LoadFromCell(&scalarDst, c.MustBeginParse()); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(scalarDst, scalars) {
		t.Fatalf("scalar array round trip mismatch:\n got: %#v\nwant: %#v", scalarDst, scalars)
	}

	nested := testNestedArrayTags{
		Nested: [][]uint8{{1, 2}, {}, {3, 4, 5}},
		Inline: []testArrayPayload{
			{ID: 1, Label: "one"},
			{ID: 2, Label: "two"},
		},
		Refs: []testArrayPayload{
			{ID: 3, Label: "three"},
			{ID: 4, Label: "four"},
		},
	}

	c, err = ToCell(nested)
	if err != nil {
		t.Fatal(err)
	}

	var dst testNestedArrayTags
	if err = LoadFromCell(&dst, c.MustBeginParse()); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(dst, nested) {
		t.Fatalf("nested array round trip mismatch:\n got: %#v\nwant: %#v", dst, nested)
	}
}

func TestArrayTagEmptyLayout(t *testing.T) {
	type emptyArray struct {
		Values []uint8 `tlb:"array ## 8"`
	}

	c, err := ToCell(emptyArray{})
	if err != nil {
		t.Fatal(err)
	}

	if c.BitsSize() != 9 || c.RefsNum() != 0 {
		t.Fatalf("empty array should be 9 bits and no refs, got %d bits and %d refs", c.BitsSize(), c.RefsNum())
	}

	loader := c.MustBeginParse()
	ln, err := loader.LoadUInt(8)
	if err != nil {
		t.Fatal(err)
	}
	hasRef, err := loader.LoadBoolBit()
	if err != nil {
		t.Fatal(err)
	}
	if ln != 0 || hasRef {
		t.Fatalf("unexpected empty array layout: len=%d hasRef=%v", ln, hasRef)
	}
}

func TestArrayTagChunkingRoundTrip(t *testing.T) {
	type chunkedArray struct {
		Values []uint8 `tlb:"array ## 8"`
	}

	values := make([]uint8, 200)
	for i := range values {
		values[i] = uint8(i)
	}

	c, err := ToCell(chunkedArray{Values: values})
	if err != nil {
		t.Fatal(err)
	}

	root := c.MustBeginParse()
	root.MustLoadUInt(8)
	if hasRef := root.MustLoadBoolBit(); !hasRef {
		t.Fatal("non-empty array should have data ref")
	}
	first := root.MustLoadRef()
	firstChunk := first.Copy()
	if hasNext := firstChunk.MustLoadBoolBit(); !hasNext {
		t.Fatal("200 uint8 values should be split into multiple chunks")
	}
	if firstChunk.RefsNum() != 1 {
		t.Fatalf("first chunk should reserve one next ref, got %d refs", firstChunk.RefsNum())
	}

	var dst chunkedArray
	if err = LoadFromCell(&dst, c.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dst.Values, values) {
		t.Fatalf("chunked values mismatch")
	}
}

func TestArrayTagRefElementsReserveNextRef(t *testing.T) {
	type stringArray struct {
		Values []string `tlb:"array string"`
	}

	src := stringArray{
		Values: []string{"one", "two", "three", "four", "five"},
	}
	c, err := ToCell(src)
	if err != nil {
		t.Fatal(err)
	}

	root := c.MustBeginParse()
	root.MustLoadUInt(8)
	root.MustLoadBoolBit()
	first := root.MustLoadRef()
	firstChunk := first.Copy()
	if hasNext := firstChunk.MustLoadBoolBit(); !hasNext {
		t.Fatal("five string refs should be split into multiple chunks")
	}
	if firstChunk.RefsNum() != 2 {
		t.Fatalf("first chunk should contain one string ref and one next ref, got %d refs", firstChunk.RefsNum())
	}

	var dst stringArray
	if err = LoadFromCell(&dst, c.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dst, src) {
		t.Fatalf("string array mismatch: got %#v want %#v", dst, src)
	}
}

func TestArrayTagRefChunkLayouts(t *testing.T) {
	type arrayMessage struct {
		Mode    uint8      `tlb:"## 8"`
		Message *cell.Cell `tlb:"^"`
	}
	type messageArray struct {
		Messages []arrayMessage `tlb:"array ."`
	}
	type tolkMessageArray struct {
		Messages []arrayMessage `tlb:"tolk_array ."`
	}

	for _, count := range []int{5, 7} {
		messages := make([]arrayMessage, count)
		for i := range messages {
			messages[i] = arrayMessage{
				Mode:    uint8(i + 1),
				Message: cell.BeginCell().MustStoreUInt(uint64(100+i), 32).EndCell(),
			}
		}
		var legacyDst messageArray
		var tolkDst tolkMessageArray
		formats := []struct {
			name    string
			tolk    bool
			src     any
			dst     any
			decoded *[]arrayMessage
		}{
			{"legacy", false, messageArray{Messages: messages}, &legacyDst, &legacyDst.Messages},
			{"tolk", true, tolkMessageArray{Messages: messages}, &tolkDst, &tolkDst.Messages},
		}
		for _, format := range formats {
			t.Run(format.name+"/"+fmt.Sprint(count), func(t *testing.T) {
				// Both layouts put four message refs in the final chunk. The
				// preceding chunk has one or three message refs and the next ref.
				last := cell.BeginCell().MustStoreBoolBit(false)
				for _, msg := range messages[count-4:] {
					last.MustStoreUInt(uint64(msg.Mode), 8).MustStoreRef(msg.Message)
				}
				next := last.EndCell()
				first := cell.BeginCell().MustStoreBoolBit(true)
				if format.tolk {
					first.MustStoreRef(next)
				}
				for _, msg := range messages[:count-4] {
					first.MustStoreUInt(uint64(msg.Mode), 8).MustStoreRef(msg.Message)
				}
				if !format.tolk {
					first.MustStoreRef(next)
				}
				expected := cell.BeginCell().
					MustStoreUInt(uint64(count), 8).
					MustStoreBoolBit(true).
					MustStoreRef(first.EndCell()).
					EndCell()

				encoded, err := ToCell(format.src)
				if err != nil {
					t.Fatal(err)
				}
				if encoded.HashKey() != expected.HashKey() {
					t.Fatal("array encoding does not match expected chunk layout")
				}

				if err = LoadFromCell(format.dst, expected.MustBeginParse()); err != nil {
					t.Fatal(err)
				}
				if len(*format.decoded) != len(messages) {
					t.Fatalf("decoded %d messages, want %d", len(*format.decoded), len(messages))
				}
				for i, msg := range *format.decoded {
					want := messages[i]
					if msg.Mode != want.Mode || msg.Message.HashKey() != want.Message.HashKey() {
						t.Fatalf("message %d does not match its original mode and cell", i)
					}
				}
			})
		}
	}
}

func TestArrayTagNestedTolkRoundTrip(t *testing.T) {
	type nestedArray struct {
		Values [][]uint8 `tlb:"tolk_array tolk_array ## 8"`
	}

	src := nestedArray{Values: [][]uint8{{1, 2}, {}, {3, 4, 5}, {6}, {7, 8}}}
	c, err := ToCell(src)
	if err != nil {
		t.Fatal(err)
	}
	var dst nestedArray
	if err = LoadFromCell(&dst, c.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dst, src) {
		t.Fatalf("nested Tolk array mismatch: got %#v want %#v", dst, src)
	}
}

func TestArrayTagFixedArrayRoundTrip(t *testing.T) {
	type fixedArray struct {
		Values [3]uint8 `tlb:"array ## 8"`
	}

	src := fixedArray{Values: [3]uint8{7, 8, 9}}
	c, err := ToCell(src)
	if err != nil {
		t.Fatal(err)
	}

	var dst fixedArray
	if err = LoadFromCell(&dst, c.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	if dst != src {
		t.Fatalf("fixed array mismatch: got %#v want %#v", dst, src)
	}
}

func TestArrayTagZeroSizeElementsRoundTrip(t *testing.T) {
	type emptyStructArray struct {
		Values []testArrayEmptyPayload `tlb:"array ."`
	}

	src := emptyStructArray{Values: make([]testArrayEmptyPayload, 3)}
	c, err := ToCell(src)
	if err != nil {
		t.Fatal(err)
	}

	var dst emptyStructArray
	if err = LoadFromCell(&dst, c.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	if len(dst.Values) != len(src.Values) {
		t.Fatalf("unexpected zero-size array len: got %d want %d", len(dst.Values), len(src.Values))
	}
}

func TestArrayTagRejectsTooManyElements(t *testing.T) {
	type largeArray struct {
		Values []uint8 `tlb:"array ## 8"`
	}

	if _, err := ToCell(largeArray{Values: make([]uint8, 256)}); err == nil {
		t.Fatal("expected array len > 255 to fail")
	}
}

func TestArrayTagRejectsMalformedData(t *testing.T) {
	t.Run("empty with ref", func(t *testing.T) {
		type emptyArray struct {
			Values []uint8 `tlb:"array ## 8"`
		}

		c := cell.BeginCell().
			MustStoreUInt(0, 8).
			MustStoreBoolBit(true).
			MustStoreRef(cell.BeginCell().EndCell()).
			EndCell()

		var dst emptyArray
		if err := LoadFromCell(&dst, c.MustBeginParse()); err == nil {
			t.Fatal("expected empty array with data ref to fail")
		}
	})

	t.Run("next without ref", func(t *testing.T) {
		type malformedArray struct {
			Values []uint8 `tlb:"array ## 8"`
		}

		chunk := cell.BeginCell().
			MustStoreBoolBit(true).
			MustStoreUInt(0xAA, 8).
			EndCell()
		c := cell.BeginCell().
			MustStoreUInt(2, 8).
			MustStoreBoolBit(true).
			MustStoreRef(chunk).
			EndCell()

		var dst malformedArray
		if err := LoadFromCell(&dst, c.MustBeginParse()); err == nil {
			t.Fatal("expected chunk with missing next ref to fail")
		}
	})
}

func BenchmarkArrayTagLoadUint8_200(b *testing.B) {
	values := make([]uint8, 200)
	for i := range values {
		values[i] = uint8(i)
	}

	c, err := ToCell(benchUint8Array{Values: values})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var dst benchUint8Array
		if err = LoadFromCell(&dst, c.MustBeginParse()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkArrayTagStoreUint8_200(b *testing.B) {
	values := make([]uint8, 200)
	for i := range values {
		values[i] = uint8(i)
	}
	src := benchUint8Array{Values: values}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ToCell(src); err != nil {
			b.Fatal(err)
		}
	}
}
