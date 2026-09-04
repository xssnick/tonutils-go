package overlay

import (
	"bytes"
	"container/list"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xssnick/raptorq"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

const DefaultTwoStepBroadcastMaxActiveStreams = 128
const DefaultTwoStepBroadcastMaxActiveBytes = 256 << 20
const DefaultTwoStepDeliveredCacheSize = 4096
const DefaultTwoStepRelayQueueSize = 1024
const DefaultTwoStepRelayConcurrency = 64
const DefaultTwoStepRelayMaxActiveBytes int64 = DefaultTwoStepBroadcastMaxActiveBytes

const DefaultTwoStepRelayPeerTimeout = 750 * time.Millisecond

const broadcastTwoStepDateSkew = 20 * time.Second
const broadcastTwoStepStreamTTL = 25 * time.Second

type broadcastTwoStepStream struct {
	decoder           *raptorq.Decoder
	decodeBuffer      []byte
	seenParts         map[uint32]struct{}
	admission         *broadcastAdmission
	budgetBytes       int64
	date              uint32
	dataHash          []byte
	dataSize          uint32
	partSize          uint32
	sourceID          []byte
	sourceKey         ed25519.PublicKey
	trusted           bool
	extra             []byte
	rebroadcastedPart bool
	lastMessageAt     time.Time
	mx                sync.Mutex
	// removed is set under state.mx before the stream leaves state.streams, so
	// a lookup can release state.mx before taking stream.mx and still notice an
	// eviction that happened while it waited.
	removed atomic.Bool
}

// lockLiveTwoStepStream takes stream.mx and reports whether the stream is still
// registered. A false return leaves the mutex unlocked.
func lockLiveTwoStepStream(stream *broadcastTwoStepStream) bool {
	stream.mx.Lock()
	if stream.removed.Load() {
		stream.mx.Unlock()
		return false
	}
	return true
}

type broadcastTwoStepIDKey [32]byte

type BroadcastTwoStepStats struct {
	ActiveStreams           int
	ActiveBytes             int64
	DeliveredBroadcasts     int
	DroppedTotal            uint64
	EvictedTotal            uint64
	CompletedTotal          uint64
	DeliveredCacheHitsTotal uint64
}

// BroadcastTwoStepRelayStats is a per-receiver snapshot of asynchronous
// two-step peer fanout. All totals count peer sends, not broadcasts.
type BroadcastTwoStepRelayStats struct {
	QueueDepth         int
	ActiveBytes        int64
	EnqueuedTotal      uint64
	QueueFullTotal     uint64
	ByteFullTotal      uint64
	ClosedTotal        uint64
	CanceledTotal      uint64
	PrepareFailedTotal uint64
	SentTotal          uint64
	FailedTotal        uint64
	TimedOutTotal      uint64
}

type broadcastTwoStepRelayTask struct {
	peer    BroadcastPeer
	payload *broadcastTwoStepRelayPayload
}

type broadcastTwoStepRelayPayload struct {
	message tl.Serializable
	body    *PreparedBroadcastMessage
	bytes   int64
	refs    atomic.Int64
}

type broadcastTwoStepRelaySubmitStatus uint8

const (
	broadcastTwoStepRelayQueued broadcastTwoStepRelaySubmitStatus = iota
	broadcastTwoStepRelayQueueFull
	broadcastTwoStepRelayClosed
)

// broadcastTwoStepRelayDispatcher owns a fixed worker pool. Peer sends are
// bounded by queue slots, while shared serialized bodies are independently
// bounded by bytes so large broadcasts cannot hide behind small task objects.
type broadcastTwoStepRelayDispatcher struct {
	ctx            context.Context
	cancel         context.CancelFunc
	queue          chan broadcastTwoStepRelayTask
	peerTimeout    time.Duration
	maxActiveBytes int64

	closed   bool
	submitMx sync.RWMutex
	close    sync.Once
	workers  sync.WaitGroup

	enqueued          atomic.Uint64
	queueFull         atomic.Uint64
	byteFull          atomic.Uint64
	closedSubmissions atomic.Uint64
	canceled          atomic.Uint64
	prepareFailed     atomic.Uint64
	sent              atomic.Uint64
	failed            atomic.Uint64
	timedOut          atomic.Uint64
	activeBytes       atomic.Int64
}

func newBroadcastTwoStepRelayDispatcher(queueSize, concurrency int, maxActiveBytes int64, peerTimeout time.Duration) *broadcastTwoStepRelayDispatcher {
	if queueSize < 1 {
		queueSize = DefaultTwoStepRelayQueueSize
	}
	if concurrency < 1 {
		concurrency = DefaultTwoStepRelayConcurrency
	}
	if peerTimeout <= 0 {
		peerTimeout = DefaultTwoStepRelayPeerTimeout
	}
	if maxActiveBytes < 1 {
		maxActiveBytes = DefaultTwoStepRelayMaxActiveBytes
	}

	ctx, cancel := context.WithCancel(context.Background())
	d := &broadcastTwoStepRelayDispatcher{
		ctx:            ctx,
		cancel:         cancel,
		queue:          make(chan broadcastTwoStepRelayTask, queueSize),
		peerTimeout:    peerTimeout,
		maxActiveBytes: maxActiveBytes,
	}
	d.workers.Add(concurrency)
	for range concurrency {
		go d.runWorker()
	}
	return d
}

func (d *broadcastTwoStepRelayDispatcher) Submit(task broadcastTwoStepRelayTask) broadcastTwoStepRelaySubmitStatus {
	d.submitMx.RLock()
	defer d.submitMx.RUnlock()

	if d.closed {
		d.closedSubmissions.Add(1)
		d.releasePayload(task.payload)
		return broadcastTwoStepRelayClosed
	}

	select {
	case d.queue <- task:
		d.enqueued.Add(1)
		return broadcastTwoStepRelayQueued
	default:
		d.queueFull.Add(1)
		d.releasePayload(task.payload)
		return broadcastTwoStepRelayQueueFull
	}
}

func (d *broadcastTwoStepRelayDispatcher) reservePayload(message tl.Serializable, body *PreparedBroadcastMessage, refs int) (*broadcastTwoStepRelayPayload, bool) {
	// The slice retains its complete backing allocation while any peer task is
	// alive, so charge capacity rather than only the serialized length.
	bodyBytes := int64(cap(body.Body()))
	for {
		active := d.activeBytes.Load()
		if bodyBytes > d.maxActiveBytes || active > d.maxActiveBytes-bodyBytes {
			d.byteFull.Add(uint64(refs))
			return nil, false
		}
		if d.activeBytes.CompareAndSwap(active, active+bodyBytes) {
			break
		}
	}

	payload := &broadcastTwoStepRelayPayload{
		message: message,
		body:    body,
		bytes:   bodyBytes,
	}
	payload.refs.Store(int64(refs))
	return payload, true
}

func (d *broadcastTwoStepRelayDispatcher) releasePayload(payload *broadcastTwoStepRelayPayload) {
	if payload != nil && payload.refs.Add(-1) == 0 {
		d.activeBytes.Add(-payload.bytes)
	}
}

func (d *broadcastTwoStepRelayDispatcher) Close() {
	d.close.Do(func() {
		d.submitMx.Lock()
		d.closed = true
		d.cancel()
		d.submitMx.Unlock()

		d.workers.Wait()
		for {
			select {
			case task := <-d.queue:
				d.canceled.Add(1)
				d.releasePayload(task.payload)
			default:
				return
			}
		}
	})
}

func (d *broadcastTwoStepRelayDispatcher) runWorker() {
	defer d.workers.Done()

	for {
		// Give shutdown priority over queued work. A task accepted before Close
		// may be abandoned, but an in-flight peer gets its context cancelled and
		// Close waits for every worker to return.
		select {
		case <-d.ctx.Done():
			return
		default:
		}

		select {
		case <-d.ctx.Done():
			return
		case task := <-d.queue:
			d.send(task)
		}
	}
}

func (d *broadcastTwoStepRelayDispatcher) send(task broadcastTwoStepRelayTask) {
	defer d.releasePayload(task.payload)

	ctx, cancel := context.WithTimeout(d.ctx, d.peerTimeout)
	err := SendPreparedBroadcast(ctx, task.peer, task.payload.message, task.payload.body)
	cancel()
	if err == nil {
		d.sent.Add(1)
		return
	}

	d.failed.Add(1)
	if errors.Is(err, context.Canceled) && d.ctx.Err() != nil {
		d.canceled.Add(1)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		d.timedOut.Add(1)
	}
}

func (d *broadcastTwoStepRelayDispatcher) Stats() BroadcastTwoStepRelayStats {
	return BroadcastTwoStepRelayStats{
		QueueDepth:         len(d.queue),
		ActiveBytes:        d.activeBytes.Load(),
		EnqueuedTotal:      d.enqueued.Load(),
		QueueFullTotal:     d.queueFull.Load(),
		ByteFullTotal:      d.byteFull.Load(),
		ClosedTotal:        d.closedSubmissions.Load(),
		CanceledTotal:      d.canceled.Load(),
		PrepareFailedTotal: d.prepareFailed.Load(),
		SentTotal:          d.sent.Load(),
		FailedTotal:        d.failed.Load(),
		TimedOutTotal:      d.timedOut.Load(),
	}
}

type BroadcastTwoStepState struct {
	streams          map[broadcastTwoStepIDKey]*broadcastTwoStepStream
	simpleAdmissions map[broadcastTwoStepIDKey]*broadcastAdmission
	delivered        map[broadcastTwoStepIDKey]*list.Element
	deliveredList    *list.List

	maxActiveStreams int
	maxActiveBytes   int64
	deliveredMax     int
	activeBytes      int64
	nextCleanupAt    time.Time

	dropped            uint64
	evicted            uint64
	completed          uint64
	deliveredCacheHits uint64

	mx sync.Mutex
}

func NewBroadcastTwoStepState() *BroadcastTwoStepState {
	return &BroadcastTwoStepState{
		streams:          map[broadcastTwoStepIDKey]*broadcastTwoStepStream{},
		simpleAdmissions: map[broadcastTwoStepIDKey]*broadcastAdmission{},
		delivered:        map[broadcastTwoStepIDKey]*list.Element{},
		deliveredList:    list.New(),
		maxActiveStreams: DefaultTwoStepBroadcastMaxActiveStreams,
		maxActiveBytes:   DefaultTwoStepBroadcastMaxActiveBytes,
		deliveredMax:     DefaultTwoStepDeliveredCacheSize,
	}
}

func (s *BroadcastTwoStepState) beginSimpleAdmission(id broadcastTwoStepIDKey, now time.Time) broadcastAdmissionAttempt {
	s.mx.Lock()
	s.cleanupLocked(now, false)
	if s.isDeliveredLocked(id) {
		s.deliveredCacheHits++
		s.mx.Unlock()
		return broadcastAdmissionAttempt{status: broadcastAdmissionCommitted}
	}
	if admission := s.simpleAdmissions[id]; admission != nil {
		if admission.standby > 0 {
			s.mx.Unlock()
			return broadcastAdmissionAttempt{status: broadcastAdmissionDuplicate}
		}
		admission.standby++
		s.mx.Unlock()
		return broadcastAdmissionAttempt{admission: admission, status: broadcastAdmissionWait}
	}
	if len(s.simpleAdmissions) >= DefaultBroadcastMaxConcurrentAdmissions {
		s.dropped++
		s.mx.Unlock()
		return broadcastAdmissionAttempt{status: broadcastAdmissionOverloaded}
	}

	admission := &broadcastAdmission{done: make(chan struct{})}
	s.simpleAdmissions[id] = admission
	s.mx.Unlock()
	return broadcastAdmissionAttempt{admission: admission, status: broadcastAdmissionOwner}
}

func (s *BroadcastTwoStepState) finishSimpleAdmission(id broadcastTwoStepIDKey, admission *broadcastAdmission, disposition BroadcastDisposition) {
	s.mx.Lock()
	if s.simpleAdmissions[id] != admission {
		s.mx.Unlock()
		return
	}
	if disposition == BroadcastDispositionAcceptAndRelay || disposition == BroadcastDispositionIgnore {
		s.registerDeliveredLocked(id)
		s.completed++
	}
	delete(s.simpleAdmissions, id)
	admission.disposition = disposition
	close(admission.done)
	s.mx.Unlock()
}

func newBroadcastTwoStepIDKey(id []byte) (key broadcastTwoStepIDKey) {
	copy(key[:], id)
	return key
}

func (s *BroadcastTwoStepState) SetLimits(maxActiveStreams int, maxActiveBytes int64) {
	if maxActiveStreams < 1 {
		maxActiveStreams = 1
	}
	if maxActiveBytes < 1 {
		maxActiveBytes = 1
	}

	now := time.Now()
	s.mx.Lock()
	s.maxActiveStreams = maxActiveStreams
	s.maxActiveBytes = maxActiveBytes
	s.cleanupLocked(now, true)
	s.mx.Unlock()
}

func (s *BroadcastTwoStepState) SetDeliveredCacheSize(max int) {
	if max < 0 {
		max = 0
	}

	s.mx.Lock()
	s.deliveredMax = max
	s.trimDeliveredLocked()
	s.mx.Unlock()
}

func (s *BroadcastTwoStepState) Stats() BroadcastTwoStepStats {
	s.mx.Lock()
	defer s.mx.Unlock()

	return BroadcastTwoStepStats{
		ActiveStreams:           len(s.streams),
		ActiveBytes:             s.activeBytes,
		DeliveredBroadcasts:     len(s.delivered),
		DroppedTotal:            s.dropped,
		EvictedTotal:            s.evicted,
		CompletedTotal:          s.completed,
		DeliveredCacheHitsTotal: s.deliveredCacheHits,
	}
}

func (s *BroadcastTwoStepState) cleanupLocked(now time.Time, force bool) {
	if !force && !s.nextCleanupAt.IsZero() && now.Before(s.nextCleanupAt) {
		return
	}
	s.nextCleanupAt = now.Add(fecBroadcastCleanupInterval)

	for id, stream := range s.streams {
		// TryLock, never Lock: this runs under s.mx and stream.mx is held
		// across RaptorQ decode. A busy stream is being worked on, so it is not
		// idle; the next sweep reconsiders it.
		if !stream.mx.TryLock() {
			continue
		}
		stale := stream.admission == nil && stream.lastMessageAt.Add(broadcastTwoStepStreamTTL).Before(now)
		stream.mx.Unlock()
		if !stale {
			continue
		}

		s.removeStreamLocked(id, stream, false)
		s.evicted++
	}
}

func (s *BroadcastTwoStepState) reserveLocked(now time.Time, budgetBytes int64) bool {
	s.cleanupLocked(now, false)
	if budgetBytes > s.maxActiveBytes || len(s.streams)+1 > s.maxActiveStreams || s.activeBytes+budgetBytes > s.maxActiveBytes {
		s.dropped++
		return false
	}

	s.activeBytes += budgetBytes
	return true
}

func (s *BroadcastTwoStepState) releaseLocked(budgetBytes int64) {
	s.activeBytes -= budgetBytes
	if s.activeBytes < 0 {
		s.activeBytes = 0
	}
}

func (s *BroadcastTwoStepState) removeStreamLocked(id broadcastTwoStepIDKey, stream *broadcastTwoStepStream, delivered bool) {
	if s.streams[id] != stream {
		return
	}

	// Marked before the delete becomes visible, so a lookup already holding
	// this pointer and waiting for stream.mx notices the eviction.
	stream.removed.Store(true)
	delete(s.streams, id)
	s.releaseLocked(stream.budgetBytes)
	if delivered {
		s.registerDeliveredLocked(id)
	}
}

func (s *BroadcastTwoStepState) registerDeliveredLocked(id broadcastTwoStepIDKey) {
	if s.deliveredMax == 0 {
		return
	}

	if elem := s.delivered[id]; elem != nil {
		s.deliveredList.MoveToBack(elem)
		return
	}

	s.delivered[id] = s.deliveredList.PushBack(id)
	s.trimDeliveredLocked()
}

func (s *BroadcastTwoStepState) isDeliveredLocked(id broadcastTwoStepIDKey) bool {
	elem := s.delivered[id]
	if elem == nil {
		return false
	}

	s.deliveredList.MoveToBack(elem)
	return true
}

func (s *BroadcastTwoStepState) trimDeliveredLocked() {
	for len(s.delivered) > s.deliveredMax {
		elem := s.deliveredList.Front()
		if elem == nil {
			return
		}

		id := elem.Value.(broadcastTwoStepIDKey)
		delete(s.delivered, id)
		s.deliveredList.Remove(elem)
	}
}

func (a *ADNLOverlayWrapper) cleanupTwoStepBroadcasts(now time.Time, force bool) {
	state := a.activeTwoStepState()

	state.mx.Lock()
	state.cleanupLocked(now, force)
	state.mx.Unlock()
}

func checkBroadcastTwoStepDate(date uint32, now time.Time) error {
	unix := now.Unix()
	if int64(date) < unix-int64(broadcastTwoStepDateSkew/time.Second) {
		return fmt.Errorf("too old broadcast")
	}
	if int64(date) > unix+int64(broadcastTwoStepDateSkew/time.Second) {
		return fmt.Errorf("too new broadcast")
	}
	return nil
}

func estimateTwoStepBroadcastBudgetBytes(dataSize, partSize uint32) int64 {
	if partSize == 0 {
		return maxFECBroadcastBudgetEstimate
	}

	symbols := broadcastTwoStepSymbolsNeeded(dataSize, partSize)
	symbolBytes := multiplyFECBroadcastBudgetEstimate(uint64(symbols), uint64(partSize))
	hashBytes := multiplyFECBroadcastBudgetEstimate(uint64(symbols), 16)
	return addFECBroadcastBudgetEstimate(int64(dataSize), symbolBytes, hashBytes, 4096)
}

func broadcastTwoStepSymbolsNeeded(dataSize, partSize uint32) uint32 {
	if partSize == 0 {
		return 0
	}
	symbols := dataSize / partSize
	if dataSize%partSize != 0 {
		symbols++
	}
	return symbols
}

func broadcastTwoStepSeqnoLimit(dataSize, partSize uint32) uint32 {
	symbols := broadcastTwoStepSymbolsNeeded(dataSize, partSize)
	if symbols > (math.MaxUint32-4)/2 {
		return math.MaxUint32
	}
	return symbols*2 + 4
}

func broadcastTwoStepSourceInfo(source any) (keys.PublicKeyED25519, []byte, error) {
	sourceKey, ok := source.(keys.PublicKeyED25519)
	if !ok {
		return keys.PublicKeyED25519{}, nil, fmt.Errorf("invalid signer key format")
	}

	sourceID, err := tl.Hash(source)
	if err != nil {
		return keys.PublicKeyED25519{}, nil, fmt.Errorf("source key id serialize failed: %w", err)
	}
	return sourceKey, sourceID, nil
}

func (a *ADNLOverlayWrapper) runBroadcastPrecheck(info BroadcastPrecheckInfo) error {
	handler := a.precheckHandler()
	if handler == nil {
		return nil
	}
	return handler(cloneBroadcastPrecheckInfo(info))
}

type twoStepDeliveryResult struct {
	disposition BroadcastDisposition
	err         error
}

func (a *ADNLOverlayWrapper) deliverTwoStepBroadcast(data []byte, info BroadcastInfo) twoStepDeliveryResult {
	var res any
	parseStarted := time.Time{}
	if info.DecodeTime > 0 {
		parseStarted = time.Now()
	}
	_, err := tl.ParseNoCopy(&res, data, true)
	if err != nil {
		// The signed payload is deterministic for this broadcast ID. Treating a
		// parse failure as terminal prevents every replay from spending parse CPU.
		return twoStepDeliveryResult{
			disposition: BroadcastDispositionIgnore,
			err:         fmt.Errorf("failed to parse two-step broadcast message: %w", err),
		}
	}
	if !parseStarted.IsZero() {
		info.DecodeTime += time.Since(parseStarted)
	}

	disposition := BroadcastDispositionAcceptAndRelay
	if handler := a.broadcastHandler(); handler != nil {
		disposition = handler(res, cloneBroadcastInfo(info))
	}
	switch disposition {
	case BroadcastDispositionAcceptAndRelay, BroadcastDispositionIgnore:
		return twoStepDeliveryResult{disposition: disposition}
	case BroadcastDispositionRetry:
		return twoStepDeliveryResult{disposition: disposition, err: ErrBroadcastRejected}
	case BroadcastDispositionUnknown:
		return twoStepDeliveryResult{
			disposition: BroadcastDispositionRetry,
			err:         fmt.Errorf("broadcast handler returned an unknown disposition"),
		}
	default:
		return twoStepDeliveryResult{
			disposition: BroadcastDispositionRetry,
			err:         fmt.Errorf("broadcast handler returned invalid disposition %d", disposition),
		}
	}
}

func cloneBroadcastInfo(info BroadcastInfo) BroadcastInfo {
	return BroadcastInfo{
		SourceID:        append([]byte(nil), info.SourceID...),
		SourceKey:       append(ed25519.PublicKey(nil), info.SourceKey...),
		SourceADNL:      append([]byte(nil), info.SourceADNL...),
		ImmediatePeerID: append([]byte(nil), info.ImmediatePeerID...),
		Trusted:         info.Trusted,
		OverlayID:       append([]byte(nil), info.OverlayID...),
		BroadcastID:     append([]byte(nil), info.BroadcastID...),
		Extra:           append([]byte(nil), info.Extra...),
		Delivery:        info.Delivery,
		DecodeTime:      info.DecodeTime,
		Payload:         info.Payload,
	}
}

func cloneBroadcastPrecheckInfo(info BroadcastPrecheckInfo) BroadcastPrecheckInfo {
	return BroadcastPrecheckInfo{
		SourceID:         append([]byte(nil), info.SourceID...),
		SourceKey:        append(ed25519.PublicKey(nil), info.SourceKey...),
		SourceADNL:       append([]byte(nil), info.SourceADNL...),
		ImmediatePeerID:  append([]byte(nil), info.ImmediatePeerID...),
		Trusted:          info.Trusted,
		OverlayID:        append([]byte(nil), info.OverlayID...),
		BroadcastID:      append([]byte(nil), info.BroadcastID...),
		Extra:            append([]byte(nil), info.Extra...),
		Delivery:         info.Delivery,
		SignatureChecked: info.SignatureChecked,
	}
}

func (a *ADNLOverlayWrapper) twoStepPrecheckInfo(sourceID []byte, sourceKey ed25519.PublicKey, sourceADNL, immediatePeerID []byte, trusted bool, broadcastID, extra []byte, delivery BroadcastDelivery, signatureChecked bool) BroadcastPrecheckInfo {
	return BroadcastPrecheckInfo{
		SourceID:         sourceID,
		SourceKey:        sourceKey,
		SourceADNL:       sourceADNL,
		ImmediatePeerID:  immediatePeerID,
		Trusted:          trusted,
		OverlayID:        a.overlayId,
		BroadcastID:      broadcastID,
		Extra:            extra,
		Delivery:         delivery,
		SignatureChecked: signatureChecked,
	}
}

func (a *ADNLOverlayWrapper) twoStepBroadcastInfo(sourceID []byte, sourceKey ed25519.PublicKey, sourceADNL, immediatePeerID []byte, trusted bool, broadcastID, extra []byte, delivery BroadcastDelivery) BroadcastInfo {
	return BroadcastInfo{
		SourceID:        sourceID,
		SourceKey:       sourceKey,
		SourceADNL:      sourceADNL,
		ImmediatePeerID: immediatePeerID,
		Trusted:         trusted,
		OverlayID:       a.overlayId,
		BroadcastID:     broadcastID,
		Extra:           extra,
		Delivery:        delivery,
	}
}

func (r *BroadcastReceiver) ensureBroadcastTwoStepRelayDispatcher() *broadcastTwoStepRelayDispatcher {
	r.twoStepRelayMx.Lock()
	defer r.twoStepRelayMx.Unlock()

	if r.closed.Load() {
		return nil
	}
	if relay := r.twoStepRelay.Load(); relay != nil {
		return relay
	}

	relay := newBroadcastTwoStepRelayDispatcher(
		DefaultTwoStepRelayQueueSize,
		DefaultTwoStepRelayConcurrency,
		DefaultTwoStepRelayMaxActiveBytes,
		DefaultTwoStepRelayPeerTimeout,
	)
	r.twoStepRelay.Store(relay)
	return relay
}

// enqueueRebroadcastTwoStep preserves diffuse-before-admission ordering by
// completing every bounded queue submission before returning to local
// application delivery. Network sends happen on the owned worker pool, so a
// slow peer cannot delay decoding or validation. Queue saturation deliberately
// drops only the affected relay send; it never rejects an otherwise valid
// candidate that can still be relayed by other overlay members.
func (a *ADNLOverlayWrapper) enqueueRebroadcastTwoStep(sourceADNL []byte, msg tl.Serializable) {
	peerSet, localID := a.twoStepRelayConfig()
	if peerSet == nil {
		return
	}

	peers := peerSet.Peers()
	targets := make([]BroadcastPeer, 0, len(peers))
	needsLegacyMessage := false
	for _, peer := range peers {
		peerID := peer.ID()
		if bytes.Equal(peerID, sourceADNL) || (len(localID) > 0 && bytes.Equal(peerID, localID)) {
			continue
		}

		targets = append(targets, peer)
		if _, ok := peer.(PreparedBroadcastPeer); !ok {
			needsLegacyMessage = true
		}
	}
	if len(targets) == 0 {
		return
	}

	relay := a.ensureBroadcastTwoStepRelayDispatcher()
	if relay == nil {
		return
	}

	// Incoming TL slices may alias the pooled datagram buffer. Serialize before
	// local delivery returns, then let every async prepared send share this
	// immutable owned body.
	body, err := PrepareBroadcastMessage(msg)
	if err != nil {
		relay.prepareFailed.Add(uint64(len(targets)))
		return
	}

	var stableMessage tl.Serializable
	if needsLegacyMessage {
		// Released BroadcastPeer implementations may still use the reflective
		// message API. Reparse the owned body once so those async calls never
		// retain pooled receive memory, while preserving the original pointer form.
		var parsed any
		if _, err = tl.Parse(&parsed, body.Body(), true); err != nil {
			relay.prepareFailed.Add(uint64(len(targets)))
			return
		}
		switch msg.(type) {
		case *BroadcastTwoStepSimple:
			parsedMessage, ok := parsed.(BroadcastTwoStepSimple)
			if !ok {
				relay.prepareFailed.Add(uint64(len(targets)))
				return
			}
			stableMessage = &parsedMessage
		case *BroadcastTwoStepFEC:
			parsedMessage, ok := parsed.(BroadcastTwoStepFEC)
			if !ok {
				relay.prepareFailed.Add(uint64(len(targets)))
				return
			}
			stableMessage = &parsedMessage
		default:
			stableMessage = parsed
		}
	}
	payload, ok := relay.reservePayload(stableMessage, body, len(targets))
	if !ok {
		return
	}

	for _, peer := range targets {
		relay.Submit(broadcastTwoStepRelayTask{
			peer:    peer,
			payload: payload,
		})
	}
}

func (a *ADNLOverlayWrapper) processBroadcastTwoStepSimple(t *BroadcastTwoStepSimple, srcPeerID []byte) error {
	now := time.Now()
	if err := checkBroadcastTwoStepDate(t.Date, now); err != nil {
		return err
	}
	if uint64(len(t.Data)) > math.MaxUint32 {
		return fmt.Errorf("data is too large")
	}

	sourceKey, sourceID, err := broadcastTwoStepSourceInfo(t.Source)
	if err != nil {
		return err
	}

	dataHash := calcBroadcastTwoStepDataHash(t.Data)
	broadcastID, err := calcBroadcastTwoStepIDFromSourceID(sourceID, t.Flags, t.Date, t.SourceADNL, dataHash, uint32(len(t.Data)), uint32(len(t.Data)), t.Extra)
	if err != nil {
		return err
	}
	id := newBroadcastTwoStepIDKey(broadcastID)

	state := a.activeTwoStepState()
	for {
		attempt := state.beginSimpleAdmission(id, time.Now())
		switch attempt.status {
		case broadcastAdmissionCommitted, broadcastAdmissionOverloaded, broadcastAdmissionDuplicate:
			return nil
		case broadcastAdmissionWait:
			<-attempt.admission.done
			switch attempt.admission.disposition {
			case BroadcastDispositionAcceptAndRelay, BroadcastDispositionIgnore:
				return nil
			case BroadcastDispositionRetry:
				continue
			default:
				return fmt.Errorf("two-step simple admission completed with invalid disposition %d", attempt.admission.disposition)
			}
		case broadcastAdmissionOwner:
			return a.processBroadcastTwoStepSimpleAdmission(t, srcPeerID, sourceID, sourceKey.Key, broadcastID, id, state, attempt.admission)
		default:
			return fmt.Errorf("invalid two-step simple admission status %d", attempt.status)
		}
	}
}

func (a *ADNLOverlayWrapper) processBroadcastTwoStepSimpleAdmission(
	t *BroadcastTwoStepSimple,
	srcPeerID, sourceID []byte,
	sourceKey ed25519.PublicKey,
	broadcastID []byte,
	id broadcastTwoStepIDKey,
	state *BroadcastTwoStepState,
	admission *broadcastAdmission,
) error {
	disposition := BroadcastDispositionRetry
	defer func() {
		state.finishSimpleAdmission(id, admission, disposition)
	}()

	checkRes, err := a.checkBroadcastSourceRules(sourceID, t.Certificate, uint32(len(t.Data)), true)
	if err != nil {
		return err
	}

	trusted := checkRes == CertCheckResultTrusted
	precheck := a.twoStepPrecheckInfo(sourceID, sourceKey, t.SourceADNL, srcPeerID, trusted, broadcastID, t.Extra, BroadcastDeliveryTwoStepSimple, false)
	if err = a.runBroadcastPrecheck(precheck); err != nil {
		return fmt.Errorf("two-step broadcast precheck failed: %w", err)
	}
	if err = verifyBroadcastTwoStepSimpleSignature(t.Source, broadcastID, t.Data, t.Signature); err != nil {
		return err
	}
	precheck.SignatureChecked = true
	if err = a.runBroadcastPrecheck(precheck); err != nil {
		return fmt.Errorf("two-step broadcast precheck failed: %w", err)
	}
	if err = checkBroadcastTwoStepDate(t.Date, time.Now()); err != nil {
		return err
	}

	if bytes.Equal(srcPeerID, t.SourceADNL) {
		// Match the C++ node: an authorized, signed two-step message is relayed
		// before application admission. Retry only rolls back local admission;
		// relay work already accepted by the bounded queue cannot be rolled back.
		a.enqueueRebroadcastTwoStep(t.SourceADNL, t)
	}

	info := a.twoStepBroadcastInfo(sourceID, sourceKey, t.SourceADNL, srcPeerID, trusted, broadcastID, t.Extra, BroadcastDeliveryTwoStepSimple)
	info.Payload = t.Data
	delivery := a.deliverTwoStepBroadcast(t.Data, info)
	disposition = delivery.disposition
	if delivery.err != nil {
		return delivery.err
	}
	return nil
}

func (a *ADNLOverlayWrapper) processBroadcastTwoStepFEC(t *BroadcastTwoStepFEC, srcPeerID []byte) error {
	now := time.Now()
	if err := checkBroadcastTwoStepDate(t.Date, now); err != nil {
		return err
	}
	if t.DataSize == 0 {
		return fmt.Errorf("data size is zero")
	}
	if len(t.Part) == 0 {
		return fmt.Errorf("part is empty")
	}
	if uint64(len(t.Part)) > math.MaxUint32 {
		return fmt.Errorf("part is too large")
	}

	sourceKey, sourceID, err := broadcastTwoStepSourceInfo(t.Source)
	if err != nil {
		return err
	}

	partSize := uint32(len(t.Part))
	if t.Seqno >= broadcastTwoStepSeqnoLimit(t.DataSize, partSize) {
		return fmt.Errorf("too big seqno")
	}

	broadcastID, err := calcBroadcastTwoStepIDFromSourceID(sourceID, t.Flags, t.Date, t.SourceADNL, t.DataHash, t.DataSize, partSize, t.Extra)
	if err != nil {
		return err
	}
	id := newBroadcastTwoStepIDKey(broadcastID)
	state := a.activeTwoStepState()

	for {
		result := a.processBroadcastTwoStepFECPart(t, srcPeerID, sourceID, sourceKey.Key, broadcastID, id, state)
		if !result.recontend {
			return result.err
		}
	}
}

type twoStepFECPartResult struct {
	recontend bool
	err       error
}

func (a *ADNLOverlayWrapper) processBroadcastTwoStepFECPart(
	t *BroadcastTwoStepFEC,
	srcPeerID, sourceID []byte,
	sourceKey ed25519.PublicKey,
	broadcastID []byte,
	id broadcastTwoStepIDKey,
	state *BroadcastTwoStepState,
) twoStepFECPartResult {
	now := time.Now()
	partSize := uint32(len(t.Part))

	state.mx.Lock()
	state.cleanupLocked(now, false)
	if state.isDeliveredLocked(id) {
		state.deliveredCacheHits++
		state.mx.Unlock()
		return twoStepFECPartResult{}
	}
	stream := state.streams[id]
	state.mx.Unlock()

	checkedNewStream := false
	var trusted bool
	if stream == nil {
		checkRes, err := a.checkBroadcastSourceRules(sourceID, t.Certificate, t.DataSize, true)
		if err != nil {
			return twoStepFECPartResult{err: err}
		}

		trusted = checkRes == CertCheckResultTrusted
		precheck := a.twoStepPrecheckInfo(sourceID, sourceKey, t.SourceADNL, srcPeerID, trusted, broadcastID, t.Extra, BroadcastDeliveryTwoStepFEC, false)
		if err = a.runBroadcastPrecheck(precheck); err != nil {
			return twoStepFECPartResult{err: fmt.Errorf("two-step broadcast precheck failed: %w", err)}
		}
		checkedNewStream = true
	}

	if err := verifyBroadcastTwoStepFECSignature(t.Source, broadcastID, t.Seqno, t.Part, t.Signature); err != nil {
		return twoStepFECPartResult{err: err}
	}

	if checkedNewStream {
		precheck := a.twoStepPrecheckInfo(sourceID, sourceKey, t.SourceADNL, srcPeerID, trusted, broadcastID, t.Extra, BroadcastDeliveryTwoStepFEC, true)
		if err := a.runBroadcastPrecheck(precheck); err != nil {
			return twoStepFECPartResult{err: fmt.Errorf("two-step broadcast precheck failed: %w", err)}
		}
		if err := checkBroadcastTwoStepDate(t.Date, time.Now()); err != nil {
			return twoStepFECPartResult{err: err}
		}
	}

	state.mx.Lock()
	if state.isDeliveredLocked(id) {
		state.deliveredCacheHits++
		state.mx.Unlock()
		return twoStepFECPartResult{}
	}
	stream = state.streams[id]
	if stream == nil && !checkedNewStream {
		state.mx.Unlock()
		return twoStepFECPartResult{recontend: true}
	}
	if stream == nil {
		budgetBytes := estimateTwoStepBroadcastBudgetBytes(t.DataSize, partSize)
		if !state.reserveLocked(now, budgetBytes) {
			state.mx.Unlock()
			return twoStepFECPartResult{err: fmt.Errorf("two-step broadcast receiver budget exceeded")}
		}

		decoder, err := raptorq.NewRaptorQ(partSize).CreateDecoder(t.DataSize)
		if err != nil {
			state.releaseLocked(budgetBytes)
			state.mx.Unlock()
			return twoStepFECPartResult{err: fmt.Errorf("failed to init raptorq decoder: %w", err)}
		}

		stream = &broadcastTwoStepStream{
			decoder:       decoder,
			seenParts:     map[uint32]struct{}{},
			budgetBytes:   budgetBytes,
			date:          t.Date,
			dataHash:      append([]byte(nil), t.DataHash...),
			dataSize:      t.DataSize,
			partSize:      partSize,
			sourceID:      append([]byte(nil), sourceID...),
			sourceKey:     append(ed25519.PublicKey(nil), sourceKey...),
			trusted:       trusted,
			extra:         append([]byte(nil), t.Extra...),
			lastMessageAt: now,
		}
		state.streams[id] = stream
	}
	// state.mx is released before stream.mx: stream.mx is held across the
	// RaptorQ decode, so holding both would serialize the whole overlay behind
	// one decode. stream.removed covers the eviction race instead.
	state.mx.Unlock()
	if !lockLiveTwoStepStream(stream) {
		return twoStepFECPartResult{recontend: true}
	}

	var (
		decodedData    []byte
		decoded        bool
		decodeTime     time.Duration
		rebroadcastNow bool
		deliverTrusted bool
		waitAdmission  *broadcastAdmission
		admission      *broadcastAdmission
	)

	if !bytes.Equal(stream.sourceKey, sourceKey) || !bytes.Equal(stream.sourceID, sourceID) {
		stream.mx.Unlock()
		return twoStepFECPartResult{err: fmt.Errorf("malformed source")}
	}
	if stream.date != t.Date || stream.dataSize != t.DataSize || stream.partSize != partSize || !bytes.Equal(stream.dataHash, t.DataHash) || !bytes.Equal(stream.extra, t.Extra) {
		stream.mx.Unlock()
		return twoStepFECPartResult{err: fmt.Errorf("malformed broadcast parameters")}
	}
	deliverTrusted = stream.trusted

	if stream.admission != nil {
		waitAdmission = stream.admission
		if _, seen := stream.seenParts[t.Seqno]; !seen {
			stream.seenParts[t.Seqno] = struct{}{}
			stream.lastMessageAt = now
			if bytes.Equal(srcPeerID, t.SourceADNL) && !stream.rebroadcastedPart {
				stream.rebroadcastedPart = true
				rebroadcastNow = true
			}
		}
		stream.mx.Unlock()
	} else {
		if _, seen := stream.seenParts[t.Seqno]; seen {
			stream.mx.Unlock()
			return twoStepFECPartResult{}
		}

		stream.seenParts[t.Seqno] = struct{}{}
		stream.lastMessageAt = now
		if bytes.Equal(srcPeerID, t.SourceADNL) && !stream.rebroadcastedPart {
			stream.rebroadcastedPart = true
			rebroadcastNow = true
		}

		canTryDecode, err := stream.decoder.AddSymbol(t.Seqno, t.Part)
		if err != nil {
			delete(stream.seenParts, t.Seqno)
			stream.mx.Unlock()
			return twoStepFECPartResult{err: fmt.Errorf("failed to add two-step raptorq symbol %d: %w", t.Seqno, err)}
		}

		if canTryDecode {
			decodeStarted := time.Now()
			if stream.decodeBuffer == nil {
				stream.decodeBuffer = make([]byte, stream.dataSize)
			}
			decodedNow, err := stream.decoder.DecodeInto(stream.decodeBuffer)
			if err != nil {
				stream.mx.Unlock()
				return twoStepFECPartResult{err: fmt.Errorf("failed to decode two-step raptorq packet: %w", err)}
			}
			if decodedNow {
				data := stream.decodeBuffer
				dHash := sha256.Sum256(data)
				if !bytes.Equal(dHash[:], t.DataHash) {
					stream.mx.Unlock()
					return twoStepFECPartResult{err: fmt.Errorf("broadcast data hash mismatch")}
				}

				admission = &broadcastAdmission{done: make(chan struct{})}
				stream.admission = admission
				stream.decoder = nil
				stream.decodeBuffer = nil
				decodedData = data
				decodeTime = time.Since(decodeStarted)
				decoded = true
			}
		}
		stream.mx.Unlock()
	}

	if rebroadcastNow {
		// As in the C++ node, valid two-step FEC traffic is diffused before
		// application admission. Submission to the bounded relay queue happens
		// here; a later Retry only forgets local decode state, while accepted
		// relay work remains accepted.
		a.enqueueRebroadcastTwoStep(t.SourceADNL, t)
	}

	if waitAdmission != nil {
		<-waitAdmission.done
		switch waitAdmission.disposition {
		case BroadcastDispositionAcceptAndRelay, BroadcastDispositionIgnore:
			return twoStepFECPartResult{}
		case BroadcastDispositionRetry:
			return twoStepFECPartResult{recontend: true}
		default:
			return twoStepFECPartResult{err: fmt.Errorf("two-step fec admission completed with invalid disposition %d", waitAdmission.disposition)}
		}
	}
	if !decoded {
		return twoStepFECPartResult{}
	}

	info := a.twoStepBroadcastInfo(sourceID, sourceKey, t.SourceADNL, srcPeerID, deliverTrusted, broadcastID, t.Extra, BroadcastDeliveryTwoStepFEC)
	info.DecodeTime = decodeTime
	info.Payload = decodedData
	delivery := a.deliverTwoStepBroadcast(decodedData, info)
	state.finishFECAdmission(id, stream, admission, delivery.disposition)
	if delivery.err != nil {
		return twoStepFECPartResult{err: delivery.err}
	}
	return twoStepFECPartResult{}
}

func (s *BroadcastTwoStepState) finishFECAdmission(id broadcastTwoStepIDKey, stream *broadcastTwoStepStream, admission *broadcastAdmission, disposition BroadcastDisposition) {
	// stream.mx first: a concurrent part of the same broadcast may be holding
	// it across a decode, and waiting for that while holding s.mx would stall
	// every other broadcast in the overlay. Nothing takes these in the opposite
	// order blockingly -- cleanupLocked only ever TryLocks a stream.
	stream.mx.Lock()
	s.mx.Lock()
	if s.streams[id] != stream || stream.admission != admission {
		s.mx.Unlock()
		stream.mx.Unlock()
		return
	}

	committed := disposition == BroadcastDispositionAcceptAndRelay || disposition == BroadcastDispositionIgnore
	s.removeStreamLocked(id, stream, committed)
	if committed {
		s.completed++
	}
	admission.disposition = disposition
	close(admission.done)
	s.mx.Unlock()
	stream.mx.Unlock()
}
