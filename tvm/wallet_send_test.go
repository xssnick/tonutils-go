package tvm

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	walletpkg "github.com/xssnick/tonutils-go/ton/wallet"
	"github.com/xssnick/tonutils-go/tvm/cell"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

const (
	walletSendTestBalance     = uint64(10_000_000_000)
	walletSendTestGasMax      = int64(1_000_000)
	walletSendTestCredit      = int64(10_000)
	walletSendCrossVersion    = 13
	walletSendInitialSeqno    = uint32(0)
	walletSendSecondSeqno     = uint32(1)
	walletSendNetworkGlobalID = int32(-3)
)

var (
	walletSendTestSeed = bytes.Repeat([]byte{0x37}, 32)
	walletSendTestAddr = address.MustParseAddr("EQC9bWZd29foipyPOGWlVNVCQzpGAjvi1rGWF7EbNcSVClpA")
)

type walletSendFixture struct {
	address *address.Address
	code    *cell.Cell
	data    *cell.Cell
	body    *cell.Cell
	now     uint32
	message *walletpkg.Message
}

func makeWalletV5SendFixture(t *testing.T, seqno uint32) walletSendFixture {
	t.Helper()

	key := ed25519.NewKeyFromSeed(walletSendTestSeed)
	cfg := walletpkg.ConfigV5R1Final{
		NetworkGlobalID: walletSendNetworkGlobalID,
		Workchain:       0,
	}

	w, err := walletpkg.FromPrivateKeyWithOptions(key, cfg)
	if err != nil {
		t.Fatalf("failed to init v5 wallet fixture: %v", err)
	}

	spec, ok := w.GetSpec().(*walletpkg.SpecV5R1Final)
	if !ok {
		t.Fatalf("unexpected spec type: %T", w.GetSpec())
	}
	spec.SetMessagesTTL(3600)
	spec.SetSeqnoFetcher(func(ctx context.Context, subWallet uint32) (uint32, error) {
		return seqno, nil
	})

	msg := walletpkg.SimpleMessage(walletSendTestAddr, tlb.FromNanoTONU(1), cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell())
	ext, err := w.PrepareExternalMessageForMany(context.Background(), false, []*walletpkg.Message{msg})
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	state, err := walletpkg.GetStateInit(key.Public().(ed25519.PublicKey), cfg, 0)
	if err != nil {
		t.Fatalf("failed to build state init: %v", err)
	}

	return walletSendFixture{
		address: w.WalletAddress(),
		code:    state.Code,
		data:    state.Data,
		body:    ext.Body,
		now:     walletV5MessageValidUntil(t, ext.Body) - 1,
		message: msg,
	}
}

func walletV5MessageValidUntil(t *testing.T, body *cell.Cell) uint32 {
	t.Helper()

	s := body.BeginParse()
	if op, err := s.LoadUInt(32); err != nil {
		t.Fatalf("failed to load wallet v5 op: %v", err)
	} else if op != 0x7369676e {
		t.Fatalf("unexpected wallet v5 external op: %x", op)
	}
	if _, err := s.LoadUInt(32); err != nil {
		t.Fatalf("failed to load wallet id: %v", err)
	}
	validUntil, err := s.LoadUInt(32)
	if err != nil {
		t.Fatalf("failed to load valid_until: %v", err)
	}
	return uint32(validUntil)
}

func walletV5SeqnoFromData(t *testing.T, data *cell.Cell) uint32 {
	t.Helper()

	s := data.BeginParse()
	flag, err := s.LoadBoolBit()
	if err != nil {
		t.Fatalf("failed to load wallet v5 flag: %v", err)
	}
	if !flag {
		t.Fatal("unexpected wallet v5 flag value")
	}
	seqno, err := s.LoadUInt(32)
	if err != nil {
		t.Fatalf("failed to load wallet v5 seqno: %v", err)
	}
	return uint32(seqno)
}

func emulateWalletSendExternal(t *testing.T, code, data *cell.Cell, addr *address.Address, body *cell.Cell, now uint32, globalVersion int) (*ExternalMessageResult, error) {
	t.Helper()

	msg := &tlb.ExternalMessage{
		DstAddr: addr,
		Body:    body,
	}

	machine := NewTVM()
	machine.globalVersion = globalVersion

	if os.Getenv("TVM_TRACE_WALLET_SEND") != "" {
		prevTrace := vmcore.TraceHook
		vmcore.TraceHook = func(format string, args ...any) {
			t.Logf(format, args...)
		}
		defer func() {
			vmcore.TraceHook = prevTrace
		}()
	}

	return machine.EmulateExternalMessage(code, data, msg, EmulateExternalMessageConfig{
		Address:  addr,
		Now:      now,
		Balance:  new(big.Int).SetUint64(walletSendTestBalance),
		RandSeed: walletSendTestSeed,
		Gas: vmcore.NewGas(vmcore.GasConfig{
			Max:    walletSendTestGasMax,
			Limit:  0,
			Credit: walletSendTestCredit,
		}),
	})
}

func mustSingleActionCell(t *testing.T, msg *walletpkg.Message) *cell.Cell {
	t.Helper()

	outMsg, err := tlb.ToCell(msg.InternalMessage)
	if err != nil {
		t.Fatalf("failed to serialize internal message: %v", err)
	}

	action, err := tlb.ToCell(tlb.OutList{
		Prev: cell.BeginCell().EndCell(),
		Out: tlb.ActionSendMsg{
			Mode: msg.Mode,
			Msg:  outMsg,
		},
	})
	if err != nil {
		t.Fatalf("failed to serialize expected action list: %v", err)
	}
	return action
}

func TestWalletV5SendExternalGo(t *testing.T) {
	t.Run("AcceptsAndCommitsOutgoingMessage", func(t *testing.T) {
		fx := makeWalletV5SendFixture(t, walletSendInitialSeqno)

		res, err := emulateWalletSendExternal(t, fx.code, fx.data, fx.address, fx.body, fx.now, vmcore.DefaultGlobalVersion)
		if err != nil {
			t.Fatalf("wallet send failed: %v", err)
		}
		if !res.Accepted {
			t.Fatal("expected wallet to accept external message")
		}
		if res.ExitCode != 0 {
			t.Fatalf("unexpected exit code: %d", res.ExitCode)
		}
		if res.Actions == nil {
			t.Fatal("expected output actions on successful send")
		}
		if got := walletV5SeqnoFromData(t, res.Data); got != 1 {
			t.Fatalf("expected seqno to increment to 1, got %d", got)
		}

		wantAction := mustSingleActionCell(t, fx.message)
		if !bytes.Equal(res.Actions.Hash(), wantAction.Hash()) {
			t.Fatalf("unexpected actions:\nwant=%s\ngot=%s", wantAction.Dump(), res.Actions.Dump())
		}
	})

	t.Run("SecondSendUsesUpdatedData", func(t *testing.T) {
		fx1 := makeWalletV5SendFixture(t, walletSendInitialSeqno)
		first, err := emulateWalletSendExternal(t, fx1.code, fx1.data, fx1.address, fx1.body, fx1.now, vmcore.DefaultGlobalVersion)
		if err != nil {
			t.Fatalf("first wallet send failed: %v", err)
		}
		if !first.Accepted {
			t.Fatal("expected first wallet send to be accepted")
		}

		fx2 := makeWalletV5SendFixture(t, walletSendSecondSeqno)
		second, err := emulateWalletSendExternal(t, fx2.code, first.Data, fx2.address, fx2.body, fx2.now, vmcore.DefaultGlobalVersion)
		if err != nil {
			t.Fatalf("second wallet send failed: %v", err)
		}
		if !second.Accepted {
			t.Fatal("expected second wallet send to be accepted")
		}
		if got := walletV5SeqnoFromData(t, second.Data); got != 2 {
			t.Fatalf("expected seqno to increment to 2, got %d", got)
		}
	})

	t.Run("RejectsStaleSeqnoWithoutCommit", func(t *testing.T) {
		fx := makeWalletV5SendFixture(t, walletSendSecondSeqno)

		res, err := emulateWalletSendExternal(t, fx.code, fx.data, fx.address, fx.body, fx.now, vmcore.DefaultGlobalVersion)
		if err != nil {
			t.Fatalf("stale seqno emulation returned setup error: %v", err)
		}
		if res.Accepted {
			t.Fatal("stale seqno message should not be accepted")
		}
		if res.Actions != nil {
			t.Fatal("rejected message should not produce actions")
		}
		if !bytes.Equal(res.Data.Hash(), fx.data.Hash()) {
			t.Fatal("rejected message should keep original data")
		}
	})
}
