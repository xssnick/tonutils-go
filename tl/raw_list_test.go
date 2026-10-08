package tl

import (
	"bytes"
	"testing"
)

func TestRawListElementAppendsCanonicalBytes(t *testing.T) {
	prefix := Raw{0x01, 0x02, 0x03}
	body := Raw{0x04, 0x05, 0x06}
	want := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}

	got, err := Serialize([]Serializable{prefix, body}, true)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("serialize = %x, want %x", got, want)
	}

	got, err = Append([]byte{0x00}, []Serializable{prefix, body}, true)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	want = append([]byte{0x00}, want...)
	if !bytes.Equal(got, want) {
		t.Fatalf("append = %x, want %x", got, want)
	}
}
