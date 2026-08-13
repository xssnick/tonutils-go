package tvm

import (
	"fmt"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestPrepareMessageRejectsReferencedSpecialBody(t *testing.T) {
	for name, body := range transactionSpecialMessageBodies(t) {
		t.Run(name, func(t *testing.T) {
			msgCell := transactionMessageWithReferencedBody(t, body)
			if _, err := PrepareMessage(msgCell); err == nil || !strings.Contains(err.Error(), "referenced message body is special") {
				t.Fatalf("PrepareMessage error = %v, want referenced special-body rejection", err)
			}
		})
	}
}

func TestPrepareMessageAcceptsReferencedOrdinaryBody(t *testing.T) {
	body := cell.BeginCell().MustStoreUInt(0x1234, 16).EndCell()
	eager := transactionMessageWithReferencedBody(t, body)
	lazy, err := cell.FromBOCWithOptions(eager.ToBOC(), cell.BOCParseOptions{Lazy: true})
	if err != nil {
		t.Fatalf("failed to parse lazy message: %v", err)
	}

	loader, err := lazy.BeginParse()
	if err != nil {
		t.Fatalf("failed to parse lazy message root: %v", err)
	}
	var lazyMessage tlb.Message
	if err = tlb.LoadFromCell(&lazyMessage, loader); err != nil {
		t.Fatalf("failed to decode lazy message: %v", err)
	}
	if !lazyMessage.Msg.Payload().IsLazy() {
		t.Fatal("referenced body was materialized before PrepareMessage")
	}

	for name, msgCell := range map[string]*cell.Cell{
		"eager": eager,
		"lazy":  lazy,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := PrepareMessage(msgCell); err != nil {
				t.Fatalf("PrepareMessage rejected ordinary referenced body: %v", err)
			}
		})
	}
}

func TestPrepareMessageTracesReferencedBodyValidation(t *testing.T) {
	body := cell.BeginCell().MustStoreUInt(0x1234, 16).EndCell()
	message := transactionMessageWithReferencedBody(t, body)
	read := cell.NewReadSet(message)
	traced := read.Root()

	loader, err := traced.BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	var parsed tlb.Message
	if err = tlb.LoadFromCell(&parsed, loader); err != nil {
		t.Fatal(err)
	}
	bodyHash := parsed.Msg.Payload().HashKey()
	// Decoding the message reached the body only as a reference of a cell it did
	// read, which is exactly the state the node lookup used to report: known to
	// the recorder, but not yet read itself.
	if _, known := read.Prunable(bodyHash); !known {
		t.Fatal("referenced body was not reached through the recorded message")
	}
	if _, loaded := read.Contains(bodyHash); loaded {
		t.Fatal("referenced body was loaded before validation")
	}

	if _, err = PrepareMessage(traced); err != nil {
		t.Fatal(err)
	}
	if _, loaded := read.Contains(bodyHash); !loaded {
		t.Fatal("referenced body validation was not recorded")
	}
}

func TestPrepareMessageKeepsInlineSpecialPayloadOpaque(t *testing.T) {
	body := transactionSpecialMessageBodies(t)["library"]
	msgCell := transactionMessageWithBodyLayout(t, body, false)

	prepared, err := PrepareMessage(msgCell)
	if err != nil {
		t.Fatalf("PrepareMessage rejected inline special payload: %v", err)
	}
	if prepared.msg.Msg.Payload().IsSpecial() {
		t.Fatal("inline payload unexpectedly retained the special-cell descriptor")
	}
}

func TestPrepareMessageRejectsMalformedStateInitLibraryFork(t *testing.T) {
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
				if _, err := PrepareMessage(msgCell); err == nil {
					t.Fatal("PrepareMessage accepted malformed StateInit library dictionary")
				}
			})
		}
	}
}

func TestPrepareMessageRejectsPresentEmptyStateInitLibraryRoot(t *testing.T) {
	stateInit := cell.BeginCell().
		MustStoreUInt(0, 4).
		MustStoreBoolBit(true).
		MustStoreRef(cell.BeginCell().EndCell()).
		EndCell()
	msgCell := transactionTestMessageWithReferencedStateInit(stateInit, false)
	if _, err := PrepareMessage(msgCell); err == nil {
		t.Fatal("PrepareMessage accepted a present empty StateInit library root")
	}
}

func TestPrepareMessageAcceptsCanonicalStateInitLibrariesFromLazyBOC(t *testing.T) {
	for _, stateInitInRef := range []bool{false, true} {
		eager := transactionMessageWithStateInitLibrariesFork(t, stateInitInRef, false, false)
		lazy, err := cell.FromBOCWithOptions(eager.ToBOC(), cell.BOCParseOptions{Lazy: true})
		if err != nil {
			t.Fatalf("parse lazy message BOC: %v", err)
		}
		if _, err = PrepareMessage(lazy); err != nil {
			t.Fatalf("PrepareMessage rejected canonical lazy StateInit libraries (ref=%t): %v", stateInitInRef, err)
		}
	}
}

func transactionSpecialMessageBodies(t *testing.T) map[string]*cell.Cell {
	t.Helper()

	left := cell.BeginCell().MustStoreUInt(0x11, 8).MustStoreRef(cell.BeginCell().EndCell()).EndCell()
	right := cell.BeginCell().MustStoreUInt(0x22, 8).EndCell()

	proof, err := cell.CreateMerkleProof(left)
	if err != nil {
		t.Fatalf("failed to create Merkle proof body: %v", err)
	}
	update, err := cell.CreateMerkleUpdate(left, right)
	if err != nil {
		t.Fatalf("failed to create Merkle update body: %v", err)
	}
	pruned, err := cell.CreatePrunedBranch(left, 1, 3)
	if err != nil {
		t.Fatalf("failed to create pruned body: %v", err)
	}
	library, err := cell.BeginCell().
		MustStoreUInt(uint64(cell.LibraryCellType), 8).
		MustStoreSlice(make([]byte, 32), 256).
		EndCellSpecial(true)
	if err != nil {
		t.Fatalf("failed to create library body: %v", err)
	}

	return map[string]*cell.Cell{
		"pruned":        pruned,
		"library":       library,
		"merkle_proof":  proof,
		"merkle_update": update,
	}
}

func transactionMessageWithReferencedBody(t *testing.T, body *cell.Cell) *cell.Cell {
	t.Helper()

	return transactionMessageWithBodyLayout(t, body, true)
}

func transactionMessageWithBodyLayout(t *testing.T, body *cell.Cell, inRef bool) *cell.Cell {
	t.Helper()

	msg := &tlb.Message{
		MsgType: tlb.MsgTypeInternal,
		Msg: &tlb.InternalMessage{
			IHRDisabled: true,
			SrcAddr:     address.NewAddress(0, 0, make([]byte, 32)),
			DstAddr:     tonopsTestAddr,
			Amount:      tlb.FromNanoTONU(1_000_000_000),
			Body:        body,
		},
	}
	builder := cell.BeginCell()
	if err := tlb.StoreMessageWithLayout(builder, msg, tlb.MessageLayout{BodyInRef: inRef}); err != nil {
		t.Fatalf("failed to build message with body layout ref=%t: %v", inRef, err)
	}
	return builder.EndCell()
}

func transactionMessageWithStateInitLibrariesFork(t *testing.T, stateInitInRef, extraBit, extraRef bool) *cell.Cell {
	t.Helper()

	libraries := cell.NewDict(256)
	for i, key := range [][]byte{make([]byte, 32), append([]byte{0x80}, make([]byte, 31)...)} {
		value := cell.BeginCell().MustStoreBoolBit(i == 0).MustStoreRef(cell.BeginCell().MustStoreUInt(uint64(i), 1).EndCell())
		if err := libraries.SetBuilder(cell.BeginCell().MustStoreSlice(key, 256).EndCell(), value); err != nil {
			t.Fatalf("store StateInit library %d: %v", i, err)
		}
	}

	root := libraries.AsCell()
	rootSlice := root.MustBeginParse()
	malformedRoot := cell.BeginCell().MustStoreSlice(rootSlice.MustLoadSlice(root.BitsSize()), root.BitsSize())
	if extraBit {
		malformedRoot.MustStoreBoolBit(true)
	}
	for rootSlice.RefsNum() > 0 {
		ref, err := rootSlice.LoadRefCell()
		if err != nil {
			t.Fatalf("load StateInit library fork ref: %v", err)
		}
		malformedRoot.MustStoreRef(ref)
	}
	if extraRef {
		malformedRoot.MustStoreRef(cell.BeginCell().EndCell())
	}

	msg := &tlb.Message{
		MsgType: tlb.MsgTypeInternal,
		Msg: &tlb.InternalMessage{
			IHRDisabled: true,
			SrcAddr:     address.NewAddress(0, 0, make([]byte, 32)),
			DstAddr:     tonopsTestAddr,
			Amount:      tlb.FromNanoTONU(1_000_000_000),
			StateInit:   &tlb.StateInit{Lib: malformedRoot.EndCell().AsDict(256)},
			Body:        cell.BeginCell().EndCell(),
		},
	}
	builder := cell.BeginCell()
	if err := tlb.StoreMessageWithLayout(builder, msg, tlb.MessageLayout{StateInitInRef: stateInitInRef}); err != nil {
		t.Fatalf("build message with malformed StateInit libraries: %v", err)
	}
	return builder.EndCell()
}
