package overlay

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
)

type releasedRLDPTransport struct {
	delegate *mockRLDP
}

var _ RLDP = (*releasedRLDPTransport)(nil)

func (r *releasedRLDPTransport) GetADNL() rldp.ADNL {
	return r.delegate.GetADNL()
}

func (r *releasedRLDPTransport) GetRateInfo() (left int64, total int64) {
	return r.delegate.GetRateInfo()
}

func (r *releasedRLDPTransport) Stats() rldp.Stats {
	return r.delegate.Stats()
}

func (r *releasedRLDPTransport) Close() {
	r.delegate.Close()
}

func (r *releasedRLDPTransport) DoQuery(ctx context.Context, maxAnswerSize uint64, query, result tl.Serializable) error {
	return r.delegate.DoQuery(ctx, maxAnswerSize, query, result)
}

func (r *releasedRLDPTransport) DoQueryAsync(
	ctx context.Context,
	maxAnswerSize uint64,
	id []byte,
	query tl.Serializable,
	result chan<- rldp.AsyncQueryResult,
) error {
	return r.delegate.DoQueryAsync(ctx, maxAnswerSize, id, query, result)
}

func (r *releasedRLDPTransport) SetOnQuery(handler func(transferId []byte, query *rldp.Query) error) {
	r.delegate.SetOnQuery(handler)
}

func (r *releasedRLDPTransport) SetOnMessage(handler func(id []byte, data []byte) error) {
	r.delegate.SetOnMessage(handler)
}

func (r *releasedRLDPTransport) SetOnDisconnect(handler func()) {
	r.delegate.SetOnDisconnect(handler)
}

func (r *releasedRLDPTransport) SendAnswer(
	ctx context.Context,
	maxAnswerSize uint64,
	timeoutAt uint32,
	queryId,
	transferId []byte,
	answer tl.Serializable,
) error {
	return r.delegate.SendAnswer(ctx, maxAnswerSize, timeoutAt, queryId, transferId, answer)
}

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

func TestRLDPHandlerPublicationConcurrentDispatch(t *testing.T) {
	adnlMock := newMockADNL()
	rMock := newMockRLDP(adnlMock)
	w := CreateExtendedRLDP(rMock)
	overlayID := bytes.Repeat([]byte{0x90}, 32)
	missingID := bytes.Repeat([]byte{0x8F}, 32)
	o := w.CreateOverlay(overlayID)
	transferID := bytes.Repeat([]byte{0xAA}, 32)
	overlayQuery := &rldp.Query{Data: WrapQuery(overlayID, tl.Raw{1})}
	missingQuery := &rldp.Query{Data: WrapQuery(missingID, tl.Raw{2})}
	rootQuery := &rldp.Query{Data: tl.Raw{3}}

	queryHandler := func([]byte, *rldp.Query) error { return nil }
	disconnectHandler := func() {}
	start := make(chan struct{})
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 2000; i++ {
			if i%2 == 0 {
				o.SetOnQuery(queryHandler)
				o.SetOnDisconnect(disconnectHandler)
				w.SetOnQuery(queryHandler)
				w.SetOnUnknownOverlayQuery(queryHandler)
				w.SetOnDisconnect(disconnectHandler)
				continue
			}
			o.SetOnQuery(nil)
			o.SetOnDisconnect(nil)
			w.SetOnQuery(nil)
			w.SetOnUnknownOverlayQuery(nil)
			w.SetOnDisconnect(nil)
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 2000; i++ {
			if err := w.queryHandler(transferID, overlayQuery); err != nil {
				errCh <- err
				return
			}
			if err := w.queryHandler(transferID, rootQuery); err != nil {
				errCh <- err
				return
			}
			_ = w.queryHandler(transferID, missingQuery)
			w.disconnectHandler("127.0.0.1:1", nil)
		}
	}()
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent RLDP handler dispatch failed: %v", err)
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

func TestRLDPManagerRoutesMessagesThroughADNLOverlay(t *testing.T) {
	baseADNL := newMockADNL()
	adnlWrapper := CreateExtendedADNL(baseADNL)
	rMock := newMockRLDP(adnlWrapper)
	CreateExtendedRLDP(rMock)
	if rMock.onMessage == nil {
		t.Fatal("expected message handler to be set")
	}

	overlayID := bytes.Repeat([]byte{0x9B}, 32)
	payload := GetRandomPeers{}

	o := adnlWrapper.WithOverlay(overlayID)
	called := false
	o.SetCustomMessageHandler(func(msg *adnl.MessageCustom) error {
		called = true
		if _, ok := msg.Data.(GetRandomPeers); !ok {
			t.Fatalf("payload was not routed through overlay custom handler: %T", msg.Data)
		}
		return nil
	})

	data, err := tl.Serialize(WrapMessage(overlayID, payload), true)
	if err != nil {
		t.Fatalf("message serialize failed: %v", err)
	}
	if err = rMock.onMessage(bytes.Repeat([]byte{0x9C}, 32), data); err != nil {
		t.Fatalf("message routing failed: %v", err)
	}
	if !called {
		t.Fatal("expected overlay custom handler call")
	}
}

func TestParseRLDPMessagePayloadRejectsExtraObjects(t *testing.T) {
	overlayID := bytes.Repeat([]byte{0x9C}, 32)
	data, err := tl.Serialize([]tl.Serializable{
		Message{Overlay: overlayID},
		GetRandomPeers{},
		Ping{},
	}, true)
	if err != nil {
		t.Fatalf("serialize payload: %v", err)
	}

	if _, err = parseRLDPMessagePayload(data); err == nil || !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("extra-object parse error = %v, want trailing data rejection", err)
	}
}

func TestParseRLDPMessagePayloadRequiresEnvelopeForTwoObjects(t *testing.T) {
	data, err := tl.Serialize([]tl.Serializable{GetRandomPeers{}, Ping{}}, true)
	if err != nil {
		t.Fatalf("serialize payload: %v", err)
	}

	if _, err = parseRLDPMessagePayload(data); err == nil || !strings.Contains(err.Error(), "require an overlay message envelope") {
		t.Fatalf("multi-root parse error = %v, want envelope rejection", err)
	}
}

func TestRLDPOverlaySendCustomMessageFramesPayload(t *testing.T) {
	adnlMock := newMockADNL()
	rMock := newMockRLDP(adnlMock)
	wrapper := CreateExtendedRLDP(rMock)
	overlayID := bytes.Repeat([]byte{0x9D}, 32)
	overlay := wrapper.CreateOverlay(overlayID)

	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "message")
	if err := overlay.SendCustomMessage(ctx, GetRandomPeers{}); err != nil {
		t.Fatalf("send custom message: %v", err)
	}
	if len(rMock.sendMessageCalls) != 1 {
		t.Fatalf("RLDP message calls = %d, want 1", len(rMock.sendMessageCalls))
	}
	if rMock.sendMessageCtx.Value(contextKey{}) != "message" {
		t.Fatal("send custom message did not propagate its context")
	}

	var envelope any
	rest, err := tl.ParseNoCopy(&envelope, rMock.sendMessageCalls[0], true)
	if err != nil {
		t.Fatalf("parse overlay message envelope: %v", err)
	}
	header, ok := envelope.(Message)
	if !ok {
		t.Fatalf("envelope type = %T, want Message value", envelope)
	}
	if !bytes.Equal(header.Overlay, overlayID) {
		t.Fatalf("envelope overlay = %x, want %x", header.Overlay, overlayID)
	}

	var payload any
	rest, err = tl.ParseNoCopy(&payload, rest, true)
	if err != nil {
		t.Fatalf("parse overlay message payload: %v", err)
	}
	if _, ok = payload.(GetRandomPeers); !ok {
		t.Fatalf("payload type = %T, want GetRandomPeers value", payload)
	}
	if len(rest) != 0 {
		t.Fatalf("overlay message has %d trailing bytes", len(rest))
	}
}

func TestRLDPOverlaySendCustomMessageRequiresMessageCapability(t *testing.T) {
	rMock := newMockRLDP(newMockADNL())
	transport := &releasedRLDPTransport{delegate: rMock}
	wrapper := CreateExtendedRLDP(transport)
	overlay := wrapper.CreateOverlay(bytes.Repeat([]byte{0x4C}, 32))

	err := overlay.SendCustomMessage(context.Background(), GetRandomPeers{})
	if !errors.Is(err, ErrRLDPMessageUnsupported) {
		t.Fatalf("send custom message error = %v, want %v", err, ErrRLDPMessageUnsupported)
	}
	if len(rMock.sendMessageCalls) != 0 {
		t.Fatalf("underlying message calls = %d, want 0", len(rMock.sendMessageCalls))
	}
}

func TestRLDPManagerFECControlUsesRLDPReplyPath(t *testing.T) {
	overlayID := bytes.Repeat([]byte{0x9E}, 32)
	peerID := bytes.Repeat([]byte{0x9F}, 32)

	baseADNL := newMockADNL()
	baseADNL.id = peerID
	adnlWrapper := CreateExtendedADNL(baseADNL)

	receiver := newTestBroadcastReceiver(t, overlayID)
	receiver.SetBroadcastHandlerWithInfo(func(tl.Serializable, BroadcastInfo) BroadcastDisposition {
		return BroadcastDispositionIgnore
	})
	attached, err := adnlWrapper.AttachOverlay(receiver)
	if err != nil {
		t.Fatalf("attach broadcast receiver: %v", err)
	}
	t.Cleanup(attached.Close)

	rMock := newMockRLDP(adnlWrapper)
	CreateExtendedRLDP(rMock)
	if rMock.onMessage == nil {
		t.Fatal("expected RLDP message handler to be set")
	}

	_, privateKey := keyPairFromSeed(104)
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

	for seqno := uint32(0); seqno < sender.fec.SymbolsCount; seqno++ {
		part, partErr := sender.part(seqno)
		if partErr != nil {
			t.Fatalf("build FEC part %d: %v", seqno, partErr)
		}
		data, serializeErr := tl.Serialize(WrapMessage(overlayID, *part.full), true)
		if serializeErr != nil {
			t.Fatalf("serialize FEC part %d: %v", seqno, serializeErr)
		}
		if handleErr := rMock.onMessage(bytes.Repeat([]byte{0xA0}, 32), data); handleErr != nil {
			t.Fatalf("handle RLDP FEC part %d: %v", seqno, handleErr)
		}
	}

	if len(baseADNL.sendCustomCalls) != 0 {
		t.Fatalf("ADNL custom sends = %d, want 0", len(baseADNL.sendCustomCalls))
	}
	if len(rMock.sendMessageCalls) == 0 {
		t.Fatal("FEC receiver sent no control over RLDP")
	}

	var envelope any
	rest, err := tl.ParseNoCopy(&envelope, rMock.sendMessageCalls[len(rMock.sendMessageCalls)-1], true)
	if err != nil {
		t.Fatalf("parse RLDP reply envelope: %v", err)
	}
	header, ok := envelope.(Message)
	if !ok {
		t.Fatalf("RLDP reply envelope type = %T, want Message value", envelope)
	}
	if !bytes.Equal(header.Overlay, overlayID) {
		t.Fatalf("RLDP reply overlay = %x, want %x", header.Overlay, overlayID)
	}

	var control any
	rest, err = tl.ParseNoCopy(&control, rest, true)
	if err != nil {
		t.Fatalf("parse RLDP FEC control: %v", err)
	}
	if _, ok = control.(FECReceived); !ok {
		t.Fatalf("RLDP control type = %T, want FECReceived value", control)
	}
	if len(rest) != 0 {
		t.Fatalf("RLDP FEC control has %d trailing bytes", len(rest))
	}
}

func TestRLDPManagerRoutesFramedFECControlToBroadcastReceiver(t *testing.T) {
	overlayID := bytes.Repeat([]byte{0xA1}, 32)

	baseADNL := newMockADNL()
	adnlWrapper := CreateExtendedADNL(baseADNL)
	receiver := newTestBroadcastReceiver(t, overlayID)
	attached, err := adnlWrapper.AttachOverlay(receiver)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(attached.Close)

	customCalled := false
	attached.SetCustomMessageHandler(func(*adnl.MessageCustom) error {
		customCalled = true
		return nil
	})

	rMock := newMockRLDP(adnlWrapper)
	CreateExtendedRLDP(rMock)
	data, err := tl.Serialize(WrapMessage(overlayID, FECReceived{
		Hash: bytes.Repeat([]byte{0xA2}, 32),
	}), true)
	if err != nil {
		t.Fatal(err)
	}
	if err = rMock.onMessage(bytes.Repeat([]byte{0xA3}, 32), data); err != nil {
		t.Fatal(err)
	}
	if customCalled {
		t.Fatal("framed FEC control was routed as an application message")
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

func TestRLDPOverlayStaleCloseDoesNotDetachReplacement(t *testing.T) {
	adnlMock := newMockADNL()
	rMock := newMockRLDP(adnlMock)
	w := CreateExtendedRLDP(rMock)
	overlayID := bytes.Repeat([]byte{0xB2}, 32)

	old := w.CreateOverlay(overlayID)
	// Model the interval after Close marks a wrapper closed but before its
	// pointer-checked detach acquires the manager lock.
	old.closed.Store(true)
	replacement := w.CreateOverlay(overlayID)
	if replacement == old {
		t.Fatal("closed overlay wrapper was reused")
	}

	old.Close()
	if got := w.CreateOverlay(overlayID); got != replacement {
		t.Fatal("stale close detached the replacement overlay generation")
	}
}
