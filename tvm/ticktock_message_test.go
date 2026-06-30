package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
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

func makeTickTockChksigAlwaysVariantCode(t *testing.T, tt executionConfigSignatureCase, signature []byte) *cell.Cell {
	t.Helper()

	builders := []*cell.Builder{
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
	}
	if tt.fromSlice {
		builders = append(builders, stackop.PUSHSLICE(cell.BeginCell().MustStoreSlice([]byte{0x10, 0x20, 0x30, 0x40}, 32).ToSlice()).Serialize())
	} else {
		builders = append(builders, stackop.PUSHINT(big.NewInt(0)).Serialize())
	}

	builders = append(builders, stackop.PUSHSLICE(cell.BeginCell().MustStoreSlice(signature, 512).ToSlice()).Serialize())
	if tt.p256 {
		key := make([]byte, 33)
		key[0] = 0x05
		builders = append(builders, stackop.PUSHSLICE(cell.BeginCell().MustStoreSlice(key, 264).ToSlice()).Serialize())
	} else {
		builders = append(builders, stackop.PUSHINT(big.NewInt(2)).Serialize())
	}

	builders = append(builders, chksigAlwaysVariantOpcode(t, tt))
	return codeFromBuilders(t, builders...)
}

func emulateTickTockForTest(t *testing.T, code, data *cell.Cell, isTock bool) (*TransactionExecutionResult, *cell.Cell, *cell.Cell, error) {
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

	shard, err := buildTickTockShardAccountForTest(t, tickTockTestAddr, code, data, tickTockTestBalance)
	if err != nil {
		return nil, nil, nil, err
	}

	cfg := TransactionEmulationConfig{
		Now:        uint32(tonopsTestTime.Unix()),
		Balance:    new(big.Int).SetUint64(tickTockTestBalance),
		RandSeed:   append([]byte(nil), tonopsTestSeed...),
		ConfigRoot: transactionTestConfigWithGlobalVersion(t, uint32(vmcore.DefaultGlobalVersion)).Root,
		Gas: vmcore.NewGas(vmcore.GasConfig{
			Max:   DefaultTickTockTransactionGasMax,
			Limit: DefaultTickTockTransactionGasMax,
		}),
	}

	res, err := NewTVM().EmulateTickTockTransaction(shard, isTock, cfg)
	return res, tickMsg, tockMsg, err
}

func buildTickTockShardAccountForTest(t *testing.T, addr *address.Address, code, data *cell.Cell, balance uint64) (*tlb.ShardAccount, error) {
	t.Helper()

	account := &tlb.AccountState{
		IsValid: true,
		Address: addr,
		StorageInfo: tlb.StorageInfo{
			StorageUsed: tlb.StorageUsed{
				CellsUsed: big.NewInt(0),
				BitsUsed:  big.NewInt(0),
			},
			StorageExtra: tlb.StorageExtraNone{},
			LastPaid:     0,
		},
		AccountStorage: tlb.AccountStorage{
			LastTransactionLT: 0,
			Balance:           tlb.FromNanoTONU(balance),
			Status:            tlb.AccountStatusActive,
			StateInit: &tlb.StateInit{
				TickTock: &tlb.TickTock{Tick: true, Tock: true},
				Code:     code,
				Data:     data,
			},
		},
	}

	accountCell, err := tlb.ToCell(account)
	if err != nil {
		return nil, err
	}
	return &tlb.ShardAccount{
		Account:       accountCell,
		LastTransHash: make([]byte, 32),
		LastTransLT:   0,
	}, nil
}

func TestEmulateTickTockTransactionChksigAlwaysSucceedPerRun(t *testing.T) {
	data := cell.BeginCell().EndCell()
	signature := make([]byte, 64)
	signature[0] = 0xC3
	signature[63] = 0x3C

	for _, tt := range executionConfigSignatureCases {
		t.Run(tt.name, func(t *testing.T) {
			code := makeTickTockChksigAlwaysVariantCode(t, tt, signature)

			for _, isTock := range []bool{false, true} {
				name := "tick"
				if isTock {
					name = "tock"
				}

				t.Run(name, func(t *testing.T) {
					for version := uint32(MinSupportedGlobalVersion); version <= uint32(MaxSupportedGlobalVersion); version++ {
						t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
							shard, err := buildTickTockShardAccountForTest(t, tickTockTestAddr, code, data, tickTockTestBalance)
							if err != nil {
								t.Fatalf("failed to build tick/tock shard: %v", err)
							}
							cfg := TransactionEmulationConfig{
								Now:        uint32(tonopsTestTime.Unix()),
								Balance:    new(big.Int).SetUint64(tickTockTestBalance),
								RandSeed:   append([]byte(nil), tonopsTestSeed...),
								ConfigRoot: transactionTestConfigWithGlobalVersion(t, version).Root,
								Gas: vmcore.NewGas(vmcore.GasConfig{
									Max:   DefaultTickTockTransactionGasMax,
									Limit: DefaultTickTockTransactionGasMax,
								}),
							}

							if tt.minVersion > 0 && version < uint32(tt.minVersion) {
								for _, run := range []struct {
									name   string
									always bool
								}{
									{name: "default", always: false},
									{name: "configured", always: true},
									{name: "next_default", always: false},
								} {
									res := runTickTockChksigAlwaysVariant(t, NewTVM(), shard, isTock, cfg, tt, run.always)
									if res.exit != vmerr.CodeInvalidOpcode {
										t.Fatalf("%s %s v%d %s exit=%d, want invalid opcode", tt.name, name, version, run.name, res.exit)
									}
								}
								return
							}

							assertMessageChksigAlwaysVariant(t, tt, version, false, runTickTockChksigAlwaysVariant(t, NewTVM(), shard, isTock, cfg, tt, false), false)
							assertMessageChksigAlwaysVariant(t, tt, version, true, runTickTockChksigAlwaysVariant(t, NewTVM(), shard, isTock, cfg, tt, true), true)
							assertMessageChksigAlwaysVariant(t, tt, version, false, runTickTockChksigAlwaysVariant(t, NewTVM(), shard, isTock, cfg, tt, false), false)
						})
					}
				})
			}
		})
	}
}

func runTickTockChksigAlwaysVariant(t *testing.T, machine *TVM, shard *tlb.ShardAccount, isTock bool, cfg TransactionEmulationConfig, tt executionConfigSignatureCase, always bool) messageChksigAlwaysVariantResult {
	t.Helper()

	cfg.ChksigAlwaysSucceed = always
	res, err := machine.EmulateTickTockTransaction(shard, isTock, cfg)
	var execRes *ExecutionResult
	if res != nil {
		execRes = &res.ExecutionResult
	}
	exit := exitCodeFromResult(execRes, err)
	if exit == -1 {
		t.Fatalf("EmulateTickTockTransaction %s always=%v failed: %v", tt.name, always, err)
	}
	if !vmcore.IsSuccessExitCode(exit) {
		return messageChksigAlwaysVariantResult{exit: exit}
	}
	got, err := res.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop tick/tock %s result always=%v: %v", tt.name, always, err)
	}
	return messageChksigAlwaysVariantResult{exit: exit, ok: got}
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

	t.Run("RequiresShardAccount", func(t *testing.T) {
		_, err := NewTVM().EmulateTickTockTransaction(nil, false, TransactionEmulationConfig{})
		if err == nil {
			t.Fatal("expected missing shard account to fail")
		}
	})
}
