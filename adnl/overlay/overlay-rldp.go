package overlay

import (
	"context"
	"encoding/hex"
	"sync/atomic"

	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
)

type RLDPOverlayWrapper struct {
	overlayId []byte
	closed    atomic.Bool

	queryHandler      atomic.Pointer[rldpQueryHandler]
	disconnectHandler atomic.Pointer[rldpDisconnectHandler]

	*RLDPWrapper
}

func (r *RLDPWrapper) CreateOverlay(id []byte) *RLDPOverlayWrapper {
	r.mx.Lock()
	defer r.mx.Unlock()

	strId := hex.EncodeToString(id)

	w := r.overlays[strId]
	if w != nil && !w.closed.Load() {
		return w
	}
	w = &RLDPOverlayWrapper{
		overlayId:   append([]byte(nil), id...),
		RLDPWrapper: r,
	}
	r.overlays[strId] = w

	return w
}

func (r *RLDPWrapper) detachOverlay(overlay *RLDPOverlayWrapper) {
	r.mx.Lock()
	defer r.mx.Unlock()

	id := hex.EncodeToString(overlay.overlayId)
	if r.overlays[id] == overlay {
		delete(r.overlays, id)
	}
}

func (r *RLDPOverlayWrapper) SetOnQuery(handler func(transferId []byte, query *rldp.Query) error) {
	storeRLDPQueryHandler(&r.queryHandler, handler)
}

func (r *RLDPOverlayWrapper) SetOnDisconnect(handler func()) {
	storeRLDPDisconnectHandler(&r.disconnectHandler, handler)
}

func (r *RLDPOverlayWrapper) overlayQueryHandler() func(transferId []byte, query *rldp.Query) error {
	return loadRLDPQueryHandler(&r.queryHandler)
}

func (r *RLDPOverlayWrapper) overlayDisconnectHandler() func() {
	return loadRLDPDisconnectHandler(&r.disconnectHandler)
}

func (r *RLDPOverlayWrapper) DoQuery(ctx context.Context, maxAnswerSize uint64, req, result tl.Serializable) error {
	return r.RLDPWrapper.DoQuery(ctx, maxAnswerSize, []tl.Serializable{Query{Overlay: r.overlayId}, req}, result)
}

func (r *RLDPOverlayWrapper) Close() {
	r.closed.Store(true)
	r.RLDPWrapper.detachOverlay(r)
}
