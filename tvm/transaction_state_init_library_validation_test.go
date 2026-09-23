package tvm

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTransactionStateInitLibraryStructuralValidation(t *testing.T) {
	code := cell.BeginCell().MustStoreUInt(0xab, 8).EndCell()
	leaf := cell.BeginCell().MustStoreBoolBit(true).MustStoreRef(code).EndCell()
	dictionary := func(value *cell.Cell) *cell.Cell {
		t.Helper()
		dict := cell.NewDict(256)
		// Schema validation checks SimpleLib, not equality of its key to the
		// code hash. The library code itself is an opaque Cell here.
		if err := dict.SetIntKey(big.NewInt(1), value); err != nil {
			t.Fatal(err)
		}
		return dict.AsCell()
	}
	fork := cell.NewDict(256)
	for _, key := range []*big.Int{big.NewInt(0), new(big.Int).Lsh(big.NewInt(1), 255)} {
		if err := fork.SetIntKey(key, leaf); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name  string
		root  *cell.Cell
		valid bool
	}{
		{name: "valid", root: dictionary(leaf), valid: true},
		{name: "valid fork", root: fork.AsCell(), valid: true},
		{name: "extra fork bit", root: fork.AsCell().ToBuilder().MustStoreBoolBit(false).EndCell()},
		{name: "extra fork reference", root: fork.AsCell().ToBuilder().MustStoreRef(code).EndCell()},
		{name: "empty root", root: cell.BeginCell().EndCell()},
		{name: "missing fork references", root: cell.BeginCell().MustStoreUInt(0, 2).EndCell()},
		{name: "missing public bit", root: dictionary(cell.BeginCell().MustStoreRef(code).EndCell())},
		{name: "missing code reference", root: dictionary(cell.BeginCell().MustStoreBoolBit(true).EndCell())},
		{name: "extra leaf bit", root: dictionary(leaf.ToBuilder().MustStoreBoolBit(false).EndCell())},
		{name: "extra leaf reference", root: dictionary(leaf.ToBuilder().MustStoreRef(code).EndCell())},
	}
	for _, tc := range cases {
		for _, external := range []bool{false, true} {
			for _, referenced := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/external_%t/ref_%t", tc.name, external, referenced), func(t *testing.T) {
					state := cell.BeginCell().MustStoreUInt(1, 5).MustStoreRef(tc.root).EndCell()
					message := transactionStateInitLibraryTestMessage(state, external, referenced)
					_, err := transactionValidateRelaxedActionMessageCurrencies(message)
					if err != nil {
						t.Fatalf("StateInit library reference must remain opaque in the modern schema: %v", err)
					}

					library := tc.root.AsDict(256)
					stateInit := &tlb.StateInit{Lib: library}
					for _, parsed := range []*tlb.Message{
						{MsgType: tlb.MsgTypeInternal, Msg: &tlb.InternalMessage{StateInit: stateInit}},
						{MsgType: tlb.MsgTypeExternalIn, Msg: &tlb.ExternalMessage{StateInit: stateInit}},
						{MsgType: tlb.MsgTypeExternalOut, Msg: &tlb.ExternalMessageOut{StateInit: stateInit}},
					} {
						if err := transactionValidateMessageStateInitLibs(parsed); (err == nil) != tc.valid {
							t.Fatalf("parsed %s: valid=%t error=%v", parsed.MsgType, tc.valid, err)
						}
					}
					for _, version := range []uint32{0, 2, 3, 4, 7, 8, 13, 16} {
						for _, mode := range []uint8{0, 2, 3, 16, 18} {
							actions := buildTransactionActionList(t,
								tlb.ActionSetCode{NewCode: code},
								tlb.ActionSendMsg{Mode: mode, Msg: message})
							for _, historical := range []bool{false, true} {
								loaded, err := transactionLoadActions(actions, version, historical)
								if err != nil {
									t.Fatal(err)
								}
								if tc.valid || !historical || version > 3 {
									if loaded.resultCode != 0 || len(loaded.actions) != 2 || loaded.actions[1].skipped || loaded.skippedActions != 0 {
										t.Fatalf("v%d mode%d historical=%t: unexpected prepass result %+v", version, mode, historical, loaded)
									}
								} else {
									if loaded.resultCode != 34 || loaded.totalActions != 2 || len(loaded.actions) != 0 {
										t.Fatalf("v%d mode%d: malformed library passed historical prepass: %+v", version, mode, loaded)
									}
									if loaded.resultArg == nil || *loaded.resultArg != 1 || loaded.bounce {
										t.Fatalf("v%d mode%d: incorrect historical failure position/bounce: %+v", version, mode, loaded)
									}
								}
							}
						}
					}
				})
			}
		}
	}
}

func transactionStateInitLibraryTestMessage(state *cell.Cell, external, referenced bool) *cell.Cell {
	b := cell.BeginCell()
	if external {
		b.MustStoreUInt(3, 2).MustStoreAddr(address.NewAddressNone()).MustStoreAddr(address.NewAddressNone())
	} else {
		b.MustStoreUInt(4, 4).MustStoreAddr(address.NewAddressNone()).MustStoreAddr(tonopsTestAddr).
			MustStoreCoins(0).MustStoreDict(nil).MustStoreCoins(0).MustStoreCoins(0)
	}
	b.MustStoreUInt(0, 64).MustStoreUInt(0, 32).MustStoreBoolBit(true).MustStoreBoolBit(referenced)
	if referenced {
		b.MustStoreRef(state)
	} else {
		b.MustStoreBuilder(state.ToBuilder())
	}
	return b.MustStoreBoolBit(false).EndCell()
}
