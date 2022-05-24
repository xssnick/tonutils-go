package cell

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestCell_Hash(t *testing.T) {
	cc1 := BeginCell().MustStoreUInt(111, 63).EndCell()
	cc2 := BeginCell().MustStoreUInt(772227, 63).MustStoreRef(cc1).EndCell()
	cc3 := BeginCell().MustStoreUInt(333, 63).MustStoreRef(cc2).EndCell()
	cc := BeginCell().MustStoreUInt(777, 63).MustStoreRef(cc3).EndCell()

	b, _ := hex.DecodeString("bb2509fe3cff8f1faae19213774d218c018f9616cd397850c8ad9038db84eaa9")

	if !bytes.Equal(cc.Hash(), b) {
		t.Fatal("hash diff")
	}
}
