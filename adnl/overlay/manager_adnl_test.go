package overlay

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"strings"
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
