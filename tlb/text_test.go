package tlb

import (
	"testing"
)

func TestText(t *testing.T) {
	for _, s := range []string{
		"Hello garage,Hello garage,Hello garage,Hello garage,Hello " +
			"garage,Hello garage,Hello garage,Hello garage,Hello garage,Hello garage",
		"1", "", string(make([]byte, 1024*30)),
	} {
		txt := &Text{
			MaxFirstChunkSize: 30,
			Value:             s,
		}

		cl, err := ToCell(txt)
		if err != nil {
			t.Fatal(err)
		}

		txt2 := Text{}
		err = LoadFromCell(&txt2, cl.BeginParse())
		if err != nil {
			t.Fatal(err)
		}

		if txt.Value != txt2.Value {
			t.Fatal("incorrect value, want", txt.Value, "got", txt2.Value)
		}

		if int(txt2.MaxFirstChunkSize) > len(txt2.Value) {
			t.Fatal("incorrect sz, want", len(txt2.Value), "got", txt2.MaxFirstChunkSize)
		}
	}
}
