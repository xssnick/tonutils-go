package tlb

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

type testStringTag struct {
	Text string `tlb:"string"`
	Data []byte `tlb:"string"`
}

func TestStringTagRoundTrip(t *testing.T) {
	src := testStringTag{
		Text: strings.Repeat("hello", 40),
		Data: []byte{0x00, 0x01, 0x02, 0x7F, 0x80, 0xFF},
	}

	c, err := ToCell(src)
	if err != nil {
		t.Fatal(err)
	}

	if c.BitsSize() != 0 || c.RefsNum() != 2 {
		t.Fatalf("string tag should store refs only, got %d bits and %d refs", c.BitsSize(), c.RefsNum())
	}

	text, err := c.MustPeekRef(0).BeginParse().LoadStringSnake()
	if err != nil {
		t.Fatal(err)
	}
	if text != src.Text {
		t.Fatalf("unexpected string ref value: got %q want %q", text, src.Text)
	}

	data, err := c.MustPeekRef(1).BeginParse().LoadBinarySnake()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, src.Data) {
		t.Fatalf("unexpected binary ref value: got %x want %x", data, src.Data)
	}

	var dst testStringTag
	if err = LoadFromCell(&dst, c.BeginParse()); err != nil {
		t.Fatal(err)
	}
	if dst.Text != src.Text {
		t.Fatalf("unexpected text after round trip: got %q want %q", dst.Text, src.Text)
	}
	if !bytes.Equal(dst.Data, src.Data) {
		t.Fatalf("unexpected data after round trip: got %x want %x", dst.Data, src.Data)
	}
}

func TestStringTagMaybeRoundTrip(t *testing.T) {
	type maybeStrings struct {
		Text *string `tlb:"maybe string"`
		Data *[]byte `tlb:"maybe string"`
		None *string `tlb:"maybe string"`
	}

	text := "optional"
	data := []byte{0xCA, 0xFE}
	src := maybeStrings{
		Text: &text,
		Data: &data,
	}

	c, err := ToCell(src)
	if err != nil {
		t.Fatal(err)
	}

	var dst maybeStrings
	if err = LoadFromCell(&dst, c.BeginParse()); err != nil {
		t.Fatal(err)
	}
	if dst.Text == nil || *dst.Text != text {
		t.Fatalf("unexpected maybe text: got %v want %q", dst.Text, text)
	}
	if dst.Data == nil || !bytes.Equal(*dst.Data, data) {
		t.Fatalf("unexpected maybe data: got %v want %x", dst.Data, data)
	}
	if dst.None != nil {
		t.Fatalf("expected nil maybe string, got %q", *dst.None)
	}
}

func TestStringTagRejectsUnsupportedType(t *testing.T) {
	type badString struct {
		Value uint32 `tlb:"string"`
	}

	if _, err := ToCell(badString{}); err == nil {
		t.Fatal("expected string tag store to reject unsupported field type")
	}

	src := cell.BeginCell().
		MustStoreRef(cell.BeginCell().MustStoreStringSnake("bad").EndCell()).
		EndCell()

	var dst badString
	if err := LoadFromCell(&dst, src.BeginParse()); err == nil {
		t.Fatal("expected string tag load to reject unsupported field type")
	}
}
