//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
)

// Keep the body and StateInit inline even at the output-layout boundary. The
// source is addr_none, so replacing it with MYADDR grows the output by 265 bits.
// The extra-currency dictionary and three body children occupy all four refs.
func reviewTonopsSendMsgInlineExtra(t *testing.T, bodyBits uint, withExtra bool) *cell.Cell {
	t.Helper()

	extra := cell.NewDict(32)
	if withExtra {
		if err := extra.SetIntKey(big.NewInt(1), cell.BeginCell().MustStoreVarUInt(1, 32).EndCell()); err != nil {
			t.Fatal(err)
		}
	}

	return cell.BeginCell().
		MustStoreUInt(4, 4). // internal, IHR disabled, no bounce
		MustStoreUInt(0, 2). // source addr_none
		MustStoreAddr(tonopsTestAddr).
		MustStoreCoins(1).
		MustStoreDict(extra).
		MustStoreCoins(0).
		MustStoreCoins(0).
		MustStoreUInt(0, 64).
		MustStoreUInt(0, 32).
		MustStoreUInt(2, 2).     // present inline StateInit
		MustStoreUInt(0, 5).     // empty StateInit
		MustStoreBoolBit(false). // inline body
		MustStoreSlice(make([]byte, (bodyBits+7)/8), bodyBits).
		MustStoreRef(cell.BeginCell().MustStoreUInt(1, 8).EndCell()).
		MustStoreRef(cell.BeginCell().MustStoreUInt(2, 8).EndCell()).
		MustStoreRef(cell.BeginCell().MustStoreUInt(3, 8).EndCell()).EndCell()
}

func TestReviewTonopsSendMsgSharedFeeOverflow(t *testing.T) {
	// Both engines wrap SENDMSG's uint64 intermediate, although GETFORWARDFEE
	// returns the full mathematical fee for the same one-cell message tail.
	// Reaching this boundary requires an extreme custom fee configuration.
	prices := tlb.ConfigMsgForwardPrices{LumpPrice: ^uint64(0), CellPrice: 1 << 16}
	body := cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0xA5}, 125), 1000).EndCell()
	msg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     tonopsTestAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1),
		Body:        body,
	})
	if err != nil {
		t.Fatal(err)
	}
	code := prependRawMethodDrop(codeFromBuilders(t,
		funcsop.SENDMSG().Serialize(),
		stackop.PUSHINT(big.NewInt(1)).Serialize(),
		stackop.PUSHINT(big.NewInt(1000)).Serialize(),
		stackop.PUSHINT(big.NewInt(0)).Serialize(),
		funcsop.GETFORWARDFEE().Serialize(),
	))
	want := []any{int64(0), new(big.Int).Lsh(big.NewInt(1), 64)}

	for _, version := range []int{9, 10, 13, 14} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			config := tonopsCrossSendMsgConfig(t, uint32(version), prices)
			c7 := makeTonopsTestC7(t, tonopsTestC7Config{
				ConfigRoot:     config,
				UnpackedConfig: tonopsCrossSendMsgUnpackedConfig(t, prices),
			})
			stack, err := buildCrossStack(msg, int64(1024))
			if err != nil {
				t.Fatal(err)
			}
			goRes, err := runGoCrossCodeWithVersion(code, cell.BeginCell().EndCell(), c7, stack, version)
			if err != nil {
				t.Fatal(err)
			}
			refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), stack, *tonopsCrossRefConfig(config))
			if err != nil {
				t.Fatal(err)
			}
			if goRes.exitCode != 0 || refRes.exitCode != 0 || goRes.gasUsed != refRes.gasUsed {
				t.Fatalf("Go exit/gas=%d/%d, C++=%d/%d", goRes.exitCode, goRes.gasUsed, refRes.exitCode, refRes.gasUsed)
			}
			assertCrossSkippedGoStack(t, goRes.stack, want)
			assertCrossSkippedGoStack(t, refRes.stack, want)
		})
	}
}

func TestReviewTonopsSendMsgMode128ExtraCurrencyLayout(t *testing.T) {
	// Mode 128 replaces grams, preserving the message's extra-currency ref.
	// Once StateInit moves out, the five refs also force the body out.
	for _, version := range []int{9, 10, 13, 14} {
		for _, bodyBits := range []uint{337, 338, 341} {
			t.Run(fmt.Sprintf("v%d/body%d", version, bodyBits), func(t *testing.T) {
				fee := int64(5)
				if version < 10 {
					fee++
				}
				reviewTonopsSendMsgFees(t, version, reviewTonopsSendMsgInlineExtra(t, bodyBits, true), 1024|128, fee)
			})
		}
	}
}

func TestReviewTonopsSendMsgExtraCurrencyLayoutBoundaries(t *testing.T) {
	for _, version := range []int{9, 10, 13, 14} {
		for _, tc := range []struct {
			name      string
			bodyBits  uint
			withExtra bool
			mode      int64
			fee       int64
		}{
			{name: "last_inline_bit", bodyBits: 336, withExtra: true, mode: 1024 | 128, fee: 3},
			{name: "body_must_move_by_bits", bodyBits: 342, withExtra: true, mode: 1024 | 128, fee: 5},
			{name: "no_extra_ref", bodyBits: 338, mode: 1024 | 128, fee: 4},
			{name: "original_amount", bodyBits: 338, withExtra: true, mode: 1024, fee: 3},
		} {
			t.Run(fmt.Sprintf("v%d/%s", version, tc.name), func(t *testing.T) {
				fee := tc.fee
				if version < 10 && tc.withExtra {
					fee++
				}
				reviewTonopsSendMsgFees(t, version, reviewTonopsSendMsgInlineExtra(t, tc.bodyBits, tc.withExtra), tc.mode, fee)
			})
		}
	}
}

func reviewTonopsSendMsgFees(t *testing.T, version int, msg *cell.Cell, mode, fee int64) {
	t.Helper()

	prices := tlb.ConfigMsgForwardPrices{CellPrice: 1 << 16}
	config := tonopsCrossSendMsgConfig(t, uint32(version), prices)
	c7 := makeTonopsTestC7(t, tonopsTestC7Config{
		ConfigRoot:     config,
		UnpackedConfig: tonopsCrossSendMsgUnpackedConfig(t, prices),
	})
	code := prependRawMethodDrop(codeFromBuilders(t, funcsop.SENDMSG().Serialize()))
	stack, err := buildCrossStack(msg, mode)
	if err != nil {
		t.Fatal(err)
	}
	goRes, err := runGoCrossCodeWithVersion(code, cell.BeginCell().EndCell(), c7, stack, version)
	if err != nil {
		t.Fatal(err)
	}
	refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), stack, *tonopsCrossRefConfig(config))
	if err != nil {
		t.Fatal(err)
	}
	if goRes.exitCode != 0 || refRes.exitCode != 0 {
		t.Fatalf("exit: Go=%d C++=%d, want both 0", goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas: Go=%d C++=%d", goRes.gasUsed, refRes.gasUsed)
	}
	assertCrossSkippedGoStack(t, goRes.stack, []any{fee})
	assertCrossSkippedGoStack(t, refRes.stack, []any{fee})
}
