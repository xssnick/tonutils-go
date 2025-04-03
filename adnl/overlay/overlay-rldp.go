package overlay

import (
	"context"
	"encoding/hex"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
)

type RLDPOverlayWrapper struct {
	overlayId []byte

	queryHandler      func(transferId []byte, query *rldp.Query) error
	disconnectHandler func()

	*RLDPWrapper
}

func (r *RLDPWrapper) CreateOverlay(id []byte) *RLDPOverlayWrapper {
	r.mx.Lock()
	defer r.mx.Unlock()

	strId := hex.EncodeToString(id)

	w := r.overlays[strId]
	if w != nil {
		return w
	}
	w = &RLDPOverlayWrapper{
		overlayId:   id,
		RLDPWrapper: r,
	}
	r.overlays[strId] = w

	return w
}

func (r *RLDPWrapper) UnregisterOverlay(id []byte) {
	r.mx.Lock()
	defer r.mx.Unlock()

	delete(r.overlays, hex.EncodeToString(id))
}

func (r *RLDPOverlayWrapper) SetOnQuery(handler func(transferId []byte, query *rldp.Query) error) {
	r.queryHandler = handler
}

func (r *RLDPOverlayWrapper) SetOnDisconnect(handler func()) {
	r.disconnectHandler = handler
}

func (r *RLDPOverlayWrapper) DoQuery(ctx context.Context, maxAnswerSize uint64, req, result tl.Serializable) error {
	return r.RLDPWrapper.DoQuery(ctx, maxAnswerSize, []tl.Serializable{Query{Overlay: r.overlayId}, req}, result)
}

func (r *RLDPOverlayWrapper) Close() {
	r.RLDPWrapper.UnregisterOverlay(r.overlayId)
}
