package overlay

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
	"sync"
)

type RLDP interface {
	GetADNL() rldp.ADNL
	GetRateInfo() (left int64, total int64)
	Close()
	DoQuery(ctx context.Context, maxAnswerSize uint64, query, result tl.Serializable) error
	DoQueryAsync(ctx context.Context, maxAnswerSize uint64, id []byte, query tl.Serializable, result chan<- rldp.AsyncQueryResult) error
	SetOnQuery(handler func(transferId []byte, query *rldp.Query) error)
	SetOnDisconnect(handler func())
	SendAnswer(ctx context.Context, maxAnswerSize uint64, timeoutAt uint32, queryId, transferId []byte, answer tl.Serializable) error
}

type RLDPWrapper struct {
	mx sync.RWMutex

	overlays map[string]*RLDPOverlayWrapper

	rootQueryHandler      func(transferId []byte, query *rldp.Query) error
	rootDisconnectHandler func()
	unknownOverlayHandler func(transferId []byte, query *rldp.Query) error

	RLDP
}

func CreateExtendedRLDP(rldp RLDP) *RLDPWrapper {
	w := &RLDPWrapper{
		RLDP:     rldp,
		overlays: map[string]*RLDPOverlayWrapper{},
	}
	w.RLDP.SetOnQuery(w.queryHandler)
	w.GetADNL().SetDisconnectHandler(w.disconnectHandler)

	return w
}

func (r *RLDPWrapper) SetOnQuery(handler func(transferId []byte, query *rldp.Query) error) {
	r.rootQueryHandler = handler
}

func (r *RLDPWrapper) SetOnUnknownOverlayQuery(handler func(transferId []byte, query *rldp.Query) error) {
	r.unknownOverlayHandler = handler
}

func (r *RLDPWrapper) SetOnDisconnect(handler func()) {
	r.rootDisconnectHandler = handler
}

func (r *RLDPWrapper) queryHandler(transferId []byte, query *rldp.Query) error {
	obj, over := UnwrapQuery(query.Data)
	if over != nil {
		id := hex.EncodeToString(over)
		r.mx.RLock()
		o := r.overlays[id]
		r.mx.RUnlock()
		if o == nil {
			if h := r.unknownOverlayHandler; h != nil {
				return h(transferId, query)
			}
			return fmt.Errorf("got query for unregistered overlay with id: %s", id)
		}

		h := o.queryHandler
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

	h := r.rootQueryHandler
	if h == nil {
		return nil
	}
	return h(transferId, query)
}

func (r *RLDPWrapper) disconnectHandler(addr string, key ed25519.PublicKey) {
	var list []func()

	r.mx.RLock()
	for _, w := range r.overlays {
		dis := w.disconnectHandler
		if dis != nil {
			list = append(list, dis)
		}
	}
	r.mx.RUnlock()

	dis := r.rootDisconnectHandler
	if dis != nil {
		list = append(list, dis)
	}

	for _, dis = range list {
		dis()
	}
}
