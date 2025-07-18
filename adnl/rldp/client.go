package rldp

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/xssnick/raptorq"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
	"math/bits"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
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

var PartSize = uint32(1 << 20)

type activeTransferPart struct {
	encoder *raptorq.Encoder
	seqno   uint32
	index   uint32

	feq FECRaptorQ

	lastConfirmSeqnoProcessed uint32
	lastConfirmSeqno          uint32
	lastConfirmAt             int64
	lastRecoverAt             int64

	nextRecoverDelay int64

	transfer *activeTransfer
}

type activeTransfer struct {
	id        []byte
	data      []byte
	timeoutAt int64

	currentPart atomic.Pointer[activeTransferPart]
	rldp        *RLDP

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
	useV2 bool

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

	lastNetworkProcessAt   int64
	lastNetworkPacketsRecv int64
	lastNetworkBatchesNum  int64

	lastReport time.Time
}

type decoderStreamPart struct {
	index uint32

	decoder *raptorq.Decoder

	lastCompleteSeqno    uint32
	maxSeqno             uint32
	receivedMask         uint32
	receivedNum          uint32
	receivedNumConfirmed uint32

	lastConfirmAt  time.Time
	lastCompleteAt time.Time
}

type decoderStream struct {
	finishedAt    *time.Time
	lastMessageAt time.Time
	currentPart   decoderStreamPart

	messages chan *MessagePart

	totalSize uint64
	buf       []byte

	mx sync.Mutex
}

var MaxUnexpectedTransferSize uint64 = 1 << 16 // 64 KB
var MaxFECDataSize uint64 = 2 << 20            // 2 MB
var DefaultFECDataSize uint64 = 1 << 20        // 1 MB
var DefaultSymbolSize uint32 = 768

const _MTU = 1 << 37

func NewClient(a ADNL) *RLDP {
	r := &RLDP{
		adnl:                   a,
		activeRequests:         map[string]*activeRequest{},
		activeTransfers:        map[string]*activeTransfer{},
		recvStreams:            map[string]*decoderStream{},
		expectedTransfers:      map[string]*expectedTransfer{},
		activateRecoverySender: make(chan bool, 1),
		rateLimit:              NewTokenBucket(3000, a.RemoteAddr()),
	}

	a.SetCustomMessageHandler(r.handleMessage)
	go r.recoverySender()

	return r
}

func NewClientV2(a ADNL) *RLDP {
	c := NewClient(a)
	c.useV2 = true
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

	switch m := msg.Data.(type) {
	case MessagePart:
		fec, ok := m.FecType.(FECRaptorQ)
		if !ok {
			return fmt.Errorf("not supported fec type")
		}

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

			if m.TotalSize < uint64(fec.DataSize) {
				return fmt.Errorf("bad rldp total size %d, expected at least %d", m.TotalSize, fec.DataSize)
			}

			stream = &decoderStream{
				lastMessageAt: tm,
				messages:      make(chan *MessagePart, 256),
				currentPart: decoderStreamPart{
					index: 0,
				},
				totalSize: m.TotalSize,
				buf:       make([]byte, 0, m.TotalSize),
			}

			r.mx.Lock()
			// check again because of possible concurrency
			if r.recvStreams[id] != nil {
				stream = r.recvStreams[id]
			} else {
				r.recvStreams[id] = stream
			}
			r.mx.Unlock()
		}

		select {
		case stream.messages <- &m:
			// put message to queue in case it will be locked by other processor
		default:
		}

		if !stream.mx.TryLock() {
			return nil
		}
		defer stream.mx.Unlock()

		for {
			var part *MessagePart
			select {
			case part = <-stream.messages:
			default:
				return nil
			}

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
					// TODO: just mark to auto send later?
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
				fec, ok := part.FecType.(FECRaptorQ)
				if !ok {
					return fmt.Errorf("not supported fec type in part: %d", part.Part)
				}

				if uint64(fec.DataSize) > stream.totalSize || fec.DataSize > uint32(MaxFECDataSize) ||
					fec.SymbolSize == 0 || fec.SymbolsCount == 0 {
					return fmt.Errorf("invalid fec")
				}

				dec, err := raptorq.NewRaptorQ(fec.SymbolSize).CreateDecoder(fec.DataSize)
				if err != nil {
					return fmt.Errorf("failed to init raptorq decoder: %w", err)
				}
				stream.currentPart.decoder = dec
				Logger("[ID]", hex.EncodeToString(part.TransferID), "[RLDP] created decoder for part:", part.Part, "data size:", fec.DataSize, "symbol size:", fec.SymbolSize, "symbols:", fec.SymbolsCount)
			}

			canTryDecode, err := stream.currentPart.decoder.AddSymbol(part.Seqno, part.Data)
			if err != nil {
				return fmt.Errorf("failed to add raptorq symbol %d: %w", part.Seqno, err)
			}

			stream.lastMessageAt = tm
			stream.currentPart.receivedNum++

			if canTryDecode {
				tmd := time.Now()
				decoded, data, err := stream.currentPart.decoder.Decode()
				if err != nil {
					return fmt.Errorf("failed to decode raptorq packet: %w", err)
				}

				// it may not be decoded due to unsolvable math system, it means we need more symbols
				if decoded {
					Logger("[RLDP] part", part.Part, "decoded on seqno", part.Seqno, "symbols:", fec.SymbolsCount, "decode took", time.Since(tmd).String())

					stream.currentPart = decoderStreamPart{
						index: stream.currentPart.index + 1,
					}
					stream.buf = append(stream.buf, data...)

					var complete tl.Serializable = Complete{
						TransferID: part.TransferID,
						Part:       part.Part,
					}

					if isV2 {
						complete = CompleteV2(complete.(Complete))
					}
					_ = r.adnl.SendCustomMessage(context.Background(), complete)

					if uint64(len(stream.buf)) >= stream.totalSize {
						stream.finishedAt = &tmd

						r.mx.Lock()
						if len(r.recvStreams) > 100 {
							for sID, s := range r.recvStreams {
								// remove streams that was finished more than 30 sec ago or when it was no messages for more than 60 seconds.
								if s.lastMessageAt.Add(60*time.Second).Before(tm) ||
									(s.finishedAt != nil && s.finishedAt.Add(30*time.Second).Before(tm)) {
									delete(r.recvStreams, sID)
								}
							}
						}
						r.mx.Unlock()

						if uint64(len(stream.buf)) > stream.totalSize {
							return fmt.Errorf("received more data than expected, expected %d, got %d", stream.totalSize, len(stream.buf))
						}

						var res any
						if _, err = tl.Parse(&res, stream.buf, true); err != nil {
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

								// if channel is full we sacrifice processing speed, responses better
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
					Logger("[RLDP] part ", part.Part, "decode attempt failure on seqno", part.Seqno, "symbols:", fec.SymbolsCount, "decode took", time.Since(tmd).String())
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
		if part == nil {
			break
		}

		for { // guaranteed replace to higher val
			if oldSeq := atomic.LoadUint32(&part.lastConfirmSeqno); oldSeq < m.MaxSeqno {
				if !atomic.CompareAndSwapUint32(&part.lastConfirmSeqno, oldSeq, m.MaxSeqno) {
					continue
				}
				// replaced

				lastProc := atomic.LoadUint32(&part.lastConfirmSeqnoProcessed)
				if isV2 && lastProc+32 <= m.MaxSeqno &&
					atomic.CompareAndSwapUint32(&part.lastConfirmSeqnoProcessed, lastProc, m.MaxSeqno) {

					nowMs := time.Now().UnixMilli()

					lastAt := atomic.LoadInt64(&r.lastNetworkProcessAt)
					packetsRecv := atomic.AddInt64(&r.lastNetworkPacketsRecv, int64(bits.OnesCount32(m.ReceivedMask)))
					batches := atomic.AddInt64(&r.lastNetworkBatchesNum, 1)

					rate := r.rateLimit.GetRate()

					// boost when low rate, and slowdown checks when high
					delay := 10 + rate/2000
					if delay < 10 {
						delay = 10
					} else if delay > 500 {
						delay = 500
					}

					if batches >= 3 && lastAt+delay <= nowMs && atomic.CompareAndSwapInt64(&r.lastNetworkBatchesNum, batches, 0) {
						atomic.StoreInt64(&r.lastNetworkProcessAt, nowMs)
						atomic.AddInt64(&r.lastNetworkPacketsRecv, -packetsRecv)

						ratio := float64(packetsRecv) / float64(batches*32)

						tokens := r.rateLimit.GetTokensLeft()

						if ratio >= 0.95 {
							if tokens < (rate/3)*2 {
								r.rateLimit.SetRate(rate + rate/10) // +10%
							}
						} else if ratio < 0.35 {
							r.rateLimit.SetRate(rate - rate/10) // -10%
						} else if ratio < 0.75 {
							r.rateLimit.SetRate(rate - rate/20) // -5%
						}
					}
				}
			}
			break
		}
		atomic.StoreInt64(&part.lastConfirmAt, time.Now().UnixMilli())
	default:
		return fmt.Errorf("unexpected message type %s", reflect.TypeOf(m).String())
	}

	return nil
}

func (r *RLDP) recoverySender() {
	transfersToProcess := make([]*activeTransferPart, 0, 128)
	timedOut := make([]*activeTransfer, 0, 32)
	timedOutReq := make([]string, 0, 32)
	timedOutExp := make([]string, 0, 32)
	closerCtx := r.adnl.GetCloserCtx()
	ticker := time.NewTicker(1 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-closerCtx.Done():
			return
		case <-ticker.C:
			if r.lastReport.Before(time.Now().Add(-10 * time.Second)) {
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
				if part == nil || atomic.LoadUint32(&part.seqno) < part.feq.SymbolsCount {
					// no parts or fast symbols not yet sent
					continue
				}

				if ms-part.lastRecoverAt > part.nextRecoverDelay ||
					(atomic.LoadUint32(&part.lastConfirmSeqno) >= part.feq.SymbolsCount &&
						part.lastRecoverAt < atomic.LoadInt64(&part.lastConfirmAt)) {
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
				// stop ticks to not consume resources
				ticker.Stop()
			}
			r.mx.RUnlock()

		loop:
			for _, part := range transfersToProcess {
				part.lastRecoverAt = ms
				part.nextRecoverDelay = 30 // fixed for now

				numToResend := 1
				if sc := part.feq.SymbolsCount / 100; sc > 1 { // up to 1%
					numToResend = int(sc)
				}

				seqno := atomic.LoadUint32(&part.seqno)
				for i := 0; i < numToResend; i++ {
					p := MessagePart{
						TransferID: part.transfer.id,
						FecType:    part.feq,
						Part:       part.index,
						TotalSize:  uint64(len(part.transfer.data)),
						Seqno:      seqno,
						Data:       part.encoder.GenSymbol(seqno),
					}
					seqno++

					var msgPart tl.Serializable = p
					if r.useV2 {
						msgPart = MessagePartV2(p)
					}

					for {
						if r.useV2 && !r.rateLimit.TryConsume() {
							select {
							case <-closerCtx.Done():
								return
							case <-time.After(1 * time.Millisecond):
							}
							continue
						}

						if err := r.adnl.SendCustomMessage(context.Background(), msgPart); err != nil {
							Logger("failed to send recovery message part", p.Seqno, err.Error())
							break loop
						}
						break
					}
				}
				atomic.StoreUint32(&part.seqno, seqno)
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

			transfersToProcess = transfersToProcess[:0]
			timedOut = timedOut[:0]
			timedOutReq = timedOutReq[:0]
			timedOutExp = timedOutExp[:0]
		case <-r.activateRecoverySender:
			ticker.Reset(1 * time.Millisecond)
		}
	}
}

func (r *RLDP) startTransfer(ctx context.Context, transferId, data []byte, recoverTimeoutAt int64) error {
	at := &activeTransfer{
		id:        transferId,
		timeoutAt: recoverTimeoutAt * 1000, // ms
		data:      data,
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

	partIndex := uint32(0)
	if cp := t.getCurrentPart(); cp != nil {
		partIndex = cp.index + 1
	}

	if len(t.data) <= int(partIndex*PartSize) {
		// all parts sent
		return false, nil
	}

	payload := t.data[partIndex*PartSize:]
	if len(payload) > int(PartSize) {
		payload = payload[:PartSize]
	}

	enc, err := raptorq.NewRaptorQ(DefaultSymbolSize).CreateEncoder(payload)
	if err != nil {
		return false, fmt.Errorf("failed to create raptorq object encoder: %w", err)
	}

	part := activeTransferPart{
		encoder: enc,
		seqno:   0,
		index:   partIndex,
		feq: FECRaptorQ{
			DataSize:     uint32(len(payload)),
			SymbolSize:   DefaultSymbolSize,
			SymbolsCount: enc.BaseSymbolsNum(),
		},
		nextRecoverDelay: 30,
		transfer:         t,
	}

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
		FecType:    part.feq,
		Part:       part.index,
		TotalSize:  uint64(len(transfer.data)),
	}

	smb := uint32(1)
	if s := part.feq.SymbolsCount / 33; s > smb {
		smb = s
	}

	sc := part.feq.SymbolsCount + smb // ~3% recovery

	for i := uint32(0); i < sc; i++ {
		p.Seqno = i
		p.Data = part.encoder.GenSymbol(i)

		var msgPart tl.Serializable = p
		if r.useV2 {
			msgPart = MessagePartV2(p)
		}

		for {
			if r.useV2 && !r.rateLimit.TryConsume() {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(1 * time.Millisecond):
				}
				continue
			}

			if err := r.adnl.SendCustomMessage(ctx, msgPart); err != nil {
				return fmt.Errorf("failed to send message part %d: %w", i, err)
			}
			break
		}
	}
	atomic.StoreUint32(&part.seqno, sc)

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
	timeout, ok := ctx.Deadline()
	if !ok {
		timeout = time.Now().Add(15 * time.Second)
	}

	if len(id) != 32 {
		return errors.New("invalid id")
	}

	q := &Query{
		ID:            id,
		MaxAnswerSize: maxAnswerSize,
		Timeout:       uint32(timeout.Unix()),
		Data:          query,
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
