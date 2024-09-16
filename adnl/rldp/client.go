package rldp

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/rldp/raptorq"
	"github.com/xssnick/tonutils-go/tl"
	"log"
	"reflect"
	"sync"
	"time"
)

type ADNL interface {
	RemoteAddr() string
	GetID() []byte
	SetCustomMessageHandler(handler func(msg *adnl.MessageCustom) error)
	SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey))
	GetDisconnectHandler() func(addr string, key ed25519.PublicKey)
	SendCustomMessage(ctx context.Context, req tl.Serializable) error
	Close()
}

var Logger = func(a ...any) {}

type RLDP struct {
	adnl  ADNL
	useV2 bool

	activeRequests  map[string]chan any
	activeTransfers map[string]chan bool

	recvStreams map[string]*decoderStream

	onQuery      func(transferId []byte, query *Query) error
	onDisconnect func()

	mx sync.RWMutex
}

type decoderStream struct {
	decoder        *raptorq.Decoder
	finishedAt     *time.Time
	lastCompleteAt time.Time
	lastMessageAt  time.Time
	lastConfirmAt  time.Time

	maxSeqno             int32
	receivedNum          int32
	receivedNumConfirmed int32

	mx sync.Mutex
}

const _MTU = 1 << 37
const _SymbolSize = 768
const _PacketWaitTime = 5 * time.Millisecond

func NewClient(a ADNL) *RLDP {
	r := &RLDP{
		adnl:            a,
		activeRequests:  map[string]chan any{},
		activeTransfers: map[string]chan bool{},
		recvStreams:     map[string]*decoderStream{},
	}

	a.SetCustomMessageHandler(r.handleMessage)

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

		id := string(m.TransferID)
		r.mx.RLock()
		stream := r.recvStreams[id]
		r.mx.RUnlock()

		if stream == nil {
			// TODO: limit unexpected transfer size to 1024 bytes
			if m.TotalSize > _MTU || m.TotalSize <= 0 {
				return fmt.Errorf("bad rldp packet total size")
			}
			dec, err := raptorq.NewRaptorQ(uint32(fec.SymbolSize)).CreateDecoder(uint32(fec.DataSize))
			if err != nil {
				return fmt.Errorf("failed to init raptorq decoder: %w", err)
			}
			stream = &decoderStream{
				decoder:       dec,
				lastMessageAt: time.Now(),
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

		stream.mx.Lock()
		defer stream.mx.Unlock()

		if stream.finishedAt != nil {
			if stream.lastCompleteAt.Add(5 * time.Millisecond).Before(time.Now()) { // we not send completions too often, to not get socket buffer overflow

				var complete tl.Serializable = Complete{
					TransferID: m.TransferID,
					Part:       m.Part,
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
				r.recvStreams[id].lastCompleteAt = time.Now()
				r.mx.Unlock()
			}
			return nil
		}

		canTryDecode, err := stream.decoder.AddSymbol(uint32(m.Seqno), m.Data)
		if err != nil {
			return fmt.Errorf("failed to add raptorq symbol %d: %w", m.Seqno, err)
		}

		tm := time.Now()
		stream.lastMessageAt = tm
		stream.receivedNum++

		completed := false
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
				_, err = tl.Parse(&res, data, true)
				if err != nil {
					return fmt.Errorf("failed to parse custom message: %w", err)
				}

				var complete tl.Serializable = Complete{
					TransferID: m.TransferID,
					Part:       m.Part,
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
						copy(transferId, m.TransferID)

						go func() {
							if err = handler(transferId, &rVal); err != nil {
								Logger("failed to handle query: ", err)
							}
						}()
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
						req <- rVal.Data
					}
				default:
					log.Println("skipping unwanted rldp message of type", reflect.TypeOf(res).String())
				}
				completed = true
			}
		}

		if !completed && m.Seqno > stream.maxSeqno {
			stream.maxSeqno = m.Seqno

			// send confirm for each 10 packets or after 30 ms
			if stream.lastConfirmAt.Add(20*time.Millisecond).Before(tm) ||
				stream.receivedNumConfirmed+0 < stream.receivedNum {
				var confirm tl.Serializable
				if isV2 {
					confirm = ConfirmV2{
						TransferID:    m.TransferID,
						Part:          m.Part,
						MaxSeqno:      stream.maxSeqno,
						ReceivedMask:  0x7FFFFFFF, // TODO
						ReceivedCount: stream.receivedNum,
					}
				} else {
					confirm = Confirm{
						TransferID: m.TransferID,
						Part:       m.Part,
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
	case Complete: // receiver has fully received transfer, close our stream
		id := string(m.TransferID)

		r.mx.Lock()
		t := r.activeTransfers[id]
		if t != nil {
			delete(r.activeTransfers, id)
		}
		r.mx.Unlock()

		if t != nil {
			close(t)
		}
	case Confirm: // receiver has received some parts
		// TODO: use confirmed seqno to limit sends
		// id := string(m.TransferID)
		// r.mx.RLock()
		// t := r.activeTransfers[id]
		// r.mx.RUnlock()
	default:
		return fmt.Errorf("unexpected message type %s", reflect.TypeOf(m).String())
	}

	return nil
}

func (r *RLDP) sendMessageParts(ctx context.Context, transferId, data []byte) error {
	enc, err := raptorq.NewRaptorQ(_SymbolSize).CreateEncoder(data)
	if err != nil {
		return fmt.Errorf("failed to create raptorq object encoder: %w", err)
	}

	id := string(transferId)

	ch := make(chan bool, 1)
	r.mx.Lock()
	r.activeTransfers[id] = ch
	r.mx.Unlock()

	defer func() {
		r.mx.Lock()
		delete(r.activeTransfers, id)
		r.mx.Unlock()
	}()

	symbolsSent := uint32(0)
	for {
		select {
		case <-ctx.Done():
			// too slow receiver, finish sending
			return ctx.Err()
		case <-ch:
			// we got complete from receiver, finish sending
			return nil
		default:
		}

		fastSymbols := enc.BaseSymbolsNum() + enc.BaseSymbolsNum()/5 // send 120% packets fast

		if symbolsSent > fastSymbols {
			x := (symbolsSent - fastSymbols) / 2
			if x > 70 { // 7 ms max delay
				x = 70
			}

			select {
			case <-ctx.Done():
				// too slow receiver, finish sending
				return ctx.Err()
			case <-ch:
				// we got complete from receiver, finish sending
				return nil
			case <-time.After(time.Duration(x) * (time.Millisecond / 10)):
				// send additional FEC recovery parts until complete
			}
		}

		p := MessagePart{
			TransferID: transferId,
			FecType: FECRaptorQ{
				DataSize:     int32(len(data)),
				SymbolSize:   _SymbolSize,
				SymbolsCount: int32(enc.BaseSymbolsNum()),
			},
			Part:      int32(0),
			TotalSize: int64(len(data)),
			Seqno:     int32(symbolsSent),
			Data:      enc.GenSymbol(symbolsSent),
		}

		var msgPart tl.Serializable = p
		if r.useV2 {
			msgPart = MessagePartV2(p)
		}

		err = r.adnl.SendCustomMessage(ctx, msgPart)
		if err != nil {
			return fmt.Errorf("failed to send message part %d: %w", symbolsSent, err)
		}

		symbolsSent++
	}
}

func (r *RLDP) DoQuery(ctx context.Context, maxAnswerSize int64, query, result tl.Serializable) error {
	timeout, ok := ctx.Deadline()
	if !ok {
		timeout = time.Now().Add(15 * time.Second)
	}

	qid := make([]byte, 32)
	_, err := rand.Read(qid)
	if err != nil {
		return err
	}

	q := &Query{
		ID:            qid,
		MaxAnswerSize: maxAnswerSize,
		Timeout:       int32(timeout.Unix()),
		Data:          query,
	}

	queryID := string(q.ID)

	res := make(chan any, 2)

	r.mx.Lock()
	r.activeRequests[queryID] = res
	r.mx.Unlock()
	defer func() {
		// we need it to delete in case of err
		r.mx.Lock()
		delete(r.activeRequests, queryID)
		r.mx.Unlock()
	}()

	data, err := tl.Serialize(q, true)
	if err != nil {
		return fmt.Errorf("failed to serialize query: %w", err)
	}

	sndCtx, cancel := context.WithDeadline(ctx, timeout)
	defer cancel()

	transferId := make([]byte, 32)
	_, err = rand.Read(transferId)
	if err != nil {
		return err
	}

	go func() {
		err = r.sendMessageParts(sndCtx, transferId, data)
		if err != nil {
			res <- fmt.Errorf("failed to send query parts: %w", err)
		}
	}()

	select {
	case resp := <-res:
		if err, ok = resp.(error); ok {
			return err
		}
		reflect.ValueOf(result).Elem().Set(reflect.ValueOf(resp))
		return nil
	case <-ctx.Done():
		return fmt.Errorf("response deadline exceeded, err: %w", ctx.Err())
	}
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

	if err = r.sendMessageParts(ctx, transferId, data); err != nil {
		return fmt.Errorf("failed to send partitioned answer: %w", err)
	}
	return nil
}
