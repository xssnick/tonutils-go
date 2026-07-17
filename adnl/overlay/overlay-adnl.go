package overlay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xssnick/raptorq"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
)

type CertCheckResult int

const CertCheckResultForbidden CertCheckResult = 0
const CertCheckResultTrusted CertCheckResult = 1
const CertCheckResultNeedCheck CertCheckResult = 2

const DefaultFECBroadcastMaxActiveStreams = 128
const DefaultFECBroadcastMaxActiveBytes = 1 << 30
const DefaultFECDeliveredCacheSize = 4096
const DefaultBroadcastMaxConcurrentAdmissions = 128

const fecBroadcastStreamIdleTTL = 40 * time.Second
const fecBroadcastFinishedTTL = 90 * time.Second
const fecBroadcastCleanupInterval = 5 * time.Second
const maxFECBroadcastBudgetEstimate = int64(^uint64(0) >> 1)
const maxFECBroadcastSymbolSize = 1 << 11
const maxFECBroadcastSymbols = 1 << 24
const fecBroadcastPartBudgetOverheadBytes uint64 = 1024
const fecBroadcastStreamBudgetOverheadBytes int64 = 16 << 10
const fecBroadcastRetainedEncoderPayloadCopies uint64 = 3
const fecBroadcastRetainedPartStateBudgetBytes uint64 = 1024

var errFECBroadcastDelivered = errors.New("fec broadcast already delivered")

type fecBroadcastStream struct {
	decoder        *raptorq.Decoder
	encoder        *raptorq.Encoder
	partHashes     map[uint32][32]byte
	parts          map[uint32]broadcastFECRelayPart
	receivedPeers  map[string]struct{}
	completedPeers map[string]struct{}
	budgetBytes    int64
	finishedAt     *time.Time
	completedAt    *time.Time
	lastMessageAt  time.Time
	sourceObj      any
	certificate    any
	source         ed25519.PublicKey
	sourceID       []byte
	dataHash       []byte
	fec            rldp.FECRaptorQ
	date           uint32
	flags          int32
	trusted        bool
	checked        bool
	admissionDone  chan struct{}
	admissionErr   error
	disposition    BroadcastDisposition
	mx             sync.Mutex
}

type fecBroadcastStreamInit struct {
	fec         rldp.FECRaptorQ
	partSize    int
	sourceObj   any
	certificate any
	source      ed25519.PublicKey
	sourceID    []byte
	dataHash    []byte
	date        uint32
	flags       int32
	trusted     bool
}

type fecAdmissionResult struct {
	disposition BroadcastDisposition
	err         error
}

func (s *fecBroadcastStream) waitAdmission() fecAdmissionResult {
	s.mx.Lock()
	for s.admissionErr == nil && s.disposition == BroadcastDispositionUnknown && s.admissionDone != nil {
		done := s.admissionDone
		s.mx.Unlock()
		<-done
		s.mx.Lock()
	}
	result := fecAdmissionResult{
		disposition: s.disposition,
		err:         s.admissionErr,
	}
	s.mx.Unlock()
	return result
}

func (s *fecBroadcastStream) compactLocked(now time.Time) {
	s.decoder = nil
	s.encoder = nil
	s.partHashes = nil
	s.parts = nil
	s.receivedPeers = nil
	s.completedPeers = nil
	s.sourceObj = nil
	s.certificate = nil
	s.dataHash = nil
	s.finishedAt = &now
}

func (s *fecBroadcastStream) finishAdmissionLocked(disposition BroadcastDisposition) {
	s.disposition = disposition
	if s.admissionDone != nil {
		close(s.admissionDone)
	}
}

func (s *fecBroadcastStream) failLocked(now time.Time, err error) {
	s.compactLocked(now)
	s.admissionErr = err
	if s.admissionDone != nil {
		close(s.admissionDone)
	}
}

type FECBroadcastStats struct {
	ActiveStreams           int
	ActiveBytes             int64
	DeliveredBroadcasts     int
	DroppedTotal            uint64
	EvictedTotal            uint64
	CompletedTotal          uint64
	DeliveredCacheHitsTotal uint64
	SimpleRelaySentTotal    uint64
	SimpleRelayFailedTotal  uint64
	FECRelaySentTotal       uint64
	FECRelayFailedTotal     uint64
}

type BroadcastInfo struct {
	SourceID        []byte
	SourceKey       ed25519.PublicKey
	ImmediatePeerID []byte
	Trusted         bool
	OverlayID       []byte
	BroadcastID     []byte
	Extra           []byte
	Delivery        BroadcastDelivery
	DecodeTime      time.Duration
	// Payload is the immutable serialized TL payload. It remains valid after the
	// handler returns and may be retained, but must not be modified.
	Payload []byte
}

type BroadcastPrecheckInfo struct {
	SourceID         []byte
	SourceKey        ed25519.PublicKey
	ImmediatePeerID  []byte
	Trusted          bool
	OverlayID        []byte
	BroadcastID      []byte
	Extra            []byte
	Delivery         BroadcastDelivery
	SignatureChecked bool
}

type ADNLOverlayWrapper struct {
	customHandler     atomic.Pointer[adnlOverlayCustomHandler]
	queryHandler      atomic.Pointer[adnlOverlayQueryHandler]
	disconnectHandler atomic.Pointer[adnlOverlayDisconnectHandler]

	*BroadcastReceiver
	*ADNLWrapper
}

type adnlOverlayCustomHandler func(msg *adnl.MessageCustom) error
type adnlOverlayQueryHandler func(msg *adnl.MessageQuery) error
type adnlOverlayDisconnectHandler func(addr string, key ed25519.PublicKey)

func (a *ADNLWrapper) AttachOverlay(receiver *BroadcastReceiver) (*ADNLOverlayWrapper, error) {
	a.mx.Lock()
	defer a.mx.Unlock()

	w := a.overlays[receiver.overlayKey]
	if w != nil {
		if w.BroadcastReceiver == receiver {
			return w, nil
		}
		if !w.BroadcastReceiver.closed.Load() {
			return nil, fmt.Errorf("overlay %x is already attached to another broadcast receiver", receiver.overlayId)
		}
	}
	w = &ADNLOverlayWrapper{
		BroadcastReceiver: receiver,
		ADNLWrapper:       a,
	}
	a.overlays[receiver.overlayKey] = w
	a.rebuildBroadcastReceiversLocked()

	return w, nil
}

func (a *ADNLWrapper) detachOverlay(w *ADNLOverlayWrapper) {
	a.mx.Lock()
	defer a.mx.Unlock()

	if a.overlays[w.overlayKey] == w {
		delete(a.overlays, w.overlayKey)
		a.rebuildBroadcastReceiversLocked()
	}
}

func (a *ADNLOverlayWrapper) SendCustomMessage(ctx context.Context, req tl.Serializable) error {
	return a.ADNLWrapper.SendCustomMessage(ctx, []tl.Serializable{Message{Overlay: a.overlayId}, req})
}

func (a *ADNLOverlayWrapper) Query(ctx context.Context, req, result tl.Serializable) error {
	return a.ADNLWrapper.Query(ctx, []tl.Serializable{Query{Overlay: a.overlayId}, req}, result)
}

func (a *ADNLOverlayWrapper) GetRandomPeers(ctx context.Context) ([]Node, error) {
	var res NodesList
	err := a.Query(ctx, GetRandomPeers{}, &res)
	if err != nil {
		return nil, err
	}
	return res.List, nil
}

func (a *ADNLOverlayWrapper) SetCustomMessageHandler(handler func(msg *adnl.MessageCustom) error) {
	if handler == nil {
		a.customHandler.Store(nil)
		return
	}

	h := adnlOverlayCustomHandler(handler)
	a.customHandler.Store(&h)
}

func (a *ADNLOverlayWrapper) SetQueryHandler(handler func(msg *adnl.MessageQuery) error) {
	if handler == nil {
		a.queryHandler.Store(nil)
		return
	}

	h := adnlOverlayQueryHandler(handler)
	a.queryHandler.Store(&h)
}

func (a *ADNLOverlayWrapper) SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey)) {
	if handler == nil {
		a.disconnectHandler.Store(nil)
		return
	}

	h := adnlOverlayDisconnectHandler(handler)
	a.disconnectHandler.Store(&h)
}

func (a *ADNLOverlayWrapper) customMessageHandler() func(msg *adnl.MessageCustom) error {
	handler := a.customHandler.Load()
	if handler == nil {
		return nil
	}
	return *handler
}

func (a *ADNLOverlayWrapper) overlayQueryHandler() func(msg *adnl.MessageQuery) error {
	handler := a.queryHandler.Load()
	if handler == nil {
		return nil
	}
	return *handler
}

func (a *ADNLOverlayWrapper) overlayDisconnectHandler() func(addr string, key ed25519.PublicKey) {
	handler := a.disconnectHandler.Load()
	if handler == nil {
		return nil
	}
	return *handler
}

func (a *ADNLOverlayWrapper) Close() {
	a.ADNLWrapper.detachOverlay(a)
}

func (a *ADNLOverlayWrapper) checkRules(keyId string, dataSize uint32, isFEC bool) CertCheckResult {
	if dataSize == 0 {
		return CertCheckResultForbidden
	}

	maxSz, authorized := a.authorizedKey(keyId)
	if authorized {
		if maxSz >= dataSize {
			return CertCheckResultTrusted
		}
		return CertCheckResultForbidden // too big size of data
	}

	if a.maxUnauthSize < dataSize {
		return CertCheckResultForbidden // too big size of data
	}
	if !a.allowFEC && isFEC {
		return CertCheckResultForbidden
	}
	if a.trustUnauthorized {
		return CertCheckResultTrusted
	}
	return CertCheckResultNeedCheck
}

func broadcastFECSeqnoLimit(symbolsCount uint32) uint32 {
	limit := broadcastFECPartLimit(symbolsCount)
	if limit > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(limit)
}

func broadcastFECPartLimit(symbolsCount uint32) uint64 {
	return uint64(symbolsCount)*2 + 4
}

func validateFECBroadcastType(fec rldp.FECRaptorQ) error {
	if fec.SymbolSize == 0 {
		return fmt.Errorf("invalid fec type: symbol_size is 0")
	}
	if fec.SymbolSize > maxFECBroadcastSymbolSize {
		return fmt.Errorf("invalid fec type: symbol_size is too big")
	}
	if uint64(fec.DataSize) > uint64(fec.SymbolSize)*maxFECBroadcastSymbols {
		return fmt.Errorf("invalid fec type: too many symbols")
	}

	expectedSymbols := fec.DataSize / fec.SymbolSize
	if expectedSymbols*fec.SymbolSize != fec.DataSize {
		expectedSymbols++
	}
	if fec.SymbolsCount != expectedSymbols {
		return fmt.Errorf("invalid fec type: wrong symbols_count")
	}
	return nil
}

func estimateFECBroadcastBudgetBytes(fec rldp.FECRaptorQ, partSize int) int64 {
	baseSymbols := uint64(fec.SymbolsCount)
	maxParts := broadcastFECPartLimit(fec.SymbolsCount)
	repairSymbols := maxParts - baseSymbols
	symbolSize := uint64(fec.SymbolSize)
	partPayloadSize := symbolSize
	if partSize > 0 && uint64(partSize) > partPayloadSize {
		partPayloadSize = uint64(partSize)
	}

	// Decoder storage consists of the eagerly allocated K-symbol fast buffer
	// and a repair buffer whose append capacity can approach twice its length.
	// Until application admission, every accepted symbol is also retained as a
	// full relay part. Decode plus relay-enabled encoder construction can keep
	// another three base payload-sized buffers live before the next GC.
	fastDecoderBytes := multiplyFECBroadcastBudgetEstimate(baseSymbols, symbolSize)
	repairDecoderBytes := multiplyFECBroadcastBudgetEstimate(repairSymbols, symbolSize)
	relayPartBytes := multiplyFECBroadcastBudgetEstimate(maxParts, partPayloadSize)
	decodeWorkspaceBytes := multiplyFECBroadcastBudgetEstimate(baseSymbols, symbolSize)
	partMetadataBytes := multiplyFECBroadcastBudgetEstimate(maxParts, fecBroadcastPartBudgetOverheadBytes)

	return addFECBroadcastBudgetEstimate(
		fastDecoderBytes,
		repairDecoderBytes,
		repairDecoderBytes,
		relayPartBytes,
		decodeWorkspaceBytes,
		decodeWorkspaceBytes,
		decodeWorkspaceBytes,
		partMetadataBytes,
		fecBroadcastStreamBudgetOverheadBytes,
	)
}

func estimateRetainedFECBroadcastBudgetBytes(fec rldp.FECRaptorQ) int64 {
	baseSymbols := uint64(fec.SymbolsCount)
	symbolSize := uint64(fec.SymbolSize)
	maxParts := broadcastFECPartLimit(fec.SymbolsCount)

	// The encoder retains source symbols, its relaxed matrix and symbol metadata.
	// Exact duplicate/conflict detection may retain one hash for every legal
	// seqno. Its deliberately large per-part allowance also covers Go map slack
	// and the received/completed peer maps kept for late relay parts.
	encoderBytes := multiplyFECBroadcastBudgetEstimate(
		baseSymbols,
		symbolSize*fecBroadcastRetainedEncoderPayloadCopies,
	)
	partStateBytes := multiplyFECBroadcastBudgetEstimate(maxParts, fecBroadcastRetainedPartStateBudgetBytes)

	return addFECBroadcastBudgetEstimate(
		encoderBytes,
		partStateBytes,
		fecBroadcastStreamBudgetOverheadBytes,
	)
}

// lockOrCreateFECStream returns the currently registered stream with stream.mx
// held. It always takes state.mx before stream.mx, matching cleanupLocked, so a
// returned stream cannot be evicted between its registry lookup and processing.
func (s *BroadcastFECRelayState) lockOrCreateFECStream(id string, now time.Time, init fecBroadcastStreamInit) (*fecBroadcastStream, error) {
	budgetBytes := estimateFECBroadcastBudgetBytes(init.fec, init.partSize)

	s.mx.Lock()
	s.cleanupLocked(now, false)
	if s.isDeliveredLocked(id, now) {
		s.deliveredCacheHits++
		s.mx.Unlock()
		return nil, errFECBroadcastDelivered
	}
	if stream := s.streams[id]; stream != nil {
		stream.mx.Lock()
		s.mx.Unlock()
		return stream, nil
	}
	if !s.reserveLocked(now, budgetBytes) {
		s.mx.Unlock()
		return nil, fmt.Errorf("fec broadcast receiver budget exceeded")
	}
	s.mx.Unlock()

	decoder, err := raptorq.NewRaptorQ(uint32(init.fec.SymbolSize)).CreateDecoder(uint32(init.fec.DataSize))
	if err != nil {
		s.mx.Lock()
		s.cancelReservationLocked(budgetBytes)
		s.mx.Unlock()
		return nil, fmt.Errorf("failed to init raptorq decoder: %w", err)
	}

	candidate := &fecBroadcastStream{
		decoder:        decoder,
		partHashes:     map[uint32][32]byte{},
		parts:          map[uint32]broadcastFECRelayPart{},
		receivedPeers:  map[string]struct{}{},
		completedPeers: map[string]struct{}{},
		budgetBytes:    budgetBytes,
		lastMessageAt:  now,
		sourceObj:      init.sourceObj,
		certificate:    init.certificate,
		source:         init.source,
		sourceID:       append([]byte(nil), init.sourceID...),
		dataHash:       append([]byte(nil), init.dataHash...),
		fec:            init.fec,
		date:           init.date,
		flags:          init.flags,
		trusted:        init.trusted,
	}

	s.mx.Lock()
	if s.isDeliveredLocked(id, now) {
		s.cancelReservationLocked(budgetBytes)
		s.deliveredCacheHits++
		s.mx.Unlock()
		return nil, errFECBroadcastDelivered
	}
	if stream := s.streams[id]; stream != nil {
		s.cancelReservationLocked(budgetBytes)
		stream.mx.Lock()
		s.mx.Unlock()
		return stream, nil
	}

	s.commitReservationLocked()
	s.streams[id] = candidate
	candidate.mx.Lock()
	s.mx.Unlock()
	return candidate, nil
}

func multiplyFECBroadcastBudgetEstimate(a uint64, b uint64) int64 {
	if a == 0 || b == 0 {
		return 0
	}
	if a > uint64(maxFECBroadcastBudgetEstimate)/b {
		return maxFECBroadcastBudgetEstimate
	}
	return int64(a * b)
}

func addFECBroadcastBudgetEstimate(values ...int64) int64 {
	var total int64
	for _, value := range values {
		if value < 0 {
			return maxFECBroadcastBudgetEstimate
		}
		if value > maxFECBroadcastBudgetEstimate-total {
			return maxFECBroadcastBudgetEstimate
		}
		total += value
	}
	return total
}

// terminalFECBroadcastErrorLocked compacts a stream after a deterministic
// decode failure. The small terminal entry is retained until normal cleanup,
// preventing repeated expensive decode attempts for the same broadcast ID.
// The caller must hold stream.mx; this function releases it before returning.
func terminalFECBroadcastErrorLocked(state *BroadcastFECRelayState, id string, stream *fecBroadcastStream,
	now time.Time, err error) error {
	stream.failLocked(now, err)
	stream.mx.Unlock()

	state.mx.Lock()
	state.reduceStreamBudgetLocked(id, stream, broadcastFECTerminalBudgetBytes)
	state.mx.Unlock()
	return err
}

func (a *ADNLOverlayWrapper) sendFECControlMessage(msg tl.Serializable) error {
	if err := a.ADNL.SendCustomMessage(context.Background(), msg); err != nil {
		return fmt.Errorf("failed to send overlay fec control message: %w", err)
	}
	return nil
}

func (a *ADNLOverlayWrapper) relaySimpleBroadcast(ctx context.Context, sourcePeerID []byte, msg *Broadcast) error {
	relayCfg := a.broadcastSimpleRelayConfig()
	if relayCfg.peerSet == nil {
		return nil
	}

	var sendErr error
	var sent, failed uint64
	for _, peer := range relayCfg.peerSet.Peers() {
		if peer == nil {
			continue
		}
		peerID := peer.ID()
		if len(peerID) == 0 || bytes.Equal(peerID, sourcePeerID) || bytes.Equal(peerID, relayCfg.localID) {
			continue
		}
		if err := peer.SendCustomMessage(ctx, msg); err != nil {
			failed++
			if sendErr == nil {
				sendErr = fmt.Errorf("failed to relay broadcast to peer %x: %w", peerID, err)
			}
			continue
		}
		sent++
	}
	relayCfg.state.addRelayStats(false, sent, failed)
	return sendErr
}

func (a *ADNLOverlayWrapper) checkBroadcastSource(source any, certificate any, dataSize uint32, isFEC bool) (CertCheckResult, []byte, error) {
	srcID, err := tl.Hash(source)
	if err != nil {
		return CertCheckResultForbidden, nil, fmt.Errorf("source key id serialize failed: %w", err)
	}

	checkRes, err := a.checkBroadcastSourceRules(srcID, certificate, dataSize, isFEC)
	if err != nil {
		return CertCheckResultForbidden, nil, err
	}
	return checkRes, srcID, nil
}

func (a *ADNLOverlayWrapper) checkBroadcastSourceRules(sourceID []byte, certificate any, dataSize uint32, isFEC bool) (CertCheckResult, error) {
	checkRes := a.checkRules(string(sourceID), dataSize, isFEC)
	if checkRes != CertCheckResultTrusted && certificate != nil {
		var issuerID []byte

		var certRes CertCheckResult
		switch crt := certificate.(type) {
		case CheckableCert:
			certRes, err := crt.Check(sourceID, a.overlayId, dataSize, isFEC)
			if err != nil {
				return CertCheckResultForbidden, fmt.Errorf("cert check failed: %w", err)
			}
			if certRes == CertCheckResultForbidden {
				break
			}

			var issuedBy any
			switch cert := crt.(type) {
			case Certificate:
				issuedBy = cert.IssuedBy
			case CertificateV2:
				issuedBy = cert.IssuedBy
			}

			issuerID, err = tl.Hash(issuedBy)
			if err != nil {
				return CertCheckResultForbidden, fmt.Errorf("issuer key id serialize failed: %w", err)
			}
		case CertificateEmpty:
		default:
			return CertCheckResultForbidden, fmt.Errorf("not supported cert type %s", reflect.TypeOf(certificate).String())
		}

		if issuerID != nil {
			issuerRes := a.checkRules(string(issuerID), dataSize, isFEC)
			if issuerRes > certRes {
				issuerRes = certRes
			}

			if issuerRes > checkRes {
				checkRes = issuerRes
			}
		}
	}

	if checkRes == CertCheckResultForbidden {
		return CertCheckResultForbidden, fmt.Errorf("not allowed")
	}
	return checkRes, nil
}

func (a *ADNLOverlayWrapper) processBroadcast(t *Broadcast, sourcePeerID []byte) error {
	if t.Date < 0 {
		return fmt.Errorf("invalid broadcast date")
	}

	now := time.Now()
	if err := checkBroadcastFECDate(uint32(t.Date), now); err != nil {
		return err
	}

	sourceKey, ok := t.Source.(keys.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("invalid signer key format")
	}
	if uint64(len(t.Data)) > math.MaxUint32 {
		return fmt.Errorf("data is too large")
	}

	broadcastHash, _, err := calcBroadcastID(t.Source, t.Flags, t.Data)
	if err != nil {
		return err
	}
	var id broadcastSimpleIDKey
	copy(id[:], broadcastHash)

	state := a.activeFECState()
	if !a.IsActive() {
		return nil
	}

	var admission *broadcastAdmission
	for admission == nil {
		attempt := state.beginSimpleAdmission(id)
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
				return fmt.Errorf("broadcast admission completed with invalid disposition %d", attempt.admission.disposition)
			}
		case broadcastAdmissionOwner:
			admission = attempt.admission
		default:
			return fmt.Errorf("invalid broadcast admission status %d", attempt.status)
		}
	}
	defer func() {
		if admission != nil {
			state.finishSimpleAdmission(id, admission, BroadcastDispositionRetry)
		}
	}()

	checkRes, srcID, err := a.checkBroadcastSource(t.Source, t.Certificate, uint32(len(t.Data)), false)
	if err != nil {
		return err
	}

	trusted := checkRes == CertCheckResultTrusted
	precheck := BroadcastPrecheckInfo{
		SourceID:        srcID,
		SourceKey:       sourceKey.Key,
		ImmediatePeerID: sourcePeerID,
		Trusted:         trusted,
		OverlayID:       a.overlayId,
		BroadcastID:     broadcastHash,
		Delivery:        BroadcastDeliverySimple,
	}
	if err = a.runBroadcastPrecheck(precheck); err != nil {
		return fmt.Errorf("broadcast precheck failed: %w", err)
	}
	if err = verifyBroadcastSignature(t.Source, broadcastHash, uint32(t.Date), t.Signature); err != nil {
		return err
	}
	precheck.SignatureChecked = true
	if err = a.runBroadcastPrecheck(precheck); err != nil {
		return fmt.Errorf("broadcast precheck failed: %w", err)
	}
	if err = checkBroadcastFECDate(uint32(t.Date), time.Now()); err != nil {
		return err
	}

	var res any
	_, err = tl.ParseNoCopy(&res, t.Data, true)
	if err != nil {
		state.finishSimpleAdmission(id, admission, BroadcastDispositionIgnore)
		admission = nil
		return fmt.Errorf("failed to parse broadcast message: %w", err)
	}

	info := BroadcastInfo{
		SourceID:        append([]byte(nil), srcID...),
		SourceKey:       append(ed25519.PublicKey(nil), sourceKey.Key...),
		ImmediatePeerID: append([]byte(nil), sourcePeerID...),
		Trusted:         trusted,
		OverlayID:       append([]byte(nil), a.overlayId...),
		BroadcastID:     append([]byte(nil), broadcastHash...),
		Delivery:        BroadcastDeliverySimple,
		Payload:         t.Data,
	}
	if !a.IsActive() {
		return nil
	}

	disposition := BroadcastDispositionAcceptAndRelay
	if handler := a.broadcastHandler(); handler != nil {
		disposition = handler(res, info)
	}
	switch disposition {
	case BroadcastDispositionAcceptAndRelay, BroadcastDispositionIgnore, BroadcastDispositionRetry:
	case BroadcastDispositionUnknown:
		return fmt.Errorf("broadcast handler returned an unknown disposition")
	default:
		return fmt.Errorf("broadcast handler returned invalid disposition %d", disposition)
	}
	state.finishSimpleAdmission(id, admission, disposition)
	admission = nil
	if disposition != BroadcastDispositionAcceptAndRelay {
		return nil
	}
	if !a.IsActive() {
		return nil
	}
	return a.relaySimpleBroadcast(context.Background(), sourcePeerID, t)
}

func (a *ADNLOverlayWrapper) processFECBroadcast(t *BroadcastFEC) error {
	tm := time.Now()
	if err := checkBroadcastFECDate(t.Date, tm); err != nil {
		return err
	}

	sourceKey, ok := t.Source.(keys.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("invalid signer key format")
	}

	fec, ok := t.FEC.(rldp.FECRaptorQ)
	if !ok {
		return fmt.Errorf("not supported fec type")
	}
	if fec.DataSize != t.DataSize {
		return fmt.Errorf("incorrect data size")
	}
	if err := validateFECBroadcastType(fec); err != nil {
		return err
	}
	if uint64(len(t.Data)) != uint64(fec.SymbolSize) {
		return fmt.Errorf("incorrect symbol size %d, should be %d", len(t.Data), fec.SymbolSize)
	}
	if uint64(t.Seqno) >= broadcastFECPartLimit(fec.SymbolsCount) {
		return fmt.Errorf("too big seqno")
	}

	broadcastHash, err := t.CalcID()
	if err != nil {
		return fmt.Errorf("failed to calc broadcast hash: %w", err)
	}
	id := string(broadcastHash)
	state := a.activeFECState()
	partDataHash := calcBroadcastFECPartDataHash(t.Data)
	var stream *fecBroadcastStream
	for {
		state.mx.Lock()
		state.cleanupLocked(tm, false)
		if state.isDeliveredLocked(id, tm) {
			state.deliveredCacheHits++
			state.mx.Unlock()
			return a.sendFECControlMessage(FECCompleted{Hash: broadcastHash})
		}
		stream = state.streams[id]
		if stream != nil {
			stream.mx.Lock()
		}
		state.mx.Unlock()
		if stream == nil {
			break
		}

		if stream.flags&BroadcastFlagAnySender == 0 && !bytes.Equal(stream.source, sourceKey.Key) {
			stream.mx.Unlock()
			return fmt.Errorf("malformed source")
		}
		if t.Date != stream.date {
			stream.mx.Unlock()
			return fmt.Errorf("broadcast date mismatch")
		}
		if uint64(t.Seqno) >= broadcastFECPartLimit(stream.fec.SymbolsCount) {
			stream.mx.Unlock()
			return fmt.Errorf("too big seqno")
		}

		finished := stream.finishedAt != nil
		if finished && stream.partHashes == nil &&
			(stream.admissionErr != nil || stream.disposition != BroadcastDispositionUnknown) {
			result := fecAdmissionResult{
				disposition: stream.disposition,
				err:         stream.admissionErr,
			}
			stream.mx.Unlock()
			if result.err != nil {
				return result.err
			}
			switch result.disposition {
			case BroadcastDispositionAcceptAndRelay:
				return a.sendFECControlMessage(FECCompleted{Hash: broadcastHash})
			case BroadcastDispositionIgnore:
				return a.sendFECControlMessage(FECReceived{Hash: broadcastHash})
			case BroadcastDispositionRetry:
				return nil
			default:
				return fmt.Errorf("finished fec broadcast has invalid disposition %d", result.disposition)
			}
		}
		if stream.receivedPart(t.Seqno) {
			if stream.partHashes != nil {
				if existing, ok := stream.partHashes[t.Seqno]; ok && !bytes.Equal(existing[:], partDataHash) {
					stream.mx.Unlock()
					return fmt.Errorf("conflicting data for seqno %d", t.Seqno)
				}
			}
			stream.mx.Unlock()
			if finished {
				result := stream.waitAdmission()
				state.mx.RLock()
				registered := state.streams[id] == stream
				state.mx.RUnlock()
				if !registered {
					continue
				}
				if result.err != nil {
					return result.err
				}
				switch result.disposition {
				case BroadcastDispositionAcceptAndRelay:
					return a.sendFECControlMessage(FECCompleted{Hash: broadcastHash})
				case BroadcastDispositionIgnore:
					return a.sendFECControlMessage(FECReceived{Hash: broadcastHash})
				case BroadcastDispositionRetry:
					return nil
				default:
					return fmt.Errorf("finished fec broadcast has no disposition")
				}
			}
			return nil
		}
		stream.mx.Unlock()
		break
	}

	checkRes, srcID, err := a.checkBroadcastSource(t.Source, t.Certificate, t.DataSize, true)
	if err != nil {
		return err
	}

	partHash, err := calcBroadcastFECPartID(broadcastHash, partDataHash, t.Seqno)
	if err != nil {
		return err
	}
	if err = verifyBroadcastFECPartSignature(t.Source, partHash, t.Date, t.Signature); err != nil {
		return err
	}

	var (
		decodedRes     any
		decodedPayload []byte
		decoded        bool
		decodeTime     time.Duration
		relayOps       []broadcastFECRelayOp
	)
	trusted := false
	relayCfg := a.broadcastFECRelayConfig()
	relayPeers := []BroadcastPeer(nil)
	sourcePeerID := a.GetID()
	if relayCfg.enabled {
		relayPeers = relayCfg.peerSet.Peers()
	}

	init := fecBroadcastStreamInit{
		fec:         fec,
		partSize:    len(t.Data),
		sourceObj:   t.Source,
		certificate: t.Certificate,
		source:      sourceKey.Key,
		sourceID:    srcID,
		dataHash:    t.DataHash,
		date:        t.Date,
		flags:       t.Flags,
		trusted:     checkRes == CertCheckResultTrusted,
	}
	for {
		stream, err = state.lockOrCreateFECStream(id, tm, init)
		if err != nil {
			if errors.Is(err, errFECBroadcastDelivered) {
				return a.sendFECControlMessage(FECCompleted{Hash: broadcastHash})
			}
			return err
		}

		if stream.admissionErr != nil {
			err = stream.admissionErr
			stream.mx.Unlock()
			return err
		}
		if stream.disposition == BroadcastDispositionUnknown && stream.admissionDone != nil {
			done := stream.admissionDone
			stream.mx.Unlock()
			<-done
			continue
		}
		switch stream.disposition {
		case BroadcastDispositionIgnore:
			stream.mx.Unlock()
			return a.sendFECControlMessage(FECReceived{Hash: broadcastHash})
		case BroadcastDispositionRetry:
			stream.mx.Unlock()
			return nil
		case BroadcastDispositionUnknown, BroadcastDispositionAcceptAndRelay:
		default:
			disposition := stream.disposition
			stream.mx.Unlock()
			return fmt.Errorf("invalid fec broadcast disposition %d", disposition)
		}
		break
	}

	if stream.flags&BroadcastFlagAnySender == 0 && !bytes.Equal(stream.source, sourceKey.Key) {
		stream.mx.Unlock()
		return fmt.Errorf("malformed source")
	}
	if t.Date != stream.date {
		stream.mx.Unlock()
		return fmt.Errorf("broadcast date mismatch")
	}
	if uint64(t.Seqno) >= broadcastFECPartLimit(stream.fec.SymbolsCount) {
		stream.mx.Unlock()
		return fmt.Errorf("too big seqno")
	}
	if checkRes != CertCheckResultTrusted {
		stream.trusted = false
	}
	trusted = stream.trusted

	finished := stream.finishedAt != nil
	completed := stream.completedAt != nil
	if finished && !relayCfg.enabled {
		stream.mx.Unlock()
		if completed {
			return a.sendFECControlMessage(FECCompleted{Hash: broadcastHash})
		}
		return a.sendFECControlMessage(FECReceived{Hash: broadcastHash})
	}

	tm = time.Now()
	stream.lastMessageAt = tm

	if stream.receivedPart(t.Seqno) {
		if stream.partHashes != nil {
			if existing, ok := stream.partHashes[t.Seqno]; ok && !bytes.Equal(existing[:], partDataHash) {
				stream.mx.Unlock()
				return fmt.Errorf("conflicting data for seqno %d", t.Seqno)
			}
		}
		stream.mx.Unlock()
		if finished {
			result := stream.waitAdmission()
			if result.err != nil {
				return result.err
			}
			switch result.disposition {
			case BroadcastDispositionAcceptAndRelay:
				return a.sendFECControlMessage(FECCompleted{Hash: broadcastHash})
			case BroadcastDispositionIgnore:
				return a.sendFECControlMessage(FECReceived{Hash: broadcastHash})
			case BroadcastDispositionRetry:
				return nil
			default:
				return fmt.Errorf("finished fec broadcast has no disposition")
			}
		}
		return nil
	}

	stream.addReceivedPart(t.Seqno, partDataHash)
	stream.addRelayPart(t.Seqno, t, broadcastHash, partDataHash, sourcePeerID)

	if !finished {
		canTryDecode, err := stream.decoder.AddSymbol(t.Seqno, t.Data)
		if err != nil {
			delete(stream.partHashes, t.Seqno)
			delete(stream.parts, t.Seqno)
			stream.mx.Unlock()
			return fmt.Errorf("failed to add raptorq symbol %d: %w", t.Seqno, err)
		}

		if canTryDecode {
			decodeStarted := time.Now()
			decodedNow, data, err := stream.decoder.Decode()
			if err != nil {
				err = fmt.Errorf("failed to decode raptorq packet: %w", err)
				return terminalFECBroadcastErrorLocked(state, id, stream, tm, err)
			}

			// it may not be decoded due to unsolvable math system, it means we need more symbols
			if decodedNow {
				dHash := sha256.Sum256(data)
				if !bytes.Equal(dHash[:], t.DataHash) {
					return terminalFECBroadcastErrorLocked(state, id, stream, tm, fmt.Errorf("incorrect data hash"))
				}

				var res any
				_, err = tl.ParseNoCopy(&res, data, true)
				if err != nil {
					err = fmt.Errorf("failed to parse decoded broadcast message: %w", err)
					return terminalFECBroadcastErrorLocked(state, id, stream, tm, err)
				}
				decodeTime = time.Since(decodeStarted)

				stream.finishedAt = &tm
				stream.admissionDone = make(chan struct{})
				stream.decoder = nil
				if relayCfg.enabled {
					encoder, err := raptorq.NewRaptorQ(uint32(stream.fec.SymbolSize)).CreateEncoder(data)
					if err != nil {
						err = fmt.Errorf("failed to init raptorq encoder: %w", err)
						return terminalFECBroadcastErrorLocked(state, id, stream, tm, err)
					}
					stream.encoder = encoder
				}
				decodedRes = res
				decodedPayload = data
				decoded = true
			}
		}
	}

	if relayCfg.enabled && (stream.trusted || stream.checked) {
		relayOps = stream.relayPartOpsLocked(t.Seqno, relayPeers, relayCfg.localID, true)
	}
	stream.mx.Unlock()

	relayErr := sendBroadcastFECRelayOps(context.Background(), relayCfg.state, relayOps)

	if finished {
		if completed {
			if err = a.sendFECControlMessage(FECCompleted{Hash: broadcastHash}); err != nil {
				return err
			}
		} else if err = a.sendFECControlMessage(FECReceived{Hash: broadcastHash}); err != nil {
			return err
		}
		return relayErr
	}

	if decoded {
		info := BroadcastInfo{
			SourceID:        append([]byte(nil), stream.sourceID...),
			SourceKey:       append(ed25519.PublicKey(nil), stream.source...),
			ImmediatePeerID: append([]byte(nil), sourcePeerID...),
			Trusted:         trusted,
			OverlayID:       append([]byte(nil), a.overlayId...),
			BroadcastID:     append([]byte(nil), broadcastHash...),
			Delivery:        BroadcastDeliveryFEC,
			DecodeTime:      decodeTime,
			Payload:         decodedPayload,
		}

		disposition := BroadcastDispositionAcceptAndRelay
		if handler := a.broadcastHandler(); handler != nil {
			disposition = handler(decodedRes, info)
		}

		if disposition == BroadcastDispositionUnknown || disposition > BroadcastDispositionRetry {
			stream.mx.Lock()
			stream.compactLocked(tm)
			stream.finishAdmissionLocked(BroadcastDispositionRetry)
			stream.mx.Unlock()
			state.mx.Lock()
			state.removeStreamLocked(id, stream, false, time.Now())
			state.mx.Unlock()
			return fmt.Errorf("broadcast handler returned invalid disposition %d", disposition)
		}
		if disposition == BroadcastDispositionRetry {
			stream.mx.Lock()
			stream.compactLocked(tm)
			stream.finishAdmissionLocked(disposition)
			stream.mx.Unlock()
			state.mx.Lock()
			state.removeStreamLocked(id, stream, false, time.Now())
			state.mx.Unlock()
			return relayErr
		}
		if disposition == BroadcastDispositionIgnore {
			stream.mx.Lock()
			stream.compactLocked(tm)
			stream.finishAdmissionLocked(disposition)
			stream.mx.Unlock()
			state.mx.Lock()
			state.reduceStreamBudgetLocked(id, stream, broadcastFECTerminalBudgetBytes)
			state.mx.Unlock()
			if err = a.sendFECControlMessage(FECReceived{Hash: broadcastHash}); err != nil {
				return err
			}
			return relayErr
		}

		completedNow := false
		stream.mx.Lock()
		if stream.completedAt == nil {
			stream.completedAt = &tm
			stream.checked = true
			completedNow = true
		}
		drainOps := stream.drainRelayPartOpsLocked(relayPeers, relayCfg.localID)
		stream.finishAdmissionLocked(disposition)
		stream.mx.Unlock()
		if relayCfg.enabled {
			if err = sendBroadcastFECRelayOps(context.Background(), relayCfg.state, drainOps); err != nil && relayErr == nil {
				relayErr = err
			}
		}
		state.mx.Lock()
		if completedNow {
			state.completed++
		}
		if relayCfg.enabled {
			if stream.encoder == nil {
				state.reduceStreamBudgetLocked(id, stream, broadcastFECTerminalBudgetBytes)
			} else {
				state.reduceStreamBudgetLocked(id, stream, estimateRetainedFECBroadcastBudgetBytes(stream.fec))
			}
		} else {
			state.removeStreamLocked(id, stream, true, time.Now())
		}
		state.mx.Unlock()
	}

	if decoded {
		if err = a.sendFECControlMessage(FECCompleted{Hash: broadcastHash}); err != nil {
			return err
		}
	}
	return relayErr
}

func (a *ADNLOverlayWrapper) processFECBroadcastShort(t *BroadcastFECShort) error {
	if t.Seqno < 0 {
		return fmt.Errorf("invalid seqno")
	}
	sourceKey, ok := t.Source.(keys.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("invalid signer key format")
	}

	id := string(t.BroadcastHash)
	state := a.activeFECState()
	seqno := uint32(t.Seqno)
	relayCfg := a.broadcastFECRelayConfig()
	sourcePeerID := a.GetID()

	for {
		now := time.Now()
		state.mx.Lock()
		state.cleanupLocked(now, false)
		if state.isDeliveredLocked(id, now) {
			state.deliveredCacheHits++
			state.mx.Unlock()
			return a.sendFECControlMessage(FECCompleted{Hash: t.BroadcastHash})
		}
		stream := state.streams[id]
		if stream != nil {
			stream.mx.Lock()
		}
		state.mx.Unlock()
		if stream == nil {
			return fmt.Errorf("short part of unknown broadcast")
		}

		if stream.admissionErr != nil {
			err := stream.admissionErr
			stream.mx.Unlock()
			return err
		}
		if stream.disposition == BroadcastDispositionUnknown && stream.admissionDone != nil {
			done := stream.admissionDone
			stream.mx.Unlock()
			<-done
			continue
		}
		switch stream.disposition {
		case BroadcastDispositionIgnore:
			stream.mx.Unlock()
			return a.sendFECControlMessage(FECReceived{Hash: t.BroadcastHash})
		case BroadcastDispositionRetry:
			stream.mx.Unlock()
			return nil
		case BroadcastDispositionUnknown, BroadcastDispositionAcceptAndRelay:
		default:
			disposition := stream.disposition
			stream.mx.Unlock()
			return fmt.Errorf("invalid fec broadcast disposition %d", disposition)
		}

		if stream.flags&BroadcastFlagAnySender == 0 && !bytes.Equal(stream.source, sourceKey.Key) {
			stream.mx.Unlock()
			return fmt.Errorf("malformed source")
		}
		if uint64(seqno) >= broadcastFECPartLimit(stream.fec.SymbolsCount) {
			stream.mx.Unlock()
			return fmt.Errorf("too big seqno")
		}
		if stream.finishedAt == nil {
			stream.mx.Unlock()
			return fmt.Errorf("short part of unfinished broadcast")
		}
		if stream.receivedPart(seqno) {
			stream.mx.Unlock()
			return nil
		}
		if stream.encoder == nil {
			stream.mx.Unlock()
			return fmt.Errorf("encoder not ready")
		}

		date := stream.date
		fec := stream.fec
		flags := stream.flags
		dataHash := stream.dataHash
		completed := stream.completedAt != nil
		partData := append([]byte(nil), stream.encoder.GenSymbol(seqno)...)
		stream.mx.Unlock()

		if err := checkBroadcastFECDate(date, now); err != nil {
			return err
		}
		if !bytes.Equal(calcBroadcastFECPartDataHash(partData), t.PartDataHash) {
			return fmt.Errorf("wrong part data hash")
		}
		checkRes, _, err := a.checkBroadcastSource(t.Source, t.Certificate, fec.DataSize, true)
		if err != nil {
			return err
		}
		if err = t.VerifySignature(date); err != nil {
			return err
		}

		broadcastFEC := &BroadcastFEC{
			Source:      t.Source,
			Certificate: t.Certificate,
			DataHash:    dataHash,
			DataSize:    fec.DataSize,
			Flags:       flags,
			Data:        partData,
			Seqno:       seqno,
			FEC:         fec,
			Date:        date,
			Signature:   t.Signature,
		}

		// Fetched only after the duplicate/validity early-outs above: peer sets
		// may be expensive to build and this handler runs for every short part.
		relayPeers := []BroadcastPeer(nil)
		if relayCfg.enabled {
			relayPeers = relayCfg.peerSet.Peers()
		}

		state.mx.Lock()
		if state.streams[id] != stream {
			state.mx.Unlock()
			continue
		}
		stream.mx.Lock()
		state.mx.Unlock()
		if stream.receivedPart(seqno) {
			stream.mx.Unlock()
			return nil
		}
		if stream.finishedAt == nil || stream.encoder == nil {
			stream.mx.Unlock()
			continue
		}
		if checkRes != CertCheckResultTrusted {
			stream.trusted = false
		}
		trusted := stream.trusted
		stream.lastMessageAt = now
		stream.addRelayPart(seqno, broadcastFEC, t.BroadcastHash, t.PartDataHash, sourcePeerID)
		stream.addReceivedPart(seqno, t.PartDataHash)
		var relayOps []broadcastFECRelayOp
		if relayCfg.enabled && (trusted || stream.checked) {
			relayOps = stream.relayPartOpsLocked(seqno, relayPeers, relayCfg.localID, true)
		}
		stream.mx.Unlock()

		relayErr := sendBroadcastFECRelayOps(context.Background(), relayCfg.state, relayOps)
		if completed {
			if err = a.sendFECControlMessage(FECCompleted{Hash: t.BroadcastHash}); err != nil {
				return err
			}
		} else if err = a.sendFECControlMessage(FECReceived{Hash: t.BroadcastHash}); err != nil {
			return err
		}
		return relayErr
	}
}
