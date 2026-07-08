package tvm

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

const internalMessageTestAmount = uint64(777)

func makeInternalMessageSuccessCode(t *testing.T, newData, outMsg *cell.Cell) *cell.Cell {
	t.Helper()
	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHREF(outMsg).Serialize(),
		stackop.PUSHINT(big.NewInt(1)).Serialize(),
		funcsop.SENDRAWMSG().Serialize(),
		stackop.PUSHREF(newData).Serialize(),
		execop.POPCTR(4).Serialize(),
	)
}

func makeInternalMessageFailureCode(t *testing.T, newData *cell.Cell) *cell.Cell {
	t.Helper()
	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHREF(newData).Serialize(),
		execop.POPCTR(4).Serialize(),
		cell.BeginCell().MustStoreUInt(uint64(0xF200|33), 16),
	)
}

func emulateInternalForTest(t *testing.T, code, data, body *cell.Cell) (*MessageExecutionResult, error) {
	t.Helper()

	return NewTVM().EmulateInternalMessage(code, data, body, internalMessageTestAmount, EmulateInternalMessageConfig{
		Address:  tonopsTestAddr,
		Now:      uint32(tonopsTestTime.Unix()),
		Balance:  new(big.Int).Set(tonopsTestBalance),
		RandSeed: append([]byte(nil), tonopsTestSeed...),
		Config:   transactionTestConfigWithGlobalVersion(t, uint32(vmcore.DefaultGlobalVersion)),
		Gas: vmcore.NewGas(vmcore.GasConfig{
			Max:    DefaultInternalMessageGasMax,
			Limit:  int64(internalMessageTestAmount) * InternalMessageGasAmountFactor,
			Credit: 0,
		}),
	})
}

func TestEmulateInternalMessage(t *testing.T) {
	t.Run("AcceptsAndProducesTypedActions", func(t *testing.T) {
		origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
		newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
		body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
		wantMsg, err := buildInternalMessageForEmulation(tonopsTestAddr, body, internalMessageTestAmount)
		if err != nil {
			t.Fatalf("failed to build expected internal message: %v", err)
		}
		code := makeInternalMessageSuccessCode(t, newData, wantMsg)

		res, err := emulateInternalForTest(t, code, origData, body)
		if err != nil {
			t.Fatalf("emulate internal failed: %v", err)
		}
		if !res.Accepted {
			t.Fatal("expected internal message to be accepted")
		}
		if res.ExitCode != 0 {
			t.Fatalf("unexpected exit code: %d", res.ExitCode)
		}
		if res.Code == nil || !bytes.Equal(res.Code.Hash(), code.Hash()) {
			t.Fatal("expected result code cell to match contract code")
		}
		if !bytes.Equal(res.Data.Hash(), newData.Hash()) {
			t.Fatalf("unexpected data:\nwant=%s\ngot=%s", newData.Dump(), res.Data.Dump())
		}
		if res.Actions == nil {
			t.Fatal("expected output actions")
		}

		actions, err := tlb.LoadOutList(res.Actions)
		if err != nil {
			t.Fatalf("failed to decode actions: %v", err)
		}
		if len(actions) != 1 {
			t.Fatalf("expected 1 action, got %d", len(actions))
		}

		send, ok := actions[0].(tlb.ActionSendMsg)
		if !ok {
			t.Fatalf("unexpected action type: %T", actions[0])
		}
		if send.Mode != 1 {
			t.Fatalf("unexpected send mode: %d", send.Mode)
		}
		if !bytes.Equal(send.Msg.Hash(), wantMsg.Hash()) {
			t.Fatalf("unexpected sent message:\nwant=%s\ngot=%s", wantMsg.Dump(), send.Msg.Dump())
		}
	})

	t.Run("FailedExecutionKeepsOriginalState", func(t *testing.T) {
		origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
		newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
		body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
		code := makeInternalMessageFailureCode(t, newData)

		res, err := emulateInternalForTest(t, code, origData, body)
		if err != nil {
			t.Fatalf("emulate internal failed: %v", err)
		}
		if !res.Accepted {
			t.Fatal("internal failure should still be accepted at TVM level")
		}
		if res.ExitCode != 33 {
			t.Fatalf("unexpected exit code: %d", res.ExitCode)
		}
		if !bytes.Equal(res.Data.Hash(), origData.Hash()) {
			t.Fatal("failed internal execution should keep original data")
		}
		if res.Actions != nil {
			t.Fatal("failed internal execution should not produce actions")
		}
	})
}

func TestEmulateInternalMessageWithAccountProofUsesAccountRoot(t *testing.T) {
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	wantMsg, err := buildInternalMessageForEmulation(tonopsTestAddr, body, internalMessageTestAmount)
	if err != nil {
		t.Fatalf("failed to build expected internal message: %v", err)
	}
	code := makeInternalMessageSuccessCode(t, newData, wantMsg)
	accountRoot := executionProofAccountStateRoot(t, tlb.AccountState{
		IsValid:     true,
		Address:     tonopsTestAddr,
		StorageInfo: executionProofStorageInfo(),
		AccountStorage: tlb.AccountStorage{
			Status:  tlb.AccountStatusActive,
			Balance: tlb.FromNanoTONU(0),
			StateInit: &tlb.StateInit{
				Code: code,
				Data: origData,
			},
		},
	})

	res, err := NewTVM().EmulateInternalMessage(nil, nil, body, internalMessageTestAmount, EmulateInternalMessageConfig{
		BuildProof:  true,
		AccountRoot: accountRoot,
		Now:         uint32(tonopsTestTime.Unix()),
		Balance:     new(big.Int).Set(tonopsTestBalance),
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		Config:      transactionTestConfigWithGlobalVersion(t, uint32(vmcore.DefaultGlobalVersion)),
		Gas: vmcore.NewGas(vmcore.GasConfig{
			Max:   DefaultInternalMessageGasMax,
			Limit: int64(internalMessageTestAmount) * InternalMessageGasAmountFactor,
		}),
	})
	if err != nil {
		t.Fatalf("emulate internal with account proof failed: %v", err)
	}
	if !res.Accepted {
		t.Fatal("expected internal message to be accepted")
	}
	if res.Proof == nil {
		t.Fatal("expected execution proof")
	}
	if _, err = cell.UnwrapProof(res.Proof, accountRoot.Hash()); err != nil {
		t.Fatalf("account execution proof is invalid: %v", err)
	}
	if !bytes.Equal(res.Code.Hash(), code.Hash()) {
		t.Fatal("result code should come from account root")
	}
	if !bytes.Equal(res.Data.Hash(), newData.Hash()) {
		t.Fatal("result data should come from execution over account root data")
	}
}

func TestEmulateInternalMessageChksigAlwaysSucceedPerRun(t *testing.T) {
	signature := make([]byte, 64)
	signature[0] = 1
	signature[63] = 2
	code := makeMessageChksigAlwaysCode(t, signature)
	data := cell.BeginCell().EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	machine := NewTVM()

	for _, tt := range []struct {
		name   string
		always bool
		want   bool
	}{
		{name: "default_rejects", always: false, want: false},
		{name: "configured_accepts", always: true, want: true},
		{name: "next_default_rejects", always: false, want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res, err := machine.EmulateInternalMessage(code, data, body, internalMessageTestAmount, EmulateInternalMessageConfig{
				Address:             tonopsTestAddr,
				Now:                 uint32(tonopsTestTime.Unix()),
				Balance:             new(big.Int).Set(tonopsTestBalance),
				RandSeed:            append([]byte(nil), tonopsTestSeed...),
				Config:              transactionTestConfigWithGlobalVersion(t, uint32(vmcore.DefaultGlobalVersion)),
				ChksigAlwaysSucceed: tt.always,
				Gas: vmcore.NewGas(vmcore.GasConfig{
					Max:   DefaultInternalMessageGasMax,
					Limit: int64(internalMessageTestAmount) * InternalMessageGasAmountFactor,
				}),
			})
			if err != nil {
				t.Fatalf("emulate internal failed: %v", err)
			}
			if !vmcore.IsSuccessExitCode(res.ExitCode) {
				t.Fatalf("exit code = %d, want success", res.ExitCode)
			}

			got, err := res.Stack.PopBool()
			if err != nil {
				t.Fatalf("pop CHKSIGNU result: %v", err)
			}
			if got != tt.want {
				t.Fatalf("CHKSIGNU result = %t, want %t", got, tt.want)
			}
		})
	}
}
