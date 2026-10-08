package overlay

import (
	"bytes"
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tl"
)

func TestOrdinaryFECRelayDoesNotDelayDeliveryOrCompleted(t *testing.T) {
	transport := newMockADNL()
	receiver := newTestBroadcastReceiver(t, bytes.Repeat([]byte{0x37}, 32))
	receiver.trustUnauthorized = true
	receiver.ordinaryRelay.Store(newBroadcastRelayDispatcher(16, 2, 1<<20, time.Minute, true))
	wrapper, err := CreateExtendedADNL(transport).AttachOverlay(receiver)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	fastSent := make(chan struct{})
	stopped := make(chan struct{})
	slow := &twoStepRelayTestPeer{id: bytes.Repeat([]byte{0x38}, 32), send: func(ctx context.Context, _ []byte) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	}}
	fast := &twoStepRelayTestPeer{id: bytes.Repeat([]byte{0x39}, 32), send: func(context.Context, []byte) error {
		close(fastSent)
		return nil
	}}
	receiver.EnableBroadcastFECRelay(bytes.Repeat([]byte{0x40}, 32), StaticBroadcastPeerSet{slow, fast})
	delivered := make(chan struct{})
	receiver.SetBroadcastHandlerWithInfo(func(tl.Serializable, BroadcastInfo) BroadcastDisposition {
		close(delivered)
		return BroadcastDispositionAcceptAndRelay
	})
	_, key := keyPairFromSeed(37)
	sender, err := NewBroadcastFECSenderFromTL(key, CertificateEmpty{}, Message{Overlay: bytes.Repeat([]byte{0x41}, 32)}, 0,
		WithBroadcastFECSymbolSize(256))
	if err != nil {
		t.Fatal(err)
	}
	part, err := sender.Part(0)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- wrapper.processFECBroadcast(part.Full) }()
	for name, signal := range map[string]<-chan struct{}{"slow start": started, "fast send": fastSent, "local delivery": delivered} {
		select {
		case <-signal:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %s while slow relay was blocked", name)
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("FEC receive waited for a slow relay")
	}
	if len(transport.sendCustomCalls) != 1 {
		t.Fatalf("completed control calls = %d, want 1", len(transport.sendCustomCalls))
	}
	if _, ok := transport.sendCustomCalls[0].(FECCompleted); !ok {
		t.Fatalf("control = %#v, want FECCompleted", transport.sendCustomCalls[0])
	}
	receiver.Close()
	select {
	case <-stopped:
	default:
		t.Fatal("receiver Close did not wait for relay cancellation")
	}
	stats := receiver.FECBroadcastStats()
	if stats.FECRelaySentTotal != 1 || stats.FECRelayFailedTotal != 1 {
		t.Fatalf("unexpected async relay results: %+v", stats)
	}
}

func TestOrdinaryRelayOwnsPreparedAndLegacyData(t *testing.T) {
	receiver := newTestBroadcastReceiver(t, bytes.Repeat([]byte{0x51}, 32))
	_, key := keyPairFromSeed(51)
	message := signedBroadcast(t, key, []byte("retained payload"), 0)
	want, err := tl.Serialize(message, true)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	results := make(chan []byte, 2)
	prepared := &twoStepRelayTestPeer{id: bytes.Repeat([]byte{0x52}, 32), send: func(ctx context.Context, body []byte) error {
		select {
		case <-release:
			results <- bytes.Clone(body)
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	legacy := &mockBroadcastPeer{id: bytes.Repeat([]byte{0x53}, 32), sendFunc: func(ctx context.Context, msg tl.Serializable) error {
		select {
		case <-release:
			body, err := tl.Serialize(msg, true)
			if err == nil {
				results <- body
			}
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	receiver.EnableBroadcastSimpleRelay(bytes.Repeat([]byte{0x54}, 32), StaticBroadcastPeerSet{prepared, legacy})
	wrapper := &ADNLOverlayWrapper{BroadcastReceiver: receiver}
	if err = wrapper.relaySimpleBroadcast(nil, message); err != nil {
		t.Fatal(err)
	}
	clear(message.Data)
	clear(message.Signature)
	close(release)
	for range 2 {
		select {
		case body := <-results:
			if !bytes.Equal(body, want) {
				t.Fatalf("relay retained receive memory: got %x want %x", body, want)
			}
		case <-time.After(time.Second):
			t.Fatal("relay did not finish")
		}
	}
	waitOrdinaryBroadcastRelay(t, receiver)
}

func TestOrdinaryRelayBoundsQueueBytesAndShutdown(t *testing.T) {
	receiver := newTestBroadcastReceiver(t, bytes.Repeat([]byte{0x61}, 32))
	_, key := keyPairFromSeed(61)
	message := signedBroadcast(t, key, []byte("payload"), 0)
	body, err := PrepareBroadcastMessage(message)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := newBroadcastRelayDispatcher(2, 2, int64(cap(body.Body()))*2, time.Minute, true)
	receiver.ordinaryRelay.Store(dispatcher)
	started := make(chan struct{})
	peer := &twoStepRelayTestPeer{id: bytes.Repeat([]byte{0x62}, 32), send: func(ctx context.Context, _ []byte) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	wrapper := &ADNLOverlayWrapper{BroadcastReceiver: receiver}
	op := broadcastFECRelayOp{peer: peer, msg: message, wire: body}
	if err = wrapper.enqueueBroadcastRelayOps(false, []broadcastFECRelayOp{op}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first relay did not start")
	}
	second := NewPreparedBroadcastMessage(make([]byte, len(body.Body()), cap(body.Body())))
	copy(second.body, body.body)
	op.wire = second
	if err = wrapper.enqueueBroadcastRelayOps(false, []broadcastFECRelayOp{op, op}); err != nil {
		t.Fatal(err)
	}
	third := NewPreparedBroadcastMessage(make([]byte, len(body.Body()), cap(body.Body())))
	copy(third.body, body.body)
	op.wire = third
	if err = wrapper.enqueueBroadcastRelayOps(false, []broadcastFECRelayOp{op}); err != nil {
		t.Fatal(err)
	}
	stats := dispatcher.Stats()
	if stats.QueueDepth != 1 || stats.QueueFullTotal != 1 || stats.ByteFullTotal != 1 {
		t.Fatalf("queue/byte bound failed: %+v", stats)
	}
	receiver.Close()
	if dispatcher.activeBytes.Load() != 0 {
		t.Fatalf("Close retained %d bytes", dispatcher.activeBytes.Load())
	}
	if stats := receiver.FECBroadcastStats(); stats.SimpleRelayFailedTotal != 4 || stats.SimpleRelaySentTotal != 0 {
		t.Fatalf("dropped/canceled relay results = %+v", stats)
	}
}

func TestOrdinaryRelayBacklogDoesNotOccupyOtherPeerWorkers(t *testing.T) {
	for _, queueSize := range []int{1, 16} {
		t.Run(fmt.Sprintf("queue_%d", queueSize), func(t *testing.T) {
			testOrdinaryRelayBacklog(t, queueSize)
		})
	}
}

func testOrdinaryRelayBacklog(t *testing.T, queueSize int) {
	receiver := newTestBroadcastReceiver(t, bytes.Repeat([]byte{0x81}, 32))
	dispatcher := newBroadcastRelayDispatcher(queueSize, 4, 1<<20, time.Minute, true)
	receiver.ordinaryRelay.Store(dispatcher)
	_, key := keyPairFromSeed(81)
	message := signedBroadcast(t, key, []byte("queued FEC traffic"), 0)
	body, err := PrepareBroadcastMessage(message)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	fastSent := make(chan struct{})
	var slowCalls atomic.Int32
	slow := &twoStepRelayTestPeer{id: bytes.Repeat([]byte{0x82}, 32), send: func(ctx context.Context, _ []byte) error {
		if slowCalls.Add(1) == 1 {
			close(started)
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	fast := &twoStepRelayTestPeer{id: bytes.Repeat([]byte{0x83}, 32), send: func(context.Context, []byte) error {
		close(fastSent)
		return nil
	}}
	wrapper := &ADNLOverlayWrapper{BroadcastReceiver: receiver}
	slowOp := broadcastFECRelayOp{peer: slow, msg: message, wire: body}
	if err = wrapper.enqueueBroadcastRelayOps(true, []broadcastFECRelayOp{slowOp}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("slow peer did not start")
	}
	const pending = 20 // More than both the global queue and worker count.
	ops := make([]broadcastFECRelayOp, 0, pending+1)
	for range pending {
		ops = append(ops, slowOp)
	}
	ops = append(ops, broadcastFECRelayOp{peer: fast, msg: message, wire: body})
	if err = wrapper.enqueueBroadcastRelayOps(true, ops); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fastSent:
	case <-time.After(time.Second):
		t.Fatal("one peer's backlog occupied the workers needed by another peer")
	}
	if calls := slowCalls.Load(); calls != 1 {
		t.Fatalf("blocked peer occupied %d workers, want 1", calls)
	}
	if stats := dispatcher.Stats(); stats.QueueFullTotal != uint64(pending-queueSize/4) {
		t.Fatalf("slow-peer pending limit did not reject its excess backlog: %+v", stats)
	}
	receiver.Close()
	if dispatcher.activeBytes.Load() != 0 {
		t.Fatal("shutdown retained queued payloads")
	}
	if stats := receiver.FECBroadcastStats(); stats.FECRelaySentTotal != 1 || stats.FECRelayFailedTotal != pending+1 {
		t.Fatalf("queued tasks were not released exactly once: %+v", stats)
	}
}

func TestOrdinaryRelayRotatesReadyPeersAndReusesSlots(t *testing.T) {
	receiver := newTestBroadcastReceiver(t, bytes.Repeat([]byte{0x91}, 32))
	dispatcher := newBroadcastRelayDispatcher(4, 1, 1024, time.Minute, true)
	receiver.ordinaryRelay.Store(dispatcher)
	started := make(chan struct{})
	release := make(chan struct{})
	order := make(chan byte, 5)
	peerA := &twoStepRelayTestPeer{id: bytes.Repeat([]byte{0x92}, 32), send: func(ctx context.Context, body []byte) error {
		order <- body[0]
		if body[0] == '0' {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}}
	peerB := &twoStepRelayTestPeer{id: bytes.Repeat([]byte{0x93}, 32), send: func(_ context.Context, body []byte) error {
		order <- body[0]
		return nil
	}}
	submit := func(peer BroadcastPeer, value byte) {
		t.Helper()
		payload, ok := dispatcher.reservePayload(nil, NewPreparedBroadcastMessage([]byte{value}), 1)
		if !ok {
			t.Fatal("reserve failed")
		}
		payload.state = receiver.fecState
		if dispatcher.Submit(broadcastTwoStepRelayTask{peer: peer, payload: payload}) != broadcastTwoStepRelayQueued {
			t.Fatal("queue rejected available slot")
		}
	}
	submit(peerA, '0')
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first peer did not start")
	}
	submit(peerA, 'a')
	submit(peerA, 'c')
	submit(peerB, 'b')
	submit(peerB, 'd')
	if stats := dispatcher.Stats(); stats.QueueDepth != 4 {
		t.Fatalf("pending queue depth = %d, want 4", stats.QueueDepth)
	}
	close(release)
	for _, want := range []byte("0badc") {
		select {
		case got := <-order:
			if got != want {
				t.Fatalf("round-robin order got %q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("ready peer work was lost")
		}
	}
	waitOrdinaryBroadcastRelay(t, receiver)
	if len(dispatcher.ordinary.slots) != 4 {
		t.Fatal("per-peer bursts grew the global task storage")
	}
	// Every queued slot must be available again, including the slot originally
	// popped by the first send while the two peer queues were being populated.
	dispatcher.ordinary.mx.Lock()
	free := 0
	for index := dispatcher.ordinary.free; index >= 0; index = dispatcher.ordinary.slots[index].next {
		free++
		if free > 4 {
			break
		}
	}
	dispatcher.ordinary.mx.Unlock()
	if free != 4 {
		t.Fatalf("free slots = %d, want 4", free)
	}
}
