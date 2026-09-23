package tvm

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTransactionChangeLibraryLookupError(t *testing.T) {
	lib := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	// Stored account libraries are opaque during account validation. Lookup
	// must distinguish a malformed dictionary from an absent library (41).
	malformed := cell.BeginCell().EndCell().AsDict(256)
	for _, version := range []uint32{0, 3, 4, 14, 15} {
		for _, mode := range []uint8{0, 1, 2, 16, 18} {
			t.Run(fmt.Sprintf("v%d/mode%d", version, mode), func(t *testing.T) {
				wantCode := int32(42)
				if mode&16 != 0 && version < 4 {
					wantCode = 34
				} else if mode == 1 && version >= 15 {
					wantCode = 46
				}
				res, err := transactionProcessChangeLibraryAction(tlb.ActionChangeLibrary{
					Mode: mode, LibRef: tlb.LibRefHash{LibHash: lib.Hash()},
				}, malformed, transactionTestConfigWithGlobalVersion(t, version), version, true)
				if err != nil {
					t.Fatal(err)
				}
				if res.resultCode != wantCode || res.nextLibraries != nil {
					t.Fatalf("result=%d libraries=%v, want code %d without mutation", res.resultCode, res.nextLibraries, wantCode)
				}
			})
		}
	}

	for _, current := range []*cell.Dictionary{nil, cell.NewDict(256), buildTransactionV13LibraryDict(t, cell.BeginCell().EndCell(), false)} {
		res, err := transactionProcessChangeLibraryAction(tlb.ActionChangeLibrary{
			Mode: 2, LibRef: tlb.LibRefHash{LibHash: lib.Hash()},
		}, current, transactionTestConfigWithGlobalVersion(t, 14), 14, false)
		if err != nil || res.resultCode != 41 || res.nextLibraries != nil {
			t.Fatalf("missing library result=%+v err=%v, want 41", res, err)
		}
	}

	// A nonempty malformed node must also keep the dictionary error. Passing
	// the library itself must not replace an unreadable dictionary silently.
	for _, current := range []*cell.Dictionary{malformed, cell.BeginCell().MustStoreUInt(0, 2).EndCell().AsDict(256)} {
		for _, ref := range []any{tlb.LibRefHash{LibHash: lib.Hash()}, tlb.LibRefRef{Library: lib}} {
			res, err := transactionProcessChangeLibraryAction(tlb.ActionChangeLibrary{Mode: 2, LibRef: ref}, current, transactionTestConfigWithGlobalVersion(t, 14), 14, false)
			if err != nil || res.resultCode != 42 || res.nextLibraries != nil {
				t.Fatalf("malformed library lookup result=%+v err=%v, want 42", res, err)
			}
		}
	}
}

func TestTransactionChangeLibraryLookupErrorActionPhase(t *testing.T) {
	data := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	malformed := cell.BeginCell().EndCell().AsDict(256)
	acc := &transactionRuntimeAccount{
		addr: tonopsTestAddr, code: cell.BeginCell().EndCell(), data: data,
		status: tlb.AccountStatusActive, balance: big.NewInt(1000), libraries: malformed,
	}
	res := &MessageExecutionResult{Accepted: true, ExecutionResult: ExecutionResult{
		Committed: true, Data: data,
		Actions: buildTransactionActionList(t, tlb.ActionChangeLibrary{Mode: 18, LibRef: tlb.LibRefHash{LibHash: make([]byte, 32)}}),
	}}
	out, err := transactionApplyActions(acc, res, 1000, 1, transactionTestConfigWithGlobalVersion(t, 14), big.NewInt(1000), nil, transactionZeroCurrencyBalance(), big.NewInt(0), nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if out.phase == nil || !out.phase.Valid || out.phase.Success || out.phase.ResultCode != 42 || !out.bounce || out.nextLibraries != malformed || out.balance.Int64() != 1000 {
		t.Fatalf("unexpected failed library action: phase=%+v bounce=%t balance=%v", out.phase, out.bounce, out.balance)
	}
}
