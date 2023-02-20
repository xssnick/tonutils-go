package overlay

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
)

type ADNLOverlayWrapper struct {
	overlayId []byte

	customHandler     func(msg *adnl.MessageCustom) error
	queryHandler      func(msg *adnl.MessageQuery) error
	disconnectHandler func(addr string, key ed25519.PublicKey)

	broadcastHandler func(msg tl.Serializable) error

	*ADNLWrapper
}

func (a *ADNLWrapper) CreateOverlay(id []byte) *ADNLOverlayWrapper {
	a.mx.Lock()
	defer a.mx.Unlock()

	strId := hex.EncodeToString(id)

	w := a.overlays[strId]
	if w != nil {
		return w
	}
	w = &ADNLOverlayWrapper{
		overlayId:   id,
		ADNLWrapper: a,
	}
	a.overlays[strId] = w

	return w
}

func (a *ADNLWrapper) UnregisterOverlay(id []byte) {
	a.mx.Lock()
	defer a.mx.Unlock()

	delete(a.overlays, hex.EncodeToString(id))
}

func (a *ADNLOverlayWrapper) SendCustomMessage(ctx context.Context, req tl.Serializable) error {
	return a.ADNLWrapper.SendCustomMessage(ctx, []tl.Serializable{Query{Overlay: a.overlayId}, req})
}

func (a *ADNLOverlayWrapper) Query(ctx context.Context, req, result tl.Serializable) error {
	return a.ADNLWrapper.Query(ctx, []tl.Serializable{Query{Overlay: a.overlayId}, req}, result)
}

func (a *ADNLOverlayWrapper) SetCustomMessageHandler(handler func(msg *adnl.MessageCustom) error) {
	a.customHandler = handler
}

func (a *ADNLOverlayWrapper) SetQueryHandler(handler func(msg *adnl.MessageQuery) error) {
	a.queryHandler = handler
}

func (a *ADNLOverlayWrapper) SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey)) {
	a.disconnectHandler = handler
}

func (a *ADNLOverlayWrapper) SetBroadcastHandler(handler func(msg tl.Serializable) error) {
	a.broadcastHandler = handler
}

func (a *ADNLOverlayWrapper) Close() {
	a.ADNLWrapper.UnregisterOverlay(a.overlayId)
}
