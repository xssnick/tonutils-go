package rldp

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tl"
)

func newCancelledTransferTestClient(t *testing.T) (*RLDP, *[]tl.Serializable, *sync.Mutex) {
	t.Helper()

	var mx sync.Mutex
	var sent []tl.Serializable
	client := NewClient(MockADNL{
		sendCustomMessage: func(_ context.Context, req tl.Serializable) error {
			mx.Lock()
			sent = append(sent, req)
			mx.Unlock()
			return nil
		},
	})
	t.Cleanup(client.Close)
	return client, &sent, &mx
}

func registerCancelledTestRequest(client *RLDP, deadline time.Time) *activeRequest {
	request := &activeRequest{
		id:                 "cancelled-transfer-test",
		transferID:         bytes.Repeat([]byte{0x11}, 32),
		expectedTransferID: [32]byte(bytes.Repeat([]byte{0x22}, 32)),
		deadline:           deadline.UnixMilli(),
		maxAnswerSize:      10 << 20,
	}
	client.mx.Lock()
	client.activeRequests[request.id] = request
	client.expectedTransfers[request.expectedTransferID] = request
	client.mx.Unlock()
	return request
}

func oversizedTestMessagePart(transferID [32]byte, seqno uint32) *MessagePart {
	return &MessagePart{
		TransferID: append([]byte(nil), transferID[:]...),
		FecType: FECRaptorQ{
			DataSize:     100 << 10,
			SymbolSize:   768,
			SymbolsCount: 140,
		},
		Part:      0,
		TotalSize: 100 << 10,
		Seqno:     seqno,
		Data:      make([]byte, 768),
	}
}

func TestCancelledRequestLateAnswerPartsSilencedAndCompleted(t *testing.T) {
	client, sent, mx := newCancelledTransferTestClient(t)
	request := registerCancelledTestRequest(client, time.Now().Add(2*time.Second))

	if answered := client.cancelActiveRequest(request); answered {
		t.Fatalf("request unexpectedly reported answered")
	}

	part := oversizedTestMessagePart(request.expectedTransferID, 0)
	if part.TotalSize <= client.unexpectedTransferSizeLimit() {
		t.Fatalf("test part must exceed the unexpected transfer limit to be meaningful")
	}

	if err := client.handleMessagePart(part, true); err != nil {
		t.Fatalf("late part of cancelled request must be dropped silently, got: %v", err)
	}

	mx.Lock()
	completes := len(*sent)
	mx.Unlock()
	if completes != 1 {
		t.Fatalf("expected one complete reply for the first late part, got %d", completes)
	}
	mx.Lock()
	first := (*sent)[0]
	mx.Unlock()
	complete, ok := first.(CompleteV2)
	if !ok {
		t.Fatalf("expected CompleteV2 reply, got %T", first)
	}
	if !bytes.Equal(complete.TransferID, request.expectedTransferID[:]) || complete.Part != 0 {
		t.Fatalf("complete reply targets wrong transfer/part: %x part %d", complete.TransferID, complete.Part)
	}

	// The throttle keeps the reply rate bounded while symbols keep arriving.
	if err := client.handleMessagePart(oversizedTestMessagePart(request.expectedTransferID, 1), true); err != nil {
		t.Fatalf("second late part must be dropped silently, got: %v", err)
	}
	mx.Lock()
	completes = len(*sent)
	mx.Unlock()
	if completes != 1 {
		t.Fatalf("throttle must swallow immediate repeat completes, got %d replies", completes)
	}

	time.Sleep(cancelledTransferCompleteThrottle + 5*time.Millisecond)
	if err := client.handleMessagePart(oversizedTestMessagePart(request.expectedTransferID, 2), true); err != nil {
		t.Fatalf("third late part must be dropped silently, got: %v", err)
	}
	mx.Lock()
	completes = len(*sent)
	mx.Unlock()
	if completes != 2 {
		t.Fatalf("expected a second complete after the throttle window, got %d", completes)
	}
}

func TestCancelledRequestV1PartGetsV1Complete(t *testing.T) {
	client, sent, mx := newCancelledTransferTestClient(t)
	request := registerCancelledTestRequest(client, time.Now().Add(2*time.Second))
	if answered := client.cancelActiveRequest(request); answered {
		t.Fatalf("request unexpectedly reported answered")
	}

	if err := client.handleMessagePart(oversizedTestMessagePart(request.expectedTransferID, 0), false); err != nil {
		t.Fatalf("late v1 part must be dropped silently, got: %v", err)
	}
	mx.Lock()
	defer mx.Unlock()
	if len(*sent) != 1 {
		t.Fatalf("expected one complete reply, got %d", len(*sent))
	}
	if _, ok := (*sent)[0].(Complete); !ok {
		t.Fatalf("expected v1 Complete reply, got %T", (*sent)[0])
	}
}

func TestExpiredCancelledTransferFallsBackToUnexpectedHandling(t *testing.T) {
	client, sent, mx := newCancelledTransferTestClient(t)

	id := [32]byte(bytes.Repeat([]byte{0x33}, 32))
	client.mx.Lock()
	client.cancelledTransfers[id] = &cancelledTransfer{expireAtMS: time.Now().Add(-time.Second).UnixMilli()}
	client.mx.Unlock()

	err := client.handleMessagePart(oversizedTestMessagePart(id, 0), true)
	if err == nil || !strings.Contains(err.Error(), "too big transfer size") {
		t.Fatalf("expired record must restore the unexpected-transfer size check, got: %v", err)
	}
	mx.Lock()
	defer mx.Unlock()
	if len(*sent) != 0 {
		t.Fatalf("no completes expected for an expired record, got %d", len(*sent))
	}
}

func TestCancelledTransferSweepBoundsMap(t *testing.T) {
	client, _, _ := newCancelledTransferTestClient(t)

	client.mx.Lock()
	for i := 0; i < cancelledTransferSweepMin; i++ {
		var id [32]byte
		id[0], id[1] = byte(i), byte(i>>8)
		client.cancelledTransfers[id] = &cancelledTransfer{expireAtMS: time.Now().Add(-time.Minute).UnixMilli()}
	}
	var fresh [32]byte
	fresh[31] = 0xFF
	client.rememberCancelledTransferLocked(fresh, time.Now().Add(time.Second).UnixMilli())
	size := len(client.cancelledTransfers)
	record := client.cancelledTransfers[fresh]
	client.mx.Unlock()

	if size != 1 {
		t.Fatalf("sweep must drop expired records, map size %d", size)
	}
	if record == nil {
		t.Fatalf("fresh record must survive the sweep")
	}
	if record.expireAtMS <= time.Now().UnixMilli() {
		t.Fatalf("fresh record expiry must be in the future")
	}
}
