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

var PartSize = uint32(256<<10) + 1024

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
	lossEWMA         atomic.Uint64
	drrDeficit       int64

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

	mx sync.Mutex
}

type activeRequest struct {
	deadline int64
	result   chan<- AsyncQueryResult
}

type expectedTransfer struct {
	deadline int64
	maxSize  uint64
}

type RLDP struct {
	adnl  ADNL
	useV2 atomic.Bool

	activateRecoverySender chan bool
	activeRequests         map[string]*activeRequest
	activeTransfers        map[string]*activeTransfer
	expectedTransfers      map[string]*expectedTransfer

	recvStreams map[string]*decoderStream

	onQuery      func(transferId []byte, query *Query) error
	onDisconnect func()

	mx sync.RWMutex

	lastTrack int64
	packets   uint64
	packetsSz uint64

	rateLimit *TokenBucket
	rateCtrl  *BBRv2Controller

	lastReport time.Time
}

type fecDecoder interface {
	AddSymbol(id uint32, data []byte) (bool, error)
	Decode() (bool, []byte, error)
}

type decoderStreamPart struct {
	index uint32

	decoder fecDecoder

	fecDataSize     uint32
	fecSymbolSize   uint32
	fecSymbolsCount uint32

	lastCompleteSeqno    uint32
	maxSeqno             uint32
	receivedMask         uint32
	receivedNum          uint32
	receivedFastNum      uint32
	receivedNumConfirmed uint32

	recvOrder []uint32

	lastConfirmAt  time.Time
	lastCompleteAt time.Time
	startedAt      time.Time
}

type decoderStream struct {
	finishedAt    *time.Time
	lastMessageAt time.Time
	startedAt     time.Time
	currentPart   decoderStreamPart

	/// messages chan *MessagePart
	msgBuf *Queue

	totalSize uint64

	parts     [][]byte
	partsSize uint64

	mx sync.Mutex
}

var MaxUnexpectedTransferSize uint64 = 64 << 10 // 64 KB
var MaxFECDataSize uint32 = 2 << 20             // 2 MB
var DefaultSymbolSize uint32 = 768

const _MTU = 1 << 37

var MinRateBytesSec = int64(1 << 20)
var MaxRateBytesSec = int64(512 << 20)

func NewClient(a ADNL) *RLDP {
	r := &RLDP{
		adnl:                   a,
		activeRequests:         map[string]*activeRequest{},
		activeTransfers:        map[string]*activeTransfer{},
		recvStreams:            map[string]*decoderStream{},
		expectedTransfers:      map[string]*expectedTransfer{},
		activateRecoverySender: make(chan bool, 1),
		rateLimit:              NewTokenBucket(1<<20, a.RemoteAddr()),
	}

	r.rateCtrl = NewBBRv2Controller(r.rateLimit, BBRv2Options{
		Name:               r.adnl.RemoteAddr(),
		MinRate:            MinRateBytesSec,
		MaxRate:            MaxRateBytesSec,
		HighLoss:           0.25,
		Beta:               0.9,
		DefaultRTTMs:       25,
		BtlBwWindowSec:     10,
		ProbeBwCycleMs:     200,
		ProbeRTTDurationMs: 200,
		MinRTTExpiryMs:     20_000,
		MinSampleMs:        50,
	})

	a.SetCustomMessageHandler(r.handleMessage)
	go r.recoverySender()

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
	r.onQuery = handler
}

// Deprecated: use GetADNL().SetDisconnectHandler
// WARNING: it overrides underlying adnl disconnect handler
func (r *RLDP) SetOnDisconnect(handler func()) {
	r.adnl.SetDisconnectHandler(func(addr string, key ed25519.PublicKey) {
		handler()
	})
}

func (r *RLDP) Close() {
	r.adnl.Close()
}

func (r *RLDP) handleMessage(msg *adnl.MessageCustom) error {
	isV2 := true
	switch m := msg.Data.(type) {
	case MessagePartV2:
		msg.Data = MessagePart(m)
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

	switch m := msg.Data.(type) {
	case MessagePart:
		tm := time.Now()

		id := string(m.TransferID)
		r.mx.RLock()
		stream := r.recvStreams[id]
		expected := r.expectedTransfers[id]
		r.mx.RUnlock()

		if stream == nil {
			if m.TotalSize > _MTU || m.TotalSize <= 0 {
				return fmt.Errorf("bad rldp packet total size %d", m.TotalSize)
			}

			// unexpected transfers limited to this size, for protection
			var maxTransferSize = MaxUnexpectedTransferSize
			if expected != nil {
				maxTransferSize = expected.maxSize
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
			if r.recvStreams[id] != nil {
				stream = r.recvStreams[id]
			} else {
				stream = &decoderStream{
					lastMessageAt: tm,
					startedAt:     tm,
					msgBuf:        NewQueue(qsz),
					currentPart: decoderStreamPart{
						index:     0,
						startedAt: tm,
					},
					totalSize: m.TotalSize,
				}

				r.recvStreams[id] = stream
			}
			r.mx.Unlock()
		}

		// put a message to queue in case it will be locked by another processor
		stream.msgBuf.Enqueue(&m)

		if !stream.mx.TryLock() {
			return nil
		}
		defer stream.mx.Unlock()

		for {
			part, ok := stream.msgBuf.Dequeue()
			if !ok {
				return nil
			}

			err := func() error {
				if stream.finishedAt != nil || stream.currentPart.index > part.Part {
					if stream.currentPart.lastCompleteAt.Add(10 * time.Millisecond).Before(tm) { // to not send completions too often
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
						stream.currentPart.lastCompleteAt = tm
					}
					return nil
				}

				if part.Part > stream.currentPart.index {
					return fmt.Errorf("received out of order part %d, expected %d", part.Part, stream.currentPart.index)
				}
				if part.TotalSize != stream.totalSize {
					return fmt.Errorf("received part with bad total size %d, expected %d", part.TotalSize, stream.totalSize)
				}

				if stream.currentPart.decoder == nil {
					var decoderType uint32
					switch m.FecType.(type) {
					case FECRaptorQ:
						decoderType = 0
					case FECRoundRobin:
						decoderType = 1
					default:
						return fmt.Errorf("not supported fec type")
					}

					if m.TotalSize < uint64(m.FecType.GetDataSize()) {
						return fmt.Errorf("bad rldp total size %d, expected at least %d", m.TotalSize, m.FecType.GetDataSize())
					}

					if uint64(m.FecType.GetDataSize()) > stream.totalSize || m.FecType.GetDataSize() > MaxFECDataSize ||
						m.FecType.GetSymbolSize() == 0 || m.FecType.GetSymbolsCount() == 0 {
						return fmt.Errorf("invalid fec")
					}

					var err error
					var dec fecDecoder
					if decoderType == 0 {
						dec, err = raptorq.NewRaptorQ(m.FecType.GetSymbolSize()).CreateDecoder(m.FecType.GetDataSize())
						if err != nil {
							return fmt.Errorf("failed to init raptorq decoder: %w", err)
						}
					} else {
						dec, err = roundrobin.NewDecoder(m.FecType.GetSymbolSize(), m.FecType.GetDataSize())
						if err != nil {
							return fmt.Errorf("failed to init round robin decoder: %w", err)
						}
					}

					stream.currentPart.fecDataSize = m.FecType.GetDataSize()
					stream.currentPart.fecSymbolSize = m.FecType.GetSymbolSize()
					stream.currentPart.fecSymbolsCount = m.FecType.GetSymbolsCount()
					stream.currentPart.decoder = dec

					Logger("[ID]", hex.EncodeToString(part.TransferID), "[RLDP] created decoder for part:", part.Part, "data size:", stream.currentPart.fecDataSize, "symbol size:", stream.currentPart.fecSymbolSize, "symbols:", stream.currentPart.fecSymbolsCount)
				}

				canTryDecode, err := stream.currentPart.decoder.AddSymbol(part.Seqno, part.Data)
				if err != nil {
					return fmt.Errorf("failed to add raptorq symbol %d: %w", part.Seqno, err)
				}

				stream.lastMessageAt = tm
				stream.currentPart.receivedNum++
				if part.Seqno < stream.currentPart.fecSymbolsCount {
					stream.currentPart.receivedFastNum++
				}
				stream.currentPart.recvOrder = append(stream.currentPart.recvOrder, part.Seqno)

				if canTryDecode {
					tmd := time.Now()
					decoded, data, err := stream.currentPart.decoder.Decode()
					if err != nil {
						return fmt.Errorf("failed to decode raptorq packet: %w", err)
					}

					// it may not be decoded due to an unsolvable math system, it means we need more symbols
					if decoded {
						took := tmd.Sub(stream.currentPart.startedAt)
						Logger("[RLDP] v2:", isV2, "part", part.Part, "part sz", stream.currentPart.fecDataSize, "decoded on seqno", part.Seqno, "symbols:", stream.currentPart.fecSymbolsCount, "received:", stream.currentPart.receivedNum, "fast", stream.currentPart.receivedFastNum,
							"decode took", time.Since(tmd).String(), "took", took.String())

						stream.currentPart = decoderStreamPart{
							index:     stream.currentPart.index + 1,
							startedAt: tmd,
						}

						if len(data) > 0 {
							stream.parts = append(stream.parts, data)
							stream.partsSize += uint64(len(data))
						}

						var complete tl.Serializable = Complete{
							TransferID: part.TransferID,
							Part:       part.Part,
						}

						// drop unprocessed messages related to this part
						stream.msgBuf.Drain()

						if isV2 {
							complete = CompleteV2(complete.(Complete))
						}
						_ = r.adnl.SendCustomMessage(context.Background(), complete)

						if stream.partsSize >= stream.totalSize {
							stream.finishedAt = &tmd
							stream.currentPart.decoder = nil

							r.mx.Lock()
							for sID, s := range r.recvStreams {
								// remove streams that was finished more than 15 sec ago or when it was no messages for more than 30 seconds.
								if s.lastMessageAt.Add(30*time.Second).Before(tm) ||
									(s.finishedAt != nil && s.finishedAt.Add(15*time.Second).Before(tm)) {
									delete(r.recvStreams, sID)
								}
							}
							r.mx.Unlock()

							if stream.partsSize > stream.totalSize {
								return fmt.Errorf("received more data than expected, expected %d, got %d", stream.totalSize, stream.partsSize)
							}
							buf := make([]byte, stream.totalSize)
							off := 0
							for _, p := range stream.parts {
								off += copy(buf[off:], p)
							}
							stream.parts = nil
							stream.partsSize = 0

							var res any
							if _, err = tl.Parse(&res, buf, true); err != nil {
								return fmt.Errorf("failed to parse custom message: %w", err)
							}

							Logger("[RLDP] stream finished and parsed, processing transfer data", hex.EncodeToString(part.TransferID))

							switch rVal := res.(type) {
							case Query:
								handler := r.onQuery
								if handler != nil {
									transferId := make([]byte, 32)
									copy(transferId, part.TransferID)

									if err = handler(transferId, &rVal); err != nil {
										Logger("failed to handle query: ", err)
									}
								}
							case Answer:
								qid := string(rVal.ID)

								r.mx.Lock()
								req := r.activeRequests[qid]
								if req != nil {
									delete(r.activeRequests, qid)
									delete(r.expectedTransfers, id)
								}
								r.mx.Unlock()

								if req != nil {
									queryId := make([]byte, 32)
									copy(queryId, rVal.ID)

									// if a channel is full, we sacrifice processing speed, responses better
									req.result <- AsyncQueryResult{
										QueryID: queryId,
										Result:  rVal.Data,
									}
								}
							default:
								Logger("[RLDP] skipping unwanted rldp message of type", reflect.TypeOf(res).String())
							}
						}
						return nil
					} else {
						Logger("[RLDP] part ", part.Part, "decode attempt failure on seqno", part.Seqno, "symbols:", stream.currentPart.fecSymbolsCount, "decode took", time.Since(tmd).String())
					}
				}

				if part.Seqno > stream.currentPart.maxSeqno {
					diff := part.Seqno - stream.currentPart.maxSeqno
					if diff >= 32 {
						stream.currentPart.receivedMask = 0
					} else {
						stream.currentPart.receivedMask <<= diff
					}
					stream.currentPart.maxSeqno = part.Seqno
				}

				if offset := stream.currentPart.maxSeqno - part.Seqno; offset < 32 {
					stream.currentPart.receivedMask |= 1 << offset
				}

				// send confirm for each 10 packets or after 20 ms
				if stream.currentPart.receivedNum-stream.currentPart.receivedNumConfirmed >= 10 ||
					stream.currentPart.lastConfirmAt.Add(20*time.Millisecond).Before(tm) {
					var confirm tl.Serializable
					if isV2 {
						confirm = ConfirmV2{
							TransferID:    part.TransferID,
							Part:          part.Part,
							MaxSeqno:      stream.currentPart.maxSeqno,
							ReceivedMask:  stream.currentPart.receivedMask,
							ReceivedCount: stream.currentPart.receivedNum,
						}
					} else {
						confirm = Confirm{
							TransferID: part.TransferID,
							Part:       part.Part,
							Seqno:      stream.currentPart.maxSeqno,
						}
					}
					// we don't care in case of error, not so critical
					err = r.adnl.SendCustomMessage(context.Background(), confirm)
					if err == nil {
						stream.currentPart.receivedNumConfirmed = stream.currentPart.receivedNum
						stream.currentPart.lastConfirmAt = tm
					}
				}

				return nil
			}()
			if err != nil {
				Logger("[RLDP] transfer", hex.EncodeToString(part.TransferID), "process msg part:", part.Part, "error:", err.Error())
			}
		}
	case Complete: // receiver has fully received transfer part, send new part or close our stream if done
		r.mx.RLock()
		t := r.activeTransfers[string(m.TransferID)]
		r.mx.RUnlock()

		if t != nil {
			t.mx.Lock()
			p := t.getCurrentPart()
			if p == nil || p.index != m.Part {
				// completion not for current part
				t.mx.Unlock()
				break
			}

			more, err := t.prepareNextPart()
			t.mx.Unlock()

			if err != nil {
				Logger("[RLDP] failed to prepare next part for transfer", hex.EncodeToString(m.TransferID), err.Error())
				break
			}

			if !more {
				r.mx.Lock()
				delete(r.activeTransfers, string(t.id))
				r.mx.Unlock()

				break
			}
			Logger("[RLDP] next part", m.Part+1, "prepared for transfer", hex.EncodeToString(m.TransferID), "sending fast symbols")

			if err = r.sendFastSymbols(context.Background(), t); err != nil {
				r.mx.Lock()
				delete(r.activeTransfers, string(t.id))
				r.mx.Unlock()

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

		part := t.getCurrentPart()
		if part == nil || part.index != m.Part {
			break
		}

		if isV2 {
			for {
				prevSeq := atomic.LoadUint32(&part.lastConfirmSeqnoProcessed)
				prevRecv := atomic.LoadUint32(&part.lastConfirmRecvProcessed)

				advancedSeq := m.MaxSeqno > prevSeq
				advancedRecv := m.ReceivedCount > prevRecv
				if !advancedSeq && !advancedRecv {
					break
				}

				if advancedSeq && !atomic.CompareAndSwapUint32(&part.lastConfirmSeqnoProcessed, prevSeq, m.MaxSeqno) {
					continue
				}

				if advancedRecv {
					atomic.StoreUint32(&part.lastConfirmRecvProcessed, m.ReceivedCount)
				}

				var seqDelta int64
				if advancedSeq {
					seqDelta = int64(m.MaxSeqno - prevSeq)
				}

				var recvDelta int64
				if advancedRecv {
					recvDelta = int64(m.ReceivedCount - prevRecv)
				}

				if seqDelta < 0 {
					seqDelta = 0
				}
				if recvDelta < 0 {
					recvDelta = 0
				}

				totalDelta := seqDelta
				if totalDelta < recvDelta {
					totalDelta = recvDelta
				}
				if totalDelta <= 0 {
					break
				}

				if tms, ok := part.sendClock.SentAt(m.MaxSeqno); ok {
					r.rateCtrl.ObserveRTT(time.Now().UnixMilli() - tms)
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

				break
			}
		}

		atomic.StoreInt64(&part.lastConfirmAt, time.Now().UnixMilli())
	default:
		return fmt.Errorf("unexpected message type %s", reflect.TypeOf(m).String())
	}

	return nil
}

func (r *RLDP) GetRateInfo() (left int64, total int64) {
	return r.rateLimit.GetTokensLeft(), r.rateLimit.GetRate()
}

func (r *RLDP) recoverySender() {
	transfersToProcess := make([]*activeTransferPart, 0, 128)
	timedOut := make([]*activeTransfer, 0, 32)
	timedOutReq := make([]string, 0, 32)
	timedOutExp := make([]string, 0, 32)
	closerCtx := r.adnl.GetCloserCtx()
	ticker := time.NewTicker(1 * time.Millisecond)
	defer ticker.Stop()

	// round-robin head for fair recovery
	var rrHead uint32

	active := false
	for {
		select {
		case <-closerCtx.Done():
			return
		case <-r.activateRecoverySender:
			active = true
		case <-ticker.C:
			if !active {
				break
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

			for id, req := range r.activeRequests {
				if req.deadline < ms {
					timedOutReq = append(timedOutReq, id)
				}
			}

			for id, req := range r.expectedTransfers {
				if req.deadline < ms {
					timedOutExp = append(timedOutExp, id)
				}
			}

			if len(r.activeRequests)+len(r.activeTransfers)+len(r.expectedTransfers) == 0 {
				// stop active ticks to not consume resources
				active = false
			}
			r.mx.RUnlock()

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
							break sendLoop
						}
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

			if len(timedOut) > 0 || len(timedOutReq) > 0 || len(timedOutExp) > 0 {
				r.mx.Lock()
				for _, transfer := range timedOut {
					delete(r.activeTransfers, string(transfer.id))
				}
				for _, req := range timedOutReq {
					delete(r.activeRequests, req)
				}
				for _, req := range timedOutExp {
					delete(r.expectedTransfers, req)
				}
				r.mx.Unlock()
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

			for i := range timedOutReq {
				timedOutReq[i] = ""
			}
			timedOutReq = timedOutReq[:0]

			for i := range timedOutExp {
				timedOutExp[i] = ""
			}
			timedOutExp = timedOutExp[:0]
		}
	}
}

func (r *RLDP) startTransfer(ctx context.Context, transferId, data []byte, recoverTimeoutAt int64) error {
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
	r.activeTransfers[string(at.id)] = at
	r.mx.Unlock()

	if err := r.sendFastSymbols(ctx, at); err != nil {
		r.mx.Lock()
		delete(r.activeTransfers, string(at.id))
		r.mx.Unlock()

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

	cnt := uint32(len(payload))/DefaultSymbolSize + 1

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
	batch := r.rateLimit.ConsumePackets(int(part.fastSeqnoTill), int(part.fecSymbolSize))
	now := time.Now().UnixMilli()

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
			return fmt.Errorf("failed to send message part %d: %w", currentSeqno, err)
		}

		seqno++
	}

	atomic.StoreUint32(&part.seqno, seqno)
	part.recoveryReady.Store(true)
	part.startedAt = time.Now()

	select {
	case r.activateRecoverySender <- true:
	default:
	}

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
		reflect.ValueOf(result).Elem().Set(reflect.ValueOf(resp.Result))
		return nil
	case <-ctx.Done():
		return fmt.Errorf("response deadline exceeded, err: %w", ctx.Err())
	}
}

type AsyncQueryResult struct {
	QueryID []byte
	Result  any
}

func (r *RLDP) DoQueryAsync(ctx context.Context, maxAnswerSize uint64, id []byte, query tl.Serializable, result chan<- AsyncQueryResult) error {
	if len(id) != 32 {
		return errors.New("invalid id")
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

	out := timeout.UnixNano() / int64(time.Millisecond)

	r.mx.Lock()
	r.activeRequests[string(q.ID)] = &activeRequest{
		deadline: out,
		result:   result,
	}
	r.expectedTransfers[string(reverseId)] = &expectedTransfer{
		deadline: out,
		maxSize:  maxAnswerSize,
	}
	r.mx.Unlock()

	if err = r.startTransfer(ctx, transferId, data, int64(q.Timeout)); err != nil {
		return fmt.Errorf("failed to send query parts: %w", err)
	}

	return nil
}

func (r *RLDP) SendAnswer(ctx context.Context, maxAnswerSize uint64, timeoutAt uint32, queryId, toTransferId []byte, answer tl.Serializable) error {
	a := Answer{
		ID:   queryId,
		Data: answer,
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
