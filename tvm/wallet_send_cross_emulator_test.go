//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"os"
	"strconv"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestTVMCrossEmulatorWalletV5SendExternal(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	preparedCfg := mustPrepareLenientTestConfig(mustReferenceTransactionConfigRoot(t))

	t.Run("InitialSendMatchesReference", func(t *testing.T) {
		fx := makeWalletV5SendFixture(t, walletSendInitialSeqno)

		refRes, err := runReferenceSendExternal(fx.code, fx.data, fx.address, fx.body, fx.now)
		if err != nil {
			t.Fatalf("reference send_external failed: %v", err)
		}
		goRes, err := emulateWalletSendExternal(t, fx.code, fx.data, fx.address, fx.body, fx.now, preparedCfg)
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
		goFirst, err := emulateWalletSendExternal(t, fx1.code, fx1.data, fx1.address, fx1.body, fx1.now, preparedCfg)
		if err != nil {
			t.Fatalf("go first send_external failed: %v", err)
		}
		assertMessageSendMatchesReference(t, goFirst, refFirst)

		fx2 := makeWalletV5SendFixture(t, walletSendSecondSeqno)
		refSecond, err := runReferenceSendExternal(fx2.code, refFirst.data, fx2.address, fx2.body, fx2.now)
		if err != nil {
			t.Fatalf("reference second send_external failed: %v", err)
		}
		goSecond, err := emulateWalletSendExternal(t, fx2.code, goFirst.Data, fx2.address, fx2.body, fx2.now, preparedCfg)
		if err != nil {
			t.Fatalf("go second send_external failed: %v", err)
		}
		assertMessageSendMatchesReference(t, goSecond, refSecond)
	})

	t.Run("StaleSeqnoRejectMatchesReference", func(t *testing.T) {
		fx := makeWalletV5SendFixture(t, walletSendSecondSeqno)

		goRes, err := emulateWalletSendExternal(t, fx.code, fx.data, fx.address, fx.body, fx.now, preparedCfg)
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

func TestTVMCrossEmulatorWalletV5SendExternalAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range crossEmulatorVersionAuditVersions(t, "TVM_WALLET_SEND_VERSION_AUDIT") {
		t.Run("initial_global_v"+strconv.Itoa(version), func(t *testing.T) {
			assertWalletV5SendExternalVersionParity(t, version, false)
		})

		t.Run("stale_global_v"+strconv.Itoa(version), func(t *testing.T) {
			assertWalletV5SendExternalVersionParity(t, version, true)
		})

		t.Run("sequential_global_v"+strconv.Itoa(version), func(t *testing.T) {
			assertWalletV5SendExternalSequentialVersionParity(t, version)
		})
	}
}

func FuzzTVMCrossEmulatorWalletV5SendExternalGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), false)
		f.Add(uint8(version), true)
	}
	f.Add(uint8(255), false)
	f.Add(uint8(255), true)

	f.Fuzz(func(t *testing.T, rawVersion uint8, stale bool) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertWalletV5SendExternalVersionParity(t, version, stale)
	})
}

func assertWalletV5SendExternalVersionParity(t *testing.T, version int, stale bool) {
	t.Helper()

	seqno := walletSendInitialSeqno
	if stale {
		seqno = walletSendSecondSeqno
	}
	fx := makeWalletV5SendFixture(t, seqno)
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))

	refRes, err := runReferenceSendMessageWithConfig(fx.code, fx.data, fx.body, 0, false, referenceSendMessageConfig{
		address:    fx.address,
		now:        fx.now,
		balance:    referenceDefaultWalletSendBalance,
		randSeed:   referenceDefaultWalletSendSeed,
		configRoot: configRoot,
	})
	if err != nil {
		t.Fatalf("reference send_external v%d stale=%t failed: %v", version, stale, err)
	}
	goRes, err := emulateWalletSendExternal(t, fx.code, fx.data, fx.address, fx.body, fx.now, mustPrepareLenientTestConfig(configRoot))
	if err != nil {
		t.Fatalf("go send_external v%d stale=%t failed: %v", version, stale, err)
	}

	assertMessageSendMatchesReference(t, goRes, refRes)
}

func FuzzTVMCrossEmulatorWalletV5SendExternalSequentialGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version))
	}
	f.Add(uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertWalletV5SendExternalSequentialVersionParity(t, version)
	})
}

func assertWalletV5SendExternalSequentialVersionParity(t *testing.T, version int) {
	t.Helper()

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))
	preparedCfg := mustPrepareLenientTestConfig(configRoot)
	fx1 := makeWalletV5SendFixture(t, walletSendInitialSeqno)
	refFirst, err := runReferenceSendMessageWithConfig(fx1.code, fx1.data, fx1.body, 0, false, referenceSendMessageConfig{
		address:    fx1.address,
		now:        fx1.now,
		balance:    referenceDefaultWalletSendBalance,
		randSeed:   referenceDefaultWalletSendSeed,
		configRoot: configRoot,
	})
	if err != nil {
		t.Fatalf("reference first send_external v%d failed: %v", version, err)
	}
	goFirst, err := emulateWalletSendExternal(t, fx1.code, fx1.data, fx1.address, fx1.body, fx1.now, preparedCfg)
	if err != nil {
		t.Fatalf("go first send_external v%d failed: %v", version, err)
	}
	assertMessageSendMatchesReference(t, goFirst, refFirst)

	fx2 := makeWalletV5SendFixture(t, walletSendSecondSeqno)
	refSecond, err := runReferenceSendMessageWithConfig(fx2.code, refFirst.data, fx2.body, 0, false, referenceSendMessageConfig{
		address:    fx2.address,
		now:        fx2.now,
		balance:    referenceDefaultWalletSendBalance,
		randSeed:   referenceDefaultWalletSendSeed,
		configRoot: configRoot,
	})
	if err != nil {
		t.Fatalf("reference second send_external v%d failed: %v", version, err)
	}
	goSecond, err := emulateWalletSendExternal(t, fx2.code, goFirst.Data, fx2.address, fx2.body, fx2.now, preparedCfg)
	if err != nil {
		t.Fatalf("go second send_external v%d failed: %v", version, err)
	}
	assertMessageSendMatchesReference(t, goSecond, refSecond)
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
