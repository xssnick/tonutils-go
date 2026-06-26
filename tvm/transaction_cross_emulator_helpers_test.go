//go:build cgo && tvm_cross_emulator

package tvm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
)

func buildTransactionZeroBitsCell(t *testing.T, bits uint) *cell.Cell {
	t.Helper()

	return cell.BeginCell().
		MustStoreSlice(make([]byte, (bits+7)/8), bits).
		EndCell()
}

func buildTransactionRepeatedSetCodeActions(t *testing.T, code *cell.Cell, count int) *cell.Cell {
	t.Helper()

	actions := make([]any, count)
	for i := range actions {
		actions[i] = tlb.ActionSetCode{NewCode: code}
	}
	return buildTransactionActionList(t, actions...)
}

func buildTransactionTestFrozenShardAccount(t *testing.T, addr *address.Address, stateHash []byte, balance uint64, storage tlb.StorageInfo) *tlb.ShardAccount {
	t.Helper()

	if storage.StorageUsed.CellsUsed == nil {
		storage.StorageUsed.CellsUsed = big.NewInt(0)
	}
	if storage.StorageUsed.BitsUsed == nil {
		storage.StorageUsed.BitsUsed = big.NewInt(0)
	}
	if storage.StorageExtra == nil {
		storage.StorageExtra = tlb.StorageExtraNone{}
	}

	account := &tlb.AccountState{
		IsValid:     true,
		Address:     addr,
		StorageInfo: storage,
		AccountStorage: tlb.AccountStorage{
			LastTransactionLT: 0,
			Balance:           tlb.FromNanoTONU(balance),
			Status:            tlb.AccountStatusFrozen,
			StateHash:         append([]byte(nil), stateHash...),
		},
	}

	accountCell, err := tlb.ToCell(account)
	if err != nil {
		t.Fatalf("failed to build frozen account: %v", err)
	}
	return &tlb.ShardAccount{
		Account:       accountCell,
		LastTransHash: make([]byte, 32),
		LastTransLT:   0,
	}
}

func makeTransactionInternalReserveSendCode(t *testing.T, amount uint64, reserveMode uint8, outMsg, newData *cell.Cell, sendMode uint8) *cell.Cell {
	t.Helper()

	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHINT(new(big.Int).SetUint64(amount)).Serialize(),
		stackop.PUSHINT(new(big.Int).SetUint64(uint64(reserveMode))).Serialize(),
		funcsop.RAWRESERVE().Serialize(),
		stackop.PUSHREF(outMsg).Serialize(),
		stackop.PUSHINT(new(big.Int).SetUint64(uint64(sendMode))).Serialize(),
		funcsop.SENDRAWMSG().Serialize(),
		stackop.PUSHREF(newData).Serialize(),
		execop.POPCTR(4).Serialize(),
	)
}

func makeTransactionInternalReserveOnlyCode(t *testing.T, amount uint64, mode uint8, newData *cell.Cell) *cell.Cell {
	t.Helper()

	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHINT(new(big.Int).SetUint64(amount)).Serialize(),
		stackop.PUSHINT(new(big.Int).SetUint64(uint64(mode))).Serialize(),
		funcsop.RAWRESERVE().Serialize(),
		stackop.PUSHREF(newData).Serialize(),
		execop.POPCTR(4).Serialize(),
	)
}

func makeTransactionInternalSetCodeCode(t *testing.T, code, newData *cell.Cell) *cell.Cell {
	t.Helper()

	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHREF(code).Serialize(),
		funcsop.SETCODE().Serialize(),
		stackop.PUSHREF(newData).Serialize(),
		execop.POPCTR(4).Serialize(),
	)
}

func makeTransactionInternalSetLibCode(t *testing.T, libCode, newData *cell.Cell, mode uint8) *cell.Cell {
	t.Helper()

	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHREF(libCode).Serialize(),
		stackop.PUSHINT(new(big.Int).SetUint64(uint64(mode))).Serialize(),
		funcsop.SETLIBCODE().Serialize(),
		stackop.PUSHREF(newData).Serialize(),
		execop.POPCTR(4).Serialize(),
	)
}

func makeTransactionInternalChangeLibMissingCode(t *testing.T, newData *cell.Cell, mode uint8) *cell.Cell {
	t.Helper()

	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHINT(big.NewInt(0)).Serialize(),
		stackop.PUSHINT(new(big.Int).SetUint64(uint64(mode))).Serialize(),
		funcsop.CHANGELIB().Serialize(),
		stackop.PUSHREF(newData).Serialize(),
		execop.POPCTR(4).Serialize(),
	)
}

func makeTransactionInternalActionsCode(t *testing.T, actions, newData *cell.Cell) *cell.Cell {
	t.Helper()

	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHREF(actions).Serialize(),
		execop.POPCTR(5).Serialize(),
		stackop.PUSHREF(newData).Serialize(),
		execop.POPCTR(4).Serialize(),
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
