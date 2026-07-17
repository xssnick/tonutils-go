package overlay

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
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

type RLDPWrapper struct {
	mx sync.RWMutex

	overlays map[string]*RLDPOverlayWrapper

	rootQueryHandler      atomic.Pointer[rldpQueryHandler]
	rootDisconnectHandler atomic.Pointer[rldpDisconnectHandler]
	unknownOverlayHandler atomic.Pointer[rldpQueryHandler]

	RLDP
}

type rldpQueryHandler func(transferId []byte, query *rldp.Query) error
type rldpDisconnectHandler func()

func CreateExtendedRLDP(rldp RLDP) *RLDPWrapper {
	w := &RLDPWrapper{
		RLDP:     rldp,
		overlays: map[string]*RLDPOverlayWrapper{},
	}
	w.RLDP.SetOnQuery(w.queryHandler)
	w.GetADNL().SetDisconnectHandler(w.disconnectHandler)
	if _, ok := w.GetADNL().(*ADNLWrapper); ok {
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
	adnlWrapper := r.GetADNL().(*ADNLWrapper)

	obj, err := parseRLDPMessagePayload(data)
	if err != nil {
		return fmt.Errorf("failed to parse rldp message: %w", err)
	}

	return adnlWrapper.customHandler(&adnl.MessageCustom{Data: obj})
}

func parseRLDPMessagePayload(data []byte) (tl.Serializable, error) {
	list := make([]tl.Serializable, 0, 2)
	for len(data) > 0 {
		var obj any
		rest, err := tl.ParseNoCopy(&obj, data, true)
		if err != nil {
			return nil, err
		}
		list = append(list, obj)
		data = rest
	}

	if len(list) == 0 {
		return nil, fmt.Errorf("empty payload")
	}
	if len(list) == 1 {
		return list[0], nil
	}
	return list, nil
}

func (r *RLDPWrapper) queryHandler(transferId []byte, query *rldp.Query) error {
	obj, over := UnwrapQuery(query.Data)
	if over != nil {
		id := hex.EncodeToString(over)
		r.mx.RLock()
		o := r.overlays[id]
		r.mx.RUnlock()
		if o == nil {
			if h := loadRLDPQueryHandler(&r.unknownOverlayHandler); h != nil {
				return h(transferId, query)
			}
			return fmt.Errorf("got query for unregistered overlay with id: %s", id)
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
