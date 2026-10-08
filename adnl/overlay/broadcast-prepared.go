package overlay

import (
	"bytes"
	"context"
	"fmt"
	"sync/atomic"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
)

// PreparedBroadcastMessage is a broadcast object (overlay.broadcast,
// broadcastFec, broadcastFecShort) serialized once to its canonical boxed TL
// body, together with the ADNL custom message built around that body for one
// overlay. A FEC part fans out to several peers of the same overlay; the body
// was already shared between those sends, but each of them still framed it
// again (overlay.message + body inside adnl.message.custom) through a staging
// buffer. The frame is built on the first send to an overlay and reused by
// every peer of it, so the fanout serializes once and only the per-peer packet
// (seqno, padding, channel encryption) is produced per destination.
//
// The body and the cached frame are immutable; the message is safe to share
// between goroutines.
type PreparedBroadcastMessage struct {
	body  []byte
	frame atomic.Pointer[preparedBroadcastFrame]
}

type preparedBroadcastFrame struct {
	overlayID []byte
	message   *adnl.PreparedCustomMessage
}

// NewPreparedBroadcastMessage wraps an already serialized boxed broadcast
// body. The body must not be modified afterwards.
func NewPreparedBroadcastMessage(body []byte) *PreparedBroadcastMessage {
	return &PreparedBroadcastMessage{body: body}
}

// PrepareBroadcastMessage serializes message to its canonical boxed form.
func PrepareBroadcastMessage(message tl.Serializable) (*PreparedBroadcastMessage, error) {
	body, err := tl.Serialize(message, true)
	if err != nil {
		return nil, fmt.Errorf("serialize broadcast message: %w", err)
	}
	return NewPreparedBroadcastMessage(body), nil
}

// Body returns the boxed TL body. It must not be modified.
func (m *PreparedBroadcastMessage) Body() []byte {
	return m.body
}

// ADNLMessage returns the adnl.message.custom that carries the body into
// overlayID: the bytes SendCustomMessage builds for
// []tl.Serializable{Message{Overlay: overlayID}, tl.Raw(body)}. The frame for
// the most recent overlay is cached; a message relayed into one overlay, which
// is every broadcast fanout, builds it once.
func (m *PreparedBroadcastMessage) ADNLMessage(overlayID []byte) (*adnl.PreparedCustomMessage, error) {
	if frame := m.frame.Load(); frame != nil && bytes.Equal(frame.overlayID, overlayID) {
		return frame.message, nil
	}

	prefix, err := tl.Serialize(Message{Overlay: overlayID}, true)
	if err != nil {
		return nil, fmt.Errorf("serialize overlay message prefix: %w", err)
	}
	frame := &preparedBroadcastFrame{
		overlayID: bytes.Clone(overlayID),
		message:   adnl.PrepareCustomMessageParts(prefix, m.body),
	}
	// Two peers racing on the first send both build the same frame; either
	// one may win, both are correct.
	m.frame.Store(frame)
	return frame.message, nil
}

// PreparedBroadcastMessagePeer is a broadcast peer that takes a prepared
// message with its cached ADNL frame. ADNL overlay peers implement it; the
// frame is what lets a fanout skip per-peer serialization entirely.
type PreparedBroadcastMessagePeer interface {
	BroadcastPeer
	SendPreparedBroadcastMessage(ctx context.Context, msg *PreparedBroadcastMessage) error
}

// SendPreparedBroadcast delivers a broadcast over the cheapest path the peer
// implements: the prepared message with its ADNL frame, the prepared body
// alone (QUIC and RLDP transports, which frame it their own way), or the
// reflective serialization of message for legacy BroadcastPeer
// implementations. message and prepared must describe the same broadcast.
func SendPreparedBroadcast(ctx context.Context, peer BroadcastPeer, message tl.Serializable, prepared *PreparedBroadcastMessage) error {
	if prepared != nil {
		if framed, ok := peer.(PreparedBroadcastMessagePeer); ok {
			return framed.SendPreparedBroadcastMessage(ctx, prepared)
		}
		if bodied, ok := peer.(PreparedBroadcastPeer); ok {
			return bodied.SendPreparedCustomMessage(ctx, prepared.body)
		}
	}

	// BroadcastPeer is a released extension point. Keep legacy peers working;
	// production transports implement one of the prepared paths and never
	// take this reflective serialization.
	return peer.SendCustomMessage(ctx, message)
}
