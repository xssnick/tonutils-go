package rldp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/liteclient/adnl"
	"github.com/xssnick/tonutils-go/liteclient/adnl/rldp/raptorq"
	"github.com/xssnick/tonutils-go/tl"
	"log"
	"reflect"
	"sync"
	"time"
)

func init() {
	tl.Register(Query{}, "rldp.query query_id:int256 max_answer_size:long timeout:int data:bytes = rldp.Message")
	tl.Register(Answer{}, "rldp.answer query_id:int256 data:bytes = rldp.Message")
	tl.Register(Message{}, "rldp.message id:int256 data:bytes = rldp.Message")
	tl.Register(Confirm{}, "rldp.confirm transfer_id:int256 part:int seqno:int = rldp.MessagePart")
	tl.Register(Complete{}, "rldp.complete transfer_id:int256 part:int = rldp.MessagePart")
	tl.Register(MessagePart{}, "rldp.messagePart transfer_id:int256 fec_type:fec.Type part:int total_size:long seqno:int data:bytes = rldp.MessagePart")
}

const _SymbolSize = 768
const _PacketWaitTime = 25 * time.Millisecond

type Query struct {
	ID            []byte `tl:"int256"`
	MaxAnswerSize int64  `tl:"long"`
	Timeout       int32  `tl:"int"`
	Data          any    `tl:"bytes struct boxed"`
}

type Answer struct {
	ID   []byte `tl:"int256"`
	Data any    `tl:"bytes struct boxed"`
}

type Message struct {
	ID   []byte `tl:"int256"`
	Data []byte `tl:"bytes"`
}

type Confirm struct {
	TransferID []byte `tl:"int256"`
	Part       int32  `tl:"int"`
	Seqno      int32  `tl:"int"`
}

type Complete struct {
	TransferID []byte `tl:"int256"`
	Part       int32  `tl:"int"`
}

type MessagePart struct {
	TransferID []byte `tl:"int256"`
	FecType    any    `tl:"struct boxed [fec.roundRobin,fec.raptorQ,fec.online]"`
	Part       int32  `tl:"int"`
	TotalSize  int64  `tl:"long"`
	Seqno      int32  `tl:"int"`
	Data       []byte `tl:"bytes"`
}

type RLDP struct {
	adnl *adnl.ADNL

	activeRequests  map[string]chan any
	activeTransfers map[string]chan bool

	recvStreams map[string]*decoderStream // TODO: cleanup old

	onQuery func(query *Query) error

	mx sync.Mutex
}

type decoderStream struct {
	decoder        *raptorq.Decoder
	finishedAt     *time.Time
	lastCompleteAt *time.Time
	mx             sync.Mutex
}

func NewRLDP(a *adnl.ADNL) *RLDP {
	r := &RLDP{
		adnl:            a,
		activeRequests:  map[string]chan any{},
		activeTransfers: map[string]chan bool{},
		recvStreams:     map[string]*decoderStream{},
	}

	a.SetCustomMessageHandler(r.handleMessage)
	return r
}

func (r *RLDP) SetOnQuery(handler func(query *Query) error) {
	r.onQuery = handler
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

		if m.Seqno < 5 {
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

	i := uint32(0)
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

		if i > enc.BaseSymbolsNum() {
			// send additional FEC recovery parts until complete
			time.Sleep(_PacketWaitTime)
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
			Seqno:     int32(i),
			Data:      enc.GenSymbol(i),
		}

		err = r.adnl.SendCustomMessage(ctx, p)
		if err != nil {
			return fmt.Errorf("failed to send message part %d: %w", i, err)
		}

		i++
	}
}

func (r *RLDP) DoQuery(ctx context.Context, maxAnswerSize int64, query, result tl.Serializable) error {
	timeout, ok := ctx.Deadline()
	if !ok {
		timeout = time.Now().Add(30 * time.Second)
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

	err = r.sendMessageParts(ctx, data)
	if err != nil {
		return fmt.Errorf("failed to send query parts: %w", err)
	}

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
