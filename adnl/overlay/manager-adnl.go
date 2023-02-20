package overlay

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/adnl/rldp/raptorq"
	"github.com/xssnick/tonutils-go/tl"
	"reflect"
	"sync"
	"time"
)

const _PacketWaitTime = 15 * time.Millisecond

type ADNL interface {
	SetCustomMessageHandler(handler func(msg *adnl.MessageCustom) error)
	SetQueryHandler(handler func(msg *adnl.MessageQuery) error)
	SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey))
	SendCustomMessage(ctx context.Context, req tl.Serializable) error
	Query(ctx context.Context, req, result tl.Serializable) error
	Answer(ctx context.Context, queryID []byte, result tl.Serializable) error
	RemoteAddr() string
	Close()
}

type ADNLWrapper struct {
	overlays map[string]*ADNLOverlayWrapper
	mx       sync.RWMutex

	rootQueryHandler      func(msg *adnl.MessageQuery) error
	rootDisconnectHandler func(addr string, key ed25519.PublicKey)
	rootCustomHandler     func(msg *adnl.MessageCustom) error

	broadcastStreams map[string]*fecBroadcastStream
	streamsMx        sync.RWMutex

	ADNL
}

type fecBroadcastStream struct {
	decoder        *raptorq.Decoder
	finishedAt     *time.Time
	lastMessageAt  time.Time
	lastCompleteAt time.Time
	mx             sync.Mutex
}

func CreateExtendedADNL(adnl ADNL) *ADNLWrapper {
	w := &ADNLWrapper{
		ADNL:             adnl,
		overlays:         map[string]*ADNLOverlayWrapper{},
		broadcastStreams: map[string]*fecBroadcastStream{},
	}
	w.ADNL.SetCustomMessageHandler(w.customHandler)
	w.ADNL.SetDisconnectHandler(w.disconnectHandler)

	return w
}

func (a *ADNLWrapper) SetQueryHandler(handler func(msg *adnl.MessageQuery) error) {
	a.rootQueryHandler = handler
}

func (a *ADNLWrapper) SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey)) {
	a.rootDisconnectHandler = handler
}

func (a *ADNLWrapper) SetCustomMessageHandler(handler func(msg *adnl.MessageCustom) error) {
	a.rootCustomHandler = handler
}

func (a *ADNLWrapper) queryHandler(msg *adnl.MessageQuery) error {
	obj, over := unwrapQuery(msg.Data)
	if over != nil {
		id := hex.EncodeToString(over)
		a.mx.RLock()
		o := a.overlays[id]
		a.mx.RUnlock()
		if o == nil {
			return fmt.Errorf("got query for unregistered overlay with id: %s", id)
		}

		h := o.queryHandler
		if h == nil {
			return nil
		}
		return h(&adnl.MessageQuery{ID: msg.ID, Data: obj})
	}

	h := a.rootQueryHandler
	if h == nil {
		return nil
	}
	return h(msg)
}

func (a *ADNLWrapper) disconnectHandler(addr string, key ed25519.PublicKey) {
	var list []func(addr string, key ed25519.PublicKey)

	a.mx.RLock()
	for _, w := range a.overlays {
		dis := w.disconnectHandler
		if dis != nil {
			list = append(list, dis)
		}
	}
	a.mx.RUnlock()

	dis := a.rootDisconnectHandler
	if dis != nil {
		list = append(list, dis)
	}

	for _, dis = range list {
		dis(addr, key)
	}
}

func (a *ADNLWrapper) customHandler(msg *adnl.MessageCustom) error {
	obj, over := unwrapMessage(msg.Data)
	if over != nil {
		id := hex.EncodeToString(over)
		a.mx.RLock()
		o := a.overlays[id]
		a.mx.RUnlock()
		if o == nil {
			return fmt.Errorf("got custom message for unregistered overlay with id: %s", id)
		}

		switch t := obj.(type) {
		case Broadcast:
			// println("BROADCAST", t.Date)
		case BroadcastFECShort:
			// println("BROADCAST SHORT", t.Seqno, t.BroadcastHash)
		case BroadcastFEC:
			if err := a.processFECBroadcast(o, &t); err != nil {
				return fmt.Errorf("failed to process FEC broadcast: %w", err)
			}
			return nil
		}

		h := o.customHandler
		if h == nil {
			return nil
		}
		return h(&adnl.MessageCustom{Data: obj})
	}

	h := a.rootCustomHandler
	if h == nil {
		return nil
	}
	return h(msg)
}

func (a *ADNLWrapper) processFECBroadcast(ovr *ADNLOverlayWrapper, t *BroadcastFEC) error {
	id := hex.EncodeToString(t.DataHash)
	a.streamsMx.RLock()
	stream := a.broadcastStreams[id]
	a.streamsMx.RUnlock()

	if stream == nil {
		fec, ok := t.FEC.(rldp.FECRaptorQ)
		if !ok {
			return fmt.Errorf("not supported fec type")
		}

		cert, ok := t.Certificate.(Certificate)
		if !ok {
			return fmt.Errorf("not supported cert type %s", reflect.ValueOf(t.Certificate))
		}
		if t.DataSize > cert.MaxSize || t.DataSize <= 0 {
			return fmt.Errorf("bad broadcast fec packet data size")
		}
		// TODO: Validate signature if needed

		dec, err := raptorq.NewRaptorQ(uint32(fec.SymbolSize)).CreateDecoder(uint32(fec.DataSize))
		if err != nil {
			return fmt.Errorf("failed to init raptorq decoder: %w", err)
		}

		stream = &fecBroadcastStream{
			decoder:       dec,
			lastMessageAt: time.Now(),
		}

		a.streamsMx.Lock()
		// check again because of possible concurrency
		if a.broadcastStreams[id] != nil {
			stream = a.broadcastStreams[id]
		} else {
			a.broadcastStreams[id] = stream
		}
		a.streamsMx.Unlock()
	}

	stream.mx.Lock()
	defer stream.mx.Unlock()

	if stream.finishedAt != nil {
		if stream.lastCompleteAt.Add(_PacketWaitTime).Before(time.Now()) { // we not send completions too often, to not get socket buffer overflow
			var complete tl.Serializable = FECCompleted{
				Hash: t.DataHash,
			}

			// got packet for a finished stream, let them know that it is completed, again
			err := a.ADNL.SendCustomMessage(context.Background(), complete)
			if err != nil {
				return fmt.Errorf("failed to send rldp complete message: %w", err)
			}

			a.mx.Lock()
			a.broadcastStreams[id].lastCompleteAt = time.Now()
			a.mx.Unlock()
		}
		return nil
	}

	tm := time.Now()
	stream.lastMessageAt = tm

	canTryDecode, err := stream.decoder.AddSymbol(uint32(t.Seqno), t.Data)
	if err != nil {
		return fmt.Errorf("failed to add raptorq symbol %d: %w", t.Seqno, err)
	}

	if canTryDecode {
		decoded, data, err := stream.decoder.Decode()
		if err != nil {
			return fmt.Errorf("failed to decode raptorq packet: %w", err)
		}

		// it may not be decoded due to unsolvable math system, it means we need more symbols
		if decoded {
			stream.finishedAt = &tm
			stream.decoder = nil

			a.streamsMx.Lock()
			if len(a.broadcastStreams) > 100 {
				for sID, s := range a.broadcastStreams {
					// remove streams that was finished more than 30 sec ago and stuck streams when it was no messages for 60 sec.
					if s.lastMessageAt.Add(60*time.Second).Before(tm) ||
						(s.finishedAt != nil && s.finishedAt.Add(30*time.Second).Before(tm)) {
						delete(a.broadcastStreams, sID)
					}
				}
			}
			a.streamsMx.Unlock()

			var res any
			_, err = tl.Parse(&res, data, true)
			if err != nil {
				return fmt.Errorf("failed to parse decoded broadcast message: %w", err)
			}

			var complete tl.Serializable = FECCompleted{
				Hash: t.DataHash,
			}

			err = a.ADNL.SendCustomMessage(context.Background(), complete)
			if err != nil {
				return fmt.Errorf("failed to send rldp complete message: %w", err)
			}

			// TODO: Add cert validation to broadcast, then enable handler, for now it should not be used, broadcasts are in test mode
			/*if bHandler := ovr.broadcastHandler; bHandler != nil {
				// handle result
				err = bHandler(res)
				if err != nil {
					return fmt.Errorf("failed to process broadcast message: %w", err)
				}
			}*/
		}
	}
	return nil
}
