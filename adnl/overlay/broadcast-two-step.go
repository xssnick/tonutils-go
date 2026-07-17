package overlay

import (
	"bytes"
	"container/list"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/xssnick/raptorq"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

const DefaultTwoStepBroadcastMaxActiveStreams = 128
const DefaultTwoStepBroadcastMaxActiveBytes = 256 << 20
const DefaultTwoStepDeliveredCacheSize = 4096

const broadcastTwoStepDateSkew = 20 * time.Second
const broadcastTwoStepStreamTTL = 25 * time.Second

type broadcastTwoStepStream struct {
	decoder           *raptorq.Decoder
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
		stream.mx.Lock()
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
		ImmediatePeerID:  append([]byte(nil), info.ImmediatePeerID...),
		Trusted:          info.Trusted,
		OverlayID:        append([]byte(nil), info.OverlayID...),
		BroadcastID:      append([]byte(nil), info.BroadcastID...),
		Extra:            append([]byte(nil), info.Extra...),
		Delivery:         info.Delivery,
		SignatureChecked: info.SignatureChecked,
	}
}

func (a *ADNLOverlayWrapper) twoStepPrecheckInfo(sourceID []byte, sourceKey ed25519.PublicKey, immediatePeerID []byte, trusted bool, broadcastID, extra []byte, delivery BroadcastDelivery, signatureChecked bool) BroadcastPrecheckInfo {
	return BroadcastPrecheckInfo{
		SourceID:         sourceID,
		SourceKey:        sourceKey,
		ImmediatePeerID:  immediatePeerID,
		Trusted:          trusted,
		OverlayID:        a.overlayId,
		BroadcastID:      broadcastID,
		Extra:            extra,
		Delivery:         delivery,
		SignatureChecked: signatureChecked,
	}
}

func (a *ADNLOverlayWrapper) twoStepBroadcastInfo(sourceID []byte, sourceKey ed25519.PublicKey, immediatePeerID []byte, trusted bool, broadcastID, extra []byte, delivery BroadcastDelivery) BroadcastInfo {
	return BroadcastInfo{
		SourceID:        sourceID,
		SourceKey:       sourceKey,
		ImmediatePeerID: immediatePeerID,
		Trusted:         trusted,
		OverlayID:       a.overlayId,
		BroadcastID:     broadcastID,
		Extra:           extra,
		Delivery:        delivery,
	}
}

func (a *ADNLOverlayWrapper) rebroadcastTwoStep(ctx context.Context, sourceADNL []byte, msg tl.Serializable) error {
	peerSet, localID := a.twoStepRelayConfig()
	if peerSet == nil {
		return nil
	}

	var sendErr error
	for _, peer := range peerSet.Peers() {
		peerID := peer.ID()
		if bytes.Equal(peerID, sourceADNL) || (len(localID) > 0 && bytes.Equal(peerID, localID)) {
			continue
		}
		if err := peer.SendCustomMessage(ctx, msg); err != nil && sendErr == nil {
			sendErr = fmt.Errorf("failed to rebroadcast two-step message to peer %x: %w", peerID, err)
		}
	}
	return sendErr
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
		case broadcastAdmissionCommitted, broadcastAdmissionOverloaded:
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
	precheck := a.twoStepPrecheckInfo(sourceID, sourceKey, srcPeerID, trusted, broadcastID, t.Extra, BroadcastDeliveryTwoStepSimple, false)
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

	var rebroadcastErr error
	if bytes.Equal(srcPeerID, t.SourceADNL) {
		// Match the C++ node: an authorized, signed two-step message is relayed
		// before application admission. Retry only rolls back local admission;
		// packets already sent to peers cannot be rolled back.
		rebroadcastErr = a.rebroadcastTwoStep(context.Background(), t.SourceADNL, t)
	}

	info := a.twoStepBroadcastInfo(sourceID, sourceKey, srcPeerID, trusted, broadcastID, t.Extra, BroadcastDeliveryTwoStepSimple)
	info.Payload = t.Data
	delivery := a.deliverTwoStepBroadcast(t.Data, info)
	disposition = delivery.disposition
	if delivery.err != nil {
		return delivery.err
	}
	return rebroadcastErr
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
		precheck := a.twoStepPrecheckInfo(sourceID, sourceKey, srcPeerID, trusted, broadcastID, t.Extra, BroadcastDeliveryTwoStepFEC, false)
		if err = a.runBroadcastPrecheck(precheck); err != nil {
			return twoStepFECPartResult{err: fmt.Errorf("two-step broadcast precheck failed: %w", err)}
		}
		checkedNewStream = true
	}

	if err := verifyBroadcastTwoStepFECSignature(t.Source, broadcastID, t.Seqno, t.Part, t.Signature); err != nil {
		return twoStepFECPartResult{err: err}
	}

	if checkedNewStream {
		precheck := a.twoStepPrecheckInfo(sourceID, sourceKey, srcPeerID, trusted, broadcastID, t.Extra, BroadcastDeliveryTwoStepFEC, true)
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
	// Pin the registered stream with its lock before releasing the state lock.
	// Cleanup takes locks in the same order, so it cannot evict this stream
	// between the registry lookup and processing the current symbol.
	stream.mx.Lock()
	state.mx.Unlock()

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
			decodedNow, data, err := stream.decoder.Decode()
			if err != nil {
				stream.mx.Unlock()
				return twoStepFECPartResult{err: fmt.Errorf("failed to decode two-step raptorq packet: %w", err)}
			}
			if decodedNow {
				dHash := sha256.Sum256(data)
				if !bytes.Equal(dHash[:], t.DataHash) {
					stream.mx.Unlock()
					return twoStepFECPartResult{err: fmt.Errorf("broadcast data hash mismatch")}
				}

				admission = &broadcastAdmission{done: make(chan struct{})}
				stream.admission = admission
				stream.decoder = nil
				decodedData = data
				decodeTime = time.Since(decodeStarted)
				decoded = true
			}
		}
		stream.mx.Unlock()
	}

	var rebroadcastErr error
	if rebroadcastNow {
		// As in the C++ node, valid two-step FEC traffic is diffused before
		// application admission. A later Retry only forgets local decode state;
		// the part already sent to peers remains sent.
		rebroadcastErr = a.rebroadcastTwoStep(context.Background(), t.SourceADNL, t)
	}

	if waitAdmission != nil {
		<-waitAdmission.done
		switch waitAdmission.disposition {
		case BroadcastDispositionAcceptAndRelay, BroadcastDispositionIgnore:
			return twoStepFECPartResult{err: rebroadcastErr}
		case BroadcastDispositionRetry:
			return twoStepFECPartResult{recontend: true}
		default:
			return twoStepFECPartResult{err: fmt.Errorf("two-step fec admission completed with invalid disposition %d", waitAdmission.disposition)}
		}
	}
	if !decoded {
		return twoStepFECPartResult{err: rebroadcastErr}
	}

	info := a.twoStepBroadcastInfo(sourceID, sourceKey, srcPeerID, deliverTrusted, broadcastID, t.Extra, BroadcastDeliveryTwoStepFEC)
	info.DecodeTime = decodeTime
	info.Payload = decodedData
	delivery := a.deliverTwoStepBroadcast(decodedData, info)
	state.finishFECAdmission(id, stream, admission, delivery.disposition)
	if delivery.err != nil {
		return twoStepFECPartResult{err: delivery.err}
	}
	return twoStepFECPartResult{err: rebroadcastErr}
}

func (s *BroadcastTwoStepState) finishFECAdmission(id broadcastTwoStepIDKey, stream *broadcastTwoStepStream, admission *broadcastAdmission, disposition BroadcastDisposition) {
	s.mx.Lock()
	stream.mx.Lock()
	if s.streams[id] != stream || stream.admission != admission {
		stream.mx.Unlock()
		s.mx.Unlock()
		return
	}

	committed := disposition == BroadcastDispositionAcceptAndRelay || disposition == BroadcastDispositionIgnore
	s.removeStreamLocked(id, stream, committed)
	if committed {
		s.completed++
	}
	admission.disposition = disposition
	close(admission.done)
	stream.mx.Unlock()
	s.mx.Unlock()
}
