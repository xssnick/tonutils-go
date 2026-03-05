package overlay

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
)

func TestCreateExtendedRLDPInitializesHandlers(t *testing.T) {
	adnlMock := newMockADNL()
	rMock := newMockRLDP(adnlMock)

	w := CreateExtendedRLDP(rMock)
	if w == nil {
		t.Fatalf("wrapper should not be nil")
	}
	if rMock.onQuery == nil {
		t.Fatalf("expected query handler to be set")
	}
	if adnlMock.disconnectHandler == nil {
		t.Fatalf("expected disconnect handler to be installed on ADNL")
	}
}

func TestRLDPManagerQueryRouting(t *testing.T) {
	adnlMock := newMockADNL()
	rMock := newMockRLDP(adnlMock)
	w := CreateExtendedRLDP(rMock)
	overlayID := bytes.Repeat([]byte{0x91}, 32)
	payload := tl.Raw([]byte{4, 5, 6})
	queryID := bytes.Repeat([]byte{0x9A}, 32)

	o := w.CreateOverlay(overlayID)
	called := false
	o.SetOnQuery(func(transferId []byte, query *rldp.Query) error {
		called = true
		if !bytes.Equal(transferId, []byte{0xAA}) {
			t.Fatalf("unexpected transfer id")
		}
		if !bytes.Equal(query.ID, queryID) || query.MaxAnswerSize != 123 || query.Timeout != 321 {
			t.Fatalf("query metadata mismatch after routing")
		}
		raw, ok := query.Data.(tl.Raw)
		if !ok || !bytes.Equal(raw, payload) {
			t.Fatalf("payload was not unwrapped")
		}
		return nil
	})

	err := w.queryHandler([]byte{0xAA}, &rldp.Query{ID: queryID, MaxAnswerSize: 123, Timeout: 321, Data: WrapQuery(overlayID, payload)})
	if err != nil {
		t.Fatalf("overlay rldp routing failed: %v", err)
	}
	if !called {
		t.Fatalf("expected overlay query handler call")
	}

	o.SetOnQuery(nil)
	err = w.queryHandler([]byte{0xAA}, &rldp.Query{ID: queryID, Data: WrapQuery(overlayID, payload)})
	if err != nil {
		t.Fatalf("expected nil when overlay query handler is absent, got: %v", err)
	}

	missingID := bytes.Repeat([]byte{0x92}, 32)
	err = w.queryHandler([]byte{0xAA}, &rldp.Query{ID: queryID, Data: WrapQuery(missingID, payload)})
	if err == nil || !strings.Contains(err.Error(), "unregistered overlay") {
		t.Fatalf("expected unregistered overlay error, got: %v", err)
	}

	sentinel := errors.New("unknown overlay")
	unknownCalled := false
	w.SetOnUnknownOverlayQuery(func(transferId []byte, query *rldp.Query) error {
		unknownCalled = true
		if !bytes.Equal(transferId, []byte{0xAA}) {
			t.Fatalf("transfer id mismatch for unknown overlay")
		}
		return sentinel
	})
	err = w.queryHandler([]byte{0xAA}, &rldp.Query{ID: queryID, Data: WrapQuery(missingID, payload)})
	if !errors.Is(err, sentinel) || !unknownCalled {
		t.Fatalf("expected unknown overlay callback, got: %v", err)
	}

	rootSentinel := errors.New("root query")
	rootCalled := false
	w.SetOnQuery(func(transferId []byte, query *rldp.Query) error {
		rootCalled = true
		raw, ok := query.Data.(tl.Raw)
		if !ok || !bytes.Equal(raw, payload) {
			t.Fatalf("root query must receive original payload")
		}
		return rootSentinel
	})
	err = w.queryHandler([]byte{0xAA}, &rldp.Query{ID: queryID, Data: payload})
	if !errors.Is(err, rootSentinel) || !rootCalled {
		t.Fatalf("expected root query handler call, got: %v", err)
	}
}

func TestRLDPManagerDisconnectFanOut(t *testing.T) {
	adnlMock := newMockADNL()
	rMock := newMockRLDP(adnlMock)
	w := CreateExtendedRLDP(rMock)

	o1 := w.CreateOverlay(bytes.Repeat([]byte{0xA1}, 32))
	o2 := w.CreateOverlay(bytes.Repeat([]byte{0xA2}, 32))

	calls := 0
	o1.SetOnDisconnect(func() { calls++ })
	o2.SetOnDisconnect(func() { calls++ })
	w.SetOnDisconnect(func() { calls++ })

	pub, _ := keyPairFromSeed(51)
	w.disconnectHandler("127.0.0.1:1", pub)

	if calls != 3 {
		t.Fatalf("expected 3 disconnect callbacks, got %d", calls)
	}
}

func TestRLDPOverylayDoQueryAndClose(t *testing.T) {
	adnlMock := newMockADNL()
	rMock := newMockRLDP(adnlMock)
	w := CreateExtendedRLDP(rMock)
	overlayID := bytes.Repeat([]byte{0xB1}, 32)
	o1 := w.CreateOverlay(overlayID)
	o2 := w.CreateOverlay(overlayID)
	if o1 != o2 {
		t.Fatalf("expected create overlay to reuse existing wrapper")
	}

	rMock.doQueryFn = func(ctx context.Context, maxAnswerSize uint64, query, result tl.Serializable) error {
		if maxAnswerSize != 999 {
			t.Fatalf("unexpected max answer size")
		}
		arr, ok := query.([]tl.Serializable)
		if !ok || len(arr) != 2 {
			t.Fatalf("expected wrapped query")
		}
		h, ok := arr[0].(Query)
		if !ok || !bytes.Equal(h.Overlay, overlayID) {
			t.Fatalf("unexpected wrapped query header")
		}
		if _, ok := arr[1].(GetRandomPeers); !ok {
			t.Fatalf("unexpected wrapped query payload")
		}
		return nil
	}

	var out tl.Serializable
	if err := o1.DoQuery(context.Background(), 999, GetRandomPeers{}, &out); err != nil {
		t.Fatalf("overlay do query failed: %v", err)
	}

	o1.Close()
	if len(w.overlays) != 0 {
		t.Fatalf("overlay should be removed after close")
	}
}
