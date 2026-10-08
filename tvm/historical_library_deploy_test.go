package tvm

import (
	"fmt"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestHistoricalPublicLibraryDeployScope(t *testing.T) {
	for _, version := range []uint32{0, 1, 2, 3, 4, 5, 15} {
		for _, tc := range []struct {
			name       string
			workchain  int
			public     bool
			historical bool
		}{
			{name: "masterchain public modern", workchain: -1, public: true},
			{name: "masterchain public historical", workchain: -1, public: true, historical: true},
			{name: "masterchain private modern", workchain: -1},
			{name: "masterchain private historical", workchain: -1, historical: true},
			{name: "basechain public modern", public: true},
			{name: "basechain public historical", public: true, historical: true},
			{name: "basechain private modern"},
			{name: "basechain private historical", historical: true},
		} {
			t.Run(fmt.Sprintf("v%d/%s", version, tc.name), func(t *testing.T) {
				code := cell.BeginCell().MustStoreUInt(0xA1, 8).EndCell()
				data := cell.BeginCell().MustStoreUInt(0xB2, 8).EndCell()
				state := &tlb.StateInit{Code: code, Data: data, Lib: buildTransactionV13LibraryDict(t, code, tc.public)}
				addr := state.CalcAddress(tc.workchain)
				acc := &transactionRuntimeAccount{addr: addr, status: tlb.AccountStatusUninit}
				msg := &tlb.Message{MsgType: tlb.MsgTypeExternalIn, Msg: &tlb.ExternalMessage{DstAddr: addr, StateInit: state}}
				cfg := transactionTestConfigWithGlobalVersion(t, version)

				next, used, skip, err := transactionPrepareComputeAccount(acc, acc.status, false, msg, false, cfg, false, tc.historical)
				if err != nil {
					t.Fatal(err)
				}
				wantRejected := version >= 15 || (tc.workchain == -1 && tc.public && (!tc.historical || version > 4))
				if wantRejected {
					if used || skip == nil || skip.Type != tlb.ComputeSkipReasonBadState {
						t.Fatalf("used=%t skip=%v, want bad state", used, skip)
					}
				} else {
					if !used || skip != nil {
						t.Fatalf("used=%t skip=%v, want activation", used, skip)
					}
					if next.status != tlb.AccountStatusActive || next.code != code || next.data != data || next.libraries != state.Lib {
						t.Fatal("deployment lost the inbound StateInit")
					}
				}
				if acc.status != tlb.AccountStatusUninit || acc.code != nil || acc.data != nil || acc.libraries != nil {
					t.Fatal("deployment mutated the input account")
				}
			})
		}
	}
}

func TestHistoricalPublicLibraryDeployKeepsStateChecks(t *testing.T) {
	code := cell.BeginCell().MustStoreUInt(0xA1, 8).EndCell()
	state := &tlb.StateInit{Code: code, Data: cell.BeginCell().EndCell(), Lib: buildTransactionV13LibraryDict(t, code, true)}
	addr := state.CalcAddress(-1)
	msg := &tlb.Message{MsgType: tlb.MsgTypeExternalIn, Msg: &tlb.ExternalMessage{DstAddr: addr, StateInit: state}}
	for _, historical := range []bool{false, true} {
		for _, name := range []string{"address", "suspended", "frozen hash", "newly frozen", "state size", "public library limit", "deleted", "valid unfreeze"} {
			t.Run(fmt.Sprintf("historical_%t/%s", historical, name), func(t *testing.T) {
				acc := &transactionRuntimeAccount{addr: addr, status: tlb.AccountStatusUninit}
				cfg := *transactionTestConfigWithGlobalVersion(t, 2)
				status := tlb.AccountStatus(tlb.AccountStatusUninit)
				suspended, deleted := false, false
				wantSkip := tlb.ComputeSkipReasonBadState
				switch name {
				case "address":
					badHash := append([]byte(nil), addr.Data()...)
					badHash[0] ^= 1
					acc.addr = address.NewAddress(0, 255, badHash)
				case "suspended":
					suspended = true
					wantSkip = tlb.ComputeSkipReasonSuspended
				case "frozen hash":
					acc.status, status = tlb.AccountStatusFrozen, tlb.AccountStatusFrozen
					acc.stateHash = make([]byte, 32)
				case "newly frozen":
					status = tlb.AccountStatusFrozen
				case "state size":
					cfg.sizeLimits.maxAccStateCells = 1
				case "public library limit":
					cfg.sizeLimits.maxAccPublicLibraries = 0
				case "deleted":
					deleted = true
				case "valid unfreeze":
					acc.status, status = tlb.AccountStatusFrozen, tlb.AccountStatusFrozen
					acc.stateHash = addr.Data()
				}

				next, used, skip, err := transactionPrepareComputeAccount(acc, status, deleted, msg, suspended, &cfg, false, historical)
				if err != nil {
					t.Fatal(err)
				}
				if name == "valid unfreeze" {
					if !used || skip != nil || next.status != tlb.AccountStatusActive || next.libraries != state.Lib {
						t.Fatalf("valid unfreeze rejected: used=%t skip=%v", used, skip)
					}
					return
				}
				if used || skip == nil || skip.Type != wantSkip {
					t.Fatalf("used=%t skip=%v, want %s", used, skip, wantSkip)
				}
			})
		}
	}
}

func TestHistoricalPublicLibraryDeployRejectsMalformedDictionary(t *testing.T) {
	for _, tc := range []struct {
		name string
		root *cell.Cell
	}{
		{name: "truncated label", root: cell.BeginCell().MustStoreUInt(0, 1).EndCell()},
		{name: "truncated fork", root: cell.BeginCell().MustStoreUInt(0, 2).MustStoreRef(cell.BeginCell().EndCell()).EndCell()},
	} {
		for _, historical := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/historical_%t", tc.name, historical), func(t *testing.T) {
				state := &tlb.StateInit{Code: cell.BeginCell().EndCell(), Lib: tc.root.AsDict(256)}
				addr := state.CalcAddress(-1)
				acc := &transactionRuntimeAccount{addr: addr, status: tlb.AccountStatusUninit}
				msg := &tlb.Message{MsgType: tlb.MsgTypeExternalIn, Msg: &tlb.ExternalMessage{DstAddr: addr, StateInit: state}}
				cfg := transactionTestConfigWithGlobalVersion(t, 2)

				_, used, _, err := transactionPrepareComputeAccount(acc, acc.status, false, msg, false, cfg, false, historical)
				if used || err == nil || !strings.Contains(err.Error(), "failed to validate message state libraries") {
					t.Fatalf("used=%t err=%v, want checked dictionary traversal error", used, err)
				}
			})
		}
	}
}
