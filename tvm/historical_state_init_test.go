package tvm

import (
	"bytes"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestHistoricalExternalStateInitScope(t *testing.T) {
	code := cell.BeginCell().MustStoreUInt(0xA1, 8).EndCell()
	data := cell.BeginCell().MustStoreUInt(0xB2, 8).EndCell()
	accountLibs := buildTransactionV13LibraryDict(t, code, false)
	inboundLibs := buildTransactionV13LibraryDict(t, data, false)
	stateInit := &tlb.StateInit{Code: data, Data: code, Lib: inboundLibs}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(stateCell.Hash(), tonopsTestAddr.Data()) {
		t.Fatal("test requires a mismatching StateInit hash")
	}

	for _, tc := range []struct {
		name     string
		version  uint32
		enabled  bool
		internal bool
		accepted bool
	}{
		{name: "modern version zero", version: 0},
		{name: "historical version zero", enabled: true, accepted: true},
		{name: "version one", version: 1, enabled: true, accepted: true},
		{name: "version two", version: 2, enabled: true, accepted: true},
		{name: "version three", version: 3, enabled: true, accepted: true},
		{name: "version four", version: 4, enabled: true, accepted: true},
		{name: "modern version four", version: 4},
		{name: "version five", version: 5, enabled: true},
		{name: "current version", version: vm.MaxSupportedGlobalVersion, enabled: true},
		{name: "internal modern", internal: true, accepted: true},
		{name: "internal historical", internal: true, enabled: true, accepted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acc := &transactionRuntimeAccount{
				addr: tonopsTestAddr, status: tlb.AccountStatusActive,
				code: code, data: data, libraries: accountLibs,
			}
			msg := tlb.Message{MsgType: tlb.MsgTypeExternalIn, Msg: &tlb.ExternalMessage{StateInit: stateInit}}
			if tc.internal {
				msg = tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: &tlb.InternalMessage{StateInit: stateInit}}
			}
			next, used, skip, err := transactionPrepareComputeAccount(acc, tlb.AccountStatusActive, false, &msg, false,
				transactionTestConfigWithGlobalVersion(t, tc.version), tc.enabled, false)
			if err != nil {
				t.Fatal(err)
			}
			if used {
				t.Fatal("active account must not activate from inbound StateInit")
			}
			if !tc.accepted {
				if skip == nil || skip.Type != tlb.ComputeSkipReasonBadState {
					t.Fatalf("skip = %v, want bad state", skip)
				}
				return
			}
			if skip != nil {
				t.Fatalf("unexpected compute skip: %v", skip)
			}
			if next.code != code || next.data != data || next.libraries != accountLibs || next.status != tlb.AccountStatusActive {
				t.Fatal("inbound StateInit replaced active account state")
			}
			if next.inMsgLibraries != inboundLibs || acc.inMsgLibraries != nil {
				t.Fatal("inbound libraries must be exposed only in the compute account")
			}
		})
	}
}

func TestHistoricalExternalStateInitKeepsActivationChecks(t *testing.T) {
	stateInit := &tlb.StateInit{Code: cell.BeginCell().EndCell()}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatal(err)
	}
	msg := &tlb.Message{MsgType: tlb.MsgTypeExternalIn, Msg: &tlb.ExternalMessage{StateInit: stateInit}}
	cfg := transactionTestConfigWithGlobalVersion(t, 0)
	for _, tc := range []struct {
		name    string
		status  tlb.AccountStatus
		deleted bool
	}{
		{name: "deploy uninitialized", status: tlb.AccountStatusUninit},
		{name: "deploy nonexistent", status: tlb.AccountStatusNonExist},
		{name: "unfreeze", status: tlb.AccountStatusFrozen},
		{name: "deleted active account", status: tlb.AccountStatusActive, deleted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acc := &transactionRuntimeAccount{addr: tonopsTestAddr, status: tc.status, stateHash: stateCell.Hash()}
			_, used, skip, err := transactionPrepareComputeAccount(acc, tc.status, tc.deleted, msg, false, cfg, true, false)
			if err != nil {
				t.Fatal(err)
			}
			if used || skip == nil || skip.Type != tlb.ComputeSkipReasonBadState {
				t.Fatalf("used=%t skip=%v, want bad state", used, skip)
			}
		})
	}
}

func TestHistoricalExternalStateInitLibraries(t *testing.T) {
	empty := cell.BeginCell().EndCell()
	library := codeFromBuilders(t, funcsop.ACCEPT().Serialize())
	libRef, err := cell.BeginCell().MustStoreUInt(uint64(cell.LibraryCellType), 8).
		MustStoreSlice(library.Hash(), 256).EndCellSpecial(true)
	if err != nil {
		t.Fatal(err)
	}
	code := codeFromBuilders(t, execop.JMPREF(libRef).Serialize())
	data := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	now := uint32(tonopsTestTime.Unix())
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, data, walletSendTestBalance, now)
	account, err := PrepareAccount(shard, tonopsTestAddr)
	if err != nil {
		t.Fatal(err)
	}
	msgCell, err := tlb.ToCell(&tlb.ExternalMessage{
		DstAddr: tonopsTestAddr, Body: empty,
		StateInit: &tlb.StateInit{Code: empty, Data: empty, Lib: buildTransactionV13LibraryDict(t, library, false)},
	})
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
		name    string
		enabled bool
	}{
		{name: "modern"},
		{name: "historical", enabled: true},
		{name: "modern after historical"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := TransactionOptions{LogicalTime: transactionTestLogicalTime, HistoricalExternalStateInit: tc.enabled}
			accepted, err := machine.CheckExternalMessageAccepted(block, account, message, opts)
			if err != nil {
				t.Fatal(err)
			}
			if accepted != tc.enabled {
				t.Fatalf("accepted = %t, want %t", accepted, tc.enabled)
			}
			result, err := machine.EmulateTransaction(block, account, message, opts)
			if err != nil {
				t.Fatal(err)
			}
			if result.Accepted != tc.enabled {
				t.Fatalf("transaction accepted = %t, want %t", result.Accepted, tc.enabled)
			}
			if !tc.enabled {
				if result.TransactionCell != nil || result.GasUsed != 0 || result.Steps != 0 {
					t.Fatal("modern rules must reject the mismatching StateInit before execution")
				}
				return
			}
			if result.TransactionCell == nil || result.NextAccount == nil || result.ExitCode != 0 {
				t.Fatal("historical rules must execute using the inbound library")
			}
			state := testResultAccountState(result)
			if !bytes.Equal(state.StateInit.Code.Hash(), code.Hash()) || !bytes.Equal(state.StateInit.Data.Hash(), data.Hash()) {
				t.Fatal("inbound StateInit changed the stored account code or data")
			}
			if state.StateInit.Lib != nil && !state.StateInit.Lib.IsEmpty() {
				t.Fatal("inbound libraries were persisted in the account")
			}
		})
	}
}
