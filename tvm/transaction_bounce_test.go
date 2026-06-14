package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTransactionBounceMessageUsageCountsBodyRefsOnly(t *testing.T) {
	leaf := cell.BeginCell().MustStoreUInt(0x3, 2).EndCell()
	refA := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	refB := cell.BeginCell().MustStoreUInt(0xB, 4).MustStoreRef(leaf).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xFFFF, 16).
		MustStoreRef(refA).
		MustStoreRef(refB).
		EndCell()

	got, err := transactionBounceMessageUsage(&tlb.InternalMessage{}, body)
	if err != nil {
		t.Fatal(err)
	}

	want := transactionUsage{cells: 3, bits: 14}
	if got != want {
		t.Fatalf("bounce message usage = %+v, want %+v", got, want)
	}
}
