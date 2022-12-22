package rldp

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
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
	SetCustomMessageHandler(handler func(msg *adnl.MessageCustom) error)
	SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey))
	SendCustomMessage(ctx context.Context, req tl.Serializable) error
	Close()
}

type RLDP struct {
	adnl ADNL

	activeRequests  map[string]chan any
	activeTransfers map[string]chan bool

	recvStreams map[string]*decoderStream

	onQuery      func(query *Query) error
	onDisconnect func()

	mx sync.Mutex
}

type decoderStream struct {
	decoder        *raptorq.Decoder
	finishedAt     *time.Time
	lastCompleteAt *time.Time
	mx             sync.Mutex
}

const _MTU = 1 << 37
const _SymbolSize = 768
const _PacketWaitTime = 15 * time.Millisecond

func NewClient(a ADNL) *RLDP {
	r := &RLDP{
		adnl:            a,
		activeRequests:  map[string]chan any{},
		activeTransfers: map[string]chan bool{},
		recvStreams:     map[string]*decoderStream{},
	}

	a.SetCustomMessageHandler(r.handleMessage)
	a.SetDisconnectHandler(r.handleADNLDisconnect)

	return r
}

func (r *RLDP) SetOnQuery(handler func(query *Query) error) {
	r.onQuery = handler
}

func (r *RLDP) SetOnDisconnect(handler func()) {
	r.onDisconnect = handler
}

func (r *RLDP) Close() {
	r.adnl.Close()
}

func (r *RLDP) handleADNLDisconnect(addr string, key ed25519.PublicKey) {
	disc := r.onDisconnect
	if disc != nil {
		disc()
	}
}

func (r *RLDP) handleMessage(msg *adnl.MessageCustom) error {
	switch m := msg.Data.(type) {
	case MessagePart:
		fec, ok := m.FecType.(FECRaptorQ)
		if !ok {
			return fmt.Errorf("not supported fec type")
		}

		id := hex.EncodeToString(m.TransferID)
		r.mx.Lock()
		stream := r.recvStreams[id]
		r.mx.Unlock()

		if stream == nil {
			if m.TotalSize > _MTU || m.TotalSize <= 0 {
				return fmt.Errorf("bad rldp packet total size")
			}
			dec, err := raptorq.NewRaptorQ(uint32(fec.SymbolSize)).CreateDecoder(uint32(fec.DataSize))
			if err != nil {
				return fmt.Errorf("failed to init raptorq decoder: %w", err)
			}
			stream = &decoderStream{
				decoder: dec,
			}

			r.mx.Lock()
			r.recvStreams[id] = stream
			r.mx.Unlock()
		} else if stream.finishedAt != nil {
			if stream.lastCompleteAt == nil ||
				stream.lastCompleteAt.Add(_PacketWaitTime).Before(time.Now()) { // we not send completions too often, to not get socket buffer overflow
				// got packet for a finished stream, let them know that it is completed, again
				err := r.adnl.SendCustomMessage(context.Background(), Complete{
					TransferID: m.TransferID,
					Part:       m.Part,
				})
				if err != nil {
					return fmt.Errorf("failed to send rldp complete message: %w", err)
				}

				tm := time.Now()
				r.mx.Lock()
				r.recvStreams[id].lastCompleteAt = &tm
				r.mx.Unlock()
			}
			return nil
		}

		canTryDecode, err := stream.decoder.AddSymbol(uint32(m.Seqno), m.Data)
		if err != nil {
			return fmt.Errorf("failed to add raptorq symbol %d: %w", m.Seqno, err)
		}

		if canTryDecode {
			decoded, data, err := stream.decoder.Decode()
			if err != nil {
				return fmt.Errorf("failed to decode raptorq packet: %w", err)
			}

			// it may not be decoded due to unsolvable math system, it means we need more symbols
			if decoded {
				tm := time.Now()
				r.mx.Lock()
				r.recvStreams[id] = &decoderStream{finishedAt: &tm}
				if len(r.recvStreams) > 100 {
					for sID, s := range r.recvStreams {
						// remove streams that was finished more than 30 sec ago.
						if s.finishedAt != nil && s.finishedAt.Add(30*time.Second).Before(time.Now()) {
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

				// TODO: add multiple parts support (check if applicable)
				err = r.adnl.SendCustomMessage(context.Background(), Complete{
					TransferID: m.TransferID,
					Part:       m.Part,
				})
				if err != nil {
					return fmt.Errorf("failed to send rldp complete message: %w", err)
				}

				switch rVal := res.(type) {
				case Query:
					handler := r.onQuery
					if handler != nil {
						go func() {
							if err = handler(&rVal); err != nil {
								log.Println("failed to handle query: ", err)
							}
						}()
					}
				case Answer:
					qid := hex.EncodeToString(rVal.ID)

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
			}
		}
	case Complete: // receiver has fully received transfer, close our stream
		id := hex.EncodeToString(m.TransferID)

		r.mx.Lock()
		t := r.activeTransfers[id]
		if t != nil {
			delete(r.activeTransfers, id)
		}
		r.mx.Unlock()

		if t != nil {
			t <- true
		}
	default:
		return fmt.Errorf("unexpected message type %s", reflect.TypeOf(m).String())
	}

	return nil
}

func (r *RLDP) sendMessageParts(ctx context.Context, data []byte) error {
	enc, err := raptorq.NewRaptorQ(_SymbolSize).CreateEncoder(data)
	if err != nil {
		return fmt.Errorf("failed to create raptorq object encoder: %w", err)
	}

	tid := make([]byte, 32)
	_, err = rand.Read(tid)
	if err != nil {
		return err
	}

	id := hex.EncodeToString(tid)

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

		if symbolsSent > enc.BaseSymbolsNum()+enc.BaseSymbolsNum()/2 { //+enc.BaseSymbolsNum()/2
			x := symbolsSent - (enc.BaseSymbolsNum() + enc.BaseSymbolsNum()/2)

			select {
			case <-ctx.Done():
				// too slow receiver, finish sending
				return ctx.Err()
			case <-ch:
				// we got complete from receiver, finish sending
				return nil
			case <-time.After(time.Duration(x) * _PacketWaitTime):
				// send additional FEC recovery parts until complete
			}
		}

		p := MessagePart{
			TransferID: tid,
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

		err = r.adnl.SendCustomMessage(ctx, p)
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

	queryID := hex.EncodeToString(q.ID)

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

	go func() {
		err = r.sendMessageParts(sndCtx, data)
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

func (r *RLDP) SendAnswer(ctx context.Context, maxAnswerSize int64, queryID []byte, answer tl.Serializable) error {
	a := Answer{
		ID:   queryID,
		Data: answer,
	}

	data, err := tl.Serialize(a, true)
	if err != nil {
		return fmt.Errorf("failed to serialize query: %w", err)
	}

	if int64(len(data)) > maxAnswerSize {
		return fmt.Errorf("too big answer for that client, client wants no more than %d bytes", maxAnswerSize)
	}

	if err = r.sendMessageParts(ctx, data); err != nil {
		return fmt.Errorf("failed to send partitioned answer: %w", err)
	}
	return nil
}
