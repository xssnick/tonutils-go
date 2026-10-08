package funcs

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

func TestSendMsgMode128PreservesExtraCurrencyRef(t *testing.T) {
	addr, _, _ := mustStdAddrSlice(t)
	extra := cell.NewDict(32)
	if err := extra.SetIntKey(big.NewInt(1), cell.BeginCell().MustStoreVarUInt(1, 32).EndCell()); err != nil {
		t.Fatal(err)
	}
	prices := makeMsgPricesSlice(0, 0, 1<<16)
	unpacked := makeFeeUnpackedConfig(t, nil, nil, prices)

	for _, version := range []int{9, 10, 14} {
		for _, tc := range []struct {
			name     string
			bodyBits uint
			fee      int64
		}{
			{name: "inline", bodyBits: 336, fee: 3},
			{name: "refs force body out after init moves", bodyBits: 337, fee: 5},
			{name: "bits force body out", bodyBits: 342, fee: 5},
		} {
			t.Run(fmt.Sprintf("v%d/%s", version, tc.name), func(t *testing.T) {
				msg := cell.BeginCell().
					MustStoreUInt(4, 4).
					MustStoreUInt(0, 2).
					MustStoreAddr(addr).
					MustStoreCoins(1).
					MustStoreDict(extra).
					MustStoreCoins(0).
					MustStoreCoins(0).
					MustStoreUInt(0, 64).
					MustStoreUInt(0, 32).
					MustStoreUInt(2, 2). // present inline StateInit
					MustStoreUInt(0, 5).
					MustStoreBoolBit(false).
					MustStoreSlice(make([]byte, (tc.bodyBits+7)/8), tc.bodyBits).
					MustStoreRef(cell.BeginCell().MustStoreUInt(1, 8).EndCell()).
					MustStoreRef(cell.BeginCell().MustStoreUInt(2, 8).EndCell()).
					MustStoreRef(cell.BeginCell().MustStoreUInt(3, 8).EndCell()).EndCell()
				state := newFuncTestState(t, map[int]any{
					7:                      tuple.NewTupleValue(big.NewInt(10_000_000), nil),
					8:                      cell.BeginCell().MustStoreAddr(addr).ToSlice(),
					paramIdxUnpackedConfig: unpacked,
				})
				state.GlobalVersion = version
				if err := state.Stack.PushCell(msg); err != nil {
					t.Fatal(err)
				}
				if err := state.Stack.PushSmallInt(1024 | 128); err != nil {
					t.Fatal(err)
				}
				if err := SENDMSG().Interpret(state); err != nil {
					t.Fatal(err)
				}
				fee, err := state.Stack.PopIntFinite()
				if err != nil {
					t.Fatal(err)
				}
				want := tc.fee
				if version < 10 {
					want++ // legacy fee also counts the extra-currency dictionary
				}
				if fee.Cmp(big.NewInt(want)) != 0 {
					t.Fatalf("fee = %s, want %d", fee, want)
				}
			})
		}
	}
}
