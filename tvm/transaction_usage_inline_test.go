package tvm

import (
	"fmt"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTransactionUsageInlineRootDoesNotHideReferencedCell(t *testing.T) {
	child := cell.BeginCell().MustStoreUInt(13, 7).EndCell()
	inline := cell.BeginCell().MustStoreUInt(6, 5).MustStoreRef(child).EndCell()
	collector := newTransactionUsageCollector()
	for _, tc := range []struct {
		name   string
		root   *cell.Cell
		inline bool
		want   transactionUsage
	}{
		{"inline_children", inline, true, transactionUsage{cells: 1, bits: 7}},
		{"physical_copy", inline, false, transactionUsage{cells: 1, bits: 5}},
		{"duplicate_physical_copy", inline, false, transactionUsage{}},
		{"inline_after_physical_copy", inline, true, transactionUsage{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := collector.addCell(tc.root, tc.inline)
			if err != nil || got != tc.want {
				t.Fatalf("usage=%+v, want %+v; error=%v", got, tc.want, err)
			}
		})
	}
}

func TestTransactionOutboundInlineStateBodyUsage(t *testing.T) {
	code := cell.BeginCell().MustStoreUInt(41, 8).EndCell()
	data := cell.BeginCell().MustStoreUInt(59, 9).MustStoreRef(code).EndCell()
	state := &tlb.StateInit{Code: code, Data: data}
	stateCell, err := tlb.ToCell(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		body     *cell.Cell
		bodyBits uint64
	}{
		{"identical_roots", stateCell, 5},
		{"physical_descendant", cell.BeginCell().MustStoreUInt(7, 3).MustStoreRef(stateCell).EndCell(), 3},
		{"shared_descendants", cell.BeginCell().MustStoreUInt(1, 1).MustStoreRef(data).MustStoreRef(stateCell).EndCell(), 1},
	} {
		for _, initRef := range []bool{false, true} {
			for _, bodyRef := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/initref%t/bodyref%t", tc.name, initRef, bodyRef), func(t *testing.T) {
					// Code and data are always physical. StateInit is physical when
					// referenced directly or reached through a body reference.
					want := transactionUsage{cells: 2, bits: 17}
					if tc.name != "identical_roots" || initRef || bodyRef {
						want = transactionAddUsage(want, transactionUsage{cells: 1, bits: 5})
					}
					if tc.name != "identical_roots" && bodyRef {
						want = transactionAddUsage(want, transactionUsage{cells: 1, bits: tc.bodyBits})
					}
					got, err := transactionOutboundExternalMessageFeeUsage(
						&tlb.ExternalMessageOut{StateInit: state, Body: tc.body},
						transactionOutboundLayout{stateInitInRef: initRef, bodyInRef: bodyRef},
					)
					if err != nil || got != want {
						t.Fatalf("usage=%+v, want %+v; error=%v", got, want, err)
					}
				})
			}
		}
	}
}
