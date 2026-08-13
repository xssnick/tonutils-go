//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

// HASHEXT accepts only non-null slices and builders. A typed-null builder is
// therefore a regular type-check failure, with all counted inputs consumed.
func TestTVMCrossEmulatorHashExtTypedNullBuilderParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	code := prependRawMethodDrop(codeFromBuilders(t, funcsop.HASHEXT(0).Serialize()))
	assertFlowParityCase(t, "hashext-typed-null-builder", code,
		[]any{(*cell.Builder)(nil), int64(1)}, 13, referenceDefaultMaxGas)
}

func TestTVMCrossEmulatorRewriteVarAddrFullAnycastPrefixParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	// addr_var$11 anycast(depth=4, pfx=1010) addr_len=4 wc=0 addr=0000.
	// The full-width rewrite returns the prefix slice directly.
	addr := cell.BeginCell().
		MustStoreUInt(0b11, 2).
		MustStoreBoolBit(true).
		MustStoreUInt(4, 5).
		MustStoreUInt(0b1010, 4).
		MustStoreUInt(4, 9).
		MustStoreInt(0, 32).
		MustStoreUInt(0, 4).
		ToSlice()
	code := prependRawMethodDrop(codeFromBuilders(t, funcsop.REWRITEVARADDR().Serialize()))
	assertFlowParityCase(t, "rewritevaraddr-full-anycast-prefix", code,
		[]any{addr}, 9, referenceDefaultMaxGas)
}

func TestTVMCrossEmulatorSendMsgUint64FeeWrapParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	t.Run("forward fee addition", func(t *testing.T) {
		// The referenced body contributes one tail cell. Its cell fee is exactly
		// one nanogram, making MaxUint64 lump_price wrap to zero in the reference.
		prices := tlb.ConfigMsgForwardPrices{
			LumpPrice: ^uint64(0),
			CellPrice: 1 << 16,
		}
		body := cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0xA5}, 125), 1000).EndCell()
		msg, err := tlb.ToCell(&tlb.InternalMessage{
			IHRDisabled: true,
			SrcAddr:     tonopsTestAddr,
			DstAddr:     tonopsTestAddr,
			Amount:      tlb.FromNanoTONU(1),
			Body:        body,
		})
		if err != nil {
			t.Fatalf("build SENDMSG message: %v", err)
		}
		runSendMsgUint64FeeWrapParity(t, 13, prices, msg)
	})

	t.Run("IHR fee shift", func(t *testing.T) {
		prices := tlb.ConfigMsgForwardPrices{
			LumpPrice: ^uint64(0),
			IHRFactor: ^uint32(0),
		}
		msg, err := tlb.ToCell(&tlb.InternalMessage{
			IHRDisabled: false,
			SrcAddr:     tonopsTestAddr,
			DstAddr:     tonopsTestAddr,
			Amount:      tlb.FromNanoTONU(1),
		})
		if err != nil {
			t.Fatalf("build SENDMSG message: %v", err)
		}
		runSendMsgUint64FeeWrapParity(t, 10, prices, msg)
	})
}

func TestTVMCrossEmulatorSendMsgNonSliceConfigSlotsParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	prices := tlb.ConfigMsgForwardPrices{
		LumpPrice: 100,
		BitPrice:  1 << 16,
		CellPrice: 3 << 16,
	}
	configRoot := tonopsCrossSendMsgConfig(t, 13, prices)
	msg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     tonopsTestAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1),
	})
	if err != nil {
		t.Fatalf("build SENDMSG message: %v", err)
	}
	code := prependRawMethodDrop(codeFromBuilders(t, funcsop.SENDMSG().Serialize()))

	t.Run("size limits use defaults", func(t *testing.T) {
		unpacked := tonopsCrossSendMsgUnpackedConfig(t, prices)
		mustSetTupleValue(t, &unpacked, 6, int64(42))
		c7 := makeTonopsTestC7(t, tonopsTestC7Config{
			ConfigRoot:     configRoot,
			UnpackedConfig: unpacked,
		})
		runTonOpsEdgeParityCase(t, code, []any{msg, int64(1024)}, c7, 0, 0)
	})

	t.Run("prices report unknown", func(t *testing.T) {
		unpacked := tonopsCrossSendMsgUnpackedConfig(t, prices)
		mustSetTupleValue(t, &unpacked, 5, int64(42))
		c7 := makeTonopsTestC7(t, tonopsTestC7Config{
			ConfigRoot:     configRoot,
			UnpackedConfig: unpacked,
		})
		runTonOpsEdgeParityCase(t, code, []any{msg, int64(1024)}, c7, int32(vmerr.CodeUnknown), 0)
	})

	t.Run("malformed prices report cell underflow", func(t *testing.T) {
		unpacked := tonopsCrossSendMsgUnpackedConfig(t, prices)
		mustSetTupleValue(t, &unpacked, 5, cell.BeginCell().MustStoreUInt(0, 8).ToSlice())
		c7 := makeTonopsTestC7(t, tonopsTestC7Config{
			ConfigRoot:     configRoot,
			UnpackedConfig: unpacked,
		})
		runTonOpsEdgeParityCase(t, code, []any{msg, int64(1024)}, c7, int32(vmerr.CodeCellUnderflow), 0)
	})
}

func TestTVMCrossEmulatorSendMsgLegacyNonCellConfigRootParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	msg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     tonopsTestAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1),
	})
	if err != nil {
		t.Fatalf("build SENDMSG message: %v", err)
	}
	c7 := makeTonopsTestC7(t, tonopsTestC7Config{
		ExtraParams: map[int]any{9: int64(42)},
	})
	code := codeFromBuilders(t,
		execop.POPCTR(7).Serialize(),
		funcsop.SENDMSG().Serialize(),
	)
	for _, version := range []int{4, 5} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			runTonOpsEdgeVersionedParityCase(
				t, code, []any{msg, int64(1024), c7}, tuple.Tuple{}, version, int32(vmerr.CodeUnknown), 0,
			)
		})
	}
}

func runSendMsgUint64FeeWrapParity(t *testing.T, version int, prices tlb.ConfigMsgForwardPrices, msg *cell.Cell) {
	t.Helper()

	configRoot := tonopsCrossSendMsgConfig(t, uint32(version), prices)
	c7 := makeTonopsTestC7(t, tonopsTestC7Config{
		ConfigRoot:     configRoot,
		UnpackedConfig: tonopsCrossSendMsgUnpackedConfig(t, prices),
	})
	code := prependRawMethodDrop(codeFromBuilders(t, funcsop.SENDMSG().Serialize()))
	goStack, err := buildCrossStack(msg, int64(1024))
	if err != nil {
		t.Fatalf("build Go stack: %v", err)
	}
	refStack, err := buildCrossStack(msg, int64(1024))
	if err != nil {
		t.Fatalf("build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithVersionGasAndLibs(
		code, cell.BeginCell().EndCell(), c7, nil, goStack, version, referenceDefaultMaxGas,
	)
	if err != nil {
		t.Fatalf("run Go TVM: %v", err)
	}
	refCfg := *tonopsCrossRefConfig(configRoot)
	refCfg.GasLimit = referenceDefaultMaxGas
	refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, refCfg)
	if err != nil {
		t.Fatalf("run reference TVM: %v", err)
	}
	if goRes.exitCode != 0 || refRes.exitCode != 0 {
		t.Fatalf("exit mismatch: Go=%d reference=%d", goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: Go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}
	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("normalize Go stack: %v", err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("normalize reference stack: %v", err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("stack mismatch:\nGo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
	}
}
