//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"os"
	"testing"
)

func TestTVMCrossEmulatorWalletV5SendExternal(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	t.Run("InitialSendMatchesReference", func(t *testing.T) {
		fx := makeWalletV5SendFixture(t, walletSendInitialSeqno)

		refRes, err := runReferenceSendExternal(fx.code, fx.data, fx.address, fx.body, fx.now)
		if err != nil {
			t.Fatalf("reference send_external failed: %v", err)
		}
		goRes, err := emulateWalletSendExternal(t, fx.code, fx.data, fx.address, fx.body, fx.now, walletSendCrossVersion)
		if err != nil {
			t.Fatalf("go send_external failed: %v", err)
		}

		assertMessageSendMatchesReference(t, goRes, refRes)
	})

	t.Run("SequentialSendMatchesReference", func(t *testing.T) {
		fx1 := makeWalletV5SendFixture(t, walletSendInitialSeqno)
		refFirst, err := runReferenceSendExternal(fx1.code, fx1.data, fx1.address, fx1.body, fx1.now)
		if err != nil {
			t.Fatalf("reference first send_external failed: %v", err)
		}
		goFirst, err := emulateWalletSendExternal(t, fx1.code, fx1.data, fx1.address, fx1.body, fx1.now, walletSendCrossVersion)
		if err != nil {
			t.Fatalf("go first send_external failed: %v", err)
		}
		assertMessageSendMatchesReference(t, goFirst, refFirst)

		fx2 := makeWalletV5SendFixture(t, walletSendSecondSeqno)
		refSecond, err := runReferenceSendExternal(fx2.code, refFirst.data, fx2.address, fx2.body, fx2.now)
		if err != nil {
			t.Fatalf("reference second send_external failed: %v", err)
		}
		goSecond, err := emulateWalletSendExternal(t, fx2.code, goFirst.Data, fx2.address, fx2.body, fx2.now, walletSendCrossVersion)
		if err != nil {
			t.Fatalf("go second send_external failed: %v", err)
		}
		assertMessageSendMatchesReference(t, goSecond, refSecond)
	})

	t.Run("StaleSeqnoRejectMatchesReference", func(t *testing.T) {
		fx := makeWalletV5SendFixture(t, walletSendSecondSeqno)

		goRes, err := emulateWalletSendExternal(t, fx.code, fx.data, fx.address, fx.body, fx.now, walletSendCrossVersion)
		if err != nil {
			t.Fatalf("go stale send_external failed: %v", err)
		}
		refRes, err := runReferenceSendExternal(fx.code, fx.data, fx.address, fx.body, fx.now)
		if err != nil {
			t.Fatalf("reference stale send_external failed: %v", err)
		}

		assertMessageSendMatchesReference(t, goRes, refRes)
	})
}

func assertMessageSendMatchesReference(t *testing.T, goRes *MessageExecutionResult, refRes *referenceSendMessageResult) {
	t.Helper()

	if os.Getenv("TVM_TRACE_WALLET_SEND_REF") != "" && refRes.vmLog != "" {
		t.Logf("reference vm log:\n%s", refRes.vmLog)
	}

	if goRes.ExitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.ExitCode, refRes.exitCode)
	}
	if goRes.Accepted != refRes.accepted {
		t.Fatalf("accepted mismatch: go=%t reference=%t", goRes.Accepted, refRes.accepted)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if goRes.Code == nil || refRes.code == nil || !bytes.Equal(goRes.Code.Hash(), refRes.code.Hash()) {
		t.Fatalf("code mismatch:\ngo=%v\nreference=%v", goRes.Code, refRes.code)
	}
	if !bytes.Equal(goRes.Data.Hash(), refRes.data.Hash()) {
		t.Fatalf("data mismatch:\ngo=%s\nreference=%s", goRes.Data.Dump(), refRes.data.Dump())
	}

	switch {
	case goRes.Actions == nil && refRes.actions == nil:
	case goRes.Actions == nil || refRes.actions == nil:
		t.Fatalf("actions presence mismatch: go=%v reference=%v", goRes.Actions != nil, refRes.actions != nil)
	default:
		if !bytes.Equal(goRes.Actions.Hash(), refRes.actions.Hash()) {
			t.Fatalf("actions mismatch:\ngo=%s\nreference=%s", goRes.Actions.Dump(), refRes.actions.Dump())
		}
	}
}
