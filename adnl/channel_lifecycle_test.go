package adnl

import (
	"bytes"
	"crypto/ed25519"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

type channelLifecycleFixture struct {
	gateway *Gateway
	peer    *peerConn
	adnl    *ADNL
	routing map[string]*Channel
}

func newChannelLifecycleFixture(t *testing.T) *channelLifecycleFixture {
	t.Helper()

	_, gatewayKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	peerKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	peerID, err := tl.Hash(keys.PublicKeyED25519{Key: peerKey})
	if err != nil {
		t.Fatal(err)
	}

	routing := map[string]*Channel{}
	gateway := NewGateway(gatewayKey)
	gateway.SetAddressList(nil)
	gateway.setupChannelCallbacks(func(ch *Channel) {
		routing[string(ch.id)] = ch
	}, func(id string) {
		delete(routing, id)
	})

	peer, err := gateway.registerClient(&net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 30303,
	}, peerKey, string(peerID))
	if err != nil {
		t.Fatal(err)
	}

	return &channelLifecycleFixture{
		gateway: gateway,
		peer:    peer,
		adnl:    peer.client.(*ADNL),
		routing: routing,
	}
}

func (f *channelLifecycleFixture) createChannel(t *testing.T, date int32) *Channel {
	t.Helper()

	peerChannelKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.adnl.processMessage(MessageCreateChannel{
		Key:  peerChannelKey,
		Date: date,
	}); err != nil {
		t.Fatal(err)
	}

	ch := currentTestChannel(f.adnl)
	if ch == nil || !ch.ready.Load() {
		t.Fatal("channel was not published ready")
	}

	return ch
}

func TestChannelPublicationRegistersCurrentReadyChannelSynchronously(t *testing.T) {
	fixture := newChannelLifecycleFixture(t)
	called := false

	fixture.adnl.SetChannelReadyHandler(func(ch *Channel) {
		called = true
		if currentTestChannel(fixture.adnl) != ch {
			t.Fatal("ready callback observed a channel that was not current")
		}
		if !ch.ready.Load() {
			t.Fatal("ready callback observed an unready channel")
		}

		fixture.gateway.mx.RLock()
		processor := fixture.gateway.processors[string(ch.id)]
		peerChannel := fixture.peer.channel
		fixture.gateway.mx.RUnlock()
		if processor == nil || processor.channel != ch {
			t.Fatal("ready callback ran before the gateway processor was registered")
		}
		if peerChannel != ch {
			t.Fatal("ready callback ran before the gateway peer published the channel")
		}
		if fixture.routing[string(ch.id)] != ch {
			t.Fatal("ready callback ran before channel routing was registered")
		}
	})

	fixture.createChannel(t, 100)
	if !called {
		t.Fatal("ready callback was not called")
	}
}

func TestLocalReinitRemovesCurrentChannelProcessor(t *testing.T) {
	fixture := newChannelLifecycleFixture(t)
	ch := fixture.createChannel(t, 100)
	channelID := string(ch.id)

	fixture.adnl.Reinit()

	if currentTestChannel(fixture.adnl) != nil {
		t.Fatal("local reinit kept the current channel")
	}
	fixture.gateway.mx.RLock()
	processor := fixture.gateway.processors[channelID]
	peerChannel := fixture.peer.channel
	peerChannelID := fixture.peer.channelId
	fixture.gateway.mx.RUnlock()
	if processor != nil {
		t.Fatal("local reinit kept the channel processor")
	}
	if peerChannel != nil || peerChannelID != "" {
		t.Fatal("local reinit kept the gateway peer channel")
	}
	if fixture.routing[channelID] != nil {
		t.Fatal("local reinit kept channel routing")
	}
}

func TestPeerReinitRemovesCurrentChannelProcessor(t *testing.T) {
	fixture := newChannelLifecycleFixture(t)
	ch := fixture.createChannel(t, 100)
	channelID := string(ch.id)

	if err := fixture.adnl.processMessage(MessageReinit{Date: 200}); err != nil {
		t.Fatal(err)
	}

	pending := currentTestChannel(fixture.adnl)
	if pending == nil {
		t.Fatal("peer reinit removed the pending local channel")
	}
	if pending == ch || pending.ready.Load() {
		t.Fatal("peer reinit kept the ready channel current")
	}
	if !bytes.Equal(pending.key, ch.key) || pending.initDate != ch.initDate {
		t.Fatal("peer reinit changed the pending local channel identity")
	}

	fixture.gateway.mx.RLock()
	processor := fixture.gateway.processors[channelID]
	peerChannel := fixture.peer.channel
	peerChannelID := fixture.peer.channelId
	fixture.gateway.mx.RUnlock()
	if processor != nil {
		t.Fatal("peer reinit kept the channel processor")
	}
	if peerChannel != nil || peerChannelID != "" {
		t.Fatal("peer reinit kept the gateway peer channel")
	}
	if fixture.routing[channelID] != nil {
		t.Fatal("peer reinit kept channel routing")
	}
}

func TestDelayedChannelCloseDoesNotRemoveReplacement(t *testing.T) {
	fixture := newChannelLifecycleFixture(t)
	oldChannel := fixture.createChannel(t, 100)
	oldID := string(oldChannel.id)
	current := fixture.createChannel(t, 101)
	currentID := string(current.id)
	if oldChannel == current || oldID == currentID {
		t.Fatal("newer peer channel did not produce a replacement processor identity")
	}

	fixture.adnl.channelClosed(oldChannel)

	fixture.gateway.mx.RLock()
	processor := fixture.gateway.processors[currentID]
	oldProcessor := fixture.gateway.processors[oldID]
	peerChannel := fixture.peer.channel
	fixture.gateway.mx.RUnlock()
	if processor == nil || processor.channel != current {
		t.Fatal("delayed close removed the replacement processor")
	}
	if peerChannel != current {
		t.Fatal("delayed close cleared the replacement peer channel")
	}
	if fixture.routing[currentID] != current {
		t.Fatal("delayed close removed replacement routing")
	}
	if oldProcessor != nil || fixture.routing[oldID] != nil {
		t.Fatal("old channel processor or routing remained registered")
	}
}

func TestStaleChannelProcessorHasNoSideEffects(t *testing.T) {
	fixture := newChannelLifecycleFixture(t)
	oldChannel := fixture.createChannel(t, 100)
	if !oldChannel.wantConfirm.Load() {
		t.Fatal("test requires an unconfirmed inbound channel")
	}

	handled := false
	fixture.adnl.SetCustomMessageHandler(func(*MessageCustom) error {
		handled = true
		return nil
	})
	fixture.createChannel(t, 101)
	before := fixture.adnl.Stats()

	err := oldChannel.process([]byte{1})
	if err != nil {
		t.Fatalf("stale processor returned an error: %v", err)
	}
	after := fixture.adnl.Stats()

	if !oldChannel.wantConfirm.Load() {
		t.Fatal("stale processor cleared wantConfirm")
	}
	if handled {
		t.Fatal("stale processor reached the custom message handler")
	}
	if after.Inbound.Packets != before.Inbound.Packets ||
		after.Inbound.Bytes != before.Inbound.Bytes ||
		after.Inbound.ProcessingErrors != before.Inbound.ProcessingErrors ||
		!after.Inbound.LastPacketAt.Equal(before.Inbound.LastPacketAt) ||
		!after.Inbound.LastErrorAt.Equal(before.Inbound.LastErrorAt) {
		t.Fatalf("stale processor changed inbound stats: before=%+v after=%+v", before.Inbound, after.Inbound)
	}
}

func TestLocalReinitWinsConcurrentChannelPublication(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		a, _ := testADNLWithPeer(t)
		peerChannelKey, _, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}

		assertLocalReinitWinsPublication(t, a, func() error {
			return a.processMessage(MessageCreateChannel{
				Key:  peerChannelKey,
				Date: 100,
			})
		})
	})

	t.Run("confirm", func(t *testing.T) {
		a, _ := testADNLWithPeer(t)
		if _, err := a.buildRequest(MessageNop{}); err != nil {
			t.Fatal(err)
		}
		pending := currentTestChannel(a)
		if pending == nil || pending.ready.Load() {
			t.Fatal("test requires a pending outbound channel")
		}

		peerChannelKey, _, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		assertLocalReinitWinsPublication(t, a, func() error {
			return a.processMessage(MessageConfirmChannel{
				Key:     peerChannelKey,
				PeerKey: pending.key.Public().(ed25519.PublicKey),
				Date:    100,
			})
		})
	})
}

func TestLocalReinitInvalidatesBlockedInboundChannelCreate(t *testing.T) {
	a, _ := testADNLWithPeer(t)
	a.writer = &capturePacketWriter{}

	handlerEntered := make(chan struct{})
	releaseHandler := make(chan struct{})
	a.SetCustomMessageHandler(func(*MessageCustom) error {
		close(handlerEntered)
		<-releaseHandler
		return nil
	})

	peerChannelKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	seqno := int64(1)
	processDone := make(chan error, 1)
	go func() {
		processDone <- a.processPacket(&PacketContent{
			Messages: []any{
				MessageCustom{Data: TestMsg{Data: []byte{1}}},
				MessageCreateChannel{Key: peerChannelKey, Date: 100},
			},
			Seqno: &seqno,
		}, false)
	}()

	waitChannelLifecycleSignal(t, handlerEntered, "custom handler was not entered")
	reinitDone := make(chan struct{})
	go func() {
		a.Reinit()
		close(reinitDone)
	}()
	waitChannelLifecycleSignal(t, reinitDone, "local reinit blocked behind a user handler")

	close(releaseHandler)
	if err = <-processDone; err != nil {
		t.Fatal(err)
	}
	if currentTestChannel(a) != nil {
		t.Fatal("stale inbound createChannel republished a channel after local reinit")
	}
}

func TestLocalReinitEpochStrictlyIncreases(t *testing.T) {
	a, _ := testADNLWithPeer(t)
	initial := atomic.LoadInt32(&a.reinitTime)

	a.Reinit()
	first := atomic.LoadInt32(&a.reinitTime)
	a.Reinit()
	second := atomic.LoadInt32(&a.reinitTime)

	if first <= initial {
		t.Fatalf("first reinit epoch = %d, want greater than %d", first, initial)
	}
	if second <= first {
		t.Fatalf("second reinit epoch = %d, want greater than %d", second, first)
	}
	if list := a.GetAddressList(); list.ReinitDate != second {
		t.Fatalf("address list reinit epoch = %d, want %d", list.ReinitDate, second)
	}
}

func assertLocalReinitWinsPublication(t *testing.T, a *ADNL, publish func() error) {
	t.Helper()

	callbackEntered := make(chan struct{})
	releaseCallback := make(chan struct{})
	a.SetChannelReadyHandler(func(*Channel) {
		close(callbackEntered)
		<-releaseCallback
	})

	publishDone := make(chan error, 1)
	go func() {
		publishDone <- publish()
	}()
	waitChannelLifecycleSignal(t, callbackEntered, "channel ready callback was not entered")

	reinitDone := make(chan struct{})
	go func() {
		a.Reinit()
		close(reinitDone)
	}()
	waitChannelLifecycleSignal(t, reinitDone, "local reinit blocked behind the ready callback")

	close(releaseCallback)
	if err := <-publishDone; err != nil {
		t.Fatal(err)
	}
	if currentTestChannel(a) != nil {
		t.Fatal("channel publication resurrected a channel after local reinit")
	}
}

func waitChannelLifecycleSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()

	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()

	select {
	case <-signal:
	case <-timer.C:
		t.Fatal(failure)
	}
}
