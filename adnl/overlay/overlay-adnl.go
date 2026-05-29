package overlay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"math"
	"reflect"
	"sync"
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

const fecBroadcastStreamIdleTTL = 40 * time.Second
const fecBroadcastFinishedTTL = 90 * time.Second
const fecBroadcastCleanupInterval = 5 * time.Second
const maxFECBroadcastBudgetEstimate = int64(^uint64(0) >> 1)
const maxFECBroadcastSymbolSize = 1 << 11
const maxFECBroadcastSymbols = 1 << 24

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
	nextSeqno      uint32
	receivedParts  uint64
	mx             sync.Mutex
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
	SourceID    []byte
	SourceKey   ed25519.PublicKey
	Trusted     bool
	OverlayID   []byte
	BroadcastID []byte
	Extra       []byte
	TwoStep     bool
}

type BroadcastPrecheckInfo struct {
	SourceID         []byte
	SourceKey        ed25519.PublicKey
	Trusted          bool
	OverlayID        []byte
	BroadcastID      []byte
	Extra            []byte
	SignatureChecked bool
}

type ADNLOverlayWrapper struct {
	overlayId      []byte
	authorizedKeys map[string]uint32

	maxUnauthSize     uint32
	allowFEC          bool
	trustUnauthorized bool

	customHandler     func(msg *adnl.MessageCustom) error
	queryHandler      func(msg *adnl.MessageQuery) error
	disconnectHandler func(addr string, key ed25519.PublicKey)

	fecState           *BroadcastFECRelayState
	fecRelayState      *BroadcastFECRelayState
	fecRelayPeerSet    BroadcastPeerSet
	fecRelayLocalID    []byte
	fecRelayEnabled    bool
	fecCleanupStop     chan struct{}
	fecCleanupStopOnce sync.Once

	twoStepState   *BroadcastTwoStepState
	twoStepPeerSet BroadcastPeerSet
	twoStepLocalID []byte
	twoStepEnabled bool

	broadcastHandler         func(msg tl.Serializable, trusted bool) error
	broadcastHandlerWithInfo func(msg tl.Serializable, info BroadcastInfo) error
	broadcastPrecheckHandler func(info BroadcastPrecheckInfo) error
	broadcastCheckHandler    func(msg tl.Serializable, info BroadcastInfo) error

	*ADNLWrapper
}

// WithOverlay - creates basic overlay with restrictive broadcast settings
func (a *ADNLWrapper) WithOverlay(id []byte) *ADNLOverlayWrapper {
	return a.CreateOverlayWithSettings(id, 0, true, false)
}

func (a *ADNLWrapper) CreateOverlayWithSettings(id []byte, maxUnauthBroadcastSize uint32,
	allowBroadcastFEC bool, trustUnauthorizedBroadcast bool) *ADNLOverlayWrapper {
	a.mx.Lock()
	defer a.mx.Unlock()

	strId := hex.EncodeToString(id)

	w := a.overlays[strId]
	if w != nil {
		return w
	}
	w = &ADNLOverlayWrapper{
		overlayId:         id,
		ADNLWrapper:       a,
		fecState:          NewBroadcastFECRelayState(),
		allowFEC:          allowBroadcastFEC,
		maxUnauthSize:     maxUnauthBroadcastSize,
		trustUnauthorized: trustUnauthorizedBroadcast,
		fecCleanupStop:    make(chan struct{}),
		twoStepState:      NewBroadcastTwoStepState(),
	}
	a.overlays[strId] = w
	go w.runFECBroadcastCleanup()

	return w
}

func (a *ADNLOverlayWrapper) SetAuthorizedKeys(keysWithMaxLen map[string]uint32) {
	a.mx.Lock()
	defer a.mx.Unlock()

	// reset and copy
	a.authorizedKeys = map[string]uint32{}
	maps.Copy(a.authorizedKeys, keysWithMaxLen)
}

func (a *ADNLOverlayWrapper) SetFECBroadcastLimits(maxActiveStreams int, maxActiveBytes int64) {
	a.activeFECState().SetLimits(maxActiveStreams, maxActiveBytes)
}

func (a *ADNLOverlayWrapper) SetFECDeliveredCacheSize(max int) {
	a.activeFECState().SetDeliveredCacheSize(max)
}

func (a *ADNLOverlayWrapper) FECBroadcastStats() FECBroadcastStats {
	return a.activeFECState().Stats()
}

func (a *ADNLWrapper) UnregisterOverlay(id []byte) {
	a.mx.Lock()
	defer a.mx.Unlock()

	strID := hex.EncodeToString(id)
	if w := a.overlays[strID]; w != nil {
		w.stopFECBroadcastCleanup()
	}
	delete(a.overlays, strID)
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
	a.customHandler = handler
}

func (a *ADNLOverlayWrapper) SetQueryHandler(handler func(msg *adnl.MessageQuery) error) {
	a.queryHandler = handler
}

func (a *ADNLOverlayWrapper) SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey)) {
	a.disconnectHandler = handler
}

func (a *ADNLOverlayWrapper) SetBroadcastHandler(handler func(msg tl.Serializable, trusted bool) error) {
	a.broadcastHandler = handler
}

func (a *ADNLOverlayWrapper) SetBroadcastHandlerWithInfo(handler func(msg tl.Serializable, info BroadcastInfo) error) {
	a.broadcastHandlerWithInfo = handler
}

func (a *ADNLOverlayWrapper) SetBroadcastPrecheckHandler(handler func(info BroadcastPrecheckInfo) error) {
	a.broadcastPrecheckHandler = handler
}

func (a *ADNLOverlayWrapper) SetBroadcastCheckHandler(handler func(msg tl.Serializable, info BroadcastInfo) error) {
	a.broadcastCheckHandler = handler
}

func (a *ADNLOverlayWrapper) EnableBroadcastTwoStep(localID []byte, peerSet BroadcastPeerSet, state *BroadcastTwoStepState) {
	if state == nil {
		state = NewBroadcastTwoStepState()
	}

	a.mx.Lock()
	a.twoStepLocalID = append([]byte(nil), localID...)
	a.twoStepPeerSet = peerSet
	a.twoStepState = state
	a.twoStepEnabled = true
	a.mx.Unlock()
}

func (a *ADNLOverlayWrapper) EnableBroadcastFECRelay(localID []byte, peerSet BroadcastPeerSet, state *BroadcastFECRelayState) {
	if state == nil {
		state = NewBroadcastFECRelayState()
	}

	a.mx.Lock()
	a.fecRelayLocalID = append([]byte(nil), localID...)
	a.fecRelayPeerSet = peerSet
	a.fecRelayState = state
	a.fecRelayEnabled = true
	a.mx.Unlock()
}

func (a *ADNLOverlayWrapper) DisableBroadcastFECRelay() {
	a.mx.Lock()
	a.fecRelayEnabled = false
	a.mx.Unlock()
}

func (a *ADNLOverlayWrapper) DisableBroadcastTwoStep() {
	a.mx.Lock()
	a.twoStepEnabled = false
	a.mx.Unlock()
}

func (a *ADNLOverlayWrapper) SetBroadcastTwoStepLimits(maxActiveStreams int, maxActiveBytes int64) {
	a.mx.RLock()
	state := a.twoStepState
	a.mx.RUnlock()
	if state == nil {
		return
	}
	state.SetLimits(maxActiveStreams, maxActiveBytes)
}

func (a *ADNLOverlayWrapper) SetBroadcastTwoStepDeliveredCacheSize(max int) {
	a.mx.RLock()
	state := a.twoStepState
	a.mx.RUnlock()
	if state == nil {
		return
	}
	state.SetDeliveredCacheSize(max)
}

func (a *ADNLOverlayWrapper) BroadcastTwoStepStats() BroadcastTwoStepStats {
	a.mx.RLock()
	state := a.twoStepState
	a.mx.RUnlock()
	if state == nil {
		return BroadcastTwoStepStats{}
	}
	return state.Stats()
}

func (a *ADNLOverlayWrapper) isBroadcastTwoStepEnabled() bool {
	a.mx.RLock()
	enabled := a.twoStepEnabled
	a.mx.RUnlock()
	return enabled
}

type broadcastFECRelayConfig struct {
	enabled bool
	localID []byte
	peerSet BroadcastPeerSet
	state   *BroadcastFECRelayState
}

func (a *ADNLOverlayWrapper) activeFECState() *BroadcastFECRelayState {
	a.mx.RLock()
	state := a.fecState
	if a.fecRelayEnabled && a.fecRelayState != nil {
		state = a.fecRelayState
	}
	a.mx.RUnlock()
	return state
}

func (a *ADNLOverlayWrapper) broadcastFECRelayConfig() broadcastFECRelayConfig {
	a.mx.RLock()
	state := a.fecRelayState
	cfg := broadcastFECRelayConfig{
		enabled: a.fecRelayEnabled && state != nil && a.fecRelayPeerSet != nil,
		localID: append([]byte(nil), a.fecRelayLocalID...),
		peerSet: a.fecRelayPeerSet,
		state:   state,
	}
	a.mx.RUnlock()
	return cfg
}

func (a *ADNLOverlayWrapper) trackBroadcastFECRelayControl(peerID []byte, control BroadcastFECControl) bool {
	a.mx.RLock()
	enabled := a.fecRelayEnabled
	state := a.fecRelayState
	a.mx.RUnlock()
	if !enabled || state == nil {
		return false
	}
	return state.TrackControlMessage(peerID, control)
}

func (a *ADNLOverlayWrapper) Close() {
	a.ADNLWrapper.UnregisterOverlay(a.overlayId)
}

func (a *ADNLOverlayWrapper) runFECBroadcastCleanup() {
	ticker := time.NewTicker(fecBroadcastCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case now := <-ticker.C:
			state := a.activeFECState()
			state.mx.Lock()
			state.cleanupLocked(now, true)
			state.mx.Unlock()
			a.cleanupTwoStepBroadcasts(now, true)
		case <-a.ADNLWrapper.GetCloserCtx().Done():
			return
		case <-a.fecCleanupStop:
			return
		}
	}
}

func (a *ADNLOverlayWrapper) stopFECBroadcastCleanup() {
	a.fecCleanupStopOnce.Do(func() {
		close(a.fecCleanupStop)
	})
}

func (a *ADNLOverlayWrapper) checkRules(keyId string, dataSize uint32, isFEC bool) CertCheckResult {
	if dataSize == 0 {
		return CertCheckResultForbidden
	}

	a.mx.RLock()
	maxSz, authorized := a.authorizedKeys[keyId]
	a.mx.RUnlock()
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
	return symbolsCount*2 + 4
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

func (a *ADNLOverlayWrapper) cleanupBroadcastStreams(now time.Time) {
	state := a.activeFECState()
	state.mx.Lock()
	state.cleanupLocked(now, false)
	state.mx.Unlock()
}

func estimateFECBroadcastBudgetBytes(fec rldp.FECRaptorQ, partSize int) int64 {
	payloadBytes := int64(fec.DataSize)
	symbolBytes := multiplyFECBroadcastBudgetEstimate(fec.SymbolsCount, fec.SymbolSize)
	if symbolBytes > payloadBytes {
		payloadBytes = symbolBytes
	}

	hashBytes := multiplyFECBroadcastBudgetEstimate(fec.SymbolsCount, 64)
	budgetBytes := addFECBroadcastBudgetEstimate(payloadBytes, hashBytes, int64(partSize), 4096)
	if budgetBytes < 1 {
		return 1
	}
	return budgetBytes
}

func multiplyFECBroadcastBudgetEstimate(a uint32, b uint32) int64 {
	if a == 0 || b == 0 {
		return 0
	}
	if uint64(a) > uint64(maxFECBroadcastBudgetEstimate)/uint64(b) {
		return maxFECBroadcastBudgetEstimate
	}
	return int64(uint64(a) * uint64(b))
}

func addFECBroadcastBudgetEstimate(values ...int64) int64 {
	var total int64
	for _, value := range values {
		if value > maxFECBroadcastBudgetEstimate-total {
			return maxFECBroadcastBudgetEstimate
		}
		total += value
	}
	return total
}

func (a *ADNLOverlayWrapper) sendFECControlMessage(msg tl.Serializable) error {
	if err := a.ADNL.SendCustomMessage(context.Background(), msg); err != nil {
		return fmt.Errorf("failed to send overlay fec control message: %w", err)
	}
	return nil
}

func (a *ADNLOverlayWrapper) relaySimpleBroadcast(ctx context.Context, sourcePeerID []byte, msg *Broadcast) error {
	relayCfg := a.broadcastFECRelayConfig()
	if !relayCfg.enabled {
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
	id := string(broadcastHash)

	state := a.activeFECState()
	state.mx.Lock()
	if state.isSimpleDeliveredLocked(id) {
		state.mx.Unlock()
		return nil
	}
	state.mx.Unlock()

	checkRes, srcID, err := a.checkBroadcastSource(t.Source, t.Certificate, uint32(len(t.Data)), false)
	if err != nil {
		return err
	}

	trusted := checkRes == CertCheckResultTrusted
	precheck := BroadcastPrecheckInfo{
		SourceID:    srcID,
		SourceKey:   sourceKey.Key,
		Trusted:     trusted,
		OverlayID:   a.overlayId,
		BroadcastID: broadcastHash,
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

	state.mx.Lock()
	if state.isSimpleDeliveredLocked(id) {
		state.mx.Unlock()
		return nil
	}
	state.registerSimpleDeliveredLocked(id)
	state.mx.Unlock()

	var res any
	_, err = tl.Parse(&res, t.Data, true)
	if err != nil {
		return fmt.Errorf("failed to parse broadcast message: %w", err)
	}

	info := BroadcastInfo{
		SourceID:    append([]byte(nil), srcID...),
		SourceKey:   append(ed25519.PublicKey(nil), sourceKey.Key...),
		Trusted:     trusted,
		OverlayID:   append([]byte(nil), a.overlayId...),
		BroadcastID: append([]byte(nil), broadcastHash...),
	}
	if !trusted && a.broadcastCheckHandler != nil {
		if err = a.broadcastCheckHandler(res, info); err != nil {
			return fmt.Errorf("failed to check broadcast message: %w", err)
		}
	}

	relayErr := a.relaySimpleBroadcast(context.Background(), sourcePeerID, t)
	if bHandler := a.broadcastHandlerWithInfo; bHandler != nil {
		if err = bHandler(res, info); err != nil {
			return fmt.Errorf("failed to process broadcast message: %w", err)
		}
	} else if bHandler := a.broadcastHandler; bHandler != nil {
		if err = bHandler(res, trusted); err != nil {
			return fmt.Errorf("failed to process broadcast message: %w", err)
		}
	}
	return relayErr
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
	if t.Seqno >= broadcastFECSeqnoLimit(fec.SymbolsCount) {
		return fmt.Errorf("too big seqno")
	}

	broadcastHash, err := t.CalcID()
	if err != nil {
		return fmt.Errorf("failed to calc broadcast hash: %w", err)
	}
	id := string(broadcastHash)
	state := a.activeFECState()

	state.mx.Lock()
	state.cleanupLocked(tm, false)
	if state.isDeliveredLocked(id, tm) {
		state.deliveredCacheHits++
		state.mx.Unlock()
		return a.sendFECControlMessage(FECCompleted{Hash: broadcastHash})
	}
	stream := state.streams[id]
	state.mx.Unlock()

	partDataHash := calcBroadcastFECPartDataHash(t.Data)
	if stream != nil {
		stream.mx.Lock()
		if stream.flags&BroadcastFlagAnySender == 0 && !bytes.Equal(stream.source, sourceKey.Key) {
			stream.mx.Unlock()
			return fmt.Errorf("malformed source")
		}
		if t.Date != stream.date {
			stream.mx.Unlock()
			return fmt.Errorf("broadcast date mismatch")
		}
		if t.Seqno >= broadcastFECSeqnoLimit(stream.fec.SymbolsCount) {
			stream.mx.Unlock()
			return fmt.Errorf("too big seqno")
		}

		finished := stream.finishedAt != nil
		completed := stream.completedAt != nil
		if stream.receivedPart(t.Seqno) {
			if stream.partHashes != nil {
				if existing, ok := stream.partHashes[t.Seqno]; ok && !bytes.Equal(existing[:], partDataHash) {
					stream.mx.Unlock()
					return fmt.Errorf("conflicting data for seqno %d", t.Seqno)
				}
			}
			stream.mx.Unlock()
			if finished {
				if completed {
					return a.sendFECControlMessage(FECCompleted{Hash: broadcastHash})
				}
				return a.sendFECControlMessage(FECReceived{Hash: broadcastHash})
			}
			return nil
		}
		stream.mx.Unlock()
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

	if stream == nil {
		budgetBytes := estimateFECBroadcastBudgetBytes(fec, len(t.Data))
		reserved := false
		delivered := false

		state.mx.Lock()
		if state.isDeliveredLocked(id, tm) {
			delivered = true
		} else if existing := state.streams[id]; existing != nil {
			stream = existing
		} else {
			reserved = state.reserveLocked(tm, budgetBytes)
		}
		state.mx.Unlock()

		if delivered {
			state.mx.Lock()
			state.deliveredCacheHits++
			state.mx.Unlock()
			return a.sendFECControlMessage(FECCompleted{Hash: broadcastHash})
		}
		if stream == nil && !reserved {
			return fmt.Errorf("fec broadcast receiver budget exceeded")
		}

		if stream == nil {
			dec, err := raptorq.NewRaptorQ(uint32(fec.SymbolSize)).CreateDecoder(uint32(fec.DataSize))
			if err != nil {
				state.mx.Lock()
				state.releaseLocked(budgetBytes)
				state.mx.Unlock()
				return fmt.Errorf("failed to init raptorq decoder: %w", err)
			}

			newStream := &fecBroadcastStream{
				decoder:        dec,
				partHashes:     map[uint32][32]byte{},
				parts:          map[uint32]broadcastFECRelayPart{},
				receivedPeers:  map[string]struct{}{},
				completedPeers: map[string]struct{}{},
				budgetBytes:    budgetBytes,
				lastMessageAt:  tm,
				sourceObj:      t.Source,
				certificate:    t.Certificate,
				source:         sourceKey.Key,
				sourceID:       append([]byte(nil), srcID...),
				dataHash:       append([]byte(nil), t.DataHash...),
				fec:            fec,
				date:           t.Date,
				flags:          t.Flags,
				trusted:        checkRes == CertCheckResultTrusted,
			}

			state.mx.Lock()
			if state.isDeliveredLocked(id, tm) {
				state.releaseLocked(budgetBytes)
				delivered = true
			} else if state.streams[id] != nil {
				state.releaseLocked(budgetBytes)
				stream = state.streams[id]
			} else {
				stream = newStream
				state.streams[id] = stream
			}
			state.mx.Unlock()

			if delivered {
				state.mx.Lock()
				state.deliveredCacheHits++
				state.mx.Unlock()
				return a.sendFECControlMessage(FECCompleted{Hash: broadcastHash})
			}
		}
	}

	var (
		decodedRes   any
		decoded      bool
		ackReceived  bool
		ackCompleted bool
		relayOps     []broadcastFECRelayOp
	)
	trusted := false
	relayCfg := a.broadcastFECRelayConfig()
	relayPeers := []BroadcastPeer(nil)
	sourcePeerID := a.GetID()
	if relayCfg.enabled {
		relayPeers = relayCfg.peerSet.Peers()
	}

	stream.mx.Lock()
	if stream.flags&BroadcastFlagAnySender == 0 && !bytes.Equal(stream.source, sourceKey.Key) {
		stream.mx.Unlock()
		return fmt.Errorf("malformed source")
	}
	if t.Date != stream.date {
		stream.mx.Unlock()
		return fmt.Errorf("broadcast date mismatch")
	}
	if t.Seqno >= broadcastFECSeqnoLimit(stream.fec.SymbolsCount) {
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
			if completed {
				return a.sendFECControlMessage(FECCompleted{Hash: broadcastHash})
			}
			return a.sendFECControlMessage(FECReceived{Hash: broadcastHash})
		}
		return nil
	}

	if !finished {
		if stream.partHashes == nil {
			stream.partHashes = map[uint32][32]byte{}
		}
		var partDataHashValue [32]byte
		copy(partDataHashValue[:], partDataHash)
		stream.partHashes[t.Seqno] = partDataHashValue
	}
	stream.addRelayPart(t.Seqno, t, broadcastHash, partDataHash)

	if !finished {
		canTryDecode, err := stream.decoder.AddSymbol(t.Seqno, t.Data)
		if err != nil {
			delete(stream.partHashes, t.Seqno)
			delete(stream.parts, t.Seqno)
			stream.mx.Unlock()
			return fmt.Errorf("failed to add raptorq symbol %d: %w", t.Seqno, err)
		}

		stream.addReceivedPart(t.Seqno)

		if canTryDecode {
			decodedNow, data, err := stream.decoder.Decode()
			if err != nil {
				stream.mx.Unlock()
				return fmt.Errorf("failed to decode raptorq packet: %w", err)
			}

			// it may not be decoded due to unsolvable math system, it means we need more symbols
			if decodedNow {
				dHash := sha256.Sum256(data)
				if !bytes.Equal(dHash[:], t.DataHash) {
					stream.mx.Unlock()
					return fmt.Errorf("incorrect data hash")
				}

				var res any
				_, err = tl.Parse(&res, data, true)
				if err != nil {
					stream.mx.Unlock()
					return fmt.Errorf("failed to parse decoded broadcast message: %w", err)
				}

				stream.finishedAt = &tm
				stream.decoder = nil
				if relayCfg.enabled {
					encoder, err := raptorq.NewRaptorQ(uint32(stream.fec.SymbolSize)).CreateEncoder(data)
					if err != nil {
						stream.mx.Unlock()
						return fmt.Errorf("failed to init raptorq encoder: %w", err)
					}
					stream.encoder = encoder
				}
				stream.partHashes = nil
				decodedRes = res
				ackReceived = true
				decoded = true
			}
		}
	} else {
		stream.addReceivedPart(t.Seqno)
	}

	if relayCfg.enabled && (checkRes == CertCheckResultTrusted || stream.checked) {
		relayOps = stream.relayPartOpsLocked(t.Seqno, relayPeers, relayCfg.localID, sourcePeerID, true)
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

	var ackErr error
	if ackReceived {
		if err = a.sendFECControlMessage(FECReceived{Hash: broadcastHash}); err != nil {
			ackErr = err
		}
	}

	if decoded {
		info := BroadcastInfo{
			SourceID:    append([]byte(nil), stream.sourceID...),
			SourceKey:   append(ed25519.PublicKey(nil), stream.source...),
			Trusted:     trusted,
			OverlayID:   append([]byte(nil), a.overlayId...),
			BroadcastID: append([]byte(nil), broadcastHash...),
		}
		if !trusted && a.broadcastCheckHandler != nil {
			if err = a.broadcastCheckHandler(decodedRes, info); err != nil {
				state.mx.Lock()
				state.removeStreamLocked(id, stream, false, time.Now())
				state.mx.Unlock()
				return fmt.Errorf("failed to check broadcast message: %w", err)
			}
		}

		if bHandler := a.broadcastHandlerWithInfo; bHandler != nil {
			if err = bHandler(decodedRes, info); err != nil {
				state.mx.Lock()
				state.removeStreamLocked(id, stream, false, time.Now())
				state.mx.Unlock()
				return fmt.Errorf("failed to process broadcast message: %w", err)
			}
		} else if bHandler := a.broadcastHandler; bHandler != nil {
			if err = bHandler(decodedRes, trusted); err != nil {
				state.mx.Lock()
				state.removeStreamLocked(id, stream, false, time.Now())
				state.mx.Unlock()
				return fmt.Errorf("failed to process broadcast message: %w", err)
			}
		}

		if relayCfg.enabled && !trusted {
			stream.mx.Lock()
			stream.checked = true
			drainOps := stream.drainRelayPartOpsLocked(relayPeers, relayCfg.localID, sourcePeerID)
			stream.mx.Unlock()
			if err = sendBroadcastFECRelayOps(context.Background(), relayCfg.state, drainOps); err != nil && relayErr == nil {
				relayErr = err
			}
		}

		ackCompleted = true
		completedNow := false
		stream.mx.Lock()
		if stream.completedAt == nil {
			stream.completedAt = &tm
			completedNow = true
		}
		stream.mx.Unlock()
		state.mx.Lock()
		if completedNow {
			state.completed++
		}
		if relayCfg.enabled {
			if stream.encoder == nil {
				state.reduceStreamBudgetLocked(id, stream, broadcastFECRetainedBudgetBytes)
			}
		} else {
			state.removeStreamLocked(id, stream, true, time.Now())
		}
		state.mx.Unlock()
	}

	if ackCompleted {
		if err = a.sendFECControlMessage(FECCompleted{Hash: broadcastHash}); err != nil {
			return err
		}
	}
	if ackErr != nil {
		return ackErr
	}
	return relayErr
}

func (a *ADNLOverlayWrapper) processFECBroadcastShort(t *BroadcastFECShort) error {
	if t.Seqno < 0 {
		return fmt.Errorf("invalid seqno")
	}

	id := string(t.BroadcastHash)
	now := time.Now()
	state := a.activeFECState()

	state.mx.Lock()
	state.cleanupLocked(now, false)
	if state.isDeliveredLocked(id, now) {
		state.deliveredCacheHits++
		state.mx.Unlock()
		return a.sendFECControlMessage(FECCompleted{Hash: t.BroadcastHash})
	}
	stream := state.streams[id]
	state.mx.Unlock()
	if stream == nil {
		return fmt.Errorf("short part of unknown broadcast")
	}

	sourceKey, ok := t.Source.(keys.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("invalid signer key format")
	}

	var (
		date         uint32
		completed    bool
		trusted      bool
		relayOps     []broadcastFECRelayOp
		partData     []byte
		broadcastFEC *BroadcastFEC
	)
	seqno := uint32(t.Seqno)
	relayCfg := a.broadcastFECRelayConfig()
	relayPeers := []BroadcastPeer(nil)
	sourcePeerID := a.GetID()
	if relayCfg.enabled {
		relayPeers = relayCfg.peerSet.Peers()
	}

	stream.mx.Lock()
	if stream.flags&BroadcastFlagAnySender == 0 && !bytes.Equal(stream.source, sourceKey.Key) {
		stream.mx.Unlock()
		return fmt.Errorf("malformed source")
	}

	if seqno >= broadcastFECSeqnoLimit(stream.fec.SymbolsCount) {
		stream.mx.Unlock()
		return fmt.Errorf("too big seqno")
	}
	date = stream.date
	stream.lastMessageAt = now
	completed = stream.completedAt != nil

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
	partData = append([]byte(nil), stream.encoder.GenSymbol(seqno)...)
	stream.mx.Unlock()

	if err := checkBroadcastFECDate(date, now); err != nil {
		return err
	}
	if !bytes.Equal(calcBroadcastFECPartDataHash(partData), t.PartDataHash) {
		return fmt.Errorf("wrong part data hash")
	}
	checkRes, _, err := a.checkBroadcastSource(t.Source, t.Certificate, stream.fec.DataSize, true)
	if err != nil {
		return err
	}
	if err := t.VerifySignature(date); err != nil {
		return err
	}

	broadcastFEC = &BroadcastFEC{
		Source:      t.Source,
		Certificate: t.Certificate,
		DataHash:    stream.dataHash,
		DataSize:    stream.fec.DataSize,
		Flags:       stream.flags,
		Data:        partData,
		Seqno:       seqno,
		FEC:         stream.fec,
		Date:        date,
		Signature:   t.Signature,
	}

	stream.mx.Lock()
	if stream.receivedPart(seqno) {
		stream.mx.Unlock()
		return nil
	}
	if checkRes != CertCheckResultTrusted {
		stream.trusted = false
	}
	trusted = stream.trusted
	stream.addRelayPart(seqno, broadcastFEC, t.BroadcastHash, t.PartDataHash)
	stream.addReceivedPart(seqno)
	if relayCfg.enabled && (trusted || stream.checked) {
		relayOps = stream.relayPartOpsLocked(seqno, relayPeers, relayCfg.localID, sourcePeerID, true)
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
