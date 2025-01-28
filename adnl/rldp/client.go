package rldp

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/rldp/raptorq"
	"github.com/xssnick/tonutils-go/tl"
	"log"
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

type activeTransfer struct {
	id      []byte
	enc     *raptorq.Encoder
	seqno   int32
	timeout int64

	feq FECRaptorQ

	lastConfirmSeqno int32
	lastConfirmAt    int64
	lastMorePartAt   int64
	startedAt        int64

	nextRecoverDelay int64
}

type activeRequest struct {
	id       string
	deadline int64
	result   chan<- AsyncQueryResult
}

type RLDP struct {
	adnl  ADNL
	useV2 bool

	activeRequests  map[string]*activeRequest
	activeTransfers map[string]*activeTransfer

	recvStreams map[string]*decoderStream

	onQuery      func(transferId []byte, query *Query) error
	onDisconnect func()

	mx sync.RWMutex

	lastTrack int64
	packets   uint64
	packetsSz uint64
}

type recAt struct {
	seqno int32
	at    time.Time
}

type decoderStream struct {
	decoder        *raptorq.Decoder
	finishedAt     *time.Time
	startedAt      time.Time
	lastCompleteAt time.Time
	lastMessageAt  time.Time
	lastConfirmAt  time.Time

	maxSeqno             int32
	receivedNum          int32
	receivedNumConfirmed int32
	parts                chan *MessagePart

	mx sync.Mutex
}

var DefaultSymbolSize uint32 = 768

const _MTU = 1 << 37

func NewClient(a ADNL) *RLDP {
	r := &RLDP{
		adnl:            a,
		activeRequests:  map[string]*activeRequest{},
		activeTransfers: map[string]*activeTransfer{},
		recvStreams:     map[string]*decoderStream{},
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
		// TODO: use other params too
		msg.Data = Confirm{
			TransferID: m.TransferID,
			Part:       m.Part,
			Seqno:      m.MaxSeqno,
		}
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
		r.mx.RUnlock()

		if stream == nil {
			// TODO: limit unexpected transfer size to 1024 bytes
			if m.TotalSize > _MTU || m.TotalSize <= 0 {
				return fmt.Errorf("bad rldp packet total size")
			}

			stream = &decoderStream{
				startedAt:     tm,
				lastMessageAt: tm,
				parts:         make(chan *MessagePart, 256),
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
		case stream.parts <- &m:
		default:
		}

		if !stream.mx.TryLock() {
			return nil
		}
		defer stream.mx.Unlock()

		if stream.decoder == nil {
			dec, err := raptorq.NewRaptorQ(uint32(fec.SymbolSize)).CreateDecoder(uint32(fec.DataSize))
			if err != nil {
				return fmt.Errorf("failed to init raptorq decoder: %w", err)
			}
			stream.decoder = dec
		}

		for {
			var part *MessagePart
			select {
			case part = <-stream.parts:
			default:
				return nil
			}

			if stream.finishedAt != nil {
				if stream.lastCompleteAt.Add(5 * time.Millisecond).Before(tm) { // we not send completions too often, to not get socket buffer overflow

					var complete tl.Serializable = Complete{
						TransferID: part.TransferID,
						Part:       part.Part,
					}

					if isV2 {
						complete = CompleteV2(complete.(Complete))
					}

					// got packet for a finished stream, let them know that it is completed, again
					err := r.adnl.SendCustomMessage(context.Background(), complete)
					if err != nil {
						return fmt.Errorf("failed to send rldp complete message: %w", err)
					}

					r.mx.Lock()
					r.recvStreams[id].lastCompleteAt = tm
					r.mx.Unlock()
				}
				return nil
			}

			canTryDecode, err := stream.decoder.AddSymbol(uint32(part.Seqno), part.Data)
			if err != nil {
				return fmt.Errorf("failed to add raptorq symbol %d: %w", part.Seqno, err)
			}

			stream.lastMessageAt = tm
			stream.receivedNum++

			if canTryDecode {
				decoded, data, err := stream.decoder.Decode()
				if err != nil {
					return fmt.Errorf("failed to decode raptorq packet: %w", err)
				}

				// it may not be decoded due to unsolvable math system, it means we need more symbols
				if decoded {
					stream.finishedAt = &tm
					stream.decoder = nil

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

					var res any
					if _, err = tl.Parse(&res, data, true); err != nil {
						return fmt.Errorf("failed to parse custom message: %w", err)
					}

					var complete tl.Serializable = Complete{
						TransferID: part.TransferID,
						Part:       part.Part,
					}

					if isV2 {
						complete = CompleteV2(complete.(Complete))
					}

					err = r.adnl.SendCustomMessage(context.Background(), complete)
					if err != nil {
						return fmt.Errorf("failed to send rldp complete message: %w", err)
					}

					switch rVal := res.(type) {
					case Query:
						handler := r.onQuery
						if handler != nil {
							transferId := make([]byte, 32)
							copy(transferId, part.TransferID)

							// go func() {
							if err = handler(transferId, &rVal); err != nil {
								Logger("failed to handle query: ", err)
							}
							// }()
						}
					case Answer:
						qid := string(rVal.ID)

						r.mx.Lock()
						req := r.activeRequests[qid]
						if req != nil {
							delete(r.activeRequests, qid)
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
						log.Println("skipping unwanted rldp message of type", reflect.TypeOf(res).String())
					}

					return nil
				}
			} else {

			}

			if part.Seqno > stream.maxSeqno {
				stream.maxSeqno = part.Seqno

				// send confirm for each 10 packets or after 30 ms
				if stream.receivedNum-stream.receivedNumConfirmed >= 10 ||
					stream.lastConfirmAt.Add(30*time.Millisecond).Before(tm) {
					var confirm tl.Serializable
					if isV2 {
						confirm = ConfirmV2{
							TransferID:    part.TransferID,
							Part:          part.Part,
							MaxSeqno:      stream.maxSeqno,
							ReceivedMask:  0x7FFFFFFF, // TODO
							ReceivedCount: stream.receivedNum,
						}
					} else {
						confirm = Confirm{
							TransferID: part.TransferID,
							Part:       part.Part,
							Seqno:      stream.maxSeqno,
						}
					}
					// we don't care in case of error, not so critical
					err = r.adnl.SendCustomMessage(context.Background(), confirm)
					if err == nil {
						stream.receivedNumConfirmed = stream.receivedNum
						stream.lastConfirmAt = tm
					}
				}
			}
		}
	case Complete: // receiver has fully received transfer, close our stream
		id := string(m.TransferID)

		r.mx.Lock()
		t := r.activeTransfers[id]
		if t != nil {
			delete(r.activeTransfers, id)
		}
		r.mx.Unlock()
	case Confirm: // receiver has received some parts
		// TODO: use confirmed seqno to limit sends
		id := string(m.TransferID)
		r.mx.RLock()
		t := r.activeTransfers[id]
		r.mx.RUnlock()

		if t != nil {
			for { // guaranteed replace to higher val
				if oldSeq := atomic.LoadInt32(&t.seqno); oldSeq < m.Seqno {
					if !atomic.CompareAndSwapInt32(&t.seqno, oldSeq, m.Seqno) {
						continue
					}
				}
				break
			}
			atomic.StoreInt64(&t.lastConfirmAt, time.Now().UnixNano()/int64(time.Millisecond))
		}
	default:
		return fmt.Errorf("unexpected message type %s", reflect.TypeOf(m).String())
	}

	return nil
}

func (r *RLDP) recoverySender() {
	packets := make([]tl.Serializable, 0, 1024)
	transfersToProcess := make([]*activeTransfer, 0, 1024)
	timedOut := make([]*activeTransfer, 0, 128)
	timedOutReq := make([]*activeRequest, 0, 128)
	closerCtx := r.adnl.GetCloserCtx()

	for {
		select {
		case <-closerCtx.Done():
			return
		case <-time.After(1 * time.Millisecond):
			packets = packets[:0]
			transfersToProcess = transfersToProcess[:0]
			timedOut = timedOut[:0]
			timedOutReq = timedOutReq[:0]

			ms := time.Now().UnixNano() / int64(time.Millisecond)

			r.mx.RLock()
			for _, transfer := range r.activeTransfers {
				if ms-transfer.startedAt > transfer.timeout {
					timedOut = append(timedOut, transfer)
					continue
				}

				if ms-transfer.lastMorePartAt > transfer.nextRecoverDelay ||
					(atomic.LoadInt32(&transfer.lastConfirmSeqno) >= transfer.feq.SymbolsCount &&
						transfer.lastMorePartAt < atomic.LoadInt64(&transfer.lastConfirmAt)) {
					transfersToProcess = append(transfersToProcess, transfer)
				}
			}

			for _, req := range r.activeRequests {
				if req.deadline < ms {
					timedOutReq = append(timedOutReq, req)
				}
			}
			r.mx.RUnlock()

		loop:
			for _, transfer := range transfersToProcess {
				transfer.lastMorePartAt = ms
				transfer.nextRecoverDelay = 50 // fixed for now

				numToResend := 1
				if sc := transfer.feq.SymbolsCount / 100; sc > 1 { // up to 1%
					numToResend = int(sc)
				}

				for i := 0; i < numToResend; i++ {
					p := MessagePart{
						TransferID: transfer.id,
						FecType:    transfer.feq,
						Part:       int32(0),
						TotalSize:  int64(transfer.feq.DataSize),
						Seqno:      transfer.seqno,
						Data:       transfer.enc.GenSymbol(uint32(transfer.seqno)),
					}
					transfer.seqno++

					var msgPart tl.Serializable = p
					if r.useV2 {
						msgPart = MessagePartV2(p)
					}

					if err := r.adnl.SendCustomMessage(context.Background(), msgPart); err != nil {
						Logger("failed to send recovery message part", p.Seqno, err.Error())
						break loop
					}
				}
			}

			if len(timedOut) > 0 || len(timedOutReq) > 0 {
				r.mx.Lock()
				for _, transfer := range timedOut {
					delete(r.activeTransfers, string(transfer.id))
				}
				for _, req := range timedOutReq {
					delete(r.activeRequests, req.id)
				}
				r.mx.Unlock()
			}
		}
	}
}

func (r *RLDP) sendMessageParts(ctx context.Context, transferId, data []byte, recoverTimeout time.Duration) error {
	if recoverTimeout <= 0 {
		return errors.New("recover timeout too short")
	}

	enc, err := raptorq.NewRaptorQ(DefaultSymbolSize).CreateEncoder(data)
	if err != nil {
		return fmt.Errorf("failed to create raptorq object encoder: %w", err)
	}

	id := string(transferId)

	at := &activeTransfer{
		id:      transferId,
		enc:     enc,
		timeout: int64(recoverTimeout / time.Millisecond),
		feq: FECRaptorQ{
			DataSize:     int32(len(data)),
			SymbolSize:   int32(DefaultSymbolSize),
			SymbolsCount: int32(enc.BaseSymbolsNum()),
		},
		startedAt:        time.Now().UnixNano() / int64(time.Millisecond),
		nextRecoverDelay: 50,
	}
	at.lastMorePartAt = at.startedAt

	p := MessagePart{
		TransferID: transferId,
		FecType:    at.feq,
		Part:       int32(0),
		TotalSize:  int64(at.feq.DataSize),
	}

	smb := int32(1)
	if s := at.feq.SymbolsCount / 50; s > smb {
		smb = s
	}

	sc := at.feq.SymbolsCount + smb // +2% recovery
	at.seqno = sc

	r.mx.Lock()
	r.activeTransfers[id] = at
	r.mx.Unlock()

	for i := int32(0); i < sc; i++ {
		p.Seqno = i
		p.Data = enc.GenSymbol(uint32(i))

		var msgPart tl.Serializable = p
		if r.useV2 {
			msgPart = MessagePartV2(p)
		}

		err = r.adnl.SendCustomMessage(ctx, msgPart)
		if err != nil {
			return fmt.Errorf("failed to send message part %d: %w", i, err)
		}
	}

	return nil
}

func (r *RLDP) DoQuery(ctx context.Context, maxAnswerSize int64, query, result tl.Serializable) error {
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
		r.mx.Lock()
		delete(r.activeRequests, string(qid))
		r.mx.Unlock()

		reflect.ValueOf(result).Elem().Set(reflect.ValueOf(resp.Result))
		return nil
	case <-ctx.Done():
		r.mx.Lock()
		delete(r.activeRequests, string(qid))
		r.mx.Unlock()

		return fmt.Errorf("response deadline exceeded, err: %w", ctx.Err())
	}
}

type AsyncQueryResult struct {
	QueryID []byte
	Result  any
}

func (r *RLDP) DoQueryAsync(ctx context.Context, maxAnswerSize int64, id []byte, query tl.Serializable, result chan<- AsyncQueryResult) error {
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
		Timeout:       int32(timeout.Unix()),
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

	out := timeout.UnixNano() / int64(time.Millisecond)

	r.mx.Lock()
	r.activeRequests[string(q.ID)] = &activeRequest{
		deadline: out,
		result:   result,
	}
	r.mx.Unlock()

	if err = r.sendMessageParts(ctx, transferId, data, (time.Duration(q.Timeout)-time.Duration(time.Now().Unix()))*time.Second); err != nil {
		return fmt.Errorf("failed to send query parts: %w", err)
	}

	return nil
}

func (r *RLDP) SendAnswer(ctx context.Context, maxAnswerSize int64, queryId, toTransferId []byte, answer tl.Serializable) error {
	a := Answer{
		ID:   queryId,
		Data: answer,
	}

	data, err := tl.Serialize(a, true)
	if err != nil {
		return fmt.Errorf("failed to serialize query: %w", err)
	}

	if int64(len(data)) > maxAnswerSize {
		return fmt.Errorf("too big answer for that client, client wants no more than %d bytes", maxAnswerSize)
	}

	transferId := make([]byte, 32)

	if toTransferId != nil {
		// if we have transfer to respond, invert it and use id
		copy(transferId, toTransferId)
		for i := range transferId {
			transferId[i] ^= 0xFF
		}
	} else {
		_, err = rand.Read(transferId)
		if err != nil {
			return err
		}
	}

	if err = r.sendMessageParts(ctx, transferId, data, 15*time.Second); err != nil {
		return fmt.Errorf("failed to send partitioned answer: %w", err)
	}

	return nil
}
