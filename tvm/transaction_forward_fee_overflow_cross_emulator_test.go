//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// Transaction-level forward fees are uint64 in the reference engine. One
// paid body cell makes MaxUint64 + 1 wrap the external import fee to zero;
// TVM fee opcodes intentionally keep using the arbitrary-precision API.
func TestTVMCrossEmulatorTransactionImportFeeUint64Wrap(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	prices := &tlb.ConfigMsgForwardPrices{
		LumpPrice: math.MaxUint64,
		CellPrice: 1 << 16,
	}
	pricesCell, err := tlb.ToCell(prices)
	if err != nil {
		t.Fatalf("failed to build forward prices: %v", err)
	}
	configRoot := referenceTransactionConfigRootWithOverrides(t, mustReferenceTransactionConfigRoot(t), map[int32]*cell.Cell{
		int32(tlb.ConfigParamMsgForwardPricesBasechain): pricesCell,
	})

	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	code := makeTransactionExternalSuccessCode(t, newData)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)
	body := cell.BeginCell().EndCell()
	msg := &tlb.Message{
		MsgType: tlb.MsgTypeExternalIn,
		Msg: &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
			Body:    body,
		},
	}
	msgBuilder := cell.BeginCell()
	if err = tlb.StoreMessageWithLayout(msgBuilder, msg, tlb.MessageLayout{BodyInRef: true}); err != nil {
		t.Fatalf("failed to build external message with referenced body: %v", err)
	}
	msgCell := msgBuilder.EndCell()
	usage, err := transactionMessageTailUsage(msgCell)
	if err != nil {
		t.Fatalf("failed to measure external message: %v", err)
	}
	if usage != (transactionUsage{cells: 1}) {
		t.Fatalf("external message tail usage = %+v, want one paid cell", usage)
	}

	goRes, err := testEmulateTransaction(NewTVM(), shard, msgCell, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msgCell, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	if !goRes.Accepted {
		t.Fatal("go external transaction was not accepted")
	}
	if goRes.ExitCode != refRes.exitCode || goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("execution mismatch: go exit/gas=%d/%d reference=%d/%d", goRes.ExitCode, goRes.GasUsed, refRes.exitCode, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", transactionCrossShardSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardSummary(t, refRes.shardCell))
	}
}

// The action-fine cell budget also multiplies two uint64 values in the
// reference engine before comparing the result with the account balance.
// With this valid extreme config the wrapped budget admits the two-cell
// message, which then fails for forward funds with result 37.
func TestTVMCrossEmulatorTransactionActionFineUint64Wrap(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	const finePerCell = uint64(1 << 33)
	pricesCell, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{CellPrice: finePerCell << 18})
	if err != nil {
		t.Fatalf("failed to build forward prices: %v", err)
	}
	configRoot := referenceTransactionConfigRootWithOverrides(t, mustReferenceTransactionConfigRoot(t), map[int32]*cell.Cell{
		int32(tlb.ConfigParamMsgForwardPricesBasechain): pricesCell,
		int32(tlb.ConfigParamSizeLimits): buildTransactionSizeLimitsCell(
			t,
			1<<21,
			1<<31,
			1<<16,
			1<<16,
			1<<16,
		),
	})

	leaf := cell.BeginCell().EndCell()
	body := cell.BeginCell().MustStoreRef(leaf).EndCell()
	outMsg := &tlb.Message{
		MsgType: tlb.MsgTypeInternal,
		Msg: &tlb.InternalMessage{
			IHRDisabled: true,
			SrcAddr:     tonopsTestAddr,
			DstAddr:     tonopsTestAddr,
			Amount:      tlb.FromNanoTONU(1),
			Body:        body,
		},
	}
	outBuilder := cell.BeginCell()
	if err = tlb.StoreMessageWithLayout(outBuilder, outMsg, tlb.MessageLayout{BodyInRef: true}); err != nil {
		t.Fatalf("failed to build outbound message: %v", err)
	}
	outMsgCell := outBuilder.EndCell()
	usage, err := transactionMessageTailUsage(outMsgCell)
	if err != nil {
		t.Fatalf("failed to measure outbound message: %v", err)
	}
	if usage != (transactionUsage{cells: 2}) {
		t.Fatalf("outbound message tail usage = %+v, want two paid cells", usage)
	}

	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	code := makeTransactionInternalReserveSendCode(t, 6_000_000_000, 0, outMsgCell, newData, 1)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, 20_000_000_000, now)
	inMsg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        cell.BeginCell().EndCell(),
	})

	goRes, err := testEmulateTransaction(NewTVM(), shard, inMsg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, inMsg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	wantAction := transactionActionPhaseExpectation{valid: true, resultCode: 37}
	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, wantAction)
	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, wantAction)
	// The raw two-cell fine is 2^34, but an immediately failing send can
	// collect only the temporary balance left after RAWRESERVE and gas.
	const wantImmediateFine = uint64(14_999_492_800)
	assertOrdinaryTransactionActionFees(t, "go", goRes.TransactionCell, wantImmediateFine)
	assertOrdinaryTransactionActionFees(t, "reference", refRes.txCell, wantImmediateFine)
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", transactionCrossShardSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardSummary(t, refRes.shardCell))
	}
}

// MsgPrices::try_action_send_msg compares the suggested fee with the computed
// uint64 fee after casting the latter to signed long long. At 2^63 the cast is
// negative, so internal messages keep their v7 suggestion (or the forced zero
// since v8), and external messages keep their initial zero.
func TestTVMCrossEmulatorTransactionComputedFeeSignedCastBoundary(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	pricesCell, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{LumpPrice: 1 << 63})
	if err != nil {
		t.Fatalf("failed to build forward prices: %v", err)
	}
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	body := cell.BeginCell().EndCell()
	internalOut := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     tonopsTestAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(100),
		FwdFee:      tlb.FromNanoTONU(7),
		Body:        body,
	})
	externalOut := mustTransactionMsgCell(t, &tlb.ExternalMessageOut{
		SrcAddr: tonopsTestAddr,
		DstAddr: address.NewAddressExt(0, 8, []byte{0x5A}),
		Body:    body,
	})
	inMsg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})
	baseConfigRoot := mustReferenceTransactionConfigRoot(t)

	for _, version := range []uint32{7, 8} {
		configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
		configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
			int32(tlb.ConfigParamMsgForwardPricesBasechain):   pricesCell,
			int32(tlb.ConfigParamMsgForwardPricesMasterchain): pricesCell,
		})

		for _, tc := range []struct {
			name string
			msg  *cell.Cell
			mode uint8
		}{
			{name: "internal", msg: internalOut, mode: 1},
			{name: "external", msg: externalOut, mode: 0},
		} {
			t.Run(tc.name+"_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
				code := makeTransactionInternalSendCode(t, tc.msg, newData, tc.mode)
				shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, 2_000_000_000, now)
				goRes, err := testEmulateTransaction(NewTVM(), shard, inMsg, testTxParams{
					Address:     tonopsTestAddr,
					Now:         now,
					BlockLT:     transactionTestLogicalTime,
					LogicalTime: transactionTestLogicalTime,
					RandSeed:    append([]byte(nil), tonopsTestSeed...),
					ConfigRoot:  configRoot,
				})
				if err != nil {
					t.Fatalf("go transaction emulation failed: %v", err)
				}
				refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, inMsg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
				if err != nil {
					t.Fatalf("reference transaction emulation failed: %v", err)
				}

				wantAction := transactionActionPhaseExpectation{success: true, valid: true, messagesCreated: 1}
				assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, wantAction)
				assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, wantAction)
				if goRes.GasUsed != refRes.gasUsed {
					t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
				}
				if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
					t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				}
				if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
					t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", transactionCrossShardSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardSummary(t, refRes.shardCell))
				}
			})
		}
	}
}

// prepare_bounce_phase uses the same signed cast in its no-funds predicate.
// A high uint64 fee bypasses that predicate, then makes the bounced value
// negative and the reference rejects the whole transaction while preparing
// the bounce phase.
func TestTVMCrossEmulatorTransactionBounceFeeSignedCastFatal(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	pricesCell, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{LumpPrice: 1 << 63})
	if err != nil {
		t.Fatalf("failed to build forward prices: %v", err)
	}
	configRoot := referenceTransactionConfigRootWithOverrides(t, mustReferenceTransactionConfigRoot(t), map[int32]*cell.Cell{
		int32(tlb.ConfigParamMsgForwardPricesBasechain):   pricesCell,
		int32(tlb.ConfigParamMsgForwardPricesMasterchain): pricesCell,
	})
	now := uint32(tonopsTestTime.Unix())
	code := makeTransactionStackUnderflowCode(t)
	data := cell.BeginCell().EndCell()
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, data, 2_000_000_000, now)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        cell.BeginCell().EndCell(),
	})

	goRes, goErr := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if goErr == nil {
		t.Fatalf("go transaction unexpectedly succeeded: %+v", goRes)
	}
	if !strings.Contains(goErr.Error(), "bounce phase") {
		t.Fatalf("go error = %v, want bounce-phase failure", goErr)
	}
	refRes, refErr := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if refErr == nil {
		t.Fatalf("reference transaction unexpectedly succeeded: %+v", refRes)
	}
	if !strings.Contains(refErr.Error(), "bounce phase") {
		t.Fatalf("reference error = %v, want bounce-phase failure", refErr)
	}
}
