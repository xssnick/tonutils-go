package overlay

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
	"reflect"
	"sync"
	"time"
)

const _PacketWaitTime = 15 * time.Millisecond

const _BroadcastFlagAnySender = 1

const _CertFlagAllowFEC = 1
const _CertFlagTrusted = 2

type ADNL interface {
	SetCustomMessageHandler(handler func(msg *adnl.MessageCustom) error)
	SetQueryHandler(handler func(msg *adnl.MessageQuery) error)
	SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey))
	SendCustomMessage(ctx context.Context, req tl.Serializable) error
	Query(ctx context.Context, req, result tl.Serializable) error
	Answer(ctx context.Context, queryID []byte, result tl.Serializable) error
	RemoteAddr() string
	GetID() []byte
	Close()
}

type ADNLWrapper struct {
	overlays map[string]*ADNLOverlayWrapper
	mx       sync.RWMutex

	rootQueryHandler      func(msg *adnl.MessageQuery) error
	rootDisconnectHandler func(addr string, key ed25519.PublicKey)
	rootCustomHandler     func(msg *adnl.MessageCustom) error
	unknownOverlayHandler func(msg *adnl.MessageQuery) error

	ADNL
}

func CreateExtendedADNL(adnl ADNL) *ADNLWrapper {
	w := &ADNLWrapper{
		ADNL:     adnl,
		overlays: map[string]*ADNLOverlayWrapper{},
	}
	w.ADNL.SetQueryHandler(w.queryHandler)
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

func (a *ADNLWrapper) SetOnUnknownOverlayQuery(handler func(query *adnl.MessageQuery) error) {
	a.unknownOverlayHandler = handler
}

func (a *ADNLWrapper) queryHandler(msg *adnl.MessageQuery) error {
	obj, over := UnwrapQuery(msg.Data)
	if over != nil {
		id := hex.EncodeToString(over)
		a.mx.RLock()
		o := a.overlays[id]
		a.mx.RUnlock()
		if o == nil {
			if h := a.unknownOverlayHandler; h != nil {
				return h(msg)
			}
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
	obj, over := UnwrapMessage(msg.Data)
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
			var gh any
			_, _ = tl.Parse(&gh, t.Data, true)
			println("BROADCAST", reflect.TypeOf(gh).String())
		case BroadcastFECShort:
			println("BROADCAST SHORT", t.Seqno, t.BroadcastHash)
		case BroadcastFEC:
			if err := o.processFECBroadcast(&t); err != nil {
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
