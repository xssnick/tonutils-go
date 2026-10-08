package overlay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tl"
)

type fecWorkerConcurrentPeer struct {
	id           []byte
	send         func(context.Context, tl.Serializable) error
	events       chan tl.Serializable
	calls        atomic.Uint32
	active       atomic.Int32
	overlapped   atomic.Bool
	controlMx    sync.Mutex
	control      broadcastFECControlHandler
	unregistered chan struct{}
	unregister   sync.Once
}

func newFECWorkerConcurrentPeer(id byte) *fecWorkerConcurrentPeer {
	return &fecWorkerConcurrentPeer{
		id: bytes.Repeat([]byte{id}, 32), events: make(chan tl.Serializable, 1024),
		unregistered: make(chan struct{}),
	}
}

func (p *fecWorkerConcurrentPeer) ID() []byte {
	return p.id
}

func (p *fecWorkerConcurrentPeer) SendCustomMessage(ctx context.Context, message tl.Serializable) error {
	if p.active.Add(1) != 1 {
		p.overlapped.Store(true)
	}
	defer p.active.Add(-1)
	p.calls.Add(1)
	p.events <- message
	if p.send != nil {
		return p.send(ctx, message)
	}
	return nil
}

func (p *fecWorkerConcurrentPeer) registerBroadcastFECControl(_ []byte, handler broadcastFECControlHandler) func() {
	p.controlMx.Lock()
	p.control = handler
	p.controlMx.Unlock()
	return func() {
		p.controlMx.Lock()
		p.control = nil
		p.controlMx.Unlock()
		p.unregister.Do(func() { close(p.unregistered) })
	}
}

func (p *fecWorkerConcurrentPeer) deliverControl(control BroadcastFECControl) bool {
	p.controlMx.Lock()
	handler := p.control
	p.controlMx.Unlock()
	return handler != nil && handler(p.id, control)
}

type fecWorkerConcurrentPeerSet struct {
	mx    sync.Mutex
	peers []BroadcastPeer
}

func (s *fecWorkerConcurrentPeerSet) Peers() []BroadcastPeer {
	s.mx.Lock()
	defer s.mx.Unlock()
	return append([]BroadcastPeer(nil), s.peers...)
}

func (s *fecWorkerConcurrentPeerSet) set(peers ...BroadcastPeer) {
	s.mx.Lock()
	s.peers = peers
	s.mx.Unlock()
}

func newFECWorkerConcurrentSender(t *testing.T, opts ...BroadcastFECSenderOption) *BroadcastFECSender {
	t.Helper()
	_, key := keyPairFromSeed(93)
	opts = append([]BroadcastFECSenderOption{WithBroadcastFECSymbolSize(256)}, opts...)
	sender, err := NewBroadcastFECSender(key, CertificateEmpty{}, bytes.Repeat([]byte{0x71}, 8*256), BroadcastFlagAnySender, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return sender
}

func waitFECWorkerSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for " + description)
	}
}

func waitFECWorkerSeqno(t *testing.T, peer *fecWorkerConcurrentPeer, seqno uint32) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case message := <-peer.events:
			part, ok := message.(*BroadcastFEC)
			if !ok {
				t.Fatalf("expected full FEC part, got %T", message)
			}
			if part.Seqno >= seqno {
				return
			}
		case <-timer.C:
			t.Fatalf("peer %x did not reach seqno %d; calls=%d", peer.id, seqno, peer.calls.Load())
		}
	}
}

func runFECWorkerForTest(t *testing.T, broadcaster *BroadcastFECBroadcaster) (context.CancelFunc, <-chan struct{}, *error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var result error
	go func() {
		result = broadcaster.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		waitFECWorkerSignal(t, done, "broadcaster shutdown")
	})
	return cancel, done, &result
}

func TestBroadcastFECWorkerRunSlowPeerDoesNotBlockRecovery(t *testing.T) {
	sender := newFECWorkerConcurrentSender(t)
	slow, fast := newFECWorkerConcurrentPeer(1), newFECWorkerConcurrentPeer(2)
	release := make(chan struct{})
	slow.send = func(ctx context.Context, _ tl.Serializable) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	broadcaster, err := NewBroadcastFECBroadcaster(sender, StaticBroadcastPeerSet{slow, fast}, WithBroadcastFECWorkerMinRate(16<<20))
	if err != nil {
		t.Fatal(err)
	}
	_, done, result := runFECWorkerForTest(t, broadcaster)
	waitFECWorkerSeqno(t, slow, 0)
	fastLimit := sender.fec.SymbolsCount + sender.fec.SymbolsCount/33 + 1
	waitFECWorkerSeqno(t, fast, fastLimit+2)
	if slow.calls.Load() != 1 || slow.overlapped.Load() {
		t.Fatal("slow peer had more than one send in flight")
	}
	if !fast.deliverControl(BroadcastFECControl{Hash: sender.BroadcastHash(), Completed: true}) {
		t.Fatal("fast peer completion was not handled")
	}

	close(release)
	waitFECWorkerSeqno(t, slow, fastLimit+2)
	if !slow.deliverControl(BroadcastFECControl{Hash: sender.BroadcastHash(), Completed: true}) {
		t.Fatal("slow peer completion was not handled")
	}
	waitFECWorkerSignal(t, done, "completed broadcast")
	if *result != nil {
		t.Fatal(*result)
	}
	if slow.overlapped.Load() || fast.overlapped.Load() {
		t.Fatal("a peer received concurrent send calls")
	}
}

func TestBroadcastFECWorkerControlDuringSendStopsBatch(t *testing.T) {
	for _, completed := range []bool{false, true} {
		t.Run(fmt.Sprintf("completed=%t", completed), func(t *testing.T) {
			now := time.Unix(8000, 0)
			sender := newFECWorkerConcurrentSender(t, withBroadcastFECNow(func() time.Time { return now }))
			peer := newFECWorkerConcurrentPeer(3)
			peer.send = func(_ context.Context, message tl.Serializable) error {
				if _, full := message.(*BroadcastFEC); full && !peer.deliverControl(BroadcastFECControl{Hash: sender.BroadcastHash(), Completed: completed}) {
					return errors.New("control callback was not handled")
				}
				return nil
			}
			broadcaster, err := NewBroadcastFECBroadcaster(sender, StaticBroadcastPeerSet{peer}, WithBroadcastFECWorkerMinRate(16<<20), withBroadcastFECWorkerNow(func() time.Time { return now }))
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() {
				err = broadcaster.Tick(context.Background())
				close(done)
			}()
			waitFECWorkerSignal(t, done, "control callback and synchronous Tick")
			if err != nil {
				t.Fatal(err)
			}
			if peer.calls.Load() != 1 {
				t.Fatalf("full batch continued after control: %d sends", peer.calls.Load())
			}
			worker := broadcaster.workers[newBroadcastExternalPeerIDKey(peer.id)]
			if !worker.deliveredObserved || worker.sentFull != 1 {
				t.Fatal("control during send lost the delivery observation")
			}

			now = now.Add(30 * time.Millisecond)
			if err := broadcaster.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			if completed {
				if peer.calls.Load() != 1 || !broadcaster.Done() {
					t.Fatal("completed peer was sent another message")
				}
			} else {
				if peer.calls.Load() != 2 {
					t.Fatal("received peer was not sent a short recovery probe")
				}
				<-peer.events
				message := <-peer.events
				if _, ok := message.(*BroadcastFECShort); !ok {
					t.Fatalf("received peer got %T instead of a short probe", message)
				}
			}
		})
	}
}

func TestBroadcastFECWorkerRunCancellationWaitsForSend(t *testing.T) {
	sender := newFECWorkerConcurrentSender(t)
	peer := newFECWorkerConcurrentPeer(4)
	cancelSeen, release := make(chan struct{}), make(chan struct{})
	peer.send = func(ctx context.Context, _ tl.Serializable) error {
		<-ctx.Done()
		close(cancelSeen)
		<-release
		return ctx.Err()
	}
	broadcaster, err := NewBroadcastFECBroadcaster(sender, StaticBroadcastPeerSet{peer})
	if err != nil {
		t.Fatal(err)
	}
	cancel, done, result := runFECWorkerForTest(t, broadcaster)
	defer close(release)
	waitFECWorkerSeqno(t, peer, 0)
	cancel()
	waitFECWorkerSignal(t, cancelSeen, "send context cancellation")
	select {
	case <-done:
		t.Fatal("Run returned before its active send exited")
	default:
	}
	// The deferred release happens before the registered cleanup waits for Run.
	t.Cleanup(func() {
		waitFECWorkerSignal(t, done, "cancelled broadcast")
		if !errors.Is(*result, context.Canceled) {
			t.Fatalf("Run returned %v, want context.Canceled", *result)
		}
		if peer.active.Load() != 0 {
			t.Fatal("send was still active after Run returned")
		}
	})
}

func TestBroadcastFECWorkerRunRestartsAfterCancellation(t *testing.T) {
	sender := newFECWorkerConcurrentSender(t)
	peer := newFECWorkerConcurrentPeer(7)
	var resumed atomic.Bool
	peer.send = func(ctx context.Context, _ tl.Serializable) error {
		if resumed.Load() {
			if !peer.deliverControl(BroadcastFECControl{Hash: sender.BroadcastHash(), Completed: true}) {
				return errors.New("resumed worker did not register its control handler")
			}
			return nil
		}
		<-ctx.Done()
		return ctx.Err()
	}
	broadcaster, err := NewBroadcastFECBroadcaster(sender, StaticBroadcastPeerSet{peer})
	if err != nil {
		t.Fatal(err)
	}
	cancel, done, result := runFECWorkerForTest(t, broadcaster)
	waitFECWorkerSeqno(t, peer, 0)
	cancel()
	waitFECWorkerSignal(t, done, "first Run cancellation")
	if !errors.Is(*result, context.Canceled) {
		t.Fatalf("first Run returned %v, want context.Canceled", *result)
	}

	resumed.Store(true)
	_, done, result = runFECWorkerForTest(t, broadcaster)
	waitFECWorkerSignal(t, done, "resumed Run completion")
	if *result != nil {
		t.Fatal(*result)
	}
	if peer.calls.Load() != 2 {
		t.Fatalf("peer received %d calls, want cancelled send and resumed send", peer.calls.Load())
	}
}

func TestBroadcastFECWorkerRunRemovedPeerStopsAfterInflightSend(t *testing.T) {
	sender := newFECWorkerConcurrentSender(t)
	slow, fast := newFECWorkerConcurrentPeer(5), newFECWorkerConcurrentPeer(6)
	release, exited := make(chan struct{}), make(chan struct{})
	slow.send = func(ctx context.Context, _ tl.Serializable) error {
		defer close(exited)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	peers := &fecWorkerConcurrentPeerSet{peers: []BroadcastPeer{slow, fast}}
	broadcaster, err := NewBroadcastFECBroadcaster(sender, peers, WithBroadcastFECWorkerMinRate(16<<20))
	if err != nil {
		t.Fatal(err)
	}
	_, done, result := runFECWorkerForTest(t, broadcaster)
	waitFECWorkerSeqno(t, slow, 0)
	peers.set(fast)
	waitFECWorkerSignal(t, slow.unregistered, "removed peer control retirement")
	close(release)
	waitFECWorkerSignal(t, exited, "removed peer in-flight send")
	if !fast.deliverControl(BroadcastFECControl{Hash: sender.BroadcastHash(), Completed: true}) {
		t.Fatal("remaining peer completion was not handled")
	}
	waitFECWorkerSignal(t, done, "completed broadcast")
	if *result != nil {
		t.Fatal(*result)
	}
	if slow.calls.Load() != 1 {
		t.Fatalf("removed peer received %d sends, want only the in-flight send", slow.calls.Load())
	}
}
