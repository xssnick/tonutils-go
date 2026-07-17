package overlay

import (
	"context"
	"crypto/ed25519"
	"errors"
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
	Stats() adnl.PeerStats
	Close()
}

type ADNLWrapper struct {
	overlays map[overlayIDKey]*ADNLOverlayWrapper
	mx       sync.RWMutex

	broadcastControls      map[string]*broadcastFECControlHandlers
	nextBroadcastControlID uint64
	// Replaced as a whole under mx and immutable after publication.
	broadcastReceivers []*BroadcastReceiver

	rootQueryHandler      func(msg *adnl.MessageQuery) error
	rootDisconnectHandler func(addr string, key ed25519.PublicKey)
	rootCustomHandler     func(msg *adnl.MessageCustom) error
	unknownOverlayHandler func(msg *adnl.MessageQuery) error

	broadcastReceiverResolver BroadcastReceiverResolver

	ADNL
}

type broadcastFECControlHandler func(peerID []byte, control BroadcastFECControl) bool

type broadcastFECControlHandlers struct {
	registered map[uint64]broadcastFECControlHandler
	snapshot   []broadcastFECControlHandler
}

var ErrBroadcastReceiverNotFound = errors.New("overlay: broadcast receiver not found")
var ErrBroadcastRejected = errors.New("overlay: broadcast rejected")

type BroadcastReceiverResolver func(overlayID []byte) (*BroadcastReceiver, error)

type broadcastFECControlRegistrar interface {
	registerBroadcastFECControl(hash []byte, handler broadcastFECControlHandler) func()
}

func (a *ADNLWrapper) GetDisconnectHandler() func(addr string, key ed25519.PublicKey) {
	return a.ADNL.GetDisconnectHandler()
}

func CreateExtendedADNL(adnl ADNL) *ADNLWrapper {
	w := &ADNLWrapper{
		ADNL:              adnl,
		overlays:          map[overlayIDKey]*ADNLOverlayWrapper{},
		broadcastControls: map[string]*broadcastFECControlHandlers{},
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
		a.broadcastControls = map[string]*broadcastFECControlHandlers{}
	}
	a.nextBroadcastControlID++
	id := a.nextBroadcastControlID
	handlers := a.broadcastControls[key]
	if handlers == nil {
		handlers = &broadcastFECControlHandlers{
			registered: map[uint64]broadcastFECControlHandler{},
		}
		a.broadcastControls[key] = handlers
	}
	handlers.registered[id] = handler
	handlers.rebuildSnapshot()
	a.mx.Unlock()

	return func() {
		a.mx.Lock()
		handlers := a.broadcastControls[key]
		if handlers != nil {
			delete(handlers.registered, id)
			if len(handlers.registered) == 0 {
				delete(a.broadcastControls, key)
			} else {
				handlers.rebuildSnapshot()
			}
		}
		a.mx.Unlock()
	}
}

func (h *broadcastFECControlHandlers) rebuildSnapshot() {
	snapshot := make([]broadcastFECControlHandler, 0, len(h.registered))
	for _, handler := range h.registered {
		snapshot = append(snapshot, handler)
	}
	h.snapshot = snapshot
}

func (a *ADNLWrapper) rebuildBroadcastReceiversLocked() {
	receivers := make([]*BroadcastReceiver, 0, len(a.overlays))
	for _, overlay := range a.overlays {
		receivers = append(receivers, overlay.BroadcastReceiver)
	}
	a.broadcastReceivers = receivers
}

func (a *ADNLWrapper) trackBroadcastFECControl(control BroadcastFECControl) bool {
	a.mx.RLock()
	var handlers []broadcastFECControlHandler
	if registered := a.broadcastControls[string(control.Hash)]; registered != nil {
		handlers = registered.snapshot
	}
	receivers := a.broadcastReceivers
	a.mx.RUnlock()

	handled := false
	peerID := a.GetID()
	for _, handler := range handlers {
		if handler(peerID, control) {
			handled = true
		}
	}
	for _, receiver := range receivers {
		if receiver.trackBroadcastFECRelayControl(peerID, control) {
			handled = true
		}
	}
	return handled
}

func (a *ADNLWrapper) SetQueryHandler(handler func(msg *adnl.MessageQuery) error) {
	a.mx.Lock()
	a.rootQueryHandler = handler
	a.mx.Unlock()
}

func (a *ADNLWrapper) SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey)) {
	a.mx.Lock()
	a.rootDisconnectHandler = handler
	a.mx.Unlock()
}

func (a *ADNLWrapper) SetCustomMessageHandler(handler func(msg *adnl.MessageCustom) error) {
	a.mx.Lock()
	a.rootCustomHandler = handler
	a.mx.Unlock()
}

func (a *ADNLWrapper) SetOnUnknownOverlayQuery(handler func(query *adnl.MessageQuery) error) {
	a.mx.Lock()
	a.unknownOverlayHandler = handler
	a.mx.Unlock()
}

func (a *ADNLWrapper) SetBroadcastReceiverResolver(resolver BroadcastReceiverResolver) {
	a.mx.Lock()
	a.broadcastReceiverResolver = resolver
	a.mx.Unlock()
}

func (a *ADNLWrapper) queryHandler(msg *adnl.MessageQuery) error {
	obj, over := UnwrapQuery(msg.Data)
	if over != nil {
		id, err := newOverlayIDKey(over)
		if err != nil {
			return err
		}
		a.mx.RLock()
		o := a.overlays[id]
		unknown := a.unknownOverlayHandler
		a.mx.RUnlock()
		if o == nil {
			if unknown != nil {
				return unknown(msg)
			}
			return fmt.Errorf("got query for unregistered overlay with id: %x", over)
		}

		switch obj.(type) {
		case Ping, *Ping:
			return a.Answer(context.Background(), msg.ID, Pong{})
		}

		h := o.overlayQueryHandler()
		if h == nil {
			return nil
		}
		return h(&adnl.MessageQuery{ID: msg.ID, Data: obj})
	}

	a.mx.RLock()
	h := a.rootQueryHandler
	a.mx.RUnlock()
	if h == nil {
		return nil
	}
	return h(msg)
}

func (a *ADNLWrapper) disconnectHandler(addr string, key ed25519.PublicKey) {
	var list []func(addr string, key ed25519.PublicKey)

	a.mx.RLock()
	for _, w := range a.overlays {
		dis := w.overlayDisconnectHandler()
		if dis != nil {
			list = append(list, dis)
		}
	}
	dis := a.rootDisconnectHandler
	a.mx.RUnlock()

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
		id, err := newOverlayIDKey(over)
		if err != nil {
			return err
		}
		a.mx.RLock()
		o := a.overlays[id]
		resolver := a.broadcastReceiverResolver
		a.mx.RUnlock()

		if isBroadcastMessage(obj) {
			var receiver *BroadcastReceiver
			if o != nil && o.BroadcastReceiver.IsActive() {
				receiver = o.BroadcastReceiver
			}
			if receiver == nil && resolver != nil {
				receiver, err = resolver(over)
				if err != nil {
					if errors.Is(err, ErrBroadcastReceiverNotFound) {
						return nil
					}
					return fmt.Errorf("resolve broadcast receiver for overlay %x: %w", over, err)
				}
				if receiver == nil {
					return fmt.Errorf("resolve broadcast receiver for overlay %x: resolver returned nil receiver", over)
				}
			}
			if receiver == nil {
				return nil
			}
			if receiver.overlayKey != id {
				return fmt.Errorf("broadcast receiver overlay id mismatch")
			}
			if err = receiver.processMessage(a, obj); err != nil {
				if errors.Is(err, ErrBroadcastRejected) {
					return nil
				}
				return err
			}
			return nil
		}

		if o == nil {
			return fmt.Errorf("got custom message for unregistered overlay with id: %x", over)
		}
		h := o.customMessageHandler()
		if h == nil {
			return nil
		}
		return h(&adnl.MessageCustom{Data: obj})
	}

	switch t := msg.Data.(type) {
	case FECReceived:
		if a.trackBroadcastFECControl(BroadcastFECControl{Hash: t.Hash}) {
			return nil
		}
	case FECCompleted:
		if a.trackBroadcastFECControl(BroadcastFECControl{Hash: t.Hash, Completed: true}) {
			return nil
		}
	}

	a.mx.RLock()
	h := a.rootCustomHandler
	a.mx.RUnlock()
	if h == nil {
		return nil
	}
	return h(msg)
}

func isBroadcastMessage(msg tl.Serializable) bool {
	switch msg.(type) {
	case Broadcast, BroadcastTwoStepSimple, BroadcastTwoStepFEC, BroadcastFECShort, BroadcastFEC:
		return true
	default:
		return false
	}
}

func (r *BroadcastReceiver) processMessage(transport *ADNLWrapper, msg tl.Serializable) error {
	if !r.IsActive() {
		return ErrBroadcastRejected
	}

	w := ADNLOverlayWrapper{
		BroadcastReceiver: r,
		ADNLWrapper:       transport,
	}
	immediatePeerID := transport.GetID()

	switch t := msg.(type) {
	case Broadcast:
		if err := w.processBroadcast(&t, immediatePeerID); err != nil {
			return fmt.Errorf("failed to process broadcast: %w", err)
		}
	case BroadcastTwoStepSimple:
		if !r.isBroadcastTwoStepEnabled() {
			return ErrBroadcastRejected
		}
		if err := w.processBroadcastTwoStepSimple(&t, immediatePeerID); err != nil {
			return fmt.Errorf("failed to process two-step simple broadcast: %w", err)
		}
	case BroadcastTwoStepFEC:
		if !r.isBroadcastTwoStepEnabled() {
			return ErrBroadcastRejected
		}
		if err := w.processBroadcastTwoStepFEC(&t, immediatePeerID); err != nil {
			return fmt.Errorf("failed to process two-step fec broadcast: %w", err)
		}
	case BroadcastFECShort:
		if err := w.processFECBroadcastShort(&t); err != nil {
			return fmt.Errorf("failed to process short FEC broadcast: %w", err)
		}
	case BroadcastFEC:
		if err := w.processFECBroadcast(&t); err != nil {
			return fmt.Errorf("failed to process FEC broadcast: %w", err)
		}
	default:
		return fmt.Errorf("unsupported broadcast message %T", msg)
	}
	return nil
}
