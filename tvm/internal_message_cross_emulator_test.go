//go:build cgo && tvm_cross_emulator

package tvm

import (
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTVMCrossEmulatorInternalMessage(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	t.Run("SuccessMatchesReference", func(t *testing.T) {
		origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
		newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
		body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
		wantMsg, err := buildInternalMessageForEmulation(tonopsTestAddr, body, internalMessageTestAmount)
		if err != nil {
			t.Fatalf("failed to build expected internal message: %v", err)
		}
		code := makeInternalMessageSuccessCode(t, newData, wantMsg)

		goRes, err := emulateInternalForTest(t, code, origData, body)
		if err != nil {
			t.Fatalf("go send_internal failed: %v", err)
		}
		refRes, err := runReferenceSendInternal(code, origData, tonopsTestAddr, body, internalMessageTestAmount, uint32(tonopsTestTime.Unix()))
		if err != nil {
			t.Fatalf("reference send_internal failed: %v", err)
		}

		assertMessageSendMatchesReference(t, goRes, refRes)
	})

	t.Run("FailureMatchesReference", func(t *testing.T) {
		origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
		newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
		body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
		code := makeInternalMessageFailureCode(t, newData)

		goRes, err := emulateInternalForTest(t, code, origData, body)
		if err != nil {
			t.Fatalf("go failing send_internal failed: %v", err)
		}
		refRes, err := runReferenceSendInternal(code, origData, tonopsTestAddr, body, internalMessageTestAmount, uint32(tonopsTestTime.Unix()))
		if err != nil {
			t.Fatalf("reference failing send_internal failed: %v", err)
		}

		assertMessageSendMatchesReference(t, goRes, refRes)
	})
}
