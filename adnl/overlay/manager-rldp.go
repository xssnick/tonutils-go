package overlay

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
)

type RLDP interface {
	GetADNL() rldp.ADNL
	GetRateInfo() (left int64, total int64)
	Stats() rldp.Stats
	Close()
	DoQuery(ctx context.Context, maxAnswerSize uint64, query, result tl.Serializable) error
	DoQueryAsync(ctx context.Context, maxAnswerSize uint64, id []byte, query tl.Serializable, result chan<- rldp.AsyncQueryResult) error
	SetOnQuery(handler func(transferId []byte, query *rldp.Query) error)
	SetOnMessage(handler func(id []byte, data []byte) error)
	SetOnDisconnect(handler func())
	SendAnswer(ctx context.Context, maxAnswerSize uint64, timeoutAt uint32, queryId, transferId []byte, answer tl.Serializable) error
}

// RLDPMessageSender is implemented by RLDP transports that support
// fire-and-forget messages in addition to the released RLDP query API.
type RLDPMessageSender interface {
	SendMessage(ctx context.Context, payload []byte) error
}

// ErrRLDPMessageUnsupported is returned when an RLDP transport implements the
// released query API but not the optional message-sending capability.
var ErrRLDPMessageUnsupported = errors.New("overlay: RLDP transport does not support messages")

type RLDPWrapper struct {
	mx sync.RWMutex

	// CreateOverlay historically accepts IDs of any length. Keep exact byte
	// identity in a raw string key while avoiding the old 64-byte hex key and
	// its encoding allocation for normal 32-byte overlay IDs.
	overlays map[string]*RLDPOverlayWrapper

	messageSender         RLDPMessageSender
	adnlWrapper           *ADNLWrapper
	rootQueryHandler      atomic.Pointer[rldpQueryHandler]
	rootDisconnectHandler atomic.Pointer[rldpDisconnectHandler]
	unknownOverlayHandler atomic.Pointer[rldpQueryHandler]

	RLDP
}

type rldpQueryHandler func(transferId []byte, query *rldp.Query) error
type rldpDisconnectHandler func()

type rldpBroadcastPeer struct {
	transport *RLDPWrapper
	overlayID []byte
}

func (p rldpBroadcastPeer) ID() []byte {
	return p.transport.GetADNL().GetID()
}

func (p rldpBroadcastPeer) SendCustomMessage(ctx context.Context, req tl.Serializable) error {
	return p.transport.sendOverlayMessage(ctx, p.overlayID, req)
}

func (p rldpBroadcastPeer) SendPreparedCustomMessage(ctx context.Context, body []byte) error {
	return p.transport.sendOverlayMessage(ctx, p.overlayID, tl.Raw(body))
}

func CreateExtendedRLDP(rldp RLDP) *RLDPWrapper {
	messageSender, _ := rldp.(RLDPMessageSender)
	w := &RLDPWrapper{
		RLDP:          rldp,
		messageSender: messageSender,
		overlays:      map[string]*RLDPOverlayWrapper{},
	}
	w.RLDP.SetOnQuery(w.queryHandler)
	w.GetADNL().SetDisconnectHandler(w.disconnectHandler)
	if adnlWrapper, ok := w.GetADNL().(*ADNLWrapper); ok {
		w.adnlWrapper = adnlWrapper
		w.RLDP.SetOnMessage(w.messageHandler)
	}

	return w
}

func (r *RLDPWrapper) SetOnQuery(handler func(transferId []byte, query *rldp.Query) error) {
	storeRLDPQueryHandler(&r.rootQueryHandler, handler)
}

func (r *RLDPWrapper) SetOnUnknownOverlayQuery(handler func(transferId []byte, query *rldp.Query) error) {
	storeRLDPQueryHandler(&r.unknownOverlayHandler, handler)
}

func (r *RLDPWrapper) SetOnDisconnect(handler func()) {
	storeRLDPDisconnectHandler(&r.rootDisconnectHandler, handler)
}

func storeRLDPQueryHandler(target *atomic.Pointer[rldpQueryHandler], handler func(transferId []byte, query *rldp.Query) error) {
	if handler == nil {
		target.Store(nil)
		return
	}

	h := rldpQueryHandler(handler)
	target.Store(&h)
}

func storeRLDPDisconnectHandler(target *atomic.Pointer[rldpDisconnectHandler], handler func()) {
	if handler == nil {
		target.Store(nil)
		return
	}

	h := rldpDisconnectHandler(handler)
	target.Store(&h)
}

func loadRLDPQueryHandler(source *atomic.Pointer[rldpQueryHandler]) func(transferId []byte, query *rldp.Query) error {
	handler := source.Load()
	if handler == nil {
		return nil
	}
	return *handler
}

func loadRLDPDisconnectHandler(source *atomic.Pointer[rldpDisconnectHandler]) func() {
	handler := source.Load()
	if handler == nil {
		return nil
	}
	return *handler
}

func (r *RLDPWrapper) messageHandler(_ []byte, data []byte) error {
	obj, err := parseRLDPMessagePayload(data)
	if err != nil {
		return fmt.Errorf("failed to parse rldp message: %w", err)
	}

	_, overlayID := UnwrapMessage(obj)
	peer := rldpBroadcastPeer{
		transport: r,
		overlayID: overlayID,
	}

	return r.adnlWrapper.handleCustomMessage(peer, &adnl.MessageCustom{Data: obj})
}

func parseRLDPMessagePayload(data []byte) (tl.Serializable, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty payload")
	}

	var envelope any
	rest, err := tl.ParseNoCopy(&envelope, data, true)
	if err != nil {
		return nil, err
	}
	if len(rest) == 0 {
		return envelope, nil
	}

	if _, ok := envelope.(Message); !ok {
		return nil, fmt.Errorf("multiple objects require an overlay message envelope")
	}

	var body any
	rest, err = tl.ParseNoCopy(&body, rest, true)
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("unexpected trailing data after overlay message body")
	}

	return []tl.Serializable{envelope, body}, nil
}

func (r *RLDPWrapper) queryHandler(transferId []byte, query *rldp.Query) error {
	obj, over := UnwrapQuery(query.Data)
	if over != nil {
		id := string(over)
		r.mx.RLock()
		o := r.overlays[id]
		r.mx.RUnlock()
		if o == nil {
			if h := loadRLDPQueryHandler(&r.unknownOverlayHandler); h != nil {
				return h(transferId, query)
			}
			return fmt.Errorf("got query for unregistered overlay with id: %x", over)
		}

		h := o.overlayQueryHandler()
		if h == nil {
			return nil
		}
		return h(transferId, &rldp.Query{
			ID:            query.ID,
			MaxAnswerSize: query.MaxAnswerSize,
			Timeout:       query.Timeout,
			Data:          obj,
		})
	}

	h := loadRLDPQueryHandler(&r.rootQueryHandler)
	if h == nil {
		return nil
	}
	return h(transferId, query)
}

func (r *RLDPWrapper) disconnectHandler(addr string, key ed25519.PublicKey) {
	var list []func()

	r.mx.RLock()
	for _, w := range r.overlays {
		dis := w.overlayDisconnectHandler()
		if dis != nil {
			list = append(list, dis)
		}
	}
	r.mx.RUnlock()

	dis := loadRLDPDisconnectHandler(&r.rootDisconnectHandler)
	if dis != nil {
		list = append(list, dis)
	}

	for _, dis = range list {
		dis()
	}
}
