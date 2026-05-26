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
const broadcastFECShortProbeInitialDelay = 25 * time.Millisecond
const broadcastFECShortProbeMaxDelay = time.Second
const broadcastFECSendErrorInitialDelay = 50 * time.Millisecond
const broadcastFECSendErrorMaxDelay = 2 * time.Second

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
	firstSentAt      time.Time
	recoveryReady    bool

	rateLimit *rldp.TokenBucket
	rateCtrl  *rldp.BBRv2Controller
	sendClock *rldp.SendClock

	sentFull          uint32
	deliveredObserved bool
	nextShortProbeAt  int64
	shortProbeDelay   int64
	failedUntil       int64
	sendErrorDelay    int64

	unregisterControl func()

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
	defer b.closeControls()

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
		b.closeControls()
		return nil
	}

	peers := b.peerSet.Peers()
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

func (b *BroadcastFECBroadcaster) TrackControlMessage(peerID []byte, control BroadcastFECControl) bool {
	b.mx.Lock()
	worker := b.workers[string(peerID)]
	b.mx.Unlock()

	if !b.sender.TrackControlMessage(peerID, control) {
		return false
	}

	if worker == nil {
		return true
	}

	worker.observeControl(control, b.sender, b.now())
	return true
}

func (b *BroadcastFECBroadcaster) ensureWorkers(peers []BroadcastPeer) []*broadcastFECPeerWorker {
	hash := b.sender.BroadcastHash()
	var stale []*broadcastFECPeerWorker
	workerPeers := make([]broadcastFECWorkerPeer, 0, len(peers))

	b.mx.Lock()

	workers := make([]*broadcastFECPeerWorker, 0, len(peers))
	seen := make(map[string]struct{}, len(peers))
	for _, peer := range peers {
		id := string(peer.ID())
		seen[id] = struct{}{}

		worker := b.workers[id]
		if worker == nil {
			worker = newBroadcastFECPeerWorker(peer, b.sender, b.cfg)
			b.workers[id] = worker
		} else {
			worker.setPeer(peer)
		}
		worker.syncState(b.sender.peerState(peer.ID()))
		workers = append(workers, worker)
		workerPeers = append(workerPeers, broadcastFECWorkerPeer{
			worker: worker,
			peer:   peer,
		})
	}

	for id, worker := range b.workers {
		if _, ok := seen[id]; ok {
			continue
		}
		delete(b.workers, id)
		stale = append(stale, worker)
	}
	b.mx.Unlock()

	for _, worker := range stale {
		worker.closeControl()
	}
	for _, item := range workerPeers {
		item.worker.ensureControl(item.peer, hash, b.TrackControlMessage)
	}

	return workers
}

type broadcastFECWorkerPeer struct {
	worker *broadcastFECPeerWorker
	peer   BroadcastPeer
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
		sendClock:        rldp.NewSendClock(broadcastFECSendClockSize(totalParts)),
	}
}

func broadcastFECSendClockSize(totalParts uint32) int {
	size := 64
	for size < int(totalParts) && size < 4096 {
		size <<= 1
	}
	return size
}

func (b *BroadcastFECBroadcaster) closeControls() {
	b.mx.Lock()
	workers := make([]*broadcastFECPeerWorker, 0, len(b.workers))
	for _, worker := range b.workers {
		workers = append(workers, worker)
	}
	b.mx.Unlock()

	for _, worker := range workers {
		worker.closeControl()
	}
}

func (w *broadcastFECPeerWorker) setPeer(peer BroadcastPeer) {
	w.mx.Lock()
	w.peer = peer
	w.mx.Unlock()
}

func (w *broadcastFECPeerWorker) ensureControl(peer BroadcastPeer, hash []byte, handler broadcastFECControlHandler) {
	registrar, ok := peer.(broadcastFECControlRegistrar)
	if !ok {
		return
	}

	w.mx.Lock()
	if w.completed || w.unregisterControl != nil {
		w.mx.Unlock()
		return
	}
	w.unregisterControl = registrar.registerBroadcastFECControl(hash, handler)
	w.mx.Unlock()
}

func (w *broadcastFECPeerWorker) closeControl() {
	w.mx.Lock()
	unregister := w.unregisterControl
	w.unregisterControl = nil
	w.mx.Unlock()

	if unregister != nil {
		unregister()
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

func (w *broadcastFECPeerWorker) observeControl(control BroadcastFECControl, sender *BroadcastFECSender, now time.Time) {
	var unregister func()

	w.mx.Lock()

	if control.Completed {
		if !w.received {
			w.observeDeliveredLocked(sender, now)
		}
		w.received = true
		w.completed = true
		unregister = w.unregisterControl
		w.unregisterControl = nil
	} else {
		if !w.received {
			w.observeDeliveredLocked(sender, now)
			w.shortProbeDelay = int64(broadcastFECShortProbeInitialDelay / time.Millisecond)
			w.nextShortProbeAt = now.UnixMilli() + w.shortProbeDelay
		}
		w.received = true
	}

	w.mx.Unlock()

	if unregister != nil {
		unregister()
	}
}

func (w *broadcastFECPeerWorker) step(ctx context.Context, sender *BroadcastFECSender, now time.Time) error {
	w.mx.Lock()
	defer w.mx.Unlock()

	if w.completed || sender.Expired() {
		return nil
	}

	ms := now.UnixMilli()
	if w.failedUntil > ms {
		return nil
	}

	if w.received {
		return w.stepReceivedLocked(ctx, sender, now)
	}

	totalParts := sender.totalPartsLimit()
	if w.nextSeqno >= totalParts {
		return nil
	}

	if !w.recoveryReady {
		w.rateCtrl.OnNewSendBurst()

		maxFast := int(w.fastSeqnoTill - w.nextSeqno)
		batch := w.rateLimit.ConsumePackets(maxFast, int(sender.fec.SymbolSize))
		if batch == 0 {
			return nil
		}

		if err := w.sendBatchLocked(ctx, sender, batch, now); err != nil {
			return w.handleSendErrorLocked(ctx, now, err)
		}
		w.resetSendErrorLocked()

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

	if err := w.sendBatchLocked(ctx, sender, consumed, now); err != nil {
		return w.handleSendErrorLocked(ctx, now, err)
	}
	w.resetSendErrorLocked()

	w.nextRecoverDelay = delay
	w.lastRecoverAt = ms
	return nil
}

func (w *broadcastFECPeerWorker) stepReceivedLocked(ctx context.Context, sender *BroadcastFECSender, now time.Time) error {
	w.rateCtrl.SetAppLimited(true)

	ms := now.UnixMilli()
	if w.shortProbeDelay == 0 {
		w.shortProbeDelay = int64(broadcastFECShortProbeInitialDelay / time.Millisecond)
	}
	if w.nextShortProbeAt == 0 {
		w.nextShortProbeAt = ms + w.shortProbeDelay
	}
	if ms < w.nextShortProbeAt {
		return nil
	}

	if err := w.sendShortProbeLocked(ctx, sender); err != nil {
		return w.handleSendErrorLocked(ctx, now, err)
	}
	w.resetSendErrorLocked()

	nextDelay := w.shortProbeDelay * 2
	if maxDelay := int64(broadcastFECShortProbeMaxDelay / time.Millisecond); nextDelay > maxDelay {
		nextDelay = maxDelay
	}
	w.shortProbeDelay = nextDelay
	w.nextShortProbeAt = ms + nextDelay
	return nil
}

func (w *broadcastFECPeerWorker) sendBatchLocked(ctx context.Context, sender *BroadcastFECSender, batch int, now time.Time) error {
	for i := 0; i < batch; i++ {
		part, err := sender.part(w.nextSeqno)
		if err != nil {
			return fmt.Errorf("failed to build part %d: %w", w.nextSeqno, err)
		}

		msg := tl.Serializable(part.full)
		if err = w.peer.SendCustomMessage(ctx, msg); err != nil {
			return fmt.Errorf("failed to send part %d to peer %x: %w", w.nextSeqno, w.peer.ID(), err)
		}

		if w.firstSentAt.IsZero() {
			w.firstSentAt = now
		}
		w.sendClock.OnSend(w.nextSeqno, now.UnixMilli())
		w.sentFull++
		w.nextSeqno++
	}
	return nil
}

func (w *broadcastFECPeerWorker) sendShortProbeLocked(ctx context.Context, sender *BroadcastFECSender) error {
	totalParts := sender.totalPartsLimit()
	if totalParts == 0 {
		return nil
	}

	seqno := w.nextSeqno
	if seqno > 0 {
		seqno--
	}
	if seqno >= totalParts {
		seqno = totalParts - 1
	}

	part, err := sender.part(seqno)
	if err != nil {
		return fmt.Errorf("failed to build short probe %d: %w", seqno, err)
	}
	if err = w.peer.SendCustomMessage(ctx, part.short); err != nil {
		return fmt.Errorf("failed to send short probe %d to peer %x: %w", seqno, w.peer.ID(), err)
	}
	return nil
}

func (w *broadcastFECPeerWorker) observeDeliveredLocked(sender *BroadcastFECSender, now time.Time) {
	if w.deliveredObserved {
		return
	}
	w.deliveredObserved = true
	w.rateCtrl.SetAppLimited(true)

	if w.sentFull == 0 {
		return
	}

	ms := now.UnixMilli()
	if w.nextSeqno > 0 {
		if sentAt, ok := w.sendClock.SentAt(w.nextSeqno - 1); ok && ms > sentAt {
			w.rateCtrl.ObserveRTT(ms - sentAt)
		} else if !w.firstSentAt.IsZero() && ms > w.firstSentAt.UnixMilli() {
			w.rateCtrl.ObserveRTT(ms - w.firstSentAt.UnixMilli())
		}
	}

	total := int64(w.sentFull) * int64(sender.fec.SymbolSize)
	receivedSymbols := w.sentFull
	if receivedSymbols > sender.fec.SymbolsCount {
		receivedSymbols = sender.fec.SymbolsCount
	}
	received := int64(receivedSymbols) * int64(sender.fec.SymbolSize)
	if received > total {
		received = total
	}
	w.rateCtrl.ObserveDelta(total, received)
}

func (w *broadcastFECPeerWorker) handleSendErrorLocked(ctx context.Context, now time.Time, err error) error {
	if ctx.Err() != nil {
		return err
	}

	delay := w.sendErrorDelay
	if delay == 0 {
		delay = int64(broadcastFECSendErrorInitialDelay / time.Millisecond)
	}
	w.failedUntil = now.UnixMilli() + delay

	delay *= 2
	if maxDelay := int64(broadcastFECSendErrorMaxDelay / time.Millisecond); delay > maxDelay {
		delay = maxDelay
	}
	w.sendErrorDelay = delay
	return nil
}

func (w *broadcastFECPeerWorker) resetSendErrorLocked() {
	w.failedUntil = 0
	w.sendErrorDelay = 0
}
