package overlay

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
)

const DefaultBroadcastFECWorkerTick = time.Millisecond
const DefaultBroadcastFECWorkerMinRate int64 = 1 << 20 // 1 MiB/s

type BroadcastFECWorkerOption func(cfg *broadcastFECWorkerConfig)

type broadcastFECWorkerConfig struct {
	tick    time.Duration
	now     func() time.Time
	minRate int64
}

type BroadcastFECBroadcaster struct {
	sender  *BroadcastFECSender
	peerSet BroadcastPeerSet

	tick time.Duration
	now  func() time.Time

	cfg broadcastFECWorkerConfig

	mx      sync.Mutex
	workers map[string]*broadcastFECPeerWorker
	rrHead  uint32
}

type broadcastFECPeerWorker struct {
	peer BroadcastPeer

	received  bool
	completed bool

	nextSeqno        uint32
	fastSeqnoTill    uint32
	nextRecoverDelay int64
	lastRecoverAt    int64
	startedAt        time.Time
	recoveryReady    bool

	rateLimit *rldp.TokenBucket
	rateCtrl  *rldp.BBRv2Controller

	mx sync.Mutex
}

func WithBroadcastFECWorkerTick(tick time.Duration) BroadcastFECWorkerOption {
	return func(cfg *broadcastFECWorkerConfig) {
		cfg.tick = tick
	}
}

func WithBroadcastFECWorkerMinRate(rate int64) BroadcastFECWorkerOption {
	return func(cfg *broadcastFECWorkerConfig) {
		cfg.minRate = rate
	}
}

func withBroadcastFECWorkerNow(now func() time.Time) BroadcastFECWorkerOption {
	return func(cfg *broadcastFECWorkerConfig) {
		cfg.now = now
	}
}

func NewBroadcastFECBroadcaster(sender *BroadcastFECSender, peerSet BroadcastPeerSet, opts ...BroadcastFECWorkerOption) (*BroadcastFECBroadcaster, error) {
	if sender == nil {
		return nil, fmt.Errorf("sender is nil")
	}
	if peerSet == nil {
		return nil, fmt.Errorf("peer set is nil")
	}

	cfg := broadcastFECWorkerConfig{
		tick:    DefaultBroadcastFECWorkerTick,
		now:     time.Now,
		minRate: DefaultBroadcastFECWorkerMinRate,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.tick <= 0 {
		return nil, fmt.Errorf("tick should be positive")
	}
	if cfg.minRate <= 0 {
		return nil, fmt.Errorf("min rate should be positive")
	}

	return &BroadcastFECBroadcaster{
		sender:  sender,
		peerSet: peerSet,
		tick:    cfg.tick,
		now:     cfg.now,
		cfg:     cfg,
		workers: map[string]*broadcastFECPeerWorker{},
	}, nil
}

func (b *BroadcastFECBroadcaster) Run(ctx context.Context) error {
	ticker := time.NewTicker(b.tick)
	defer ticker.Stop()

	for {
		if err := b.Tick(ctx); err != nil {
			return err
		}

		if b.Done() {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (b *BroadcastFECBroadcaster) Done() bool {
	if b.sender.Expired() {
		return true
	}

	b.mx.Lock()
	defer b.mx.Unlock()

	if len(b.workers) == 0 {
		return b.sender.Done()
	}

	for _, worker := range b.workers {
		worker.mx.Lock()
		done := worker.completed || worker.nextSeqno >= b.sender.totalPartsLimit()
		worker.mx.Unlock()
		if !done {
			return false
		}
	}
	return true
}

func (b *BroadcastFECBroadcaster) Tick(ctx context.Context) error {
	if b.sender.Expired() {
		return nil
	}

	peers := b.peerSet.Peers()
	if len(peers) == 0 {
		return nil
	}

	workers := b.ensureWorkers(peers)
	if len(workers) == 0 {
		return nil
	}

	start := int(0)
	b.mx.Lock()
	if len(workers) > 0 {
		start = int(b.rrHead % uint32(len(workers)))
		b.rrHead++
	}
	b.mx.Unlock()

	for i := range workers {
		worker := workers[(start+i)%len(workers)]
		if err := worker.step(ctx, b.sender, b.now()); err != nil {
			return err
		}
	}
	return nil
}

func (b *BroadcastFECBroadcaster) TrackControlMessage(peerID []byte, msg tl.Serializable) bool {
	if !b.sender.TrackControlMessage(peerID, msg) {
		return false
	}

	b.mx.Lock()
	worker := b.workers[string(peerID)]
	b.mx.Unlock()
	if worker == nil {
		return true
	}

	worker.observeControl(msg)
	return true
}

func (b *BroadcastFECBroadcaster) ensureWorkers(peers []BroadcastPeer) []*broadcastFECPeerWorker {
	b.mx.Lock()
	defer b.mx.Unlock()

	workers := make([]*broadcastFECPeerWorker, 0, len(peers))
	for _, peer := range peers {
		id := string(peer.ID())
		worker := b.workers[id]
		if worker == nil {
			worker = newBroadcastFECPeerWorker(peer, b.sender, b.cfg)
			b.workers[id] = worker
		} else {
			worker.peer = peer
		}
		worker.syncState(b.sender.peerState(peer.ID()))
		workers = append(workers, worker)
	}
	return workers
}

func newBroadcastFECPeerWorker(peer BroadcastPeer, sender *BroadcastFECSender, cfg broadcastFECWorkerConfig) *broadcastFECPeerWorker {
	totalParts := sender.totalPartsLimit()
	fastSeqnoTill := sender.fec.SymbolsCount + sender.fec.SymbolsCount/33 + 1
	if fastSeqnoTill > totalParts {
		fastSeqnoTill = totalParts
	}

	state := sender.peerState(peer.ID())
	limiter := rldp.NewTokenBucket(cfg.minRate, fmt.Sprintf("overlay-%x", peer.ID()))
	ctrl := rldp.NewBBRv2Controller(limiter, rldp.BBRv2Options{
		Name:    fmt.Sprintf("overlay-%x", peer.ID()),
		MinRate: cfg.minRate,
	})

	return &broadcastFECPeerWorker{
		peer:             peer,
		received:         state.received,
		completed:        state.completed,
		fastSeqnoTill:    fastSeqnoTill,
		nextRecoverDelay: 4,
		rateLimit:        limiter,
		rateCtrl:         ctrl,
	}
}

func (w *broadcastFECPeerWorker) syncState(state broadcastFECSendPeerState) {
	w.mx.Lock()
	defer w.mx.Unlock()

	if state.received {
		w.received = true
	}
	if state.completed {
		w.received = true
		w.completed = true
	}
}

func (w *broadcastFECPeerWorker) observeControl(msg tl.Serializable) {
	w.mx.Lock()
	defer w.mx.Unlock()

	switch msg.(type) {
	case FECReceived:
		w.received = true
	case FECCompleted:
		w.received = true
		w.completed = true
	default:
	}
}

func (w *broadcastFECPeerWorker) step(ctx context.Context, sender *BroadcastFECSender, now time.Time) error {
	w.mx.Lock()
	defer w.mx.Unlock()

	if w.completed || sender.Expired() {
		return nil
	}

	totalParts := sender.totalPartsLimit()
	if w.nextSeqno >= totalParts {
		return nil
	}

	ms := now.UnixMilli()
	if !w.recoveryReady {
		w.rateCtrl.OnNewSendBurst()

		maxFast := int(w.fastSeqnoTill - w.nextSeqno)
		batch := w.rateLimit.ConsumePackets(maxFast, int(sender.fec.SymbolSize))
		if batch == 0 {
			return nil
		}

		if err := w.sendBatchLocked(ctx, sender, batch); err != nil {
			return err
		}

		if w.nextSeqno >= w.fastSeqnoTill {
			w.recoveryReady = true
			w.startedAt = now
		}
		return nil
	}

	if ms-w.lastRecoverAt <= w.nextRecoverDelay {
		return nil
	}

	rtt := w.rateCtrl.CurrentMinRTT()
	if rtt >= 0 && rtt < 8 {
		rtt = 8
	}

	delay := int64(5)
	if rtt > 0 {
		delay = rtt / 4
	}

	quantum := int64(1)
	if w.nextSeqno < w.fastSeqnoTill {
		fastDiff := int64(w.fastSeqnoTill - w.nextSeqno)
		if fastDiff > quantum {
			quantum = fastDiff
		}
	} else if rtt > 0 {
		perFrame := int64(sender.fec.SymbolsCount / 4)
		if perFrame < 2 {
			perFrame = 2
		}

		prevTm := int64(0)
		if w.lastRecoverAt > 0 {
			prevTm = w.lastRecoverAt - w.startedAt.UnixMilli()
		}

		tmOfFrame := ms - w.startedAt.UnixMilli()
		amt := float64(perFrame) * (float64(tmOfFrame-prevTm) / float64(rtt))
		quantum = int64(amt)
		if quantum > perFrame/2 {
			quantum = perFrame / 2
		}
		if quantum < 1 {
			quantum = 1
		}
		delay = 0
	} else if sc := sender.fec.SymbolsCount / 100; sc > 1 {
		quantum = int64(sc)
	}

	requested := int(quantum)
	remaining := int(totalParts - w.nextSeqno)
	if requested > remaining {
		requested = remaining
	}
	consumed := w.rateLimit.ConsumePackets(requested, int(sender.fec.SymbolSize))
	if consumed == 0 {
		return nil
	}

	if err := w.sendBatchLocked(ctx, sender, consumed); err != nil {
		return err
	}
	w.nextRecoverDelay = delay
	w.lastRecoverAt = ms
	return nil
}

func (w *broadcastFECPeerWorker) sendBatchLocked(ctx context.Context, sender *BroadcastFECSender, batch int) error {
	for i := 0; i < batch; i++ {
		part, err := sender.part(w.nextSeqno)
		if err != nil {
			return fmt.Errorf("failed to build part %d: %w", w.nextSeqno, err)
		}

		msg := tl.Serializable(part.full)
		if w.received {
			msg = part.short
		}

		if err = w.peer.SendCustomMessage(ctx, msg); err != nil {
			return fmt.Errorf("failed to send part %d to peer %x: %w", w.nextSeqno, w.peer.ID(), err)
		}

		w.nextSeqno++
	}
	return nil
}
