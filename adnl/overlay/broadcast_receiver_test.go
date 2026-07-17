package overlay

import (
	"bytes"
	"crypto/ed25519"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
)

func newTestBroadcastReceiver(t testing.TB, overlayID []byte) *BroadcastReceiver {
	t.Helper()

	receiver, err := NewBroadcastReceiver(overlayID, 1<<20, true, true)
	if err != nil {
		t.Fatalf("new broadcast receiver failed: %v", err)
	}
	t.Cleanup(receiver.Close)
	return receiver
}

func testReceiverBroadcast(t testing.TB, privateKey ed25519.PrivateKey, payload tl.Serializable) Broadcast {
	t.Helper()

	data, err := tl.Serialize(payload, true)
	if err != nil {
		t.Fatalf("serialize broadcast payload failed: %v", err)
	}
	broadcast := Broadcast{
		Source:      ed25519Public(privateKey),
		Certificate: CertificateEmpty{},
		Flags:       BroadcastFlagAnySender,
		Data:        data,
		Date:        int32(time.Now().Unix()),
	}
	if err = broadcast.Sign(privateKey); err != nil {
		t.Fatalf("sign broadcast failed: %v", err)
	}
	return broadcast
}

func TestBroadcastReceiverResolverHandlesFirstUnknownBroadcast(t *testing.T) {
	overlayID := bytes.Repeat([]byte{0xA1}, 32)
	peerID := bytes.Repeat([]byte{0xA2}, 32)
	receiver := newTestBroadcastReceiver(t, overlayID)

	transport := newMockADNL()
	transport.id = peerID
	wrapper := CreateExtendedADNL(transport)
	resolverCalls := 0
	wrapper.SetBroadcastReceiverResolver(func(gotOverlayID []byte) (*BroadcastReceiver, error) {
		resolverCalls++
		if !bytes.Equal(gotOverlayID, overlayID) {
			t.Fatalf("resolver got overlay %x, want %x", gotOverlayID, overlayID)
		}
		return receiver, nil
	})

	_, privateKey := keyPairFromSeed(91)
	broadcast := testReceiverBroadcast(t, privateKey, Message{Overlay: bytes.Repeat([]byte{0xA3}, 32)})
	sourceID, err := tl.Hash(broadcast.Source)
	if err != nil {
		t.Fatalf("hash source failed: %v", err)
	}

	deliveries := 0
	var info BroadcastInfo
	receiver.SetBroadcastHandlerWithInfo(func(msg tl.Serializable, gotInfo BroadcastInfo) BroadcastDisposition {
		deliveries++
		info = gotInfo
		return BroadcastDispositionAcceptAndRelay
	})

	err = transport.customHandler(&adnl.MessageCustom{Data: WrapMessage(overlayID, broadcast)})
	if err != nil {
		t.Fatalf("route unknown-overlay broadcast failed: %v", err)
	}
	if resolverCalls != 1 || deliveries != 1 {
		t.Fatalf("resolver calls=%d deliveries=%d, want 1/1", resolverCalls, deliveries)
	}
	if len(wrapper.overlays) != 0 {
		t.Fatalf("resolver path must not allocate an attached overlay wrapper")
	}
	if info.Delivery != BroadcastDeliverySimple || !bytes.Equal(info.SourceID, sourceID) ||
		!bytes.Equal(info.ImmediatePeerID, peerID) || !bytes.Equal(info.Payload, broadcast.Data) {
		t.Fatalf("unexpected delivery info: %#v", info)
	}
}

func TestBroadcastReceiverResolverReplacesInactiveAttachedReceiver(t *testing.T) {
	overlayID := bytes.Repeat([]byte{0xA4}, 32)
	oldReceiver := newTestBroadcastReceiver(t, overlayID)
	newReceiver := newTestBroadcastReceiver(t, overlayID)

	transport := newMockADNL()
	wrapper := CreateExtendedADNL(transport)
	oldOverlay, err := wrapper.AttachOverlay(oldReceiver)
	if err != nil {
		t.Fatalf("attach old receiver failed: %v", err)
	}
	t.Cleanup(oldOverlay.Close)
	oldReceiver.Close()

	resolverCalls := 0
	wrapper.SetBroadcastReceiverResolver(func(gotOverlayID []byte) (*BroadcastReceiver, error) {
		resolverCalls++
		if !bytes.Equal(gotOverlayID, overlayID) {
			t.Fatalf("resolver got overlay %x, want %x", gotOverlayID, overlayID)
		}
		return newReceiver, nil
	})

	oldReceiver.SetBroadcastHandlerWithInfo(func(tl.Serializable, BroadcastInfo) BroadcastDisposition {
		t.Fatal("inactive attached receiver handled broadcast")
		return BroadcastDispositionIgnore
	})
	deliveries := 0
	newReceiver.SetBroadcastHandlerWithInfo(func(tl.Serializable, BroadcastInfo) BroadcastDisposition {
		deliveries++
		return BroadcastDispositionAcceptAndRelay
	})

	_, privateKey := keyPairFromSeed(100)
	broadcast := testReceiverBroadcast(t, privateKey, Message{Overlay: bytes.Repeat([]byte{0xA5}, 32)})
	if err = transport.customHandler(&adnl.MessageCustom{Data: WrapMessage(overlayID, broadcast)}); err != nil {
		t.Fatalf("route broadcast through replacement receiver failed: %v", err)
	}
	if resolverCalls != 1 || deliveries != 1 {
		t.Fatalf("resolver calls=%d deliveries=%d, want 1/1", resolverCalls, deliveries)
	}
}

func TestBroadcastReceiverSharesFECStateAcrossConnections(t *testing.T) {
	overlayID := bytes.Repeat([]byte{0xB1}, 32)
	receiver := newTestBroadcastReceiver(t, overlayID)

	firstADNL := newMockADNL()
	firstADNL.id = bytes.Repeat([]byte{0xB2}, 32)
	firstTransport := CreateExtendedADNL(firstADNL)
	firstOverlay, err := firstTransport.AttachOverlay(receiver)
	if err != nil {
		t.Fatalf("attach first connection failed: %v", err)
	}
	defer firstOverlay.Close()

	secondADNL := newMockADNL()
	secondADNL.id = bytes.Repeat([]byte{0xB3}, 32)
	secondTransport := CreateExtendedADNL(secondADNL)
	secondOverlay, err := secondTransport.AttachOverlay(receiver)
	if err != nil {
		t.Fatalf("attach second connection failed: %v", err)
	}
	defer secondOverlay.Close()

	_, privateKey := keyPairFromSeed(92)
	payload := Broadcast{
		Source:      ed25519Public(privateKey),
		Certificate: CertificateEmpty{},
		Data:        bytes.Repeat([]byte{0xB4}, 768),
		Date:        1,
	}
	expectedPayload, err := tl.Serialize(payload, true)
	if err != nil {
		t.Fatalf("serialize expected FEC payload: %v", err)
	}
	sender, err := NewBroadcastFECSenderFromTL(
		privateKey,
		CertificateEmpty{},
		payload,
		BroadcastFlagAnySender,
		WithBroadcastFECSymbolSize(64),
	)
	if err != nil {
		t.Fatalf("new fec sender failed: %v", err)
	}

	deliveries := 0
	var info BroadcastInfo
	receiver.SetBroadcastHandlerWithInfo(func(msg tl.Serializable, gotInfo BroadcastInfo) BroadcastDisposition {
		deliveries++
		info = gotInfo
		return BroadcastDispositionAcceptAndRelay
	})

	var completingPeerID []byte
	for seqno := uint32(0); seqno < sender.fec.SymbolsCount+4 && deliveries == 0; seqno++ {
		part, partErr := sender.Part(seqno)
		if partErr != nil {
			t.Fatalf("build fec part %d failed: %v", seqno, partErr)
		}

		transport := firstTransport
		peerID := firstADNL.id
		if seqno%2 == 1 {
			transport = secondTransport
			peerID = secondADNL.id
		}
		before := deliveries
		if err = transport.customHandler(&adnl.MessageCustom{Data: WrapMessage(overlayID, *part.Full)}); err != nil {
			t.Fatalf("process fec part %d failed: %v", seqno, err)
		}
		if deliveries != before {
			completingPeerID = peerID
		}
	}

	if deliveries != 1 {
		t.Fatalf("shared receiver deliveries=%d, want 1", deliveries)
	}
	if info.Delivery != BroadcastDeliveryFEC || !bytes.Equal(info.ImmediatePeerID, completingPeerID) ||
		!bytes.Equal(info.Payload, expectedPayload) {
		t.Fatalf("unexpected fec delivery info: %#v, completing peer %x", info, completingPeerID)
	}
}

func TestBroadcastReceiverRejectedSimpleBroadcastCanRetry(t *testing.T) {
	overlayID := bytes.Repeat([]byte{0xC1}, 32)
	receiver := newTestBroadcastReceiver(t, overlayID)
	transport := newMockADNL()
	wrapper := CreateExtendedADNL(transport)
	wrapper.SetBroadcastReceiverResolver(func([]byte) (*BroadcastReceiver, error) {
		return receiver, nil
	})

	_, privateKey := keyPairFromSeed(93)
	broadcast := testReceiverBroadcast(t, privateKey, Message{Overlay: bytes.Repeat([]byte{0xC2}, 32)})
	handlerCalls := 0
	receiver.SetBroadcastHandlerWithInfo(func(tl.Serializable, BroadcastInfo) BroadcastDisposition {
		handlerCalls++
		if handlerCalls == 1 {
			return BroadcastDispositionRetry
		}
		return BroadcastDispositionAcceptAndRelay
	})

	message := &adnl.MessageCustom{Data: WrapMessage(overlayID, broadcast)}
	for i := 0; i < 3; i++ {
		if err := transport.customHandler(message); err != nil {
			t.Fatalf("broadcast attempt %d failed: %v", i+1, err)
		}
	}
	if handlerCalls != 2 {
		t.Fatalf("handler calls=%d, want rejected attempt plus successful retry", handlerCalls)
	}
}

func TestBroadcastReceiverIgnoredSimpleBroadcastIsCommittedWithoutRelay(t *testing.T) {
	overlayID := bytes.Repeat([]byte{0xC3}, 32)
	receiver := newTestBroadcastReceiver(t, overlayID)
	transport := newMockADNL()
	wrapper := CreateExtendedADNL(transport)
	o, err := wrapper.AttachOverlay(receiver)
	if err != nil {
		t.Fatalf("attach receiver failed: %v", err)
	}

	relayPeer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0xC4}, 32)}
	receiver.EnableBroadcastSimpleRelay(bytes.Repeat([]byte{0xC5}, 32), mockBroadcastPeerSet{peers: []BroadcastPeer{relayPeer}})
	handlerCalls := 0
	receiver.SetBroadcastHandlerWithInfo(func(tl.Serializable, BroadcastInfo) BroadcastDisposition {
		handlerCalls++
		return BroadcastDispositionIgnore
	})

	_, privateKey := keyPairFromSeed(95)
	broadcast := testReceiverBroadcast(t, privateKey, Message{Overlay: bytes.Repeat([]byte{0xC6}, 32)})
	if err = o.processBroadcast(&broadcast, transport.id); err != nil {
		t.Fatalf("process ignored broadcast failed: %v", err)
	}

	duplicate := broadcast
	duplicate.Signature = []byte("bad signature")
	if err = o.processBroadcast(&duplicate, transport.id); err != nil {
		t.Fatalf("ignored duplicate was not suppressed before signature verification: %v", err)
	}
	if handlerCalls != 1 {
		t.Fatalf("handler calls=%d, want 1", handlerCalls)
	}
	if len(relayPeer.sent) != 0 {
		t.Fatalf("ignored broadcast relayed %d times", len(relayPeer.sent))
	}
}

func TestBroadcastReceiverMalformedSignedSimpleBroadcastIsCommitted(t *testing.T) {
	overlayID := bytes.Repeat([]byte{0xD1}, 32)
	receiver := newTestBroadcastReceiver(t, overlayID)
	transport := newMockADNL()
	wrapper := CreateExtendedADNL(transport)
	o, err := wrapper.AttachOverlay(receiver)
	if err != nil {
		t.Fatalf("attach receiver failed: %v", err)
	}

	_, privateKey := keyPairFromSeed(99)
	broadcast := Broadcast{
		Source:      ed25519Public(privateKey),
		Certificate: CertificateEmpty{},
		Flags:       BroadcastFlagAnySender,
		Data:        []byte{0xFF, 0xFF, 0xFF, 0xFF},
		Date:        int32(time.Now().Unix()),
	}
	if err = broadcast.Sign(privateKey); err != nil {
		t.Fatalf("sign malformed broadcast failed: %v", err)
	}

	handlerCalls := 0
	receiver.SetBroadcastHandlerWithInfo(func(tl.Serializable, BroadcastInfo) BroadcastDisposition {
		handlerCalls++
		return BroadcastDispositionAcceptAndRelay
	})

	err = o.processBroadcast(&broadcast, transport.id)
	if err == nil || !strings.Contains(err.Error(), "failed to parse broadcast message") {
		t.Fatalf("malformed signed broadcast error = %v", err)
	}

	duplicate := broadcast
	duplicate.Signature = []byte("bad signature")
	if err = o.processBroadcast(&duplicate, transport.id); err != nil {
		t.Fatalf("committed malformed duplicate was reprocessed: %v", err)
	}
	if handlerCalls != 0 {
		t.Fatalf("malformed broadcast handler calls = %d, want 0", handlerCalls)
	}
}

func TestBroadcastReceiverFECRelayDoesNotEnableSimpleRelay(t *testing.T) {
	overlayID := bytes.Repeat([]byte{0xC7}, 32)
	receiver := newTestBroadcastReceiver(t, overlayID)
	transport := newMockADNL()
	wrapper := CreateExtendedADNL(transport)
	o, err := wrapper.AttachOverlay(receiver)
	if err != nil {
		t.Fatalf("attach receiver failed: %v", err)
	}

	relayPeer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0xC8}, 32)}
	receiver.EnableBroadcastFECRelay(bytes.Repeat([]byte{0xC9}, 32), mockBroadcastPeerSet{peers: []BroadcastPeer{relayPeer}})
	_, privateKey := keyPairFromSeed(96)
	broadcast := testReceiverBroadcast(t, privateKey, Message{Overlay: bytes.Repeat([]byte{0xCA}, 32)})
	if err = o.processBroadcast(&broadcast, transport.id); err != nil {
		t.Fatalf("process simple broadcast failed: %v", err)
	}
	if len(relayPeer.sent) != 0 {
		t.Fatalf("FEC-only relay configuration relayed %d simple broadcasts", len(relayPeer.sent))
	}
}

func TestBroadcastReceiverSimpleWaiterRetriesAfterOwnerRetry(t *testing.T) {
	overlayID := bytes.Repeat([]byte{0xCB}, 32)
	receiver := newTestBroadcastReceiver(t, overlayID)
	transport := newMockADNL()
	wrapper := CreateExtendedADNL(transport)
	o, err := wrapper.AttachOverlay(receiver)
	if err != nil {
		t.Fatalf("attach receiver failed: %v", err)
	}

	_, privateKey := keyPairFromSeed(97)
	broadcast := testReceiverBroadcast(t, privateKey, Message{Overlay: bytes.Repeat([]byte{0xCC}, 32)})
	broadcastID, _, err := calcBroadcastID(broadcast.Source, broadcast.Flags, broadcast.Data)
	if err != nil {
		t.Fatalf("calculate broadcast id failed: %v", err)
	}
	owner := receiver.fecState.beginSimpleAdmission(testBroadcastSimpleIDKey(broadcastID))
	if owner.status != broadcastAdmissionOwner {
		t.Fatalf("initial admission status=%d, want owner", owner.status)
	}

	handled := make(chan struct{}, 1)
	receiver.SetBroadcastHandlerWithInfo(func(tl.Serializable, BroadcastInfo) BroadcastDisposition {
		handled <- struct{}{}
		return BroadcastDispositionAcceptAndRelay
	})
	result := make(chan error, 1)
	go func() {
		result <- o.processBroadcast(&broadcast, transport.id)
	}()

	receiver.fecState.finishSimpleAdmission(testBroadcastSimpleIDKey(broadcastID), owner.admission, BroadcastDispositionRetry)
	if err = <-result; err != nil {
		t.Fatalf("valid waiter failed after owner retry: %v", err)
	}
	select {
	case <-handled:
	default:
		t.Fatal("valid waiter did not rerun admission")
	}
}

func TestBroadcastReceiverValidSimpleWaiterRetriesAfterInvalidOwner(t *testing.T) {
	overlayID := bytes.Repeat([]byte{0xCE}, 32)
	receiver := newTestBroadcastReceiver(t, overlayID)
	transport := newMockADNL()
	wrapper := CreateExtendedADNL(transport)
	o, err := wrapper.AttachOverlay(receiver)
	if err != nil {
		t.Fatalf("attach receiver failed: %v", err)
	}

	_, privateKey := keyPairFromSeed(98)
	valid := testReceiverBroadcast(t, privateKey, Message{Overlay: bytes.Repeat([]byte{0xCF}, 32)})
	invalid := valid
	invalid.Signature = []byte("bad signature")

	ownerEntered := make(chan struct{})
	releaseOwner := make(chan struct{})
	var blockOwner sync.Once
	receiver.SetBroadcastPrecheckHandler(func(info BroadcastPrecheckInfo) error {
		if !info.SignatureChecked {
			blockOwner.Do(func() {
				close(ownerEntered)
				<-releaseOwner
			})
		}
		return nil
	})
	handlerCalls := 0
	receiver.SetBroadcastHandlerWithInfo(func(tl.Serializable, BroadcastInfo) BroadcastDisposition {
		handlerCalls++
		return BroadcastDispositionAcceptAndRelay
	})

	invalidResult := make(chan error, 1)
	go func() {
		invalidResult <- o.processBroadcast(&invalid, transport.id)
	}()
	<-ownerEntered

	validStarted := make(chan struct{})
	validResult := make(chan error, 1)
	go func() {
		close(validStarted)
		validResult <- o.processBroadcast(&valid, transport.id)
	}()
	<-validStarted
	for range 100 {
		runtime.Gosched()
	}
	close(releaseOwner)

	if err = <-invalidResult; err == nil {
		t.Fatal("invalid owner unexpectedly passed signature verification")
	}
	if err = <-validResult; err != nil {
		t.Fatalf("valid waiter failed after invalid owner: %v", err)
	}
	if handlerCalls != 1 {
		t.Fatalf("handler calls=%d, want only the valid waiter", handlerCalls)
	}
}

func TestBroadcastReceiverSimpleDeliveredCacheLimit(t *testing.T) {
	receiver := newTestBroadcastReceiver(t, bytes.Repeat([]byte{0xCD}, 32))
	if receiver.fecState.simpleMax != broadcastSimpleDeliveredCacheSize || receiver.fecState.simpleMax <= 100 {
		t.Fatalf("default simple delivered cache=%d, want %d and greater than 100", receiver.fecState.simpleMax, broadcastSimpleDeliveredCacheSize)
	}

	receiver.SetSimpleDeliveredCacheSize(2)
	for _, id := range []string{"first", "second", "third"} {
		key := testBroadcastSimpleIDKey([]byte(id))
		attempt := receiver.fecState.beginSimpleAdmission(key)
		if attempt.status != broadcastAdmissionOwner {
			t.Fatalf("admission %q status=%d, want owner", id, attempt.status)
		}
		receiver.fecState.finishSimpleAdmission(key, attempt.admission, BroadcastDispositionIgnore)
	}

	receiver.fecState.mx.Lock()
	firstDelivered := receiver.fecState.isSimpleDeliveredLocked(testBroadcastSimpleIDKey([]byte("first")))
	secondDelivered := receiver.fecState.isSimpleDeliveredLocked(testBroadcastSimpleIDKey([]byte("second")))
	thirdDelivered := receiver.fecState.isSimpleDeliveredLocked(testBroadcastSimpleIDKey([]byte("third")))
	receiver.fecState.mx.Unlock()
	if firstDelivered || !secondDelivered || !thirdDelivered {
		t.Fatalf("unexpected cache contents: first=%v second=%v third=%v", firstDelivered, secondDelivered, thirdDelivered)
	}
}

func testBroadcastSimpleIDKey(id []byte) broadcastSimpleIDKey {
	var key broadcastSimpleIDKey
	copy(key[:], id)
	return key
}

func TestAttachOverlayReplacesOnlyClosedReceiver(t *testing.T) {
	overlayID := bytes.Repeat([]byte{0xD1}, 32)
	transport := CreateExtendedADNL(newMockADNL())
	first := newTestBroadcastReceiver(t, overlayID)
	second := newTestBroadcastReceiver(t, overlayID)

	firstWrapper, err := transport.AttachOverlay(first)
	if err != nil {
		t.Fatalf("attach first receiver failed: %v", err)
	}
	sameWrapper, err := transport.AttachOverlay(first)
	if err != nil || sameWrapper != firstWrapper {
		t.Fatalf("same receiver attach must be idempotent, wrapper=%p err=%v", sameWrapper, err)
	}
	if _, err = transport.AttachOverlay(second); err == nil {
		t.Fatal("active receiver replacement must fail")
	}

	first.SetActive(false)
	if _, err = transport.AttachOverlay(second); err == nil {
		t.Fatal("inactive but open receiver replacement must fail")
	}

	first.Close()
	secondWrapper, err := transport.AttachOverlay(second)
	if err != nil {
		t.Fatalf("closed receiver replacement failed: %v", err)
	}
	firstWrapper.Close()
	if got := transport.overlays[second.overlayKey]; got != secondWrapper {
		t.Fatalf("closing stale wrapper detached replacement: got %p want %p", got, secondWrapper)
	}

	secondWrapper.Close()
	if len(transport.overlays) != 0 {
		t.Fatalf("closing current wrapper must detach it")
	}
}

func BenchmarkBroadcastReceiverResolverDuplicateSimple(b *testing.B) {
	overlayID := bytes.Repeat([]byte{0xE1}, 32)
	receiver := newTestBroadcastReceiver(b, overlayID)
	transport := newMockADNL()
	wrapper := CreateExtendedADNL(transport)
	wrapper.SetBroadcastReceiverResolver(func([]byte) (*BroadcastReceiver, error) {
		return receiver, nil
	})

	_, privateKey := keyPairFromSeed(94)
	broadcast := testReceiverBroadcast(b, privateKey, Message{Overlay: bytes.Repeat([]byte{0xE2}, 32)})
	message := &adnl.MessageCustom{Data: WrapMessage(overlayID, broadcast)}
	if err := transport.customHandler(message); err != nil {
		b.Fatalf("prime delivered cache failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := transport.customHandler(message); err != nil {
			b.Fatalf("route duplicate broadcast failed: %v", err)
		}
	}
}
