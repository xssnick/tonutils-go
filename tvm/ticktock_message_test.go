package tvm

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

var tickTockTestAddr = address.MustParseRawAddr("-1:0000000000000000000000000000000000000000000000000000000000000001")

const tickTockTestBalance = uint64(1_000_000_000)

func makeTickTockSuccessCode(t *testing.T, tickData, tockData, tickMsg, tockMsg *cell.Cell) *cell.Cell {
	t.Helper()

	tockRef := codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHREF(tockMsg).Serialize(),
		stackop.PUSHINT(big.NewInt(0)).Serialize(),
		funcsop.SENDRAWMSG().Serialize(),
		stackop.PUSHREF(tockData).Serialize(),
		execop.POPCTR(4).Serialize(),
	)

	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		execop.IFJMPREF(tockRef).Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHREF(tickMsg).Serialize(),
		stackop.PUSHINT(big.NewInt(0)).Serialize(),
		funcsop.SENDRAWMSG().Serialize(),
		stackop.PUSHREF(tickData).Serialize(),
		execop.POPCTR(4).Serialize(),
	)
}

func makeTickTockFailureCode(t *testing.T) *cell.Cell {
	t.Helper()
	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		cell.BeginCell().MustStoreUInt(uint64(0xF200|52), 16),
	)
}

func makeTickTockStateOnlyCode(t *testing.T, tickData, tockData *cell.Cell) *cell.Cell {
	t.Helper()

	tockRef := codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHREF(tockData).Serialize(),
		execop.POPCTR(4).Serialize(),
	)

	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		execop.IFJMPREF(tockRef).Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHREF(tickData).Serialize(),
		execop.POPCTR(4).Serialize(),
	)
}

func emulateTickTockForTest(t *testing.T, code, data *cell.Cell, isTock bool) (*MessageExecutionResult, *cell.Cell, *cell.Cell, error) {
	t.Helper()

	tickBody := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	tockBody := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	tickMsg, err := buildInternalMessageForEmulation(tickTockTestAddr, tickBody, 100)
	if err != nil {
		return nil, nil, nil, err
	}
	tockMsg, err := buildInternalMessageForEmulation(tickTockTestAddr, tockBody, 200)
	if err != nil {
		return nil, nil, nil, err
	}

	cfg := EmulateTickTockTransactionConfig{
		Address:  tickTockTestAddr,
		Now:      uint32(tonopsTestTime.Unix()),
		Balance:  new(big.Int).SetUint64(tickTockTestBalance),
		RandSeed: append([]byte(nil), tonopsTestSeed...),
		Gas: vmcore.NewGas(vmcore.GasConfig{
			Max:   DefaultTickTockTransactionGasMax,
			Limit: DefaultTickTockTransactionGasMax,
		}),
	}

	var res *MessageExecutionResult
	if isTock {
		res, err = NewTVM().EmulateTockTransaction(code, data, cfg)
	} else {
		res, err = NewTVM().EmulateTickTransaction(code, data, cfg)
	}
	return res, tickMsg, tockMsg, err
}

func TestEmulateTickTockTransaction(t *testing.T) {
	t.Run("TickAndTockCommitDifferentState", func(t *testing.T) {
		origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
		tickData := cell.BeginCell().MustStoreUInt(0x1111, 16).EndCell()
		tockData := cell.BeginCell().MustStoreUInt(0x2222, 16).EndCell()

		tickBody := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
		tockBody := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
		tickMsg, err := buildInternalMessageForEmulation(tickTockTestAddr, tickBody, 100)
		if err != nil {
			t.Fatalf("failed to build tick message: %v", err)
		}
		tockMsg, err := buildInternalMessageForEmulation(tickTockTestAddr, tockBody, 200)
		if err != nil {
			t.Fatalf("failed to build tock message: %v", err)
		}

		code := makeTickTockSuccessCode(t, tickData, tockData, tickMsg, tockMsg)

		tickRes, _, _, err := emulateTickTockForTest(t, code, origData, false)
		if err != nil {
			t.Fatalf("emulate tick failed: %v", err)
		}
		if tickRes.ExitCode != 0 {
			t.Fatalf("unexpected tick exit code: %d", tickRes.ExitCode)
		}
		if !tickRes.Accepted {
			t.Fatal("tick transaction should be accepted")
		}
		if !bytes.Equal(tickRes.Data.Hash(), tickData.Hash()) {
			t.Fatalf("unexpected tick data:\nwant=%s\ngot=%s", tickData.Dump(), tickRes.Data.Dump())
		}

		tockRes, _, _, err := emulateTickTockForTest(t, code, origData, true)
		if err != nil {
			t.Fatalf("emulate tock failed: %v", err)
		}
		if tockRes.ExitCode != 0 {
			t.Fatalf("unexpected tock exit code: %d", tockRes.ExitCode)
		}
		if !tockRes.Accepted {
			t.Fatal("tock transaction should be accepted")
		}
		if !bytes.Equal(tockRes.Data.Hash(), tockData.Hash()) {
			t.Fatalf("unexpected tock data:\nwant=%s\ngot=%s", tockData.Dump(), tockRes.Data.Dump())
		}

		tickActions, err := tlb.LoadOutList(tickRes.Actions)
		if err != nil {
			t.Fatalf("failed to decode tick actions: %v", err)
		}
		if len(tickActions) != 1 {
			t.Fatalf("expected 1 tick action, got %d", len(tickActions))
		}
		tickSend, ok := tickActions[0].(tlb.ActionSendMsg)
		if !ok {
			t.Fatalf("unexpected tick action type: %T", tickActions[0])
		}
		if !bytes.Equal(tickSend.Msg.Hash(), tickMsg.Hash()) {
			t.Fatalf("unexpected tick message:\nwant=%s\ngot=%s", tickMsg.Dump(), tickSend.Msg.Dump())
		}

		tockActions, err := tlb.LoadOutList(tockRes.Actions)
		if err != nil {
			t.Fatalf("failed to decode tock actions: %v", err)
		}
		if len(tockActions) != 1 {
			t.Fatalf("expected 1 tock action, got %d", len(tockActions))
		}
		tockSend, ok := tockActions[0].(tlb.ActionSendMsg)
		if !ok {
			t.Fatalf("unexpected tock action type: %T", tockActions[0])
		}
		if !bytes.Equal(tockSend.Msg.Hash(), tockMsg.Hash()) {
			t.Fatalf("unexpected tock message:\nwant=%s\ngot=%s", tockMsg.Dump(), tockSend.Msg.Dump())
		}
	})

	t.Run("FailureKeepsOriginalState", func(t *testing.T) {
		origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
		code := makeTickTockFailureCode(t)

		res, _, _, err := emulateTickTockForTest(t, code, origData, false)
		if err != nil {
			t.Fatalf("emulate failing tick failed: %v", err)
		}
		if res.ExitCode != 52 {
			t.Fatalf("unexpected exit code: %d", res.ExitCode)
		}
		if !res.Accepted {
			t.Fatal("tick/tock failure should still be accepted at TVM level")
		}
		if !bytes.Equal(res.Data.Hash(), origData.Hash()) {
			t.Fatal("failed tick/tock execution should keep original data")
		}
		if res.Actions != nil {
			t.Fatal("failed tick/tock execution should not produce actions")
		}
	})

	t.Run("RequiresAddress", func(t *testing.T) {
		_, err := NewTVM().EmulateTickTransaction(cell.BeginCell().EndCell(), cell.BeginCell().EndCell(), EmulateTickTockTransactionConfig{})
		if err == nil {
			t.Fatal("expected missing address to fail")
		}
	})
}
