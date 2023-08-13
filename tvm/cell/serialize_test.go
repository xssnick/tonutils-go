package cell

import (
	"bytes"
	"testing"
)

func TestToBOCWithFlags(t *testing.T) {
	cc1 := BeginCell().MustStoreUInt(111, 22).EndCell()
	cc2 := BeginCell().MustStoreUInt(777, 256).EndCell()
	cc3 := BeginCell().MustStoreBinarySnake(make([]byte, 700)).EndCell()

	boc := ToBOCWithFlags([]*Cell{cc1, cc2, cc3}, true)
	cells, err := FromBOCMultiRoot(boc)
	if err != nil {
		t.Fatal(err.Error())
		return
	}

	if len(cells) != 3 {
		t.Fatal("not 3 roots")
		return
	}

	if !bytes.Equal(cells[0].Hash(), cc1.Hash()) {
		t.Fatal("incorrect 0 cell")
		return
	}
	if !bytes.Equal(cells[1].Hash(), cc2.Hash()) {
		t.Fatal("incorrect 1 cell")
		return
	}
	if !bytes.Equal(cells[2].Hash(), cc3.Hash()) {
		t.Fatal("incorrect 2 cell")
		return
	}
}
