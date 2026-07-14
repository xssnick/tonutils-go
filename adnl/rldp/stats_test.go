package rldp

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
)

func TestStatsTracksVersionFeedbackAndClose(t *testing.T) {
	closerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := NewClientV2(MockADNL{
		closerCtx: closerCtx,
		sendCustomMessage: func(context.Context, tl.Serializable) error {
			return nil
		},
	})

	transferID := make([]byte, 32)
	transferID[0] = 1
	part := &activeTransferPart{
		index:         0,
		fecSymbolSize: DefaultSymbolSize,
		sendClock:     NewSendClock(64),
	}
	part.sendClock.OnSend(4, time.Now().UnixMilli())
	time.Sleep(time.Millisecond)
	transfer := &activeTransfer{id: transferID}
	transfer.currentPart.Store(part)

	client.mx.Lock()
	client.activeTransfers[string(transferID)] = transfer
	client.mx.Unlock()

	err := client.handleMessage(&adnl.MessageCustom{Data: ConfirmV2{
		TransferID:    transferID,
		Part:          0,
		MaxSeqno:      5,
		ReceivedCount: 4,
	}})
	if err != nil {
		t.Fatal(err)
	}

	stats := client.Stats()
	if stats.CreatedAt.IsZero() || stats.Version != Version2 || stats.Closed {
		t.Fatalf("unexpected client state: %+v", stats)
	}
	if stats.Active.OutboundTransfers != 1 {
		t.Fatalf("active outbound transfers=%d want=1", stats.Active.OutboundTransfers)
	}
	if stats.Outbound.ConfirmationsReceived != 1 || stats.Outbound.ConfirmedSymbols != 4 ||
		stats.Outbound.LastConfirmationAt.IsZero() {
		t.Fatalf("unexpected outbound feedback: %+v", stats.Outbound)
	}
	if !stats.Congestion.RTTObserved || stats.Congestion.LatestRTT <= 0 || stats.Congestion.LastFeedbackAt.IsZero() {
		t.Fatalf("unexpected congestion feedback: %+v", stats.Congestion)
	}

	client.Close()
	if stats = client.Stats(); !stats.Closed {
		t.Fatal("closed RLDP client is reported as open")
	}
}

func TestStatsConcurrentConfirmV2DeltasAreMonotonic(t *testing.T) {
	closerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := NewClientV2(MockADNL{
		closerCtx: closerCtx,
		sendCustomMessage: func(context.Context, tl.Serializable) error {
			return nil
		},
	})

	const confirmations = 128
	transferID := make([]byte, 32)
	transferID[0] = 2
	part := &activeTransferPart{
		index:         0,
		fecSymbolSize: DefaultSymbolSize,
		sendClock:     NewSendClock(256),
	}
	transfer := &activeTransfer{id: transferID}
	transfer.currentPart.Store(part)

	client.mx.Lock()
	client.activeTransfers[string(transferID)] = transfer
	client.mx.Unlock()

	start := make(chan struct{})
	errs := make(chan error, confirmations)
	var wg sync.WaitGroup
	for i := 1; i <= confirmations; i++ {
		received := uint32(i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- client.handleMessage(&adnl.MessageCustom{Data: ConfirmV2{
				TransferID:    transferID,
				Part:          0,
				MaxSeqno:      confirmations,
				ReceivedCount: received,
			}})
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadUint32(&part.lastConfirmRecvProcessed); got != confirmations {
		t.Fatalf("last processed receive count=%d want=%d", got, confirmations)
	}
	stats := client.Stats()
	if stats.Outbound.ConfirmationsReceived != confirmations || stats.Outbound.ConfirmedSymbols != confirmations {
		t.Fatalf("unexpected concurrent confirmation stats: %+v", stats.Outbound)
	}
}

func TestStatsCloseFinalizesActiveTransfersOnce(t *testing.T) {
	closerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := NewClient(MockADNL{
		closerCtx: closerCtx,
		sendCustomMessage: func(context.Context, tl.Serializable) error {
			return nil
		},
	})

	transfer := &activeTransfer{id: []byte("outbound")}
	stream := &decoderStream{
		lastMessageAt: time.Now(),
		activeParts: map[uint32]*decoderStreamPart{
			0: {
				receivedNum:       3,
				receivedRepairNum: 1,
				receivedBytes:     42,
			},
		},
	}
	client.mx.Lock()
	client.activeTransfers["outbound"] = transfer
	client.recvStreams[testTransferID("inbound")] = stream
	client.stats.outboundTransfersStarted.Add(1)
	client.stats.inboundTransfersStarted.Add(1)
	client.mx.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client.closeState()
		}()
	}
	wg.Wait()
	if err := client.handleMessage(&adnl.MessageCustom{Data: MessagePart{
		TransferID: make([]byte, 32),
		FecType: FECRaptorQ{
			DataSize:     1,
			SymbolSize:   1,
			SymbolsCount: 1,
		},
		TotalSize: 1,
	}}); err != nil {
		t.Fatal(err)
	}

	stats := client.Stats()
	if !stats.Closed || stats.Active != (ActiveStats{}) {
		t.Fatalf("unexpected closed client state: %+v", stats)
	}
	if stats.Outbound.TransfersStarted != 1 || stats.Outbound.TransfersCanceled != 1 ||
		stats.Outbound.TransfersCompleted != 0 || stats.Outbound.TransfersTimedOut != 0 || stats.Outbound.TransfersFailed != 0 {
		t.Fatalf("unexpected outbound terminal stats: %+v", stats.Outbound)
	}
	if stats.Inbound.TransfersStarted != 1 || stats.Inbound.TransfersCanceled != 1 ||
		stats.Inbound.TransfersCompleted != 0 || stats.Inbound.TransfersExpired != 0 ||
		stats.Inbound.SymbolsReceived != 3 || stats.Inbound.SymbolBytesReceived != 42 || stats.Inbound.RepairSymbolsReceived != 1 {
		t.Fatalf("unexpected inbound terminal stats: %+v", stats.Inbound)
	}
}

func TestStatsCloseDoesNotHoldStateLockWhileStreamIsBusy(t *testing.T) {
	closerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := NewClient(MockADNL{
		closerCtx: closerCtx,
		sendCustomMessage: func(context.Context, tl.Serializable) error {
			return nil
		},
	})
	stream := &decoderStream{
		lastMessageAt: time.Now(),
		activeParts:   map[uint32]*decoderStreamPart{},
	}
	client.mx.Lock()
	client.recvStreams[testTransferID("busy")] = stream
	client.stats.inboundTransfersStarted.Add(1)
	client.mx.Unlock()

	stream.mx.Lock()
	closed := make(chan struct{})
	go func() {
		client.closeState()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		stream.mx.Unlock()
		t.Fatal("close blocked on a busy receive stream")
	}

	stateLockAvailable := make(chan struct{})
	go func() {
		client.mx.Lock()
		client.mx.Unlock()
		close(stateLockAvailable)
	}()
	select {
	case <-stateLockAvailable:
	case <-time.After(time.Second):
		stream.mx.Unlock()
		t.Fatal("close retained RLDP state lock while waiting for a stream")
	}
	stream.mx.Unlock()

	deadline := time.Now().Add(time.Second)
	for client.Stats().Inbound.TransfersCanceled != 1 {
		if time.Now().After(deadline) {
			t.Fatal("busy stream was not finalized after it became available")
		}
		runtime.Gosched()
	}
}

func TestStatsRequestCleanupDistinguishesTimeoutFromCancellation(t *testing.T) {
	tests := []struct {
		name             string
		deadline         time.Time
		wantTimedOut     uint64
		wantCanceled     uint64
		wantExpired      uint64
		wantRecvCanceled uint64
	}{
		{
			name:         "deadline",
			deadline:     time.Now().Add(-time.Second),
			wantTimedOut: 1,
			wantExpired:  1,
		},
		{
			name:             "caller cancellation",
			deadline:         time.Now().Add(time.Second),
			wantCanceled:     1,
			wantRecvCanceled: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			closerCtx, cancel := context.WithCancel(context.Background())
			defer cancel()

			client := NewClient(MockADNL{
				closerCtx: closerCtx,
				sendCustomMessage: func(context.Context, tl.Serializable) error {
					return nil
				},
			})

			request := &activeRequest{
				id:                 "request",
				transferID:         []byte("outbound"),
				expectedTransferID: testTransferID("inbound"),
				deadline:           test.deadline.UnixMilli(),
			}
			transfer := &activeTransfer{id: request.transferID}
			stream := &decoderStream{
				lastMessageAt: time.Now(),
				activeParts:   map[uint32]*decoderStreamPart{},
			}
			client.mx.Lock()
			client.activeRequests[request.id] = request
			client.activeTransfers[string(request.transferID)] = transfer
			client.expectedTransfers[request.expectedTransferID] = request
			client.recvStreams[request.expectedTransferID] = stream
			client.stats.outboundTransfersStarted.Add(1)
			client.stats.inboundTransfersStarted.Add(1)
			client.mx.Unlock()

			if answered := client.cancelActiveRequest(request); answered {
				t.Fatal("request unexpectedly reported as answered")
			}

			stats := client.Stats()
			if stats.Outbound.TransfersTimedOut != test.wantTimedOut || stats.Outbound.TransfersCanceled != test.wantCanceled {
				t.Fatalf("unexpected outbound cleanup stats: %+v", stats.Outbound)
			}
			if stats.Inbound.TransfersExpired != test.wantExpired || stats.Inbound.TransfersCanceled != test.wantRecvCanceled {
				t.Fatalf("unexpected inbound cleanup stats: %+v", stats.Inbound)
			}
		})
	}
}

func TestStatsOutboundTerminalOutcomeIsRecordedOnce(t *testing.T) {
	closerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := NewClient(MockADNL{
		closerCtx: closerCtx,
		sendCustomMessage: func(context.Context, tl.Serializable) error {
			return nil
		},
	})
	transfer := &activeTransfer{}
	outcomes := []outboundTransferOutcome{
		outboundTransferCompleted,
		outboundTransferTimedOut,
		outboundTransferFailed,
		outboundTransferCanceled,
	}

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		outcome := outcomes[i%len(outcomes)]
		wg.Add(1)
		go func() {
			defer wg.Done()
			client.finishOutboundTransfer(transfer, outcome)
		}()
	}
	wg.Wait()

	stats := client.Stats().Outbound
	terminals := stats.TransfersCompleted + stats.TransfersTimedOut + stats.TransfersFailed + stats.TransfersCanceled
	if terminals != 1 {
		t.Fatalf("terminal outcomes=%d want=1: %+v", terminals, stats)
	}
}

func BenchmarkStats(b *testing.B) {
	closerCtx, cancel := context.WithCancel(context.Background())
	b.Cleanup(cancel)

	client := NewClient(MockADNL{
		closerCtx: closerCtx,
		sendCustomMessage: func(context.Context, tl.Serializable) error {
			return nil
		},
	})

	b.ReportAllocs()
	for b.Loop() {
		_ = client.Stats()
	}
}
