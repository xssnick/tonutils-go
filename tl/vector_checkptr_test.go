package tl

import (
	"bytes"
	"strings"
	"testing"
)

// The element and backing-array sizes below are exact multiples of common heap
// size classes (16/24 byte elements), so a pointer advanced one element past
// the end of a slice lands inside the neighboring heap object — the layout
// that crashes -race checkptr if a vector loop ever steps past the last element.
type checkptrVecElem struct {
	A uint64 `tl:"long"`
	B uint64 `tl:"long"`
}

type checkptrVecHolder struct {
	Items []checkptrVecElem `tl:"vector struct"`
	Strs  []string          `tl:"vector string"`
	Bts   [][]byte          `tl:"vector bytes"`
}

func init() {
	Register(checkptrVecElem{}, "")
	Register(checkptrVecHolder{}, "")
}

func TestVectorNoPastEndPointers(t *testing.T) {
	for i := 0; i < 200; i++ {
		h := checkptrVecHolder{
			Items: make([]checkptrVecElem, 1+i%4),
			// 8+ elements so grow[Serialize|Append]Vector measure them element by element
			Strs: make([]string, 8),
			Bts:  make([][]byte, 8),
		}
		for j := range h.Items {
			h.Items[j] = checkptrVecElem{A: uint64(i), B: uint64(j)}
		}
		for j := range h.Strs {
			h.Strs[j] = strings.Repeat("s", j)
			h.Bts[j] = bytes.Repeat([]byte{byte(j)}, j)
		}

		appended, err := Serialize(&h, false)
		if err != nil {
			t.Fatal(err)
		}

		var buf bytes.Buffer
		buffered, err := Serialize(&h, false, &buf)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(appended, buffered) {
			t.Fatal("append and buffer serialization results differ")
		}

		var parsed checkptrVecHolder
		if _, err = Parse(&parsed, appended, false); err != nil {
			t.Fatal(err)
		}

		reSerialized, err := Serialize(&parsed, false)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(appended, reSerialized) {
			t.Fatal("data mismatch after parse round-trip")
		}
	}
}
