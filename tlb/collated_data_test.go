package tlb

import (
	"bytes"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestCollatedDataRootConstructors(t *testing.T) {
	hash := bytes.Repeat([]byte{0x42}, 32)
	for _, tc := range []struct {
		name string
		tag  uint64
		data any
		out  any
	}{
		{"state", 0x4b2f36ec, CollatedDataRootState{Hash: hash}, &CollatedDataRootState{}},
		{"storage_dict", 0x796eaeb6, CollatedDataRootStorageDict{Hash: hash}, &CollatedDataRootStorageDict{}},
		{"separator", 0xfa8b2b92, CollatedDataSeparator{}, &CollatedDataSeparator{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := ToCell(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			want := cell.BeginCell().MustStoreUInt(tc.tag, 32)
			if tc.name != "separator" {
				want.MustStoreSlice(hash, 256)
			}
			mustCellHashEqual(t, "collated data constructor", encoded, want.EndCell())
			loader := encoded.MustBeginParse()
			if err = LoadFromCell(tc.out, loader); err != nil {
				t.Fatal(err)
			}
			if loader.BitsLeft() != 0 || loader.RefsNum() != 0 {
				t.Fatal("collated data constructor left trailing data")
			}
			back, err := ToCell(tc.out)
			if err != nil {
				t.Fatal(err)
			}
			mustCellHashEqual(t, "collated data round-trip", back, encoded)
			if err = LoadFromCell(tc.out, cell.BeginCell().MustStoreUInt(tc.tag^1, 32).MustStoreSlice(hash, 256).EndCell().MustBeginParse()); err == nil {
				t.Fatal("incorrect collated data tag was accepted")
			}
		})
	}
}
