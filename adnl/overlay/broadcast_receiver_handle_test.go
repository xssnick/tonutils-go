package overlay

import (
	"bytes"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tl"
)

func TestBroadcastReceiverHandleMessageProcessesSimpleBroadcast(t *testing.T) {
	overlayID := bytes.Repeat([]byte{0xD1}, 32)
	peerID := bytes.Repeat([]byte{0xD2}, 32)
	receiver := newTestBroadcastReceiver(t, overlayID)

	_, privateKey := keyPairFromSeed(101)
	broadcast := testReceiverBroadcast(t, privateKey, Message{Overlay: overlayID})

	var delivered tl.Serializable
	var deliveryInfo BroadcastInfo
	receiver.SetBroadcastHandlerWithInfo(func(msg tl.Serializable, info BroadcastInfo) BroadcastDisposition {
		delivered = msg
		deliveryInfo = info
		return BroadcastDispositionIgnore
	})

	peer := &mockBroadcastPeer{id: peerID}
	if err := receiver.HandleMessage(peer, broadcast); err != nil {
		t.Fatalf("handle simple broadcast: %v", err)
	}
	if _, ok := delivered.(Message); !ok {
		t.Fatalf("delivered type = %T, want decoded Message value", delivered)
	}
	if deliveryInfo.Delivery != BroadcastDeliverySimple {
		t.Fatalf("delivery = %d, want simple", deliveryInfo.Delivery)
	}
	if !bytes.Equal(deliveryInfo.ImmediatePeerID, peerID) {
		t.Fatalf("immediate peer id = %x, want %x", deliveryInfo.ImmediatePeerID, peerID)
	}
}

func TestBroadcastReceiverHandleMessageRoutesFECControl(t *testing.T) {
	receiver := newTestBroadcastReceiver(t, bytes.Repeat([]byte{0xD3}, 32))
	receiver.EnableBroadcastFECRelay(
		bytes.Repeat([]byte{0xD4}, 32),
		StaticBroadcastPeerSet{},
	)

	hash := bytes.Repeat([]byte{0xD5}, 32)
	peerID := bytes.Repeat([]byte{0xD6}, 32)
	stream := &fecBroadcastStream{lastMessageAt: time.Now()}
	receiver.fecState.mx.Lock()
	receiver.fecState.streams[testBroadcastFECIDKey(hash)] = stream
	receiver.fecState.mx.Unlock()

	peer := &mockBroadcastPeer{id: peerID}
	if err := receiver.HandleMessage(peer, FECCompleted{Hash: hash}); err != nil {
		t.Fatalf("handle FEC control: %v", err)
	}

	stream.mx.Lock()
	peerKey := newBroadcastExternalPeerIDKey(peerID)
	_, received := stream.receivedPeers[peerKey]
	_, completed := stream.completedPeers[peerKey]
	stream.mx.Unlock()
	if !received || !completed {
		t.Fatalf("FEC control state: received=%v completed=%v, want true/true", received, completed)
	}
}

func TestBroadcastReceiverHandleMessageSendsFECControlThroughPeer(t *testing.T) {
	overlayID := bytes.Repeat([]byte{0xD7}, 32)
	receiver := newTestBroadcastReceiver(t, overlayID)
	receiver.SetBroadcastHandlerWithInfo(func(tl.Serializable, BroadcastInfo) BroadcastDisposition {
		return BroadcastDispositionIgnore
	})

	_, privateKey := keyPairFromSeed(102)
	sender, err := NewBroadcastFECSenderFromTL(
		privateKey,
		CertificateEmpty{},
		Message{Overlay: overlayID},
		BroadcastFlagAnySender,
		WithBroadcastFECSymbolSize(64),
	)
	if err != nil {
		t.Fatalf("new FEC sender: %v", err)
	}

	peer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0xD8}, 32)}
	for seqno := uint32(0); seqno < sender.fec.SymbolsCount; seqno++ {
		part, err := sender.part(seqno)
		if err != nil {
			t.Fatalf("build FEC part %d: %v", seqno, err)
		}
		if err = receiver.HandleMessage(peer, *part.full); err != nil {
			t.Fatalf("handle FEC part %d: %v", seqno, err)
		}
	}

	if len(peer.sent) == 0 {
		t.Fatal("FEC receiver sent no control through transport peer")
	}
	if _, ok := peer.sent[len(peer.sent)-1].(FECReceived); !ok {
		t.Fatalf("FEC control type = %T, want FECReceived", peer.sent[len(peer.sent)-1])
	}
}
