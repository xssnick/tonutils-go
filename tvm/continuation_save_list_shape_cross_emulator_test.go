//go:build cgo && tvm_cross_emulator

package tvm

import (
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestTVMCrossEmulatorContinuationSaveListForkShape(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := []struct {
		name     string
		extraBit bool
		extraRef bool
		accept   bool
	}{
		{name: "canonical", accept: true},
		{name: "fork_payload_bit", extraBit: true},
		{name: "third_fork_ref", extraRef: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stackCell := continuationSaveListStackCell(t, tt.extraBit, tt.extraRef)
			var goStack tlb.Stack
			goErr := goStack.LoadFromCell(stackCell.MustBeginParse())
			goAccepted := goErr == nil

			_, refErr := runReferenceCrossCodeViaEmulatorStackCell(
				rawCodeCellFromHex(t, "30"),
				testEmptyCell(),
				stackCell,
				referenceGetMethodConfig{
					Address:  tonopsTestAddr,
					Now:      uint32(tonopsTestTime.Unix()),
					Balance:  tonopsTestBalance.Uint64(),
					RandSeed: tonopsTestSeed,
				},
			)
			refAccepted := refErr == nil
			if goAccepted != tt.accept || refAccepted != tt.accept {
				t.Fatalf("stack acceptance = Go %t (%v), reference %t (%v), want %t", goAccepted, goErr, refAccepted, refErr, tt.accept)
			}
		})
	}
}

func continuationSaveListStackCell(t *testing.T, extraBit, extraRef bool) *cell.Cell {
	t.Helper()

	dict := cell.NewDict(4)
	for _, index := range []uint64{0, 2} {
		value := cell.BeginCell()
		if err := tlb.SerializeStackValue(value, &vm.ExcQuitContinuation{}); err != nil {
			t.Fatalf("serialize saved continuation: %v", err)
		}
		if err := dict.SetBuilder(cell.BeginCell().MustStoreUInt(index, 4).EndCell(), value); err != nil {
			t.Fatalf("store saved continuation c%d: %v", index, err)
		}
	}

	root := dict.AsCell()
	rootSlice := root.MustBeginParse()
	alteredRoot := cell.BeginCell().MustStoreSlice(rootSlice.MustLoadSlice(root.BitsSize()), root.BitsSize())
	if extraBit {
		alteredRoot.MustStoreBoolBit(true)
	}
	for rootSlice.RefsNum() > 0 {
		ref, err := rootSlice.LoadRefCell()
		if err != nil {
			t.Fatalf("load canonical save-list fork ref: %v", err)
		}
		alteredRoot.MustStoreRef(ref)
	}
	if extraRef {
		alteredRoot.MustStoreRef(cell.BeginCell().EndCell())
	}

	continuation := cell.BeginCell().
		MustStoreUInt(0x06, 8).
		MustStoreUInt(0, 2).
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		MustStoreMaybeRef(alteredRoot.EndCell()).
		MustStoreBoolBit(false).
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreUInt(0, 26)

	return cell.BeginCell().
		MustStoreUInt(1, 24).
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreBuilder(continuation).
		EndCell()
}
