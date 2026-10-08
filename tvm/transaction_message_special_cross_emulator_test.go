//go:build cgo && tvm_cross_emulator

package tvm

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

// The reference transaction parser loads a referenced message body as an
// ordinary CellSlice before starting TVM. Special bodies therefore reject the
// whole transaction even when the contract never reads its body argument.
func TestTVMCrossEmulatorTransactionReferencedSpecialBody(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	data := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	code := makeTransactionInternalSuccessCode(t, data)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, data, walletSendTestBalance, now)

	body := transactionSpecialMessageBodies(t)["pruned"]
	msgCell := transactionMessageWithReferencedBody(t, body)
	if _, err := runReferenceOrdinaryTransaction(shard, msgCell, now, uint64(transactionTestLogicalTime), tonopsTestSeed); err == nil {
		t.Fatal("reference accepted a referenced pruned message body")
	} else if !strings.Contains(err.Error(), "bag of cells has a root with non-zero level") {
		t.Fatalf("reference error = %v, want non-zero-level root rejection", err)
	}
	if _, err := PrepareMessage(msgCell); err == nil {
		t.Fatal("Go accepted a referenced pruned message body")
	} else if !strings.Contains(err.Error(), "referenced message body is special") {
		t.Fatalf("Go error = %v, want referenced special-body rejection", err)
	}
}

// StateInitWithLibs validation walks HashmapE 256 SimpleLib as a strict plain
// dictionary. A fork payload accepted by the host's intentionally lenient
// HashmapAug-oriented LoadAll helper must still reject the inbound message.
func TestTVMCrossEmulatorTransactionMalformedStateInitLibraryFork(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	data := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	code := makeTransactionInternalSuccessCode(t, data)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, data, walletSendTestBalance, now)

	for _, malformed := range []struct {
		name     string
		extraBit bool
		extraRef bool
	}{
		{name: "fork-payload-bit", extraBit: true},
		{name: "third-fork-ref", extraRef: true},
	} {
		for _, stateInitInRef := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/state-init-ref-%t", malformed.name, stateInitInRef), func(t *testing.T) {
				msgCell := transactionMessageWithStateInitLibrariesFork(t, stateInitInRef, malformed.extraBit, malformed.extraRef)
				if _, err := runReferenceOrdinaryTransaction(shard, msgCell, now, uint64(transactionTestLogicalTime), tonopsTestSeed); err == nil {
					t.Fatal("reference accepted malformed StateInit library dictionary")
				}
				if _, err := PrepareMessage(msgCell); err == nil {
					t.Fatal("Go accepted malformed StateInit library dictionary")
				}
			})
		}
	}

	t.Run("present-empty-root", func(t *testing.T) {
		stateInit := cell.BeginCell().
			MustStoreUInt(0, 4).
			MustStoreBoolBit(true).
			MustStoreRef(cell.BeginCell().EndCell()).
			EndCell()
		msgCell := transactionTestMessageWithReferencedStateInit(stateInit, false)

		if _, err := runReferenceOrdinaryTransaction(shard, msgCell, now, uint64(transactionTestLogicalTime), tonopsTestSeed); err == nil {
			t.Fatal("reference accepted a present empty StateInit library root")
		}
		if _, err := PrepareMessage(msgCell); err == nil {
			t.Fatal("Go accepted a present empty StateInit library root")
		}
	})
}
