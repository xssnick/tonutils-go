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
	delivered         bool
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
	streams       map[broadcastTwoStepIDKey]*broadcastTwoStepStream
	delivered     map[broadcastTwoStepIDKey]*list.Element
	deliveredList *list.List

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
		delivered:        map[broadcastTwoStepIDKey]*list.Element{},
		deliveredList:    list.New(),
		maxActiveStreams: DefaultTwoStepBroadcastMaxActiveStreams,
		maxActiveBytes:   DefaultTwoStepBroadcastMaxActiveBytes,
		deliveredMax:     DefaultTwoStepDeliveredCacheSize,
	}
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
		stale := stream.lastMessageAt.Add(broadcastTwoStepStreamTTL).Before(now)
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
	a.mx.RLock()
	state := a.twoStepState
	a.mx.RUnlock()
	if state == nil {
		return
	}

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
	symbolBytes := multiplyFECBroadcastBudgetEstimate(symbols, partSize)
	hashBytes := multiplyFECBroadcastBudgetEstimate(symbols, 16)
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
	if a.broadcastPrecheckHandler == nil {
		return nil
	}
	return a.broadcastPrecheckHandler(cloneBroadcastPrecheckInfo(info))
}

func (a *ADNLOverlayWrapper) deliverTwoStepBroadcast(data []byte, info BroadcastInfo) error {
	var res any
	_, err := tl.Parse(&res, data, true)
	if err != nil {
		return fmt.Errorf("failed to parse two-step broadcast message: %w", err)
	}

	if !info.Trusted && a.broadcastCheckHandler != nil {
		if err = a.broadcastCheckHandler(res, cloneBroadcastInfo(info)); err != nil {
			return fmt.Errorf("failed to check two-step broadcast message: %w", err)
		}
	}

	if bHandler := a.broadcastHandlerWithInfo; bHandler != nil {
		if err = bHandler(res, cloneBroadcastInfo(info)); err != nil {
			return fmt.Errorf("failed to process broadcast message: %w", err)
		}
	} else if bHandler := a.broadcastHandler; bHandler != nil {
		if err = bHandler(res, info.Trusted); err != nil {
			return fmt.Errorf("failed to process broadcast message: %w", err)
		}
	}
	return nil
}

func cloneBroadcastInfo(info BroadcastInfo) BroadcastInfo {
	return BroadcastInfo{
		SourceID:    append([]byte(nil), info.SourceID...),
		SourceKey:   append(ed25519.PublicKey(nil), info.SourceKey...),
		Trusted:     info.Trusted,
		OverlayID:   append([]byte(nil), info.OverlayID...),
		BroadcastID: append([]byte(nil), info.BroadcastID...),
		Extra:       append([]byte(nil), info.Extra...),
		TwoStep:     info.TwoStep,
	}
}

func cloneBroadcastPrecheckInfo(info BroadcastPrecheckInfo) BroadcastPrecheckInfo {
	return BroadcastPrecheckInfo{
		SourceID:         append([]byte(nil), info.SourceID...),
		SourceKey:        append(ed25519.PublicKey(nil), info.SourceKey...),
		Trusted:          info.Trusted,
		OverlayID:        append([]byte(nil), info.OverlayID...),
		BroadcastID:      append([]byte(nil), info.BroadcastID...),
		Extra:            append([]byte(nil), info.Extra...),
		SignatureChecked: info.SignatureChecked,
	}
}

func (a *ADNLOverlayWrapper) twoStepPrecheckInfo(sourceID []byte, sourceKey ed25519.PublicKey, trusted bool, broadcastID, extra []byte, signatureChecked bool) BroadcastPrecheckInfo {
	return BroadcastPrecheckInfo{
		SourceID:         sourceID,
		SourceKey:        sourceKey,
		Trusted:          trusted,
		OverlayID:        a.overlayId,
		BroadcastID:      broadcastID,
		Extra:            extra,
		SignatureChecked: signatureChecked,
	}
}

func (a *ADNLOverlayWrapper) twoStepBroadcastInfo(sourceID []byte, sourceKey ed25519.PublicKey, trusted bool, broadcastID, extra []byte) BroadcastInfo {
	return BroadcastInfo{
		SourceID:    sourceID,
		SourceKey:   sourceKey,
		Trusted:     trusted,
		OverlayID:   a.overlayId,
		BroadcastID: broadcastID,
		Extra:       extra,
		TwoStep:     true,
	}
}

func (a *ADNLOverlayWrapper) rebroadcastTwoStep(ctx context.Context, sourceADNL []byte, msg tl.Serializable) error {
	a.mx.RLock()
	peerSet := a.twoStepPeerSet
	localID := append([]byte(nil), a.twoStepLocalID...)
	a.mx.RUnlock()
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

	a.mx.RLock()
	state := a.twoStepState
	a.mx.RUnlock()
	if state != nil {
		state.mx.Lock()
		state.cleanupLocked(now, false)
		if state.isDeliveredLocked(id) {
			state.deliveredCacheHits++
			state.mx.Unlock()
			return nil
		}
		state.mx.Unlock()
	}

	checkRes, err := a.checkBroadcastSourceRules(sourceID, t.Certificate, uint32(len(t.Data)), true)
	if err != nil {
		return err
	}

	trusted := checkRes == CertCheckResultTrusted
	precheck := a.twoStepPrecheckInfo(sourceID, sourceKey.Key, trusted, broadcastID, t.Extra, false)
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

	if state != nil {
		state.mx.Lock()
		if state.isDeliveredLocked(id) {
			state.deliveredCacheHits++
			state.mx.Unlock()
			return nil
		}
		state.registerDeliveredLocked(id)
		state.completed++
		state.mx.Unlock()
	}

	var rebroadcastErr error
	if bytes.Equal(srcPeerID, t.SourceADNL) {
		rebroadcastErr = a.rebroadcastTwoStep(context.Background(), t.SourceADNL, t)
	}

	info := a.twoStepBroadcastInfo(sourceID, sourceKey.Key, trusted, broadcastID, t.Extra)
	if err = a.deliverTwoStepBroadcast(t.Data, info); err != nil {
		return err
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

	a.mx.RLock()
	state := a.twoStepState
	a.mx.RUnlock()
	if state == nil {
		return fmt.Errorf("two-step state is nil")
	}

	state.mx.Lock()
	state.cleanupLocked(now, false)
	if state.isDeliveredLocked(id) {
		state.deliveredCacheHits++
		state.mx.Unlock()
		return nil
	}
	stream := state.streams[id]
	state.mx.Unlock()

	var trusted bool
	if stream == nil {
		checkRes, checkErr := a.checkBroadcastSourceRules(sourceID, t.Certificate, t.DataSize, true)
		if checkErr != nil {
			return checkErr
		}

		trusted = checkRes == CertCheckResultTrusted
		precheck := a.twoStepPrecheckInfo(sourceID, sourceKey.Key, trusted, broadcastID, t.Extra, false)
		if err = a.runBroadcastPrecheck(precheck); err != nil {
			return fmt.Errorf("two-step broadcast precheck failed: %w", err)
		}
	}

	if err = verifyBroadcastTwoStepFECSignature(t.Source, broadcastID, t.Seqno, t.Part, t.Signature); err != nil {
		return err
	}

	if stream == nil {
		precheck := a.twoStepPrecheckInfo(sourceID, sourceKey.Key, trusted, broadcastID, t.Extra, true)
		if err = a.runBroadcastPrecheck(precheck); err != nil {
			return fmt.Errorf("two-step broadcast precheck failed: %w", err)
		}
		if err = checkBroadcastTwoStepDate(t.Date, time.Now()); err != nil {
			return err
		}
	}

	state.mx.Lock()
	if state.isDeliveredLocked(id) {
		state.deliveredCacheHits++
		state.mx.Unlock()
		return nil
	}
	stream = state.streams[id]
	if stream == nil {
		budgetBytes := estimateTwoStepBroadcastBudgetBytes(t.DataSize, partSize)
		if !state.reserveLocked(now, budgetBytes) {
			state.mx.Unlock()
			return fmt.Errorf("two-step broadcast receiver budget exceeded")
		}

		decoder, decErr := raptorq.NewRaptorQ(partSize).CreateDecoder(t.DataSize)
		if decErr != nil {
			state.releaseLocked(budgetBytes)
			state.mx.Unlock()
			return fmt.Errorf("failed to init raptorq decoder: %w", decErr)
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
			sourceKey:     append(ed25519.PublicKey(nil), sourceKey.Key...),
			trusted:       trusted,
			extra:         append([]byte(nil), t.Extra...),
			lastMessageAt: now,
		}
		state.streams[id] = stream
	}
	state.mx.Unlock()

	var (
		decodedData    []byte
		decoded        bool
		rebroadcastNow bool
		deliverTrusted bool
	)

	stream.mx.Lock()
	if !bytes.Equal(stream.sourceKey, sourceKey.Key) || !bytes.Equal(stream.sourceID, sourceID) {
		stream.mx.Unlock()
		return fmt.Errorf("malformed source")
	}
	if stream.date != t.Date || stream.dataSize != t.DataSize || stream.partSize != partSize || !bytes.Equal(stream.dataHash, t.DataHash) || !bytes.Equal(stream.extra, t.Extra) {
		stream.mx.Unlock()
		return fmt.Errorf("malformed broadcast parameters")
	}
	deliverTrusted = stream.trusted
	if _, ok := stream.seenParts[t.Seqno]; ok {
		stream.mx.Unlock()
		return nil
	}

	stream.seenParts[t.Seqno] = struct{}{}
	stream.lastMessageAt = now
	if bytes.Equal(srcPeerID, t.SourceADNL) && !stream.rebroadcastedPart {
		stream.rebroadcastedPart = true
		rebroadcastNow = true
	}

	if stream.delivered {
		stream.mx.Unlock()
		if rebroadcastNow {
			return a.rebroadcastTwoStep(context.Background(), t.SourceADNL, t)
		}
		return nil
	}

	canTryDecode, err := stream.decoder.AddSymbol(t.Seqno, t.Part)
	if err != nil {
		delete(stream.seenParts, t.Seqno)
		stream.mx.Unlock()
		return fmt.Errorf("failed to add two-step raptorq symbol %d: %w", t.Seqno, err)
	}

	if canTryDecode {
		decodedNow, data, decErr := stream.decoder.Decode()
		if decErr != nil {
			stream.mx.Unlock()
			return fmt.Errorf("failed to decode two-step raptorq packet: %w", decErr)
		}
		if decodedNow {
			dHash := sha256.Sum256(data)
			if !bytes.Equal(dHash[:], t.DataHash) {
				stream.mx.Unlock()
				return fmt.Errorf("broadcast data hash mismatch")
			}

			stream.delivered = true
			stream.decoder = nil
			decodedData = data
			decoded = true
		}
	}
	stream.mx.Unlock()

	var rebroadcastErr error
	if rebroadcastNow {
		rebroadcastErr = a.rebroadcastTwoStep(context.Background(), t.SourceADNL, t)
	}

	if decoded {
		state.mx.Lock()
		state.removeStreamLocked(id, stream, true)
		state.completed++
		state.mx.Unlock()

		info := a.twoStepBroadcastInfo(sourceID, sourceKey.Key, deliverTrusted, broadcastID, t.Extra)
		if err = a.deliverTwoStepBroadcast(decodedData, info); err != nil {
			return err
		}
	}
	return rebroadcastErr
}
