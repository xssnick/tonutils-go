//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	mathop "github.com/xssnick/tonutils-go/tvm/op/math"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorConfirmedC7Parity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	shortConfigParams := tuple.NewTupleSized(paramIndexUnpackedConfig + 1)
	mustSetTupleValue(t, &shortConfigParams, paramIndexUnpackedConfig, tuple.NewTupleValue())
	shortConfigC7 := tuple.NewTupleValue(shortConfigParams)

	shortBalanceParams := tuple.NewTupleSized(8)
	mustSetTupleValue(t, &shortBalanceParams, 7, tuple.NewTupleValue(big.NewInt(1000)))
	shortBalanceC7 := tuple.NewTupleValue(shortBalanceParams)

	nanSeedParams := tuple.NewTupleSized(7)
	mustSetTupleValue(t, &nanSeedParams, 6, vm.NaN{})
	nanSeedC7 := tuple.NewTupleValue(nanSeedParams)

	prev := tuple.NewTupleSized(256)
	prevParams := tuple.NewTupleSized(14)
	mustSetTupleValue(t, &prevParams, 13, prev)
	prevC7 := tuple.NewTupleValue(prevParams)

	tests := []struct {
		name  string
		op    *cell.Builder
		stack []any
		c7    tuple.Tuple
		exit  int32
	}{
		{name: "GETGASFEE short config", op: funcsop.GETGASFEE().Serialize(), stack: []any{int64(1), int64(0)}, c7: shortConfigC7, exit: int32(vmerr.CodeRangeCheck)},
		{name: "GETSTORAGEFEE short config", op: funcsop.GETSTORAGEFEE().Serialize(), stack: []any{int64(1), int64(2), int64(3), int64(0)}, c7: shortConfigC7, exit: int32(vmerr.CodeRangeCheck)},
		{name: "GETFORWARDFEE short config", op: funcsop.GETFORWARDFEE().Serialize(), stack: []any{int64(1), int64(2), int64(0)}, c7: shortConfigC7, exit: int32(vmerr.CodeRangeCheck)},
		{name: "GETORIGINALFWDFEE short config", op: funcsop.GETORIGINALFWDFEE().Serialize(), stack: []any{int64(1), int64(0)}, c7: shortConfigC7, exit: int32(vmerr.CodeRangeCheck)},
		{name: "GETGASFEESIMPLE short config", op: funcsop.GETGASFEESIMPLE().Serialize(), stack: []any{int64(1), int64(0)}, c7: shortConfigC7, exit: int32(vmerr.CodeRangeCheck)},
		{name: "GETFORWARDFEESIMPLE short config", op: funcsop.GETFORWARDFEESIMPLE().Serialize(), stack: []any{int64(1), int64(2), int64(0)}, c7: shortConfigC7, exit: int32(vmerr.CodeRangeCheck)},
		{name: "GETEXTRABALANCE short balance", op: funcsop.GETEXTRABALANCE().Serialize(), stack: []any{int64(1)}, c7: shortBalanceC7, exit: int32(vmerr.CodeRangeCheck)},
		{name: "RANDU256 NaN seed", op: funcsop.RANDU256().Serialize(), c7: nanSeedC7, exit: int32(vmerr.CodeRangeCheck)},
		{name: "RAND NaN seed", op: funcsop.RAND().Serialize(), stack: []any{int64(1)}, c7: nanSeedC7, exit: int32(vmerr.CodeRangeCheck)},
		{name: "ADDRAND NaN seed", op: funcsop.ADDRAND().Serialize(), stack: []any{int64(1)}, c7: nanSeedC7, exit: int32(vmerr.CodeRangeCheck)},
		{name: "PREVMCBLOCKS oversized tuple", op: funcsop.PREVMCBLOCKS().Serialize(), c7: prevC7, exit: int32(vmerr.CodeTypeCheck)},
		{name: "PREVKEYBLOCK oversized tuple", op: funcsop.PREVKEYBLOCK().Serialize(), c7: prevC7, exit: int32(vmerr.CodeTypeCheck)},
		{name: "PREVMCBLOCKS_100 oversized tuple", op: funcsop.PREVMCBLOCKS_100().Serialize(), c7: prevC7, exit: int32(vmerr.CodeTypeCheck)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runTonOpsEdgeParityCase(
				t,
				prependRawMethodDrop(codeFromBuilders(t, tt.op)),
				tt.stack,
				tt.c7,
				tt.exit,
				0,
			)
		})
	}
}

func TestTVMCrossEmulatorCONDSELCHKNaNIntegerType(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := []struct {
		name  string
		code  *cell.Cell
		stack []any
	}{
		{
			name: "select NaN",
			code: codeFromBuilders(t,
				execop.CONDSELCHK().Serialize(),
				mathop.ISNAN().Serialize(),
			),
			stack: []any{int64(-1), vm.NaN{}, int64(5)},
		},
		{
			name: "select finite",
			code: codeFromBuilders(t,
				execop.CONDSELCHK().Serialize(),
				stackop.PUSHINT(big.NewInt(5)).Serialize(),
				mathop.EQUAL().Serialize(),
			),
			stack: []any{int64(0), vm.NaN{}, int64(5)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runTonOpsEdgeParityCase(t, prependRawMethodDrop(tt.code), tt.stack, tuple.Tuple{}, 0, 0)
		})
	}
}

func TestTVMCrossEmulatorSENDMSGUsesCappedCellStatistics(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	prices := tlb.ConfigMsgForwardPrices{LumpPrice: 1, CellPrice: 1 << 16}
	configRoot := confirmedSendMsgConfig(t, 13, prices, 0)
	unpacked := confirmedSendMsgUnpackedConfig(t, prices, 0)
	c7 := makeTonopsTestC7(t, tonopsTestC7Config{
		ConfigRoot:     configRoot,
		UnpackedConfig: unpacked,
	})
	body := cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0xAA}, 125), 1000).EndCell()
	msg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     tonopsTestAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(100),
		Body:        body,
	})
	if err != nil {
		t.Fatalf("failed to build SENDMSG fixture: %v", err)
	}
	code := prependRawMethodDrop(codeFromBuilders(t,
		funcsop.SENDMSG().Serialize(),
		stackop.PUSHINT(big.NewInt(1)).Serialize(),
		mathop.EQUAL().Serialize(),
	))

	runTonOpsEdgeParityCase(t, code, []any{msg, int64(1024)}, c7, 0, 0)
}

func TestTVMCrossEmulatorSENDMSGLegacyNonCellExtraIsEmpty(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	prices := tlb.ConfigMsgForwardPrices{CellPrice: 1 << 16}
	configRoot := confirmedSendMsgConfig(t, 9, prices, 128)
	c7 := makeTonopsTestC7(t, tonopsTestC7Config{
		ConfigRoot:     configRoot,
		UnpackedConfig: confirmedSendMsgUnpackedConfig(t, prices, 128),
		Balance:        tuple.NewTupleValue(big.NewInt(1000), big.NewInt(1)),
	})
	leaf := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	body := cell.BeginCell().MustStoreRef(leaf).MustStoreRef(leaf).MustStoreRef(leaf).MustStoreRef(leaf).EndCell()
	msg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     tonopsTestAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(100),
		Body:        body,
	})
	if err != nil {
		t.Fatalf("failed to build legacy SENDMSG fixture: %v", err)
	}
	code := codeFromBuilders(t,
		execop.POPCTR(7).Serialize(),
		funcsop.SENDMSG().Serialize(),
		stackop.PUSHINT(big.NewInt(1)).Serialize(),
		mathop.EQUAL().Serialize(),
	)

	runTonOpsEdgeVersionedParityCase(
		t,
		code,
		[]any{msg, int64(1024 | 128), c7},
		tuple.Tuple{},
		9,
		0,
		0,
	)
}

const paramIndexUnpackedConfig = 14

func confirmedSendMsgConfig(t *testing.T, version uint32, prices tlb.ConfigMsgForwardPrices, maxMsgCells uint32) *cell.Cell {
	t.Helper()

	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: version})
	if err != nil {
		t.Fatalf("failed to build global version config: %v", err)
	}
	pricesCell, err := tlb.ToCell(&prices)
	if err != nil {
		t.Fatalf("failed to build message prices config: %v", err)
	}
	sizeLimitsCell, err := tlb.ToCell(&tlb.SizeLimitsConfigV1{
		MaxMsgBits:      1 << 20,
		MaxMsgCells:     maxMsgCells,
		MaxLibraryCells: 1000,
		MaxVMDataDepth:  512,
		MaxExtMsgSize:   65535,
		MaxExtMsgDepth:  512,
	})
	if err != nil {
		t.Fatalf("failed to build size limits config: %v", err)
	}
	return mustConfigDictCell(t, map[uint32]*cell.Cell{
		uint32(tlb.ConfigParamGlobalVersion):               versionCell,
		uint32(tlb.ConfigParamMsgForwardPricesMasterchain): pricesCell,
		uint32(tlb.ConfigParamMsgForwardPricesBasechain):   pricesCell,
		uint32(tlb.ConfigParamSizeLimits):                  sizeLimitsCell,
	})
}

func confirmedSendMsgUnpackedConfig(t *testing.T, prices tlb.ConfigMsgForwardPrices, maxMsgCells uint64) tuple.Tuple {
	t.Helper()

	unpacked := tuple.NewTupleSized(7)
	priceSlice := makeMsgPricesSlice(
		prices.LumpPrice,
		prices.BitPrice,
		prices.CellPrice,
		prices.IHRFactor,
		prices.FirstFrac,
		prices.NextFrac,
	)
	mustSetTupleValue(t, &unpacked, 4, priceSlice)
	mustSetTupleValue(t, &unpacked, 5, priceSlice)
	mustSetTupleValue(t, &unpacked, 6, makeSizeLimitsSlice(1<<20, maxMsgCells))
	return unpacked
}
