package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestCheckExternalMessageAcceptedHistoricalPopC3Cell(t *testing.T) {
	empty := cell.BeginCell().EndCell()
	code := codeFromBuilders(t,
		stackop.PUSHREF(empty).Serialize(),
		execop.POPCTR(3).Serialize(),
		funcsop.ACCEPT().Serialize(),
	)
	now := uint32(tonopsTestTime.Unix())
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, empty, walletSendTestBalance, now)
	account, err := PrepareAccount(shard, tonopsTestAddr)
	if err != nil {
		t.Fatal(err)
	}
	msgCell, err := tlb.ToCell(&tlb.ExternalMessage{DstAddr: tonopsTestAddr, Body: empty})
	if err != nil {
		t.Fatal(err)
	}
	message, err := PrepareMessage(msgCell)
	if err != nil {
		t.Fatal(err)
	}
	block, err := transactionTestConfigWithGlobalVersion(t, 0).NewBlockContext(BlockOptions{
		Now: now, BlockLT: transactionTestLogicalTime, RandSeed: tonopsTestSeed,
	})
	if err != nil {
		t.Fatal(err)
	}

	machine := NewTVM()
	for _, tc := range []struct {
		name       string
		historical vm.HistoricalConfig
		accepted   bool
	}{
		{name: "modern"},
		{
			name:       "historical",
			historical: vm.HistoricalConfig{GasSchedule: vm.GasSchedule2019, PopC3Cell: true},
			accepted:   true,
		},
		{name: "modern after historical"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accepted, err := machine.CheckExternalMessageAccepted(block, account, message, TransactionOptions{
				LogicalTime: transactionTestLogicalTime,
				Historical:  tc.historical,
			})
			if err != nil {
				t.Fatal(err)
			}
			if accepted != tc.accepted {
				t.Fatalf("accepted = %t, want %t", accepted, tc.accepted)
			}
		})
	}
}
