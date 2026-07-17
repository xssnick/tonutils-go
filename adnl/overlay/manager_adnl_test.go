package overlay

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
)

func TestCreateExtendedADNLInitializesHandlers(t *testing.T) {
	m := newMockADNL()
	w := CreateExtendedADNL(m)
	if w == nil {
		t.Fatalf("wrapper should not be nil")
	}
	if m.queryHandler == nil || m.customHandler == nil || m.disconnectHandler == nil {
		t.Fatalf("expected all ADNL handlers to be installed")
	}
	if w.GetDisconnectHandler() == nil {
		t.Fatalf("disconnect handler passthrough must not be nil")
	}
}

func TestADNLWrapperExposesPeerStats(t *testing.T) {
	m := newMockADNL()
	m.stats.Inbound.Packets = 7

	w := CreateExtendedADNL(m)
	if got := w.Stats().Inbound.Packets; got != 7 {
		t.Fatalf("inbound packets=%d want=7", got)
	}
}

func TestADNLOverlayHandlerPublicationConcurrentDispatch(t *testing.T) {
	m := newMockADNL()
	w := CreateExtendedADNL(m)
	overlayID := bytes.Repeat([]byte{0x60}, 32)
	o := w.WithOverlay(overlayID)
	query := &adnl.MessageQuery{ID: bytes.Repeat([]byte{0xA0}, 32), Data: WrapQuery(overlayID, tl.Raw{1})}
	custom := &adnl.MessageCustom{Data: WrapMessage(overlayID, tl.Raw{2})}
	pub, _ := keyPairFromSeed(50)

	queryHandler := func(*adnl.MessageQuery) error { return nil }
	customHandler := func(*adnl.MessageCustom) error { return nil }
	disconnectHandler := func(string, ed25519.PublicKey) {}
	start := make(chan struct{})
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 2000; i++ {
			if i%2 == 0 {
				o.SetQueryHandler(queryHandler)
				o.SetCustomMessageHandler(customHandler)
				o.SetDisconnectHandler(disconnectHandler)
				continue
			}
			o.SetQueryHandler(nil)
			o.SetCustomMessageHandler(nil)
			o.SetDisconnectHandler(nil)
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 2000; i++ {
			if err := w.queryHandler(query); err != nil {
				errCh <- err
				return
			}
			if err := w.customHandler(custom); err != nil {
				errCh <- err
				return
			}
			w.disconnectHandler("127.0.0.1:1", pub)
		}
	}()
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent handler dispatch failed: %v", err)
	}
}

func TestADNLManagerQueryRouting(t *testing.T) {
	m := newMockADNL()
	w := CreateExtendedADNL(m)
	overlayID := bytes.Repeat([]byte{0x61}, 32)
	payload := tl.Raw([]byte{7, 8, 9})
	queryID := bytes.Repeat([]byte{0xA1}, 32)

	ov := w.WithOverlay(overlayID)
	called := false
	ov.SetQueryHandler(func(msg *adnl.MessageQuery) error {
		called = true
		if !bytes.Equal(msg.ID, queryID) {
			t.Fatalf("query id not propagated")
		}
		raw, ok := msg.Data.(tl.Raw)
		if !ok || !bytes.Equal(raw, payload) {
			t.Fatalf("query payload was not unwrapped")
		}
		return nil
	})

	if err := w.queryHandler(&adnl.MessageQuery{ID: queryID, Data: WrapQuery(overlayID, payload)}); err != nil {
		t.Fatalf("overlay query routing failed: %v", err)
	}
	if !called {
		t.Fatalf("expected overlay query handler call")
	}

	ov.SetQueryHandler(nil)
	if err := w.queryHandler(&adnl.MessageQuery{ID: queryID, Data: WrapQuery(overlayID, payload)}); err != nil {
		t.Fatalf("expected nil error when overlay query handler is absent, got: %v", err)
	}

	missingID := bytes.Repeat([]byte{0x62}, 32)
	err := w.queryHandler(&adnl.MessageQuery{ID: queryID, Data: WrapQuery(missingID, payload)})
	if err == nil || !strings.Contains(err.Error(), "unregistered overlay") {
		t.Fatalf("expected unregistered overlay error, got: %v", err)
	}

	sentinel := errors.New("unknown overlay")
	unknownCalled := false
	w.SetOnUnknownOverlayQuery(func(query *adnl.MessageQuery) error {
		unknownCalled = true
		if query.Data == nil {
			t.Fatalf("unknown overlay handler must receive original query")
		}
		return sentinel
	})
	err = w.queryHandler(&adnl.MessageQuery{ID: queryID, Data: WrapQuery(missingID, payload)})
	if !errors.Is(err, sentinel) || !unknownCalled {
		t.Fatalf("expected unknown overlay handler error, got: %v", err)
	}

	rootSentinel := errors.New("root query")
	rootCalled := false
	w.SetQueryHandler(func(msg *adnl.MessageQuery) error {
		rootCalled = true
		raw, ok := msg.Data.(tl.Raw)
		if !ok || !bytes.Equal(raw, payload) {
			t.Fatalf("root query should receive original payload")
		}
		return rootSentinel
	})
	err = w.queryHandler(&adnl.MessageQuery{ID: queryID, Data: payload})
	if !errors.Is(err, rootSentinel) || !rootCalled {
		t.Fatalf("expected root query handler call, got: %v", err)
	}
}

func TestADNLManagerAnswersOverlayPing(t *testing.T) {
	m := newMockADNL()
	w := CreateExtendedADNL(m)
	overlayID := bytes.Repeat([]byte{0x63}, 32)
	queryID := bytes.Repeat([]byte{0xA2}, 32)

	ov := w.WithOverlay(overlayID)
	ov.SetQueryHandler(func(msg *adnl.MessageQuery) error {
		t.Fatal("overlay ping must not reach the user query handler")
		return nil
	})

	if err := w.queryHandler(&adnl.MessageQuery{ID: queryID, Data: WrapQuery(overlayID, Ping{})}); err != nil {
		t.Fatalf("overlay ping failed: %v", err)
	}
	if len(m.answerCalls) != 1 {
		t.Fatalf("answer calls = %d, want 1", len(m.answerCalls))
	}
	if _, ok := m.answerCalls[0].(Pong); !ok {
		t.Fatalf("unexpected overlay ping answer %T", m.answerCalls[0])
	}

	missingID := bytes.Repeat([]byte{0x64}, 32)
	if err := w.queryHandler(&adnl.MessageQuery{ID: queryID, Data: WrapQuery(missingID, Ping{})}); err == nil {
		t.Fatal("expected unregistered overlay ping to fail")
	}
	if len(m.answerCalls) != 1 {
		t.Fatalf("unregistered overlay ping was answered")
	}
}

func TestADNLManagerCustomRouting(t *testing.T) {
	m := newMockADNL()
	w := CreateExtendedADNL(m)
	overlayID := bytes.Repeat([]byte{0x71}, 32)
	payload := tl.Raw([]byte{0x01})

	ov := w.WithOverlay(overlayID)
	called := false
	ov.SetCustomMessageHandler(func(msg *adnl.MessageCustom) error {
		called = true
		if _, ok := msg.Data.(tl.Raw); !ok {
			t.Fatalf("expected raw payload")
		}
		return nil
	})

	if err := w.customHandler(&adnl.MessageCustom{Data: WrapMessage(overlayID, payload)}); err != nil {
		t.Fatalf("custom message overlay routing failed: %v", err)
	}
	if !called {
		t.Fatalf("expected overlay custom handler call")
	}

	err := w.customHandler(&adnl.MessageCustom{Data: WrapMessage(bytes.Repeat([]byte{0x72}, 32), payload)})
	if err == nil || !strings.Contains(err.Error(), "unregistered overlay") {
		t.Fatalf("expected unregistered overlay error, got: %v", err)
	}

	err = w.customHandler(&adnl.MessageCustom{Data: WrapMessage(overlayID, BroadcastFEC{
		Source:   keys.PublicKeyAES{Key: bytes.Repeat([]byte{0x11}, 32)},
		DataHash: bytes.Repeat([]byte{0x22}, 32),
		DataSize: 4,
		Data:     []byte{1, 2, 3, 4},
		Seqno:    1,
		Date:     1,
		FEC:      rldp.FECRaptorQ{DataSize: 4, SymbolSize: 2, SymbolsCount: 2},
	})})
	if err == nil || !strings.Contains(err.Error(), "failed to process FEC broadcast") {
		t.Fatalf("expected wrapped fec processing error, got: %v", err)
	}

	err = w.customHandler(&adnl.MessageCustom{Data: WrapMessage(overlayID, BroadcastFECShort{
		Source:        keys.PublicKeyAES{Key: bytes.Repeat([]byte{0x11}, 32)},
		BroadcastHash: bytes.Repeat([]byte{0x33}, 32),
		PartDataHash:  bytes.Repeat([]byte{0x44}, 32),
		Seqno:         1,
	})})
	if err == nil || !strings.Contains(err.Error(), "failed to process short FEC broadcast") {
		t.Fatalf("expected wrapped short fec processing error, got: %v", err)
	}

	rootSentinel := errors.New("root custom")
	rootCalled := false
	w.SetCustomMessageHandler(func(msg *adnl.MessageCustom) error {
		rootCalled = true
		if msg.Data == nil {
			t.Fatalf("root custom should receive original message")
		}
		return rootSentinel
	})
	err = w.customHandler(&adnl.MessageCustom{Data: tl.Raw([]byte{1, 2})})
	if !errors.Is(err, rootSentinel) || !rootCalled {
		t.Fatalf("expected root custom handler call, got: %v", err)
	}
}

func TestADNLManagerRoutesFECControlInternally(t *testing.T) {
	m := newMockADNL()
	m.id = bytes.Repeat([]byte{0x91}, 32)
	w := CreateExtendedADNL(m)
	hash := bytes.Repeat([]byte{0x92}, 32)

	called := false
	unregister := w.registerBroadcastFECControl(hash, func(peerID []byte, control BroadcastFECControl) bool {
		called = true
		if !bytes.Equal(peerID, m.id) {
			t.Fatalf("unexpected peer id")
		}
		if !control.Completed {
			t.Fatalf("expected completed control")
		}
		if !bytes.Equal(control.Hash, hash) {
			t.Fatalf("unexpected control hash")
		}
		return true
	})

	rootCalled := false
	w.SetCustomMessageHandler(func(msg *adnl.MessageCustom) error {
		rootCalled = true
		return nil
	})

	if err := w.customHandler(&adnl.MessageCustom{Data: FECCompleted{Hash: hash}}); err != nil {
		t.Fatalf("control routing failed: %v", err)
	}
	if !called {
		t.Fatalf("expected internal control handler call")
	}
	if rootCalled {
		t.Fatalf("handled FEC control should not reach root handler")
	}

	unregister()
	if err := w.customHandler(&adnl.MessageCustom{Data: FECCompleted{Hash: hash}}); err != nil {
		t.Fatalf("root fallback failed: %v", err)
	}
	if !rootCalled {
		t.Fatalf("unregistered control should reach root handler")
	}
}

func TestADNLManagerFECControlHandlerCanUnregisterItself(t *testing.T) {
	m := newMockADNL()
	w := CreateExtendedADNL(m)
	hash := bytes.Repeat([]byte{0x93}, 32)
	control := BroadcastFECControl{Hash: hash, Completed: true}

	var unregister func()
	unregister = w.registerBroadcastFECControl(hash, func([]byte, BroadcastFECControl) bool {
		unregister()
		return true
	})
	if !w.trackBroadcastFECControl(control) {
		t.Fatal("self-unregistering control handler was not called")
	}
	if w.trackBroadcastFECControl(control) {
		t.Fatal("self-unregistered control handler was called again")
	}
}

func TestADNLManagerFECControlSnapshotsConcurrentMutation(t *testing.T) {
	m := newMockADNL()
	w := CreateExtendedADNL(m)
	hash := bytes.Repeat([]byte{0x94}, 32)
	control := BroadcastFECControl{Hash: hash, Completed: true}
	receiver := newTestBroadcastReceiver(t, bytes.Repeat([]byte{0x95}, 32))

	start := make(chan struct{})
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		<-start
		for range 2000 {
			unregister := w.registerBroadcastFECControl(hash, func([]byte, BroadcastFECControl) bool {
				return true
			})
			unregister()
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for range 2000 {
			overlay, err := w.AttachOverlay(receiver)
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
			overlay.Close()
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for range 4000 {
			w.trackBroadcastFECControl(control)
		}
	}()

	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent control snapshot mutation failed: %v", err)
	}
}

func BenchmarkADNLManagerTrackBroadcastFECControl(b *testing.B) {
	m := newMockADNL()
	w := CreateExtendedADNL(m)
	hash := bytes.Repeat([]byte{0x96}, 32)
	control := BroadcastFECControl{Hash: hash, Completed: true}

	for i := byte(0); i < 16; i++ {
		overlayID := bytes.Repeat([]byte{0xA0 + i}, 32)
		receiver := newTestBroadcastReceiver(b, overlayID)
		overlay, err := w.AttachOverlay(receiver)
		if err != nil {
			b.Fatalf("attach receiver %d: %v", i, err)
		}
		b.Cleanup(overlay.Close)
	}
	unregister := w.registerBroadcastFECControl(hash, func([]byte, BroadcastFECControl) bool {
		return true
	})
	b.Cleanup(unregister)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if !w.trackBroadcastFECControl(control) {
			b.Fatal("control was not handled")
		}
	}
}

func TestADNLManagerDisconnectFanOut(t *testing.T) {
	m := newMockADNL()
	w := CreateExtendedADNL(m)

	o1 := w.WithOverlay(bytes.Repeat([]byte{0x81}, 32))
	o2 := w.WithOverlay(bytes.Repeat([]byte{0x82}, 32))

	calls := 0
	o1.SetDisconnectHandler(func(addr string, key ed25519.PublicKey) { calls++ })
	o2.SetDisconnectHandler(func(addr string, key ed25519.PublicKey) { calls++ })
	w.SetDisconnectHandler(func(addr string, key ed25519.PublicKey) { calls++ })

	pub, _ := keyPairFromSeed(41)
	w.disconnectHandler("127.0.0.1:0", pub)

	if calls != 3 {
		t.Fatalf("expected 3 disconnect callbacks, got %d", calls)
	}
}
