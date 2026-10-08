package rldp

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tl"
)

// The inbound-stream cleanup ticker is armed only while streams exist, so a
// client that is merely pooled costs no timer. The hazard of any such scheme is
// a lost wakeup: a stream created around the moment the ticker disarms would
// never be cleaned, leaking the stream and its buffers for the life of the
// client. These pin the arming contract.

func newLazyCleanerClient(t *testing.T) *RLDP {
	t.Helper()

	client := NewClient(MockADNL{
		sendCustomMessage: func(context.Context, tl.Serializable) error { return nil },
	})
	t.Cleanup(client.Close)
	return client
}

// newInertClient builds a client with no maintenance goroutines, so the
// activation signal can be observed instead of being consumed by the very
// loop it is meant to wake.
func newInertClient() *RLDP {
	return &RLDP{
		adnl: MockADNL{
			sendCustomMessage: func(context.Context, tl.Serializable) error { return nil },
		},
		activeRequests:         map[string]*activeRequest{},
		activeTransfers:        map[string]*activeTransfer{},
		recvStreams:            map[[32]byte]*decoderStream{},
		expectedTransfers:      map[[32]byte]*activeRequest{},
		cancelledTransfers:     map[[32]byte]*cancelledTransfer{},
		activateRecoverySender: make(chan bool, 1),
		activateRequestCleanup: make(chan struct{}, 1),
		activateStreamCleanup:  make(chan struct{}, 1),
		stats:                  &clientStats{},
	}
}

func TestStreamCleanupIsArmedByStreamCreation(t *testing.T) {
	client := newInertClient()

	part := &MessagePart{
		TransferID: bytes.Repeat([]byte{0x5C}, 32),
		FecType:    FECRaptorQ{DataSize: 2048, SymbolSize: 768, SymbolsCount: 3},
		Part:       0,
		TotalSize:  2048,
		Seqno:      0,
		Data:       make([]byte, 768),
	}
	if err := client.handleMessagePart(part, true); err != nil {
		t.Fatalf("handle first part: %v", err)
	}

	select {
	case <-client.activateStreamCleanup:
	default:
		t.Fatal("creating an inbound stream must arm the cleanup ticker")
	}
}

// A second part of the SAME stream must not need to re-arm anything: the
// activation is per stream, not per packet, which is what keeps the hot path
// free of extra work.
func TestStreamCleanupArmsOncePerStream(t *testing.T) {
	client := newInertClient()

	id := bytes.Repeat([]byte{0x6D}, 32)
	newPart := func(seqno uint32) *MessagePart {
		return &MessagePart{
			TransferID: append([]byte(nil), id...),
			FecType:    FECRaptorQ{DataSize: 4096, SymbolSize: 768, SymbolsCount: 6},
			Part:       0,
			TotalSize:  4096,
			Seqno:      seqno,
			Data:       make([]byte, 768),
		}
	}
	if err := client.handleMessagePart(newPart(0), true); err != nil {
		t.Fatalf("handle first part: %v", err)
	}
	select {
	case <-client.activateStreamCleanup:
	default:
		t.Fatal("the first part must arm the ticker")
	}

	if err := client.handleMessagePart(newPart(1), true); err != nil {
		t.Fatalf("handle second part: %v", err)
	}
	select {
	case <-client.activateStreamCleanup:
		t.Fatal("a further part of an existing stream must not re-arm the ticker")
	default:
	}
}

// End to end: a stream that goes quiet must still be reclaimed. This is the
// test that fails if the ticker is never armed, or is disarmed while a stream
// is still registered.
func TestIdleStreamIsReclaimedWithLazyTicker(t *testing.T) {
	client := newLazyCleanerClient(t)

	part := &MessagePart{
		TransferID: bytes.Repeat([]byte{0x7E}, 32),
		FecType:    FECRaptorQ{DataSize: 8192, SymbolSize: 768, SymbolsCount: 11},
		Part:       0,
		TotalSize:  8192,
		Seqno:      0,
		Data:       make([]byte, 768),
	}
	if err := client.handleMessagePart(part, true); err != nil {
		t.Fatalf("handle part: %v", err)
	}

	client.mx.RLock()
	registered := len(client.recvStreams)
	client.mx.RUnlock()
	if registered != 1 {
		t.Fatalf("expected the stream to be registered, got %d", registered)
	}

	// Age it past the idle timeout and let the armed ticker do its pass.
	client.mx.Lock()
	for _, stream := range client.recvStreams {
		stream.mx.Lock()
		stream.lastMessageAt = time.Now().Add(-_recvStreamIdleTimeout - time.Minute)
		stream.mx.Unlock()
	}
	client.mx.Unlock()

	deadline := time.Now().Add(5 * time.Second)
	for {
		client.mx.RLock()
		remaining := len(client.recvStreams)
		client.mx.RUnlock()
		if remaining == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("an idle stream was never reclaimed: the lazy ticker lost its wakeup")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
