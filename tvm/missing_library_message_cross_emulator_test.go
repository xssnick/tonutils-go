//go:build cgo && tvm_cross_emulator

package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
)

func TestTVMCrossEmulatorMessageMissingLibraryResultParity(t *testing.T) {
	libraryLoadLimitSkipIfReferenceUnavailable(t)

	missing := cell.BeginCell().MustStoreUInt(0xABCD_EF01, 32).EndCell()
	missingRef := mustCrossLibraryCellForHash(t, missing.Hash())
	code := codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHREF(missingRef).Serialize(),
		cellsliceop.XLOADQ().Serialize(),
	)
	data := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()

	goRes, err := emulateInternalForTest(t, code, data, body)
	if err != nil {
		t.Fatalf("run Go internal message: %v", err)
	}
	refRes, err := runReferenceSendInternal(code, data, tonopsTestAddr, body, internalMessageTestAmount, uint32(tonopsTestTime.Unix()))
	if err != nil {
		t.Fatalf("run reference internal message: %v", err)
	}
	assertMessageSendMatchesReference(t, goRes, refRes)

	want := missing.HashKey()
	if goRes.MissingLibrary == nil || *goRes.MissingLibrary != want {
		t.Fatalf("Go missing library = %v, want %x", goRes.MissingLibrary, want)
	}
	if refRes.missingLibrary == nil || *refRes.missingLibrary != want {
		t.Fatalf("reference missing library = %v, want %x", refRes.missingLibrary, want)
	}
}
