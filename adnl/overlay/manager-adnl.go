package overlay

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
	"sync"
)

const _CertFlagAllowFEC = 1
const _CertFlagTrusted = 2

type ADNL interface {
	SetCustomMessageHandler(handler func(msg *adnl.MessageCustom) error)
	SetQueryHandler(handler func(msg *adnl.MessageQuery) error)
	SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey))
	GetDisconnectHandler() func(addr string, key ed25519.PublicKey)
	SendCustomMessage(ctx context.Context, req tl.Serializable) error
	Query(ctx context.Context, req, result tl.Serializable) error
	Answer(ctx context.Context, queryID []byte, result tl.Serializable) error
	GetCloserCtx() context.Context
	RemoteAddr() string
	GetID() []byte
	Close()
}

type ADNLWrapper struct {
	overlays map[string]*ADNLOverlayWrapper
	mx       sync.RWMutex

	broadcastControls      map[string]map[uint64]broadcastFECControlHandler
	nextBroadcastControlID uint64

	rootQueryHandler      func(msg *adnl.MessageQuery) error
	rootDisconnectHandler func(addr string, key ed25519.PublicKey)
	rootCustomHandler     func(msg *adnl.MessageCustom) error
	unknownOverlayHandler func(msg *adnl.MessageQuery) error

	ADNL
}

type broadcastFECControlHandler func(peerID []byte, msg tl.Serializable) bool

type broadcastFECControlRegistrar interface {
	registerBroadcastFECControl(hash []byte, handler broadcastFECControlHandler) func()
}

func (a *ADNLWrapper) GetDisconnectHandler() func(addr string, key ed25519.PublicKey) {
	return a.ADNL.GetDisconnectHandler()
}

func CreateExtendedADNL(adnl ADNL) *ADNLWrapper {
	w := &ADNLWrapper{
		ADNL:              adnl,
		overlays:          map[string]*ADNLOverlayWrapper{},
		broadcastControls: map[string]map[uint64]broadcastFECControlHandler{},
	}
	w.ADNL.SetQueryHandler(w.queryHandler)
	w.ADNL.SetCustomMessageHandler(w.customHandler)
	w.ADNL.SetDisconnectHandler(w.disconnectHandler)

	return w
}

func (a *ADNLWrapper) registerBroadcastFECControl(hash []byte, handler broadcastFECControlHandler) func() {
	key := string(hash)

	a.mx.Lock()
	if a.broadcastControls == nil {
		a.broadcastControls = map[string]map[uint64]broadcastFECControlHandler{}
	}
	a.nextBroadcastControlID++
	id := a.nextBroadcastControlID
	handlers := a.broadcastControls[key]
	if handlers == nil {
		handlers = map[uint64]broadcastFECControlHandler{}
		a.broadcastControls[key] = handlers
	}
	handlers[id] = handler
	a.mx.Unlock()

	return func() {
		a.mx.Lock()
		handlers := a.broadcastControls[key]
		if handlers != nil {
			delete(handlers, id)
			if len(handlers) == 0 {
				delete(a.broadcastControls, key)
			}
		}
		a.mx.Unlock()
	}
}

func broadcastFECControlHash(msg tl.Serializable) []byte {
	switch t := msg.(type) {
	case FECReceived:
		return t.Hash
	case *FECReceived:
		return t.Hash
	case FECCompleted:
		return t.Hash
	case *FECCompleted:
		return t.Hash
	default:
		return nil
	}
}

func (a *ADNLWrapper) trackBroadcastFECControl(msg tl.Serializable) bool {
	hash := broadcastFECControlHash(msg)
	if hash == nil {
		return false
	}

	a.mx.RLock()
	handlersMap := a.broadcastControls[string(hash)]
	handlers := make([]broadcastFECControlHandler, 0, len(handlersMap))
	for _, handler := range handlersMap {
		handlers = append(handlers, handler)
	}
	a.mx.RUnlock()

	if len(handlers) == 0 {
		return false
	}

	handled := false
	peerID := a.GetID()
	for _, handler := range handlers {
		if handler(peerID, msg) {
			handled = true
		}
	}
	return handled
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
		case BroadcastFECShort:
			if err := o.processFECBroadcastShort(&t); err != nil {
				return fmt.Errorf("failed to process short FEC broadcast: %w", err)
			}
			return nil
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

	if a.trackBroadcastFECControl(msg.Data) {
		return nil
	}

	h := a.rootCustomHandler
	if h == nil {
		return nil
	}
	return h(msg)
}
