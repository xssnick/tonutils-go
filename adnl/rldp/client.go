package rldp

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl/rldp/roundrobin"
	"math"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xssnick/raptorq"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
)

type ADNL interface {
	RemoteAddr() string
	GetID() []byte
	SetCustomMessageHandler(handler func(msg *adnl.MessageCustom) error)
	SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey))
	GetDisconnectHandler() func(addr string, key ed25519.PublicKey)
	SendCustomMessage(ctx context.Context, req tl.Serializable) error
	GetCloserCtx() context.Context
	Close()
}

var Logger = func(a ...any) {}

// C++ rldp2 peers use a fixed 2,000,000 byte part size and reject
// non-final inbound parts with a different FEC data size.
var PartSize = uint32(2_000_000)

const maxInitialFastSymbols = 32

var MultiFECMode = false // TODO: activate after some versions
var RoundRobinFECLimit = 50 * DefaultSymbolSize

type fecEncoder interface {
	GenSymbol(id uint32) []byte
}

type activeTransferPart struct {
	encoder fecEncoder
	seqno   uint32
	index   uint32

	fec             FEC
	fecSymbolSize   uint32
	fecSymbolsCount uint32

	lastConfirmRecvProcessed  uint32
	lastConfirmSeqnoProcessed uint32
	lastConfirmAt             int64
	lastRecoverAt             int64

	nextRecoverDelay int64
	fastSeqnoTill    uint32
	startedAt        time.Time
	recoveryReady    atomic.Bool

	sendClock *SendClock

	transfer *activeTransfer
}

type activeTransfer struct {
	id        []byte
	data      []byte
	totalSize uint64
	timeoutAt int64

	nextPartIndex uint32
	currentPart   atomic.Pointer[activeTransferPart]
	rldp          *RLDP
	terminal      atomic.Bool

	mx sync.Mutex
}

type activeRequest struct {
	id                 string
	transferID         []byte
	expectedTransferID [32]byte
	deadline           int64
	maxAnswerSize      uint64
	result             chan<- AsyncQueryResult
	done               <-chan struct{}
	answered           bool
}

type RLDP struct {
	adnl  ADNL
	useV2 atomic.Bool

	activateRecoverySender chan bool
	activateRequestCleanup chan struct{}
	activeRequests         map[string]*activeRequest
	activeTransfers        map[string]*activeTransfer
	expectedTransfers      map[[32]byte]*activeRequest

	recvStreams map[[32]byte]*decoderStream

	onQuery   atomic.Pointer[queryHandler]
	onMessage atomic.Pointer[messageHandler]

	maxUnexpectedTransferSize atomic.Uint64

	mx     sync.RWMutex
	closed bool // guarded by mx

	rateLimit *TokenBucket
	rateCtrl  *BBRv2Controller

	lastReport time.Time
	stats      *clientStats
}

type queryHandler func(transferId []byte, query *Query) error
type messageHandler func(id []byte, data []byte) error

type fecDecoder interface {
	AddSymbol(id uint32, data []byte) (bool, error)
	Decode() (bool, []byte, error)
}

type decoderStreamPart struct {
	decoder fecDecoder

	fecDataSize     uint32
	fecSymbolSize   uint32
	fecSymbolsCount uint32

	maxSeqno             uint32
	receivedMask         uint32
	receivedNum          uint32
	receivedFastNum      uint32
	receivedRepairNum    uint32
	receivedNumConfirmed uint32
	receivedBytes        uint64

	lastConfirmAt time.Time
	startedAt     time.Time
}

type decoderStream struct {
	finishedAt    *time.Time
	lastMessageAt time.Time
	startedAt     time.Time

	// active (not yet decoded) parts, like InboundTransfer in reference C++ rldp2;
	// parts are created strictly in order, but decoded in any order
	activeParts    map[uint32]*decoderStreamPart
	nextPartIndex  uint32
	partsOffset    uint64 // sum of fec data sizes of all created parts
	lastCompleteAt time.Time

	/// messages chan *MessagePart
	msgBuf *Queue

	totalSize uint64

	dataParts [][]byte // decoded data of each part, by part index
	partsSize uint64
	retired   bool // guarded by mx

	pending    atomic.Bool
	processing atomic.Bool
	mx         sync.Mutex
}

var MaxUnexpectedTransferSize uint64 = 64 << 10 // 64 KB
var MaxFECDataSize uint32 = 2 << 20             // 2 MB
var DefaultSymbolSize uint32 = 768
var MaxSymbolSize uint32 = 1 << 11 // same limit as in reference C++ fec validation

// reference C++ rldp2 InboundTransfer decodes at most 20 parts of a transfer concurrently
const maxActiveStreamParts = 20

const (
	_recvStreamIdleTimeout     = 30 * time.Second
	_recvStreamFinishedTimeout = 15 * time.Second
)

var streamDrainEmptyHook func()

const (
	recvStreamCleanupInterval = time.Second
	requestCleanupInterval    = time.Millisecond
)

const _MTU = 1 << 37

var MinRateBytesSec = int64(256 << 10)
var InitialRateBytesSec = int64(1 << 20)
var MaxRateBytesSec = int64(512 << 20)

func NewClient(a ADNL) *RLDP {
	now := time.Now()
	r := &RLDP{
		adnl:                   a,
		activeRequests:         map[string]*activeRequest{},
		activeTransfers:        map[string]*activeTransfer{},
		recvStreams:            map[[32]byte]*decoderStream{},
		expectedTransfers:      map[[32]byte]*activeRequest{},
		activateRecoverySender: make(chan bool, 1),
		activateRequestCleanup: make(chan struct{}, 1),
		rateLimit:              NewTokenBucket(InitialRateBytesSec, a.RemoteAddr()),
		stats:                  &clientStats{createdAt: now.UnixNano()},
	}

	r.rateCtrl = NewBBRv2Controller(r.rateLimit, BBRv2Options{
		Name:               r.adnl.RemoteAddr(),
		MinRate:            MinRateBytesSec,
		InitialRate:        InitialRateBytesSec,
		MaxRate:            MaxRateBytesSec,
		HighLoss:           0.20, // high because of compatibility with old clients, lower values may highly degrade their speed
		Beta:               0.85,
		DefaultRTTMs:       50,
		BtlBwWindowSec:     10,
		ProbeBwCycleMs:     200,
		ProbeRTTDurationMs: 200,
		MinRTTExpiryMs:     20_000,
		MinSampleMs:        50,
	})

	a.SetCustomMessageHandler(r.handleMessage)
	go r.recoverySender()
	go r.stateCleaner()

	return r
}

func NewClientV2(a ADNL) *RLDP {
	c := NewClient(a)
	c.useV2.Store(true)
	return c
}

func (r *RLDP) GetADNL() ADNL {
	return r.adnl
}

func (r *RLDP) SetOnQuery(handler func(transferId []byte, query *Query) error) {
	if handler == nil {
		r.onQuery.Store(nil)
		return
	}

	h := queryHandler(handler)
	r.onQuery.Store(&h)
}

func (r *RLDP) SetOnMessage(handler func(id []byte, data []byte) error) {
	if handler == nil {
		r.onMessage.Store(nil)
		return
	}

	h := messageHandler(handler)
	r.onMessage.Store(&h)
}

func (r *RLDP) queryHandler() func(transferId []byte, query *Query) error {
	handler := r.onQuery.Load()
	if handler == nil {
		return nil
	}
	return *handler
}

func (r *RLDP) messageHandler() func(id []byte, data []byte) error {
	handler := r.onMessage.Load()
	if handler == nil {
		return nil
	}
	return *handler
}

func (r *RLDP) SetMaxUnexpectedTransferSize(size uint64) {
	r.maxUnexpectedTransferSize.Store(size)
}

func (r *RLDP) unexpectedTransferSizeLimit() uint64 {
	if size := r.maxUnexpectedTransferSize.Load(); size > 0 {
		return size
	}
	return MaxUnexpectedTransferSize
}

// Deprecated: use GetADNL().SetDisconnectHandler
// WARNING: it overrides underlying adnl disconnect handler
func (r *RLDP) SetOnDisconnect(handler func()) {
	r.adnl.SetDisconnectHandler(func(addr string, key ed25519.PublicKey) {
		handler()
	})
}

func (r *RLDP) Close() {
	r.closeState()
	r.adnl.Close()
}

func (r *RLDP) activateRecoveryLoop() {
	select {
	case r.activateRecoverySender <- true:
	default:
	}
}

func (r *RLDP) activateRequestCleanupLoop() {
	select {
	case r.activateRequestCleanup <- struct{}{}:
	default:
	}
}

func isRecvStreamExpired(stream *decoderStream, now time.Time) bool {
	if stream == nil {
		return false
	}

	if stream.lastMessageAt.Add(_recvStreamIdleTimeout).Before(now) {
		return true
	}

	return stream.finishedAt != nil && stream.finishedAt.Add(_recvStreamFinishedTimeout).Before(now)
}

type outboundTransferOutcome uint8

const (
	outboundTransferCompleted outboundTransferOutcome = iota
	outboundTransferTimedOut
	outboundTransferFailed
	outboundTransferCanceled
)

func (r *RLDP) finishOutboundTransfer(transfer *activeTransfer, outcome outboundTransferOutcome) {
	if !transfer.terminal.CompareAndSwap(false, true) {
		return
	}

	switch outcome {
	case outboundTransferCompleted:
		r.stats.outboundTransfersCompleted.Add(1)
	case outboundTransferTimedOut:
		r.stats.outboundTransfersTimedOut.Add(1)
	case outboundTransferFailed:
		r.stats.outboundTransfersFailed.Add(1)
	case outboundTransferCanceled:
		r.stats.outboundTransfersCanceled.Add(1)
	}
}

type inboundStreamOutcome uint8

const (
	inboundStreamExpired inboundStreamOutcome = iota
	inboundStreamCanceled
)

// retireInboundStreamLocked finalizes deferred per-symbol counters. stream.mx must be held.
func (r *RLDP) retireInboundStreamLocked(stream *decoderStream, outcome inboundStreamOutcome) {
	if stream.retired {
		return
	}

	if stream.finishedAt == nil {
		r.accountInboundStreamParts(stream)
		switch outcome {
		case inboundStreamExpired:
			r.stats.inboundTransfersExpired.Add(1)
		case inboundStreamCanceled:
			r.stats.inboundTransfersCanceled.Add(1)
		}
	}

	clear(stream.activeParts)
	stream.retired = true
}

func (r *RLDP) retireInboundStream(stream *decoderStream, outcome inboundStreamOutcome) {
	stream.mx.Lock()
	r.retireInboundStreamLocked(stream, outcome)
	stream.mx.Unlock()
}

func (r *RLDP) tryRetireInboundStream(stream *decoderStream, outcome inboundStreamOutcome) bool {
	if !stream.mx.TryLock() {
		return false
	}

	r.retireInboundStreamLocked(stream, outcome)
	stream.mx.Unlock()
	return true
}

func (r *RLDP) cleanupRecvStreams(now time.Time) {
	r.mx.Lock()
	defer r.mx.Unlock()

	for id, stream := range r.recvStreams {
		if stream.pending.Load() || stream.processing.Load() {
			continue
		}
		if !stream.mx.TryLock() {
			continue
		}
		expired := isRecvStreamExpired(stream, now)
		if expired {
			r.retireInboundStreamLocked(stream, inboundStreamExpired)
		}
		stream.mx.Unlock()

		if expired {
			delete(r.recvStreams, id)
		}
	}
}

func (r *RLDP) accountInboundStreamParts(stream *decoderStream) {
	for _, part := range stream.activeParts {
		r.stats.noteInboundPart(
			uint64(part.receivedNum),
			part.receivedBytes,
			uint64(part.receivedRepairNum),
			stream.lastMessageAt,
		)
	}
}

func setQueryResult(dst tl.Serializable, src any) error {
	target := reflect.ValueOf(dst).Elem()
	if !target.CanSet() {
		return errors.New("result should be settable")
	}

	value := reflect.ValueOf(src)
	if !value.Type().AssignableTo(target.Type()) {
		return fmt.Errorf("unexpected response type %s, want %s", value.Type(), target.Type())
	}

	target.Set(value)
	return nil
}

func (r *RLDP) handleMessagePart(m *MessagePart, isV2 bool) error {
	if len(m.TransferID) != 32 {
		return errors.New("invalid transfer id")
	}

	tm := time.Now()
	id := [32]byte(m.TransferID)
	r.mx.RLock()
	stream := r.recvStreams[id]
	expected := r.expectedTransfers[id]
	r.mx.RUnlock()

	if stream == nil {
		if m.TotalSize > _MTU || m.TotalSize <= 0 {
			return fmt.Errorf("bad rldp packet total size %d", m.TotalSize)
		}

		// unexpected transfers limited to this size, for protection
		maxTransferSize := r.unexpectedTransferSizeLimit()
		if expected != nil {
			maxTransferSize = expected.maxAnswerSize
		}

		if m.TotalSize > maxTransferSize {
			return fmt.Errorf("too big transfer size %d, max allowed %d", m.TotalSize, maxTransferSize)
		}

		qsz := int(m.FecType.GetSymbolsCount()) + 32
		if qsz > 1024 {
			qsz = 1024
		}

		r.mx.Lock()
		// check again because of possible concurrency
		if r.closed {
			r.mx.Unlock()
			return nil
		} else if r.recvStreams[id] != nil {
			stream = r.recvStreams[id]
		} else if expected != nil && r.expectedTransfers[id] != expected {
			r.mx.Unlock()
			return nil
		} else {
			stream = &decoderStream{
				lastMessageAt: tm,
				startedAt:     tm,
				msgBuf:        NewQueue(qsz),
				activeParts:   map[uint32]*decoderStreamPart{},
				totalSize:     m.TotalSize,
			}

			r.recvStreams[id] = stream
			r.stats.inboundTransfersStarted.Add(1)
		}
		r.mx.Unlock()
	}

	// Keep the stream registered until the message is visible to cleanup.
	r.mx.RLock()
	if r.recvStreams[id] != stream {
		r.mx.RUnlock()
		return nil
	}
	stream.msgBuf.Enqueue(m)
	stream.pending.Store(true)
	r.mx.RUnlock()

	if !stream.processing.CompareAndSwap(false, true) {
		return nil
	}
	stream.mx.Lock()
	defer stream.mx.Unlock()
	if stream.retired {
		stream.pending.Store(false)
		stream.processing.Store(false)
		return nil
	}

	for {
		stream.pending.Store(false)

		for {
			part, ok := stream.msgBuf.Dequeue()
			if !ok {
				break
			}

			if err := r.processStreamMessagePart(stream, part, tm, isV2); err != nil {
				r.stats.inboundProcessingErrors.Add(1)
				Logger("[RLDP] transfer", hex.EncodeToString(part.TransferID), "process msg part:", part.Part, "error:", err.Error())
			}
		}

		if hook := streamDrainEmptyHook; hook != nil {
			hook()
		}

		if stream.pending.Load() {
			continue
		}

		stream.processing.Store(false)
		if !stream.pending.Load() {
			return nil
		}
		if !stream.processing.CompareAndSwap(false, true) {
			return nil
		}
	}
}

func (r *RLDP) handleMessage(msg *adnl.MessageCustom) error {
	isV2 := true
	var messagePart *MessagePart
	switch m := msg.Data.(type) {
	case MessagePartV2:
		// The layouts are identical; keep the conversion typed instead of reboxing msg.Data.
		messagePart = (*MessagePart)(&m)
	case MessagePart:
		messagePart = &m
		isV2 = false
	case CompleteV2:
		msg.Data = Complete(m)
	case ConfirmV2:
	case Confirm:
		msg.Data = ConfirmV2{
			TransferID:    m.TransferID,
			Part:          m.Part,
			MaxSeqno:      m.Seqno,
			ReceivedMask:  0,
			ReceivedCount: 0,
		}
		isV2 = false
	default:
		isV2 = false
	}

	prevUseV2 := r.useV2.Load()
	if isV2 && !prevUseV2 {
		r.useV2.Store(true)
	} else if !isV2 && prevUseV2 {
		r.useV2.Store(false)
	}

	if messagePart != nil {
		return r.handleMessagePart(messagePart, isV2)
	}

	switch m := msg.Data.(type) {
	case Complete: // receiver has fully received transfer part, send new part or close our stream if done
		r.mx.RLock()
		t := r.activeTransfers[string(m.TransferID)]
		r.mx.RUnlock()

		if t != nil {
			t.mx.Lock()
			p := t.getCurrentPart()
			if t.terminal.Load() || p == nil || p.index != m.Part {
				// completion not for current part
				t.mx.Unlock()
				break
			}

			hadRemainingData := len(t.data) > 0
			more, err := t.prepareNextPart()
			t.mx.Unlock()

			if err != nil {
				r.mx.Lock()
				if r.activeTransfers[string(t.id)] == t {
					delete(r.activeTransfers, string(t.id))
				}
				r.mx.Unlock()
				r.finishOutboundTransfer(t, outboundTransferFailed)

				Logger("[RLDP] failed to prepare next part for transfer", hex.EncodeToString(m.TransferID), err.Error())
				break
			}

			if !more {
				r.mx.Lock()
				if r.activeTransfers[string(t.id)] == t {
					delete(r.activeTransfers, string(t.id))
				}
				r.mx.Unlock()
				if hadRemainingData {
					r.finishOutboundTransfer(t, outboundTransferTimedOut)
				} else {
					r.finishOutboundTransfer(t, outboundTransferCompleted)
				}

				break
			}
			Logger("[RLDP] next part", m.Part+1, "prepared for transfer", hex.EncodeToString(m.TransferID), "sending fast symbols")

			if err = r.sendFastSymbols(context.Background(), t); err != nil {
				r.mx.Lock()
				if r.activeTransfers[string(t.id)] == t {
					delete(r.activeTransfers, string(t.id))
				}
				r.mx.Unlock()
				r.finishOutboundTransfer(t, outboundTransferFailed)

				Logger("[RLDP] failed to send fast symbols", hex.EncodeToString(m.TransferID), err.Error())
			}
		}
	case ConfirmV2: // receiver has received some parts
		id := string(m.TransferID)
		r.mx.RLock()
		t := r.activeTransfers[id]
		r.mx.RUnlock()

		if t == nil {
			break
		}

		t.mx.Lock()
		part := t.getCurrentPart()
		if t.terminal.Load() || part == nil || part.index != m.Part {
			t.mx.Unlock()
			break
		}

		confirmationAt := time.Now()
		var confirmedSymbols uint64
		if isV2 {
			prevSeq := atomic.LoadUint32(&part.lastConfirmSeqnoProcessed)
			prevRecv := atomic.LoadUint32(&part.lastConfirmRecvProcessed)

			var seqDelta int64
			if m.MaxSeqno > prevSeq {
				atomic.StoreUint32(&part.lastConfirmSeqnoProcessed, m.MaxSeqno)
				seqDelta = int64(m.MaxSeqno - prevSeq)
			}

			var recvDelta int64
			if m.ReceivedCount > prevRecv {
				atomic.StoreUint32(&part.lastConfirmRecvProcessed, m.ReceivedCount)
				recvDelta = int64(m.ReceivedCount - prevRecv)
				confirmedSymbols = uint64(recvDelta)
			}

			totalDelta := seqDelta
			if totalDelta < recvDelta {
				totalDelta = recvDelta
			}
			if totalDelta > 0 {
				if m.MaxSeqno > 0 {
					if tms, ok := part.sendClock.SentAt(m.MaxSeqno - 1); ok {
						r.rateCtrl.ObserveRTT(confirmationAt.UnixMilli() - tms)
					}
				}

				r.rateCtrl.ObserveDelta(totalDelta*int64(part.fecSymbolSize), recvDelta*int64(part.fecSymbolSize))

				/*loss := 1.0
				if totalDelta > 0 {
					loss = 1.0 - float64(recvDelta)/float64(totalDelta)
					if loss < 0 {
						loss = 0
					}
					if loss > 1 {
						loss = 1
					}
				}

				const alpha = 0.2
				prev := math.Float64frombits(part.lossEWMA.Load())
				if prev == 0 {
					prev = loss
				}
				ew := prev*(1-alpha) + loss*alpha
				part.lossEWMA.Store(math.Float64bits(ew))

				// (3% base + 1.5 * loss), limit 20%
				k := float64(part.fecSymbolsCount)
				overhead := 0.03 + 1.5*ew
				if overhead < 0.01 {
					overhead = 0.01
				}

				if overhead > 0.20 {
					overhead = 0.20
				}

				target := uint32(k + math.Ceil(k*overhead))

				cur := atomic.LoadUint32(&part.fastSeqnoTill)
				if target > cur {
					atomic.StoreUint32(&part.fastSeqnoTill, target)
				}*/

			}
		}

		r.stats.noteOutboundConfirmation(confirmedSymbols, confirmationAt)
		atomic.StoreInt64(&part.lastConfirmAt, confirmationAt.UnixMilli())
		t.mx.Unlock()
	default:
		return fmt.Errorf("unexpected message type %s", reflect.TypeOf(m).String())
	}

	return nil
}

// processStreamMessagePart consumes a single message part of an inbound transfer, stream.mx must be held.
// Parts are created strictly in order, but up to maxActiveStreamParts of them are decoded
// concurrently and may complete in any order, same as InboundTransfer in reference C++ rldp2.
func (r *RLDP) processStreamMessagePart(stream *decoderStream, part *MessagePart, tm time.Time, isV2 bool) error {
	if stream.finishedAt != nil || (part.Part < stream.nextPartIndex && stream.activeParts[part.Part] == nil) {
		if stream.lastCompleteAt.Add(10 * time.Millisecond).Before(tm) { // to not send completions too often
			var complete tl.Serializable = Complete{
				TransferID: part.TransferID,
				Part:       part.Part,
			}

			if isV2 {
				complete = CompleteV2(complete.(Complete))
			}

			// got packet for a finished part, let them know that it is completed, again
			err := r.adnl.SendCustomMessage(context.Background(), complete)
			if err != nil {
				return fmt.Errorf("failed to send rldp complete message: %w", err)
			}
			stream.lastCompleteAt = tm
		}
		return nil
	}

	if part.TotalSize != stream.totalSize {
		return fmt.Errorf("received part with bad total size %d, expected %d", part.TotalSize, stream.totalSize)
	}

	cur := stream.activeParts[part.Part]
	if cur == nil {
		// parts are created strictly in order, symbols of further parts are
		// ignored, sender will retransmit them when its parts window moves
		if part.Part != stream.nextPartIndex || len(stream.activeParts) >= maxActiveStreamParts {
			return nil
		}

		dec, err := createFECDecoder(part.FecType, stream.totalSize-stream.partsOffset)
		if err != nil {
			return fmt.Errorf("failed to create decoder for part %d: %w", part.Part, err)
		}

		cur = &decoderStreamPart{
			decoder:         dec,
			fecDataSize:     part.FecType.GetDataSize(),
			fecSymbolSize:   part.FecType.GetSymbolSize(),
			fecSymbolsCount: part.FecType.GetSymbolsCount(),
			startedAt:       tm,
		}

		stream.activeParts[part.Part] = cur
		stream.dataParts = append(stream.dataParts, nil)
		stream.partsOffset += uint64(cur.fecDataSize)
		stream.nextPartIndex++

		Logger("[ID]", hex.EncodeToString(part.TransferID), "[RLDP] created decoder for part:", part.Part, "data size:", cur.fecDataSize, "symbol size:", cur.fecSymbolSize, "symbols:", cur.fecSymbolsCount)
	}

	processSymbol := true
	if part.Seqno <= cur.maxSeqno {
		offset := cur.maxSeqno - part.Seqno
		if offset < 32 {
			// Only the receive window can prove that an older symbol is a duplicate.
			processSymbol = cur.receivedMask&(uint32(1)<<offset) == 0
		}
	}

	if !processSymbol {
		// Duplicates still refresh activity and may trigger an ACK resend.
		stream.lastMessageAt = tm
		r.sendStreamConfirmation(cur, part, tm, isV2)
		return nil
	}

	canTryDecode, err := cur.decoder.AddSymbol(part.Seqno, part.Data)
	if err != nil {
		return fmt.Errorf("failed to add raptorq symbol %d: %w", part.Seqno, err)
	}

	stream.lastMessageAt = tm
	cur.receivedNum++
	cur.receivedBytes += uint64(len(part.Data))
	if part.Seqno < cur.fecSymbolsCount {
		cur.receivedFastNum++
	} else {
		cur.receivedRepairNum++
	}

	if part.Seqno > cur.maxSeqno {
		diff := part.Seqno - cur.maxSeqno
		if diff >= 32 {
			cur.receivedMask = 0
		} else {
			cur.receivedMask <<= diff
		}
		cur.maxSeqno = part.Seqno
	}

	if offset := cur.maxSeqno - part.Seqno; offset < 32 {
		cur.receivedMask |= uint32(1) << offset
	}

	if canTryDecode {
		tmd := time.Now()
		decoded, data, err := cur.decoder.Decode()
		if err != nil {
			return fmt.Errorf("failed to decode raptorq packet: %w", err)
		}

		// it may not be decoded due to an unsolvable math system, it means we need more symbols
		if decoded {
			Logger("[RLDP] v2:", isV2, "part", part.Part, "part sz", cur.fecDataSize, "decoded on seqno", part.Seqno, "symbols:", cur.fecSymbolsCount, "received:", cur.receivedNum, "fast", cur.receivedFastNum,
				"decode took", time.Since(tmd).String(), "took", tmd.Sub(cur.startedAt).String())

			r.stats.noteInboundPart(
				uint64(cur.receivedNum),
				cur.receivedBytes,
				uint64(cur.receivedRepairNum),
				tmd,
			)
			delete(stream.activeParts, part.Part)
			stream.dataParts[part.Part] = data
			stream.partsSize += uint64(len(data))

			var complete tl.Serializable = Complete{
				TransferID: part.TransferID,
				Part:       part.Part,
			}

			if isV2 {
				complete = CompleteV2(complete.(Complete))
			}
			_ = r.adnl.SendCustomMessage(context.Background(), complete)
			stream.lastCompleteAt = tmd

			if stream.partsSize < stream.totalSize {
				return nil
			}

			stream.finishedAt = &tmd
			r.stats.noteInboundComplete(stream.totalSize, tmd)

			if stream.partsSize > stream.totalSize {
				return fmt.Errorf("received more data than expected, expected %d, got %d", stream.totalSize, stream.partsSize)
			}

			buf := make([]byte, stream.totalSize)
			off := 0
			for _, p := range stream.dataParts {
				off += copy(buf[off:], p)
			}
			stream.dataParts = nil
			stream.partsSize = 0

			var res any
			if _, err = tl.ParseNoCopy(&res, buf, true); err != nil {
				return fmt.Errorf("failed to parse custom message: %w", err)
			}

			Logger("[RLDP] stream finished and parsed, processing transfer data", hex.EncodeToString(part.TransferID))

			switch rVal := res.(type) {
			case Query:
				handler := r.queryHandler()
				if handler != nil {
					transferId := make([]byte, 32)
					copy(transferId, part.TransferID)

					if err = handler(transferId, &rVal); err != nil {
						Logger("failed to handle query: ", err)
					}
				}
			case Answer:
				qid := string(rVal.ID)
				answerTransferID := [32]byte(part.TransferID)

				var queryTransfer *activeTransfer
				r.mx.Lock()
				req := r.activeRequests[qid]
				if req != nil && req.expectedTransferID != answerTransferID {
					r.mx.Unlock()
					return fmt.Errorf("answer transfer id does not match query %s", hex.EncodeToString(rVal.ID))
				}
				if req != nil {
					req.answered = true
					queryTransfer = r.deleteActiveRequestLocked(req)
				}
				r.mx.Unlock()
				if queryTransfer != nil {
					r.finishOutboundTransfer(queryTransfer, outboundTransferCompleted)
				}

				if req != nil {
					queryId := make([]byte, 32)
					copy(queryId, rVal.ID)

					response := AsyncQueryResult{
						QueryID:     queryId,
						ResultBytes: rVal.Data,
					}
					select {
					case req.result <- response:
					default:
						// A slow consumer must not block the shared ADNL receive loop.
						go r.waitQueryResult(req, response)
					}
				}
			case Message:
				handler := r.messageHandler()
				if handler != nil {
					if err = handler(rVal.ID, rVal.Data); err != nil {
						Logger("failed to handle message: ", err)
					}
					break
				}

				Logger("[RLDP] skipping unwanted rldp message of type", reflect.TypeOf(res).String())
			default:
				Logger("[RLDP] skipping unwanted rldp message of type", reflect.TypeOf(res).String())
			}
			return nil
		}

		Logger("[RLDP] part ", part.Part, "decode attempt failure on seqno", part.Seqno, "symbols:", cur.fecSymbolsCount, "decode took", time.Since(tmd).String())
	}

	r.sendStreamConfirmation(cur, part, tm, isV2)
	return nil
}

func (r *RLDP) sendStreamConfirmation(cur *decoderStreamPart, part *MessagePart, tm time.Time, isV2 bool) {
	if cur.receivedNum-cur.receivedNumConfirmed >= 10 ||
		cur.lastConfirmAt.Add(20*time.Millisecond).Before(tm) {
		var confirm tl.Serializable
		if isV2 {
			// RLDP2 ACKs use 1-based seqno, while messagePart.seqno is 0-based.
			confirm = ConfirmV2{
				TransferID:    part.TransferID,
				Part:          part.Part,
				MaxSeqno:      cur.maxSeqno + 1,
				ReceivedMask:  cur.receivedMask,
				ReceivedCount: cur.receivedNum,
			}
		} else {
			confirm = Confirm{
				TransferID: part.TransferID,
				Part:       part.Part,
				Seqno:      cur.maxSeqno,
			}
		}
		// we don't care in case of error, not so critical
		err := r.adnl.SendCustomMessage(context.Background(), confirm)
		r.stats.noteInboundConfirmation(err, tm)
		if err == nil {
			cur.receivedNumConfirmed = cur.receivedNum
			cur.lastConfirmAt = tm
		}
	}
}

// createFECDecoder validates fec parameters of an inbound transfer part the same way
// the reference C++ node does (fec::FecType::create) and initializes a decoder.
// maxDataSize is the space not yet occupied by previous parts of the transfer.
func createFECDecoder(fec FEC, maxDataSize uint64) (fecDecoder, error) {
	dataSize, symbolSize, symbolsCount := fec.GetDataSize(), fec.GetSymbolSize(), fec.GetSymbolsCount()

	if symbolSize == 0 || symbolSize > MaxSymbolSize {
		return nil, fmt.Errorf("invalid fec symbol size %d", symbolSize)
	}

	if dataSize == 0 || dataSize > MaxFECDataSize || uint64(dataSize) > maxDataSize {
		return nil, fmt.Errorf("invalid fec data size %d", dataSize)
	}

	if uint64(dataSize) > uint64(symbolSize)<<24 {
		return nil, fmt.Errorf("too many fec symbols")
	}

	// symbols count must correspond to data and symbol sizes; older tonutils-go
	// senders report ceil+1 when data size is a multiple of symbol size, allow it too
	minSymbols := (uint64(dataSize) + uint64(symbolSize) - 1) / uint64(symbolSize)
	if c := uint64(symbolsCount); c < minSymbols || c > minSymbols+1 {
		return nil, fmt.Errorf("invalid fec symbols count %d, data size %d, symbol size %d", symbolsCount, dataSize, symbolSize)
	}

	switch fec.(type) {
	case FECRaptorQ:
		dec, err := raptorq.NewRaptorQ(symbolSize).CreateDecoder(dataSize)
		if err != nil {
			return nil, fmt.Errorf("failed to init raptorq decoder: %w", err)
		}
		return dec, nil
	case FECRoundRobin:
		dec, err := roundrobin.NewDecoder(symbolSize, dataSize)
		if err != nil {
			return nil, fmt.Errorf("failed to init round robin decoder: %w", err)
		}
		return dec, nil
	default:
		return nil, fmt.Errorf("not supported fec type")
	}
}

func (r *RLDP) GetRateInfo() (left int64, total int64) {
	return r.rateLimit.GetTokensLeft(), r.rateLimit.GetRate()
}

func (r *RLDP) recoverySender() {
	transfersToProcess := make([]*activeTransferPart, 0, 128)
	timedOut := make([]*activeTransfer, 0, 32)
	closerCtx := r.adnl.GetCloserCtx()
	var ticker *time.Ticker
	var tickerC <-chan time.Time
	stopTicker := func() {
		if ticker == nil {
			return
		}

		ticker.Stop()
		ticker = nil
		tickerC = nil
	}
	defer stopTicker()

	// round-robin head for fair recovery
	var rrHead uint32

	active := false
	for {
		select {
		case <-closerCtx.Done():
			r.closeState()
			return
		case <-r.activateRecoverySender:
			active = true
			if ticker == nil {
				ticker = time.NewTicker(1 * time.Millisecond)
				tickerC = ticker.C
			}
		case <-tickerC:
			if !active {
				stopTicker()
				continue
			}

			if r.lastReport.Before(time.Now().Add(-5 * time.Second)) {
				r.lastReport = time.Now()
				r.mx.RLock()
				Logger("[RLDP] recovery sender stats",
					"peer", hex.EncodeToString(r.adnl.GetID()),
					"active transfers", len(r.activeTransfers),
					"active requests", len(r.activeRequests),
					"expected transfers", len(r.expectedTransfers),
					"transfers to process", len(transfersToProcess),
					"timed out", len(timedOut),
				)
				r.mx.RUnlock()
			}

			ms := time.Now().UnixMilli()

			r.mx.RLock()
			for _, transfer := range r.activeTransfers {
				if ms > transfer.timeoutAt {
					timedOut = append(timedOut, transfer)
					continue
				}

				part := transfer.getCurrentPart()
				if part == nil || !part.recoveryReady.Load() {
					// no parts or fast symbols not yet sent
					continue
				}

				if ms-part.lastRecoverAt > part.nextRecoverDelay {
					transfersToProcess = append(transfersToProcess, part)
				}
			}

			if len(r.activeTransfers) == 0 {
				// stop active ticks to not consume resources
				active = false
				stopTicker()
			}
			r.mx.RUnlock()

			if len(timedOut) > 0 {
				r.mx.Lock()
				for _, transfer := range timedOut {
					id := string(transfer.id)
					if r.activeTransfers[id] != transfer {
						continue
					}

					delete(r.activeTransfers, id)
					r.finishOutboundTransfer(transfer, outboundTransferTimedOut)
				}
				r.mx.Unlock()
			}

			isV2 := r.useV2.Load()
			n := len(transfersToProcess)
			if n > 0 {
				sort.Slice(transfersToProcess, func(i, j int) bool {
					// recently confirmed transfers are prioritized
					return atomic.LoadInt64(&transfersToProcess[i].lastConfirmAt) >
						atomic.LoadInt64(&transfersToProcess[j].lastConfirmAt)
				})

				start := int(rrHead % uint32(n))
				drained := false
				lastServedIdx := -1

			sendLoop:
				for i := 0; i < n; i++ {
					idx := (start + i) % n
					part := transfersToProcess[idx]
					ms = time.Now().UnixMilli()

					seqno := atomic.LoadUint32(&part.seqno)

					// can be -1 when unknown
					rtt := r.rateCtrl.CurrentMinRTT()
					if rtt >= 0 && rtt < 8 {
						rtt = 8
					}

					var delay = int64(5)
					if rtt > 0 {
						delay = rtt / 4
					}

					quantum := int64(1)

					if seqno < part.fastSeqnoTill {
						fastDiff := int64(part.fastSeqnoTill - seqno)
						if fastDiff > quantum {
							quantum = fastDiff
						}
					} else if rtt > 0 {
						perFrame := int64(part.fecSymbolsCount / 4)
						if perFrame < 2 {
							perFrame = 2
						}

						prevTm := int64(0)
						if part.lastRecoverAt > 0 {
							prevTm = part.lastRecoverAt - part.startedAt.UnixMilli()
						}

						tmOfFrame := ms - part.startedAt.UnixMilli()
						amt := float64(perFrame) * (float64(tmOfFrame-prevTm) / float64(rtt))
						quantum = int64(amt)
						if quantum > perFrame/2 {
							quantum = perFrame / 2
						}

						// smooth recovery to decrease bursts
						delay = 0
					} else if sc := part.fecSymbolsCount / 100; sc > 1 {
						quantum = int64(sc)
					}

					requested := int(quantum)
					consumed := r.rateLimit.ConsumePackets(requested, int(part.fecSymbolSize))
					if consumed == 0 {
						drained = true
						break
					}

					prevSeqno := seqno
					var sent uint64
					sendFailed := false
					for j := 0; j < consumed; j++ {
						p := MessagePart{
							TransferID: part.transfer.id,
							FecType:    part.fec,
							Part:       part.index,
							TotalSize:  part.transfer.totalSize,
							Seqno:      seqno,
							Data:       part.encoder.GenSymbol(seqno),
						}
						seqno++

						var msgPart tl.Serializable = p
						if isV2 {
							msgPart = MessagePartV2(p)
						}

						part.sendClock.OnSend(p.Seqno, ms)
						if err := r.adnl.SendCustomMessage(closerCtx, msgPart); err != nil {
							Logger("failed to send recovery message part", p.Seqno, err.Error())
							drained = true
							sendFailed = true
							break
						}
						sent++
					}
					r.stats.noteOutboundSymbols(sent, part.fecSymbolSize, time.UnixMilli(ms))
					if sendFailed {
						break sendLoop
					}

					if seqno > prevSeqno {
						part.nextRecoverDelay = delay
						part.lastRecoverAt = ms
						lastServedIdx = idx
						atomic.StoreUint32(&part.seqno, seqno)
					}

					if consumed < requested {
						drained = true
						break
					}
				}

				if lastServedIdx >= 0 {
					rrHead = uint32((lastServedIdx + 1) % n)
				}
				if drained && lastServedIdx < 0 {
					rrHead = uint32(start)
				}
			} else {
				rrHead = 0
			}

			left := r.rateLimit.GetTokensLeft()

			if len(transfersToProcess) == 0 && left > max64(8<<20, left/2) {
				r.rateCtrl.SetAppLimited(true)
			} else {
				r.rateCtrl.SetAppLimited(false)
			}

			for i := range transfersToProcess {
				transfersToProcess[i] = nil
			}
			transfersToProcess = transfersToProcess[:0]

			for i := range timedOut {
				timedOut[i] = nil
			}
			timedOut = timedOut[:0]
		}
	}
}

func (r *RLDP) stateCleaner() {
	streamTicker := time.NewTicker(recvStreamCleanupInterval)
	defer streamTicker.Stop()

	type requestCleanup struct {
		request  *activeRequest
		timedOut bool
	}
	type transferCleanup struct {
		transfer *activeTransfer
		outcome  outboundTransferOutcome
	}
	type streamCleanup struct {
		stream  *decoderStream
		outcome inboundStreamOutcome
	}
	requestsToClean := make([]requestCleanup, 0, 32)
	transfersToClean := make([]transferCleanup, 0, 32)
	streamsToClean := make([]streamCleanup, 0, 32)
	var requestTicker *time.Ticker
	var requestTickerC <-chan time.Time
	stopRequestTicker := func() {
		if requestTicker == nil {
			return
		}

		requestTicker.Stop()
		requestTickerC = nil
	}
	defer stopRequestTicker()

	closerCtx := r.adnl.GetCloserCtx()
	for {
		select {
		case <-closerCtx.Done():
			r.closeState()
			return
		case <-r.activateRequestCleanup:
			if requestTickerC == nil {
				if requestTicker == nil {
					requestTicker = time.NewTicker(requestCleanupInterval)
				} else {
					requestTicker.Reset(requestCleanupInterval)
				}
				requestTickerC = requestTicker.C
			}
		case now := <-requestTickerC:
			ms := now.UnixMilli()

			r.mx.RLock()
			for _, request := range r.activeRequests {
				if request.deadline <= ms {
					requestsToClean = append(requestsToClean, requestCleanup{request: request, timedOut: true})
					continue
				}

				select {
				case <-request.done:
					requestsToClean = append(requestsToClean, requestCleanup{request: request})
				default:
				}
			}
			hasRequests := len(r.activeRequests) > 0
			r.mx.RUnlock()

			if len(requestsToClean) > 0 {
				r.mx.Lock()
				for _, cleanup := range requestsToClean {
					request := cleanup.request
					if r.activeRequests[request.id] != request {
						continue
					}

					if transfer := r.deleteActiveRequestLocked(request); transfer != nil {
						outcome := outboundTransferCanceled
						if cleanup.timedOut {
							outcome = outboundTransferTimedOut
						}
						transfersToClean = append(transfersToClean, transferCleanup{transfer: transfer, outcome: outcome})
					}
					if stream := r.recvStreams[request.expectedTransferID]; stream != nil {
						delete(r.recvStreams, request.expectedTransferID)
						outcome := inboundStreamCanceled
						if cleanup.timedOut {
							outcome = inboundStreamExpired
						}
						streamsToClean = append(streamsToClean, streamCleanup{stream: stream, outcome: outcome})
					}
				}
				hasRequests = len(r.activeRequests) > 0
				r.mx.Unlock()

				for _, cleanup := range transfersToClean {
					r.finishOutboundTransfer(cleanup.transfer, cleanup.outcome)
				}
				for _, cleanup := range streamsToClean {
					r.retireInboundStream(cleanup.stream, cleanup.outcome)
				}

				for i := range requestsToClean {
					requestsToClean[i] = requestCleanup{}
				}
				requestsToClean = requestsToClean[:0]
				for i := range transfersToClean {
					transfersToClean[i] = transferCleanup{}
				}
				transfersToClean = transfersToClean[:0]
				for i := range streamsToClean {
					streamsToClean[i] = streamCleanup{}
				}
				streamsToClean = streamsToClean[:0]
			}

			if !hasRequests {
				stopRequestTicker()
			}
		case <-streamTicker.C:
			r.mx.RLock()
			hasStreams := len(r.recvStreams) > 0
			r.mx.RUnlock()
			if !hasStreams {
				continue
			}

			r.cleanupRecvStreams(time.Now())
		}
	}
}

func (r *RLDP) startTransfer(ctx context.Context, transferId, data []byte, recoverTimeoutAt int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	closerCtx := r.adnl.GetCloserCtx()
	if closerCtx.Err() != nil {
		return adnl.ErrPeerConnClosed
	}

	at := &activeTransfer{
		id:        transferId,
		timeoutAt: recoverTimeoutAt * 1000, // ms
		data:      data,
		totalSize: uint64(len(data)),
		rldp:      r,
	}

	send, err := at.prepareNextPart()
	if err != nil {
		return fmt.Errorf("failed to prepare first part: %w", err)
	}

	if !send {
		// empty transfer, nothing to send
		return nil
	}

	r.mx.Lock()
	if err := ctx.Err(); err != nil {
		r.mx.Unlock()
		return err
	}
	if r.closed || closerCtx.Err() != nil {
		r.mx.Unlock()
		return adnl.ErrPeerConnClosed
	}
	r.activeTransfers[string(at.id)] = at
	r.stats.outboundTransfersStarted.Add(1)
	r.mx.Unlock()

	if err := r.sendFastSymbols(ctx, at); err != nil {
		r.mx.Lock()
		if r.activeTransfers[string(at.id)] == at {
			delete(r.activeTransfers, string(at.id))
		}
		r.mx.Unlock()
		r.finishOutboundTransfer(at, outboundTransferFailed)

		return fmt.Errorf("failed to send fast symbols: %w", err)
	}

	return nil
}

func (t *activeTransfer) getCurrentPart() *activeTransferPart {
	return t.currentPart.Load()
}

func (t *activeTransfer) prepareNextPart() (bool, error) {
	if t.timeoutAt <= time.Now().UnixMilli() {
		return false, nil // fmt.Errorf("transfer timed out")
	}

	if len(t.data) == 0 {
		return false, nil
	}

	partIndex := t.nextPartIndex

	payload := t.data
	if len(payload) > int(PartSize) {
		payload = payload[:PartSize]
	}

	if len(payload) == 0 {
		return false, nil
	}
	remaining := t.data[len(payload):]

	// ceil, reference C++ nodes drop parts with symbols count not matching data and symbol sizes
	cnt := uint32((uint64(len(payload)) + uint64(DefaultSymbolSize) - 1) / uint64(DefaultSymbolSize))

	var err error
	var enc fecEncoder
	var fec FEC

	//goland:noinspection GoBoolExpressions
	if MultiFECMode && len(payload) < int(RoundRobinFECLimit) {
		enc, err = roundrobin.NewEncoder(payload, DefaultSymbolSize)
		if err != nil {
			return false, fmt.Errorf("failed to create rr object encoder: %w", err)
		}

		fec = FECRoundRobin{
			DataSize:     uint32(len(payload)),
			SymbolSize:   DefaultSymbolSize,
			SymbolsCount: cnt,
		}
	} else {
		enc, err = raptorq.NewRaptorQ(DefaultSymbolSize).CreateEncoder(payload)
		if err != nil {
			return false, fmt.Errorf("failed to create raptorq object encoder: %w", err)
		}

		fec = FECRaptorQ{
			DataSize:     uint32(len(payload)),
			SymbolSize:   DefaultSymbolSize,
			SymbolsCount: cnt,
		}
	}

	part := activeTransferPart{
		encoder:          enc,
		seqno:            0,
		index:            partIndex,
		fec:              fec,
		fecSymbolsCount:  fec.GetSymbolsCount(),
		fecSymbolSize:    fec.GetSymbolSize(),
		nextRecoverDelay: 4,
		fastSeqnoTill:    cnt + cnt/33 + 1, // +3%
		transfer:         t,
	}

	pt := uint32(1) << uint32(math.Ceil(math.Log2(float64(part.fecSymbolsCount)*3+50)))
	if pt > 16<<10 {
		pt = 16 << 10
	}
	part.sendClock = NewSendClock(int(pt))

	if len(remaining) == 0 {
		t.data = nil
	} else {
		t.data = remaining
	}

	t.nextPartIndex++
	t.currentPart.Store(&part)
	return true, nil
}

func (r *RLDP) sendFastSymbols(ctx context.Context, transfer *activeTransfer) error {
	part := transfer.getCurrentPart()
	if part == nil {
		return fmt.Errorf("no active parts")
	}

	p := MessagePart{
		TransferID: transfer.id,
		FecType:    part.fec,
		Part:       part.index,
		TotalSize:  transfer.totalSize,
	}

	r.rateCtrl.OnNewSendBurst()

	isV2 := r.useV2.Load()

	seqno := uint32(0)
	batchLimit := int(part.fastSeqnoTill)
	if batchLimit > maxInitialFastSymbols {
		batchLimit = maxInitialFastSymbols
	}
	batch := r.rateLimit.ConsumePackets(batchLimit, int(part.fecSymbolSize))
	now := time.Now().UnixMilli()
	var sent uint64

	for i := 0; i < batch; i++ {
		currentSeqno := seqno
		p.Seqno = currentSeqno
		p.Data = part.encoder.GenSymbol(currentSeqno)

		var msgPart tl.Serializable = p
		if isV2 {
			msgPart = MessagePartV2(p)
		}

		part.sendClock.OnSend(currentSeqno, now)
		if err := r.adnl.SendCustomMessage(ctx, msgPart); err != nil {
			r.stats.noteOutboundSymbols(sent, part.fecSymbolSize, time.Now())
			return fmt.Errorf("failed to send message part %d: %w", currentSeqno, err)
		}

		seqno++
		sent++
	}

	atomic.StoreUint32(&part.seqno, seqno)
	part.recoveryReady.Store(true)
	part.startedAt = time.Now()
	r.stats.noteOutboundSymbols(sent, part.fecSymbolSize, part.startedAt)

	r.activateRecoveryLoop()

	return nil
}

func (r *RLDP) DoQuery(ctx context.Context, maxAnswerSize uint64, query, result tl.Serializable) error {
	qid := make([]byte, 32)
	_, err := rand.Read(qid)
	if err != nil {
		return err
	}

	res := make(chan AsyncQueryResult, 1)

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
	}

	if err = r.DoQueryAsync(ctx, maxAnswerSize, qid, query, res); err != nil {
		return fmt.Errorf("failed to do query: %w", err)
	}

	select {
	case resp := <-res:
		var ans any
		if _, err = tl.ParseNoCopy(&ans, resp.ResultBytes, true); err != nil {
			return fmt.Errorf("failed to parse query response: %w", err)
		}

		if err = setQueryResult(result, ans); err != nil {
			return fmt.Errorf("failed to decode query response: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("response deadline exceeded, err: %w", ctx.Err())
	}
}

type AsyncQueryResult struct {
	QueryID     []byte
	ResultBytes []byte
}

func (r *RLDP) DoQueryAsync(ctx context.Context, maxAnswerSize uint64, id []byte, query tl.Serializable, result chan<- AsyncQueryResult) error {
	if len(id) != 32 {
		return errors.New("invalid id")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	closerCtx := r.adnl.GetCloserCtx()
	if closerCtx.Err() != nil {
		return adnl.ErrPeerConnClosed
	}

	now := time.Now()
	timeout, ok := ctx.Deadline()
	if !ok {
		timeout = now.Add(15 * time.Second)
	}

	q := &Query{
		ID:            id,
		MaxAnswerSize: maxAnswerSize,
		Timeout:       uint32(timeout.Unix()),
		Data:          query,
	}

	if uxMin := now.Unix() + 2; int64(q.Timeout) < uxMin {
		// because timeout in seconds, we should add some to avoid an early drop
		q.Timeout = uint32(uxMin)
	}

	data, err := tl.Serialize(q, true)
	if err != nil {
		return fmt.Errorf("failed to serialize query: %w", err)
	}

	transferId := make([]byte, 32)
	_, err = rand.Read(transferId)
	if err != nil {
		return err
	}
	reverseId := reverseTransferId(transferId)

	deadlineMS := timeout.UnixMilli()
	request := &activeRequest{
		id:                 string(q.ID),
		transferID:         transferId,
		expectedTransferID: [32]byte(reverseId),
		deadline:           deadlineMS,
		maxAnswerSize:      maxAnswerSize,
		result:             result,
		done:               ctx.Done(),
	}

	r.mx.Lock()
	if r.closed || closerCtx.Err() != nil {
		r.mx.Unlock()
		return adnl.ErrPeerConnClosed
	}
	if _, ok := r.activeRequests[request.id]; ok {
		r.mx.Unlock()
		return fmt.Errorf("query %s is already active", hex.EncodeToString(id))
	}
	startRequestCleaner := len(r.activeRequests) == 0
	r.activeRequests[request.id] = request
	r.expectedTransfers[request.expectedTransferID] = request
	r.mx.Unlock()
	if startRequestCleaner {
		r.activateRequestCleanupLoop()
	}

	if err = r.startTransfer(ctx, transferId, data, int64(q.Timeout)); err != nil {
		if answered := r.cancelActiveRequest(request); answered {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

		return fmt.Errorf("failed to send query parts: %w", err)
	}

	r.mx.RLock()
	active := r.activeRequests[request.id] == request
	answered := request.answered
	r.mx.RUnlock()
	if !active {
		r.mx.Lock()
		transfer := r.activeTransfers[string(request.transferID)]
		if transfer != nil {
			delete(r.activeTransfers, string(request.transferID))
		}
		r.mx.Unlock()
		if transfer != nil {
			r.finishOutboundTransfer(transfer, outboundTransferCanceled)
		}
	}
	if answered {
		return nil
	}
	if err := ctx.Err(); err != nil {
		if answered := r.cancelActiveRequest(request); answered {
			return nil
		}
		return err
	}
	if !active {
		if time.Now().UnixMilli() >= deadlineMS {
			return context.DeadlineExceeded
		}
		return context.Canceled
	}

	return nil
}

func (r *RLDP) deleteActiveRequestLocked(request *activeRequest) *activeTransfer {
	transfer := r.activeTransfers[string(request.transferID)]
	delete(r.activeRequests, request.id)
	delete(r.activeTransfers, string(request.transferID))
	delete(r.expectedTransfers, request.expectedTransferID)
	return transfer
}

func (r *RLDP) cancelActiveRequest(request *activeRequest) bool {
	timedOut := time.Now().UnixMilli() >= request.deadline

	r.mx.Lock()
	if r.activeRequests[request.id] != request {
		answered := request.answered
		r.mx.Unlock()
		return answered
	}

	transfer := r.deleteActiveRequestLocked(request)
	stream := r.recvStreams[request.expectedTransferID]
	if stream != nil {
		delete(r.recvStreams, request.expectedTransferID)
	}
	r.mx.Unlock()
	if transfer != nil {
		outcome := outboundTransferCanceled
		if timedOut {
			outcome = outboundTransferTimedOut
		}
		r.finishOutboundTransfer(transfer, outcome)
	}
	if stream != nil {
		outcome := inboundStreamCanceled
		if timedOut {
			outcome = inboundStreamExpired
		}
		r.retireInboundStream(stream, outcome)
	}
	return false
}

func (r *RLDP) closeState() {
	r.mx.Lock()
	if r.closed {
		r.mx.Unlock()
		return
	}

	r.closed = true
	transfers := make([]*activeTransfer, 0, len(r.activeTransfers))
	for _, transfer := range r.activeTransfers {
		transfers = append(transfers, transfer)
	}
	streams := make([]*decoderStream, 0, len(r.recvStreams))
	for _, stream := range r.recvStreams {
		streams = append(streams, stream)
	}
	clear(r.activeRequests)
	clear(r.activeTransfers)
	clear(r.expectedTransfers)
	clear(r.recvStreams)
	r.mx.Unlock()

	for _, transfer := range transfers {
		r.finishOutboundTransfer(transfer, outboundTransferCanceled)
	}
	busyStreams := make([]*decoderStream, 0, len(streams))
	for _, stream := range streams {
		if !r.tryRetireInboundStream(stream, inboundStreamCanceled) {
			busyStreams = append(busyStreams, stream)
		}
	}
	if len(busyStreams) > 0 {
		go func() {
			for _, stream := range busyStreams {
				r.retireInboundStream(stream, inboundStreamCanceled)
			}
		}()
	}
}

func (r *RLDP) waitQueryResult(request *activeRequest, result AsyncQueryResult) {
	wait := time.Until(time.UnixMilli(request.deadline))
	if wait <= 0 {
		return
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case request.result <- result:
	case <-request.done:
	case <-r.adnl.GetCloserCtx().Done():
	case <-timer.C:
	}
}

func (r *RLDP) SendAnswer(ctx context.Context, maxAnswerSize uint64, timeoutAt uint32, queryId, toTransferId []byte, answer tl.Serializable) error {
	ansData, err := tl.Serialize(answer, true)
	if err != nil {
		return fmt.Errorf("failed to serialize query: %w", err)
	}

	a := Answer{
		ID:   queryId,
		Data: ansData,
	}

	data, err := tl.Serialize(a, true)
	if err != nil {
		return fmt.Errorf("failed to serialize query: %w", err)
	}

	if uint64(len(data)) > maxAnswerSize {
		return fmt.Errorf("too big answer for that client, client wants no more than %d bytes", maxAnswerSize)
	}

	timeout, ok := ctx.Deadline()
	if !ok {
		timeout = time.Now().Add(15 * time.Second)
	}
	tm := timeout.Unix()

	if int64(timeoutAt) < tm {
		tm = int64(timeoutAt)
	}

	if minT := time.Now().Unix() + 1; tm < minT {
		// give at least 1 sec in case of a clock problem
		tm = minT
	}

	if err = r.startTransfer(ctx, reverseTransferId(toTransferId), data, tm); err != nil {
		return fmt.Errorf("failed to send partitioned answer: %w", err)
	}

	return nil
}

func reverseTransferId(id []byte) []byte {
	rev := make([]byte, 32)
	copy(rev, id)
	for i := range rev {
		rev[i] ^= 0xFF
	}
	return rev
}
