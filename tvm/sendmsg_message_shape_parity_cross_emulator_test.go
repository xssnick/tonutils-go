//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

// Witnesses from the 2026-08 tonops parity audit: SENDMSG must reject the
// message shapes the reference rejects, read the own-address parameter as
// loosely as the reference does, resolve an exotic message root like a regular
// cell load, and parse config param 43 with the same version and
// exact-consumption rules.

// sendMsgShapeCase runs one SENDMSG scenario on both engines.
func assertSendMsgShapeParity(t *testing.T, name string, msg *cell.Cell, myAddrSlice *cell.Slice, unpackedOverride func(*tuple.Tuple), libs *cell.Cell, version int) {
	t.Helper()

	prices := tlb.ConfigMsgForwardPrices{
		LumpPrice: 100,
		BitPrice:  1 << 16,
		CellPrice: 3 << 16,
		IHRFactor: 1 << 15,
		FirstFrac: 1 << 15,
		NextFrac:  1 << 15,
	}
	configRoot := tonopsCrossSendMsgConfig(t, uint32(version), prices)
	unpacked := tonopsCrossSendMsgUnpackedConfig(t, prices)
	if unpackedOverride != nil {
		unpackedOverride(&unpacked)
	}

	c7 := makeTonopsTestC7(t, tonopsTestC7Config{
		ConfigRoot:     configRoot,
		UnpackedConfig: unpacked,
		ExtraParams:    map[int]any{8: myAddrSlice},
	})
	code := prependRawMethodDrop(codeFromBuilders(t, funcsop.SENDMSG().Serialize()))

	goStack, err := buildCrossStack(msg, int64(1024))
	if err != nil {
		t.Fatalf("%s: go stack: %v", name, err)
	}
	refStack, err := buildCrossStack(msg, int64(1024))
	if err != nil {
		t.Fatalf("%s: ref stack: %v", name, err)
	}

	var goLibs []*cell.Cell
	if libs != nil {
		goLibs = []*cell.Cell{libs}
	}
	goRes, err := runGoCrossCodeWithVersionGasAndLibs(code, cell.BeginCell().EndCell(), c7, goLibs, goStack, version, referenceDefaultMaxGas)
	if err != nil {
		t.Fatalf("%s: go run: %v", name, err)
	}

	var refRes *crossRunResult
	if unpackedOverride != nil {
		refRes, err = runReferenceCrossCodeWithLibsAndGas(
			code, cell.BeginCell().EndCell(), c7, libs, refStack, referenceDefaultMaxGas,
		)
	} else {
		refCfg := *tonopsCrossRefConfig(configRoot)
		refCfg.GasLimit = referenceDefaultMaxGas
		refCfg.Libs = libs
		refRes, err = runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, refCfg)
	}
	if err != nil {
		t.Fatalf("%s: reference run: %v", name, err)
	}

	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("%s: exit mismatch: go=%d reference=%d", name, goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("%s: gas mismatch: go=%d reference=%d", name, goRes.gasUsed, refRes.gasUsed)
	}
	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("%s: normalize go stack: %v", name, err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("%s: normalize reference stack: %v", name, err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("%s: stack mismatch:\ngo=%s\nreference=%s", name, goStackCell.Dump(), refStackCell.Dump())
	}
}

func sendMsgParityMyAddr(t *testing.T) *cell.Slice {
	t.Helper()
	return cell.BeginCell().MustStoreAddr(tonopsTestAddr).ToSlice()
}

// The message layout constrains the destination address kind per message
// type; a mismatching one makes the whole parse fail with unknown(11).
func TestTVMCrossEmulatorSENDMSGDestAddressKindParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	// int_msg_info with dest = addr_none (invalid: needs MsgAddressInt).
	intNoneDest := cell.BeginCell().
		MustStoreUInt(0, 1).     // int_msg_info$0
		MustStoreBoolBit(true).  // ihr_disabled
		MustStoreBoolBit(false). // bounce
		MustStoreBoolBit(false). // bounced
		MustStoreUInt(0, 2).     // src = addr_none
		MustStoreUInt(0, 2).     // dest = addr_none
		MustStoreUInt(0, 4).     // value: zero grams
		MustStoreBoolBit(false). // no extra currencies
		MustStoreUInt(0, 4).     // ihr_fee
		MustStoreUInt(0, 4).     // fwd_fee
		MustStoreUInt(0, 64).    // created_lt
		MustStoreUInt(0, 32).    // created_at
		MustStoreBoolBit(false). // no init
		MustStoreBoolBit(false). // body inline
		EndCell()
	assertSendMsgShapeParity(t, "int-dest-none", intNoneDest, sendMsgParityMyAddr(t), nil, nil, 13)

	// ext_out_msg_info with dest = addr_std (invalid: needs MsgAddressExt).
	extStdDest := cell.BeginCell().
		MustStoreUInt(0b11, 2). // ext_out_msg_info$11
		MustStoreUInt(0, 2).    // src = addr_none
		MustStoreAddr(tonopsTestAddr).
		MustStoreUInt(0, 64).    // created_lt
		MustStoreUInt(0, 32).    // created_at
		MustStoreBoolBit(false). // no init
		MustStoreBoolBit(false). // body inline
		EndCell()
	assertSendMsgShapeParity(t, "ext-dest-std", extStdDest, sendMsgParityMyAddr(t), nil, nil, 13)

	// Control: a valid internal message still estimates identically.
	valid, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     tonopsTestAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(100),
	})
	if err != nil {
		t.Fatalf("build valid message: %v", err)
	}
	assertSendMsgShapeParity(t, "valid-internal", valid, sendMsgParityMyAddr(t), nil, nil, 13)
}

// The message root goes through the regular cell load: a library root
// resolves, while a missing library fails with a cell underflow.
func TestTVMCrossEmulatorSENDMSGExoticRootParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	inner, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     tonopsTestAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(100),
	})
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	libCell := mustLibraryCellForHash(t, inner.Hash())
	collection := mustCrossLibraryCollection(t, inner)

	assertSendMsgShapeParity(t, "library-root-resolved", libCell, sendMsgParityMyAddr(t), nil, collection, 13)

	other := mustCrossLibraryCollection(t, cell.BeginCell().MustStoreUInt(0x55, 8).EndCell())
	assertSendMsgShapeParity(t, "library-root-missing", libCell, sendMsgParityMyAddr(t), nil, other, 13)
}

// All three size-limits record versions are accepted, and the record must
// consume the config slice exactly.
func TestTVMCrossEmulatorSENDMSGSizeLimitsConfigParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	msg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     tonopsTestAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(100),
		Body:        cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0xAA}, 100), 800).EndCell(),
	})
	if err != nil {
		t.Fatalf("build message: %v", err)
	}

	v3, err := tlb.ToCell(&tlb.SizeLimitsConfigV3{
		MaxMsgBits:                  1 << 20,
		MaxMsgCells:                 128,
		MaxLibraryCells:             1000,
		MaxVMDataDepth:              512,
		MaxExtMsgSize:               65535,
		MaxExtMsgDepth:              512,
		MaxAccStateCells:            1 << 16,
		MaxMCAccStateCells:          1 << 11,
		MaxAccPublicLibraries:       256,
		DeferOutQueueSizeLimit:      256,
		MaxMsgExtraCurrencies:       2,
		MaxAccFixedPrefixLength:     8,
		AccStateCellsForStorageDict: 2,
		MaxTotalMsgBits:             1 << 21,
		MaxTotalMsgCells:            1 << 13,
	})
	if err != nil {
		t.Fatalf("build v3 size limits: %v", err)
	}

	// v3 record: both engines accept it and use its max_msg_cells.
	setSlot6 := func(sl *cell.Slice) func(*tuple.Tuple) {
		return func(unpacked *tuple.Tuple) {
			mustSetTupleValue(t, unpacked, 6, sl)
		}
	}
	assertSendMsgShapeParity(t, "size-limits-v3", msg, sendMsgParityMyAddr(t),
		setSlot6(v3.MustBeginParse()), nil, 13)

	// Trailing bits after a v1 record make the config invalid.
	v1WithTail := cell.BeginCell().
		MustStoreUInt(0x01, 8).
		MustStoreUInt(1<<20, 32).
		MustStoreUInt(128, 32).
		MustStoreUInt(1000, 32).
		MustStoreUInt(512, 16).
		MustStoreUInt(65535, 32).
		MustStoreUInt(512, 16).
		MustStoreUInt(1, 1). // trailing garbage
		ToSlice()
	assertSendMsgShapeParity(t, "size-limits-trailing", msg, sendMsgParityMyAddr(t),
		setSlot6(v1WithTail), nil, 13)

	// A non-slice slot 6 falls back to the default limit on both engines.
	assertSendMsgShapeParity(t, "size-limits-non-slice", msg, sendMsgParityMyAddr(t),
		func(unpacked *tuple.Tuple) {
			mustSetTupleValue(t, unpacked, 6, int64(42))
		}, nil, 13)
}
