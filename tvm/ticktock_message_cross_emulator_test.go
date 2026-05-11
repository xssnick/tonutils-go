//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTVMCrossEmulatorTickTockTransaction(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	tickData := cell.BeginCell().MustStoreUInt(0x1111, 16).EndCell()
	tockData := cell.BeginCell().MustStoreUInt(0x2222, 16).EndCell()
	stateOnlyCode := makeTickTockStateOnlyCode(t, tickData, tockData)

	t.Run("TickSuccessMatchesReference", func(t *testing.T) {
		goRes, _, _, err := emulateTickTockForTest(t, stateOnlyCode, origData, false)
		if err != nil {
			t.Fatalf("go tick emulation failed: %v", err)
		}
		refRes, err := runReferenceTickTock(stateOnlyCode, origData, tickTockTestAddr, false, uint32(tonopsTestTime.Unix()), tickTockTestBalance, tonopsTestSeed)
		if err != nil {
			t.Fatalf("reference tick emulation failed: %v", err)
		}
		assertTickTockMatchesReference(t, goRes, refRes)
	})

	t.Run("TockSuccessMatchesReference", func(t *testing.T) {
		goRes, _, _, err := emulateTickTockForTest(t, stateOnlyCode, origData, true)
		if err != nil {
			t.Fatalf("go tock emulation failed: %v", err)
		}
		refRes, err := runReferenceTickTock(stateOnlyCode, origData, tickTockTestAddr, true, uint32(tonopsTestTime.Unix()), tickTockTestBalance, tonopsTestSeed)
		if err != nil {
			t.Fatalf("reference tock emulation failed: %v", err)
		}
		assertTickTockMatchesReference(t, goRes, refRes)
	})

	t.Run("FailureMatchesReference", func(t *testing.T) {
		failCode := makeTickTockFailureCode(t)
		goRes, _, _, err := emulateTickTockForTest(t, failCode, origData, false)
		if err != nil {
			t.Fatalf("go failing tick emulation failed: %v", err)
		}
		refRes, err := runReferenceTickTock(failCode, origData, tickTockTestAddr, false, uint32(tonopsTestTime.Unix()), tickTockTestBalance, tonopsTestSeed)
		if err != nil {
			t.Fatalf("reference failing tick emulation failed: %v", err)
		}
		assertTickTockMatchesReference(t, goRes, refRes)
	})
}

func assertTickTockMatchesReference(t *testing.T, goRes *MessageExecutionResult, refRes *referenceTickTockResult) {
	t.Helper()

	if goRes.ExitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.ExitCode, refRes.exitCode)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if goRes.Accepted != refRes.accepted {
		t.Fatalf("accepted mismatch: go=%t reference=%t", goRes.Accepted, refRes.accepted)
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
