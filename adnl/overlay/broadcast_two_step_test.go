package overlay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

type broadcastTwoStepTestSigner struct {
	key   ed25519.PrivateKey
	err   error
	calls atomic.Int32
}

type twoStepRelayTestPeer struct {
	id   []byte
	send func(context.Context, []byte) error

	calls atomic.Int32
}

type twoStepLegacyRelayTestPeer struct {
	id       []byte
	received chan<- tl.Serializable
	calls    atomic.Int32
}

func (p *twoStepLegacyRelayTestPeer) ID() []byte {
	return p.id
}

func (p *twoStepLegacyRelayTestPeer) SendCustomMessage(_ context.Context, msg tl.Serializable) error {
	p.calls.Add(1)
	p.received <- msg
	return nil
}

func (p *twoStepRelayTestPeer) ID() []byte {
	return p.id
}

func (p *twoStepRelayTestPeer) SendCustomMessage(ctx context.Context, _ tl.Serializable) error {
	return p.sendMessage(ctx, nil)
}

func (p *twoStepRelayTestPeer) SendPreparedCustomMessage(ctx context.Context, body []byte) error {
	return p.sendMessage(ctx, body)
}

func (p *twoStepRelayTestPeer) sendMessage(ctx context.Context, body []byte) error {
	p.calls.Add(1)
	if p.send != nil {
		return p.send(ctx, body)
	}
	return nil
}

func mustReserveTwoStepRelayPayload(t testing.TB, dispatcher *broadcastTwoStepRelayDispatcher, body []byte, refs int) *broadcastTwoStepRelayPayload {
	t.Helper()

	payload, ok := dispatcher.reservePayload(nil, NewPreparedBroadcastMessage(body), refs)
	if !ok {
		t.Fatal("failed to reserve relay test payload")
	}
	return payload
}

func (s *broadcastTwoStepTestSigner) PublicKey() ed25519.PublicKey {
	return s.key.Public().(ed25519.PublicKey)
}

func (s *broadcastTwoStepTestSigner) Sign(payload []byte) ([]byte, error) {
	s.calls.Add(1)
	if s.err != nil {
		return nil, s.err
	}

	return ed25519.Sign(s.key, payload), nil
}

type broadcastTwoStepStreamingSigner struct {
	key                   ed25519.PrivateKey
	calls                 atomic.Int32
	secondCallStarted     chan struct{}
	releaseRemainingCalls <-chan struct{}
}

func (s *broadcastTwoStepStreamingSigner) PublicKey() ed25519.PublicKey {
	return s.key.Public().(ed25519.PublicKey)
}

func (s *broadcastTwoStepStreamingSigner) Sign(payload []byte) ([]byte, error) {
	if s.calls.Add(1) > 1 {
		select {
		case s.secondCallStarted <- struct{}{}:
		default:
		}
		<-s.releaseRemainingCalls
	}

	return ed25519.Sign(s.key, payload), nil
}

func TestBroadcastTwoStepSignAndIDHelpers(t *testing.T) {
	_, priv := keyPairFromSeed(71)
	sourceADNL := bytes.Repeat([]byte{0xA1}, 32)
	extra := []byte("extra")
	payload := []byte("payload")
	date := uint32(12345)

	simple := &BroadcastTwoStepSimple{
		Flags:       BroadcastFlagNoTwoStep,
		Date:        date,
		Source:      ed25519Public(priv),
		SourceADNL:  sourceADNL,
		Certificate: CertificateEmpty{},
		Data:        payload,
		Extra:       extra,
	}
	if err := simple.Sign(priv); err != nil {
		t.Fatalf("simple sign failed: %v", err)
	}
	if err := simple.VerifySignature(); err != nil {
		t.Fatalf("simple verify failed: %v", err)
	}

	sourceID, err := tl.Hash(simple.Source)
	if err != nil {
		t.Fatalf("source id failed: %v", err)
	}
	dataHash := calcBroadcastTwoStepDataHash(payload)
	expectedID, err := calcBroadcastTwoStepIDFromSourceID(sourceID, simple.Flags, date, sourceADNL, dataHash, uint32(len(payload)), uint32(len(payload)), extra)
	if err != nil {
		t.Fatalf("expected id failed: %v", err)
	}
	gotID, err := simple.CalcID()
	if err != nil {
		t.Fatalf("simple id failed: %v", err)
	}
	if !bytes.Equal(gotID, expectedID) {
		t.Fatalf("unexpected simple id")
	}
	rawSimple, err := tl.Serialize(simple, true)
	if err != nil {
		t.Fatalf("simple serialize failed: %v", err)
	}
	var parsedSimple any
	if _, err = tl.Parse(&parsedSimple, rawSimple, true); err != nil {
		t.Fatalf("simple parse failed: %v", err)
	}
	if _, ok := parsedSimple.(BroadcastTwoStepSimple); !ok {
		t.Fatalf("expected parsed simple type, got %T", parsedSimple)
	}

	fec := &BroadcastTwoStepFEC{
		Flags:       0,
		Date:        date,
		Source:      ed25519Public(priv),
		SourceADNL:  sourceADNL,
		Certificate: CertificateEmpty{},
		DataHash:    dataHash,
		DataSize:    uint32(len(payload)),
		Seqno:       2,
		Part:        []byte("symbol"),
		Extra:       extra,
	}
	if err = fec.Sign(priv); err != nil {
		t.Fatalf("fec sign failed: %v", err)
	}
	if err = fec.VerifySignature(); err != nil {
		t.Fatalf("fec verify failed: %v", err)
	}
	fec.Signature[0] ^= 0x80
	if err = fec.VerifySignature(); err == nil {
		t.Fatalf("expected changed fec signature to fail")
	}
	fec.Signature[0] ^= 0x80
	rawFEC, err := tl.Serialize(fec, true)
	if err != nil {
		t.Fatalf("fec serialize failed: %v", err)
	}
	var parsedFEC any
	if _, err = tl.Parse(&parsedFEC, rawFEC, true); err != nil {
		t.Fatalf("fec parse failed: %v", err)
	}
	if _, ok := parsedFEC.(BroadcastTwoStepFEC); !ok {
		t.Fatalf("expected parsed fec type, got %T", parsedFEC)
	}
}

func TestSendBroadcastTwoStepSimple(t *testing.T) {
	_, priv := keyPairFromSeed(72)
	localID := bytes.Repeat([]byte{0x01}, 32)
	peers := mockBroadcastPeerSet{peers: []BroadcastPeer{
		&mockBroadcastPeer{id: bytes.Repeat([]byte{0x02}, 32)},
		&mockBroadcastPeer{id: bytes.Repeat([]byte{0x03}, 32)},
		&mockBroadcastPeer{id: bytes.Repeat([]byte{0x04}, 32)},
	}}

	res, err := SendBroadcastTwoStep(context.Background(), BroadcastTwoStepSendRequest{
		Key:         priv,
		Certificate: CertificateEmpty{},
		LocalADNLID: localID,
		Payload:     []byte("small"),
		Extra:       []byte("e"),
		Flags:       BroadcastFlagNoTwoStep,
		PeerSet:     peers,
	},
		WithBroadcastTwoStepDate(111),
	)
	if err != nil {
		t.Fatalf("send simple failed: %v", err)
	}
	if res.Mode != BroadcastTwoStepModeSimple || res.Attempted != len(peers.peers) || res.Sent != len(peers.peers) || len(res.Failed) != 0 {
		t.Fatalf("unexpected simple result: %#v", res)
	}

	for _, p := range peers.peers {
		peer := p.(*mockBroadcastPeer)
		if len(peer.sent) != 1 {
			t.Fatalf("peer %x got %d messages", peer.id, len(peer.sent))
		}
		msg, ok := peer.sent[0].(*BroadcastTwoStepSimple)
		if !ok {
			t.Fatalf("expected simple message, got %T", peer.sent[0])
		}
		if msg.Flags&BroadcastFlagNoTwoStep != 0 {
			t.Fatalf("no-two-step flag must be stripped")
		}
		if !bytes.Equal(msg.SourceADNL, localID) || !bytes.Equal(msg.Extra, []byte("e")) {
			t.Fatalf("unexpected simple metadata")
		}
		if err = msg.VerifySignature(); err != nil {
			t.Fatalf("simple wire signature invalid: %v", err)
		}
		id, err := msg.CalcID()
		if err != nil {
			t.Fatalf("simple wire id failed: %v", err)
		}
		if !bytes.Equal(id, res.BroadcastID) {
			t.Fatalf("result id differs from wire id")
		}
	}
}

func TestSendBroadcastTwoStepFanoutDoesNotWaitForSlowPeer(t *testing.T) {
	_, priv := keyPairFromSeed(89)
	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	secondSent := make(chan struct{})
	firstPeer := &mockBroadcastPeer{
		id: bytes.Repeat([]byte{0x31}, 32),
		sendFunc: func(ctx context.Context, _ tl.Serializable) error {
			close(firstStarted)
			select {
			case <-firstRelease:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	secondPeer := &mockBroadcastPeer{
		id: bytes.Repeat([]byte{0x32}, 32),
		sendFunc: func(context.Context, tl.Serializable) error {
			close(secondSent)
			return nil
		},
	}
	done := make(chan error, 1)

	go func() {
		_, err := SendBroadcastTwoStep(context.Background(), BroadcastTwoStepSendRequest{
			Key:         priv,
			Certificate: CertificateEmpty{},
			LocalADNLID: bytes.Repeat([]byte{0x30}, 32),
			Payload:     []byte("small"),
			PeerSet: mockBroadcastPeerSet{peers: []BroadcastPeer{
				firstPeer,
				secondPeer,
			}},
		}, WithBroadcastTwoStepDate(111))
		done <- err
	}()

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		close(firstRelease)
		t.Fatal("first peer was not called")
	}

	select {
	case <-secondSent:
	case <-time.After(time.Second):
		close(firstRelease)
		t.Fatal("second peer waited for the first peer")
	}

	close(firstRelease)
	if err := <-done; err != nil {
		t.Fatalf("parallel fanout failed: %v", err)
	}
}

func TestSendBroadcastTwoStepBoundedFanout(t *testing.T) {
	const concurrency = 4

	_, priv := keyPairFromSeed(90)
	localID := bytes.Repeat([]byte{0x40}, 32)

	var running, peakRunning atomic.Int32
	peers := mockBroadcastPeerSet{peers: make([]BroadcastPeer, 12)}
	arrived := make(chan struct{}, len(peers.peers))
	release := make(chan struct{})
	releaseAll := sync.OnceFunc(func() { close(release) })
	defer releaseAll()

	for i := range peers.peers {
		peers.peers[i] = &mockBroadcastPeer{
			id: bytes.Repeat([]byte{byte(0x41 + i)}, 32),
			sendFunc: func(context.Context, tl.Serializable) error {
				inFlight := running.Add(1)
				for {
					peak := peakRunning.Load()
					if inFlight <= peak || peakRunning.CompareAndSwap(peak, inFlight) {
						break
					}
				}

				arrived <- struct{}{}
				<-release
				running.Add(-1)

				return nil
			},
		}
	}

	type sendOutcome struct {
		res BroadcastTwoStepSendResult
		err error
	}
	done := make(chan sendOutcome, 1)
	go func() {
		res, err := SendBroadcastTwoStep(context.Background(), BroadcastTwoStepSendRequest{
			Key:         priv,
			Certificate: CertificateEmpty{},
			LocalADNLID: localID,
			Payload:     bytes.Repeat([]byte{0xB7}, 1024),
			PeerSet:     peers,
		},
			WithBroadcastTwoStepDate(230),
			WithBroadcastTwoStepSendConcurrency(concurrency),
		)
		done <- sendOutcome{res: res, err: err}
	}()

	for i := 0; i < concurrency; i++ {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d peers were dispatched in parallel", i, concurrency)
		}
	}

	// The bound must hold the rest back while the first batch is still stuck.
	select {
	case <-arrived:
		t.Fatal("dispatched more peers than the configured bound")
	case <-time.After(100 * time.Millisecond):
	}

	releaseAll()
	outcome := <-done
	if outcome.err != nil {
		t.Fatalf("bounded fanout failed: %v", outcome.err)
	}
	if peak := peakRunning.Load(); peak > concurrency {
		t.Fatalf("peak parallel sends %d, want at most %d", peak, concurrency)
	}
	if outcome.res.Mode != BroadcastTwoStepModeFEC {
		t.Fatalf("unexpected send mode %d", outcome.res.Mode)
	}
	if outcome.res.Attempted != len(peers.peers) || outcome.res.Sent != len(peers.peers) {
		t.Fatalf("unexpected send counters: %#v", outcome.res)
	}
	if outcome.res.Failed != nil {
		t.Fatalf("Failed must stay nil when every peer succeeds, got %#v", outcome.res.Failed)
	}

	for index, p := range peers.peers {
		peer := p.(*mockBroadcastPeer)
		if len(peer.sent) != 1 {
			t.Fatalf("peer %d got %d messages", index, len(peer.sent))
		}
		message, ok := peer.sent[0].(*BroadcastTwoStepFEC)
		if !ok {
			t.Fatalf("peer %d received %T, want FEC part", index, peer.sent[0])
		}
		if message.Seqno != uint32(index) {
			t.Fatalf("peer %d got seqno %d", index, message.Seqno)
		}
		if err := message.VerifySignature(); err != nil {
			t.Fatalf("peer %d signature: %v", index, err)
		}
	}
}

func TestSendBroadcastTwoStepFECStreamsSymbolsIntoFanout(t *testing.T) {
	const concurrency = 2

	_, privateKey := keyPairFromSeed(92)
	releaseSigners := make(chan struct{})
	releaseSends := make(chan struct{})
	releaseAll := sync.OnceFunc(func() {
		close(releaseSigners)
		close(releaseSends)
	})
	defer releaseAll()

	signer := &broadcastTwoStepStreamingSigner{
		key:                   privateKey,
		secondCallStarted:     make(chan struct{}, 1),
		releaseRemainingCalls: releaseSigners,
	}
	firstSendStarted := make(chan struct{})
	notifyFirstSend := sync.OnceFunc(func() { close(firstSendStarted) })
	peers := mockBroadcastPeerSet{peers: make([]BroadcastPeer, 9)}
	for i := range peers.peers {
		peers.peers[i] = &mockBroadcastPeer{
			id: bytes.Repeat([]byte{byte(0x80 + i)}, 32),
			sendFunc: func(ctx context.Context, _ tl.Serializable) error {
				notifyFirstSend()

				select {
				case <-releaseSends:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
		}
	}

	type sendOutcome struct {
		result BroadcastTwoStepSendResult
		err    error
	}
	done := make(chan sendOutcome, 1)
	go func() {
		result, err := SendBroadcastTwoStep(t.Context(), BroadcastTwoStepSendRequest{
			Signer:      signer,
			Certificate: CertificateEmpty{},
			LocalADNLID: bytes.Repeat([]byte{0x79}, 32),
			Payload:     bytes.Repeat([]byte{0xE2}, 4096),
			PeerSet:     peers,
		},
			WithBroadcastTwoStepDate(232),
			WithBroadcastTwoStepSendConcurrency(concurrency),
		)
		done <- sendOutcome{result: result, err: err}
	}()

	select {
	case <-signer.secondCallStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("second FEC part did not reach signing")
	}
	select {
	case <-firstSendStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("first peer send waited for remaining FEC parts")
	}

	// Both bounded jobs are occupied: one is sending and the other is paused
	// after symbol generation. The untouched jobs cannot have generated their
	// symbols yet, so observing the send here proves generation and delivery
	// are streamed instead of separated by an all-symbols barrier.
	if calls := signer.calls.Load(); calls != concurrency {
		t.Fatalf("signer calls before first send = %d, want %d", calls, concurrency)
	}

	releaseAll()
	outcome := <-done
	if outcome.err != nil {
		t.Fatalf("streamed FEC send failed: %v", outcome.err)
	}
	if outcome.result.Mode != BroadcastTwoStepModeFEC || outcome.result.Sent != len(peers.peers) {
		t.Fatalf("unexpected streamed FEC result: %#v", outcome.result)
	}
}

func TestSendBroadcastTwoStepPeerSendTimeoutIsIndependent(t *testing.T) {
	_, privateKey := keyPairFromSeed(93)
	fastSent := make(chan struct{})
	slowPeer := &mockBroadcastPeer{
		id: bytes.Repeat([]byte{0x90}, 32),
		sendFunc: func(ctx context.Context, _ tl.Serializable) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	fastPeer := &mockBroadcastPeer{
		id: bytes.Repeat([]byte{0x91}, 32),
		sendFunc: func(context.Context, tl.Serializable) error {
			close(fastSent)
			return nil
		},
	}

	result, err := SendBroadcastTwoStep(t.Context(), BroadcastTwoStepSendRequest{
		Key:         privateKey,
		Certificate: CertificateEmpty{},
		LocalADNLID: bytes.Repeat([]byte{0x8F}, 32),
		Payload:     []byte("small"),
		PeerSet:     mockBroadcastPeerSet{peers: []BroadcastPeer{slowPeer, fastPeer}},
	},
		WithBroadcastTwoStepDate(233),
		WithBroadcastTwoStepPeerSendTimeout(100*time.Millisecond),
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("send error = %v, want peer deadline", err)
	}
	select {
	case <-fastSent:
	default:
		t.Fatal("fast peer was not sent while slow peer timed out")
	}
	if result.Attempted != 2 || result.Sent != 1 || len(result.Failed) != 1 {
		t.Fatalf("unexpected timeout result: %#v", result)
	}
	if !bytes.Equal(result.Failed[0].PeerID, slowPeer.id) || !errors.Is(result.Failed[0].Err, context.DeadlineExceeded) {
		t.Fatalf("unexpected timed out peer: %#v", result.Failed[0])
	}
}

func TestSendBroadcastTwoStepBoundedFanoutIsolatesFailedPeer(t *testing.T) {
	_, priv := keyPairFromSeed(91)
	localID := bytes.Repeat([]byte{0x50}, 32)
	sendErr := errors.New("send failed")

	peers := mockBroadcastPeerSet{peers: make([]BroadcastPeer, 9)}
	for i := range peers.peers {
		peers.peers[i] = &mockBroadcastPeer{id: bytes.Repeat([]byte{byte(0x51 + i)}, 32)}
	}
	badPeer := peers.peers[5].(*mockBroadcastPeer)
	badPeer.sendErr = sendErr

	res, err := SendBroadcastTwoStep(context.Background(), BroadcastTwoStepSendRequest{
		Key:         priv,
		Certificate: CertificateEmpty{},
		LocalADNLID: localID,
		Payload:     bytes.Repeat([]byte{0xB8}, 1024),
		PeerSet:     peers,
	},
		WithBroadcastTwoStepDate(231),
		WithBroadcastTwoStepSendConcurrency(2),
	)
	if !errors.Is(err, sendErr) {
		t.Fatalf("send error = %v, want peer send error", err)
	}
	if res.Attempted != len(peers.peers) || res.Sent != len(peers.peers)-1 || len(res.Failed) != 1 {
		t.Fatalf("unexpected send counters: %#v", res)
	}
	if !bytes.Equal(res.Failed[0].PeerID, badPeer.id) || res.Failed[0].Err != sendErr {
		t.Fatalf("unexpected failed peer: %#v", res.Failed[0])
	}

	for index, p := range peers.peers {
		peer := p.(*mockBroadcastPeer)
		message, ok := peer.sent[0].(*BroadcastTwoStepFEC)
		if !ok {
			t.Fatalf("peer %d received %T, want FEC part", index, peer.sent[0])
		}
		if message.Seqno != uint32(index) {
			t.Fatalf("peer %d got seqno %d", index, message.Seqno)
		}
	}
}

func TestSendBroadcastTwoStepUsesSigner(t *testing.T) {
	_, privateKey := keyPairFromSeed(84)
	signer := &broadcastTwoStepTestSigner{key: privateKey}
	localID := bytes.Repeat([]byte{0x5A}, 32)
	peers := mockBroadcastPeerSet{peers: make([]BroadcastPeer, 5)}
	for i := range peers.peers {
		peers.peers[i] = &mockBroadcastPeer{id: bytes.Repeat([]byte{byte(0x60 + i)}, 32)}
	}

	result, err := SendBroadcastTwoStep(context.Background(), BroadcastTwoStepSendRequest{
		Signer:      signer,
		Certificate: CertificateEmpty{},
		LocalADNLID: localID,
		Payload:     bytes.Repeat([]byte{0xE1}, 1024),
		PeerSet:     peers,
	}, WithBroadcastTwoStepDate(226))
	if err != nil {
		t.Fatalf("send with signer failed: %v", err)
	}
	if result.Mode != BroadcastTwoStepModeFEC {
		t.Fatalf("unexpected send mode %d", result.Mode)
	}
	if signer.calls.Load() != int32(len(peers.peers)) {
		t.Fatalf("signer calls = %d, want %d", signer.calls.Load(), len(peers.peers))
	}

	for index, peer := range peers.peers {
		message, ok := peer.(*mockBroadcastPeer).sent[0].(*BroadcastTwoStepFEC)
		if !ok {
			t.Fatalf("peer %d received %T, want FEC part", index, peer.(*mockBroadcastPeer).sent[0])
		}
		if err = message.VerifySignature(); err != nil {
			t.Fatalf("peer %d signature: %v", index, err)
		}
		if !bytes.Equal(message.Source.(keys.PublicKeyED25519).Key, signer.PublicKey()) {
			t.Fatalf("peer %d source key differs from signer", index)
		}
	}
}

func TestSendBroadcastTwoStepSignerErrors(t *testing.T) {
	_, privateKey := keyPairFromSeed(85)
	signErr := errors.New("sign failed")
	signer := &broadcastTwoStepTestSigner{key: privateKey, err: signErr}
	peer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0x71}, 32)}

	_, err := SendBroadcastTwoStep(context.Background(), BroadcastTwoStepSendRequest{
		Signer:      signer,
		LocalADNLID: bytes.Repeat([]byte{0x70}, 32),
		Payload:     []byte("payload"),
		PeerSet:     mockBroadcastPeerSet{peers: []BroadcastPeer{peer}},
	})
	if !errors.Is(err, signErr) {
		t.Fatalf("send error = %v, want signer error", err)
	}
	if len(peer.sent) != 0 {
		t.Fatalf("sent %d messages after signer failure", len(peer.sent))
	}

	_, err = SendBroadcastTwoStep(context.Background(), BroadcastTwoStepSendRequest{
		Key:         privateKey,
		Signer:      signer,
		LocalADNLID: bytes.Repeat([]byte{0x70}, 32),
		Payload:     []byte("payload"),
		PeerSet:     mockBroadcastPeerSet{peers: []BroadcastPeer{peer}},
	})
	if err == nil {
		t.Fatal("expected mutually exclusive key and signer error")
	}
}

func TestSendBroadcastTwoStepSkipsLocalPeer(t *testing.T) {
	_, priv := keyPairFromSeed(82)
	localID := bytes.Repeat([]byte{0x18}, 32)
	firstPeer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0x19}, 32)}
	localPeer := &mockBroadcastPeer{id: localID}
	secondPeer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0x1A}, 32)}
	peers := mockBroadcastPeerSet{peers: []BroadcastPeer{firstPeer, localPeer, secondPeer}}

	res, err := SendBroadcastTwoStep(context.Background(), BroadcastTwoStepSendRequest{
		Key:         priv,
		Certificate: CertificateEmpty{},
		LocalADNLID: localID,
		Payload:     []byte("small"),
		PeerSet:     peers,
	},
		WithBroadcastTwoStepDate(112),
	)
	if err != nil {
		t.Fatalf("send simple failed: %v", err)
	}
	if res.Attempted != 2 || res.Sent != 2 || len(res.Failed) != 0 {
		t.Fatalf("unexpected send result: %#v", res)
	}
	if len(localPeer.sent) != 0 {
		t.Fatalf("local peer must not receive sender broadcast")
	}
	if len(firstPeer.sent) != 1 || len(secondPeer.sent) != 1 {
		t.Fatalf("non-local peers must receive sender broadcast")
	}
}

func TestSendBroadcastTwoStepFEC(t *testing.T) {
	_, priv := keyPairFromSeed(73)
	localID := bytes.Repeat([]byte{0x11}, 32)
	peers := mockBroadcastPeerSet{peers: make([]BroadcastPeer, 5)}
	for i := range peers.peers {
		peers.peers[i] = &mockBroadcastPeer{id: bytes.Repeat([]byte{byte(0x20 + i)}, 32)}
	}

	payload := bytes.Repeat([]byte{0xCC}, 1024)
	res, err := SendBroadcastTwoStep(context.Background(), BroadcastTwoStepSendRequest{
		Key:         priv,
		Certificate: CertificateEmpty{},
		LocalADNLID: localID,
		Payload:     payload,
		Extra:       []byte("extra"),
		PeerSet:     peers,
	},
		WithBroadcastTwoStepDate(222),
	)
	if err != nil {
		t.Fatalf("send fec failed: %v", err)
	}
	if res.Mode != BroadcastTwoStepModeFEC || res.Attempted != len(peers.peers) || res.Sent != len(peers.peers) || len(res.Failed) != 0 {
		t.Fatalf("unexpected fec result: %#v", res)
	}
	if res.PartSize != 512 {
		t.Fatalf("unexpected fec part size %d", res.PartSize)
	}

	for i, p := range peers.peers {
		peer := p.(*mockBroadcastPeer)
		if len(peer.sent) != 1 {
			t.Fatalf("peer %d got %d messages", i, len(peer.sent))
		}
		msg, ok := peer.sent[0].(*BroadcastTwoStepFEC)
		if !ok {
			t.Fatalf("expected fec message, got %T", peer.sent[0])
		}
		if msg.Seqno != uint32(i) || uint32(len(msg.Part)) != res.PartSize {
			t.Fatalf("unexpected fec part seqno=%d size=%d", msg.Seqno, len(msg.Part))
		}
		if err = msg.VerifySignature(); err != nil {
			t.Fatalf("fec wire signature invalid: %v", err)
		}
		id, err := msg.CalcID()
		if err != nil {
			t.Fatalf("fec wire id failed: %v", err)
		}
		if !bytes.Equal(id, res.BroadcastID) {
			t.Fatalf("result id differs from wire id")
		}
	}
}

func TestSendBroadcastTwoStepFECSkipsLocalBeforeSeqno(t *testing.T) {
	_, priv := keyPairFromSeed(83)
	localID := bytes.Repeat([]byte{0x28}, 32)
	localPeer := &mockBroadcastPeer{id: localID}
	peers := mockBroadcastPeerSet{peers: []BroadcastPeer{localPeer}}
	for i := 0; i < 5; i++ {
		peers.peers = append(peers.peers, &mockBroadcastPeer{id: bytes.Repeat([]byte{byte(0x29 + i)}, 32)})
	}

	res, err := SendBroadcastTwoStep(context.Background(), BroadcastTwoStepSendRequest{
		Key:         priv,
		Certificate: CertificateEmpty{},
		LocalADNLID: localID,
		Payload:     bytes.Repeat([]byte{0xCD}, 1024),
		PeerSet:     peers,
	},
		WithBroadcastTwoStepDate(225),
	)
	if err != nil {
		t.Fatalf("send fec failed: %v", err)
	}
	if res.Mode != BroadcastTwoStepModeFEC || res.Attempted != 5 || res.Sent != 5 {
		t.Fatalf("unexpected fec result: %#v", res)
	}
	if len(localPeer.sent) != 0 {
		t.Fatalf("local peer must not receive sender FEC part")
	}
	for i, p := range peers.peers[1:] {
		peer := p.(*mockBroadcastPeer)
		if len(peer.sent) != 1 {
			t.Fatalf("peer %d got %d messages", i, len(peer.sent))
		}
		msg, ok := peer.sent[0].(*BroadcastTwoStepFEC)
		if !ok {
			t.Fatalf("expected fec message, got %T", peer.sent[0])
		}
		if msg.Seqno != uint32(i) {
			t.Fatalf("unexpected compact seqno %d, want %d", msg.Seqno, i)
		}
	}
}

func TestSendBroadcastTwoStepFECThresholdTooFewPeers(t *testing.T) {
	_, priv := keyPairFromSeed(79)
	localID := bytes.Repeat([]byte{0x12}, 32)
	peer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0x13}, 32)}
	peers := mockBroadcastPeerSet{peers: []BroadcastPeer{peer}}

	res, err := SendBroadcastTwoStep(context.Background(), BroadcastTwoStepSendRequest{
		Key:         priv,
		Certificate: CertificateEmpty{},
		LocalADNLID: localID,
		Payload:     bytes.Repeat([]byte{0xCE}, 1024),
		PeerSet:     peers,
	},
		WithBroadcastTwoStepDate(223),
		WithBroadcastTwoStepFECThreshold(1, 1),
	)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if res.Mode != BroadcastTwoStepModeSimple {
		t.Fatalf("expected simple mode for one peer, got %#v", res)
	}
	if len(peer.sent) != 1 {
		t.Fatalf("expected one message, got %d", len(peer.sent))
	}
	if _, ok := peer.sent[0].(*BroadcastTwoStepSimple); !ok {
		t.Fatalf("expected simple message, got %T", peer.sent[0])
	}
}

func TestSendBroadcastTwoStepPeerFailures(t *testing.T) {
	_, priv := keyPairFromSeed(80)
	localID := bytes.Repeat([]byte{0x14}, 32)
	sendErr := errors.New("send failed")
	badPeer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0x16}, 32), sendErr: sendErr}
	peers := mockBroadcastPeerSet{peers: []BroadcastPeer{
		&mockBroadcastPeer{id: bytes.Repeat([]byte{0x15}, 32)},
		badPeer,
		&mockBroadcastPeer{id: bytes.Repeat([]byte{0x17}, 32)},
	}}

	res, err := SendBroadcastTwoStep(context.Background(), BroadcastTwoStepSendRequest{
		Key:         priv,
		Certificate: CertificateEmpty{},
		LocalADNLID: localID,
		Payload:     []byte("small"),
		PeerSet:     peers,
	},
		WithBroadcastTwoStepDate(224),
	)
	if err == nil {
		t.Fatalf("expected send error")
	}
	if res.Attempted != 3 || res.Sent != 2 || len(res.Failed) != 1 {
		t.Fatalf("unexpected send counters: %#v", res)
	}
	if !bytes.Equal(res.Failed[0].PeerID, badPeer.id) || res.Failed[0].Err != sendErr {
		t.Fatalf("unexpected failed peer: %#v", res.Failed[0])
	}
}

func TestProcessBroadcastTwoStepSimple(t *testing.T) {
	_, priv := keyPairFromSeed(74)
	overlayID := bytes.Repeat([]byte{0x31}, 32)
	sourceADNL := bytes.Repeat([]byte{0x41}, 32)
	localID := bytes.Repeat([]byte{0x42}, 32)
	rebroadcasted := make(chan tl.Serializable, 1)
	otherPeer := &twoStepLegacyRelayTestPeer{
		id:       bytes.Repeat([]byte{0x43}, 32),
		received: rebroadcasted,
	}
	sourcePeer := &mockBroadcastPeer{id: sourceADNL}
	localPeer := &mockBroadcastPeer{id: localID}

	m := newMockADNL()
	m.id = sourceADNL
	w := CreateExtendedADNL(m)
	o := w.CreateOverlayWithSettings(overlayID, 1024, true, true)
	t.Cleanup(o.BroadcastReceiver.Close)
	o.EnableBroadcastTwoStep(localID, mockBroadcastPeerSet{peers: []BroadcastPeer{sourcePeer, otherPeer, localPeer}}, NewBroadcastTwoStepState())

	payload := Message{Overlay: bytes.Repeat([]byte{0x51}, 32)}
	data, err := tl.Serialize(payload, true)
	if err != nil {
		t.Fatalf("payload serialize failed: %v", err)
	}
	msg := &BroadcastTwoStepSimple{
		Flags:       0,
		Date:        uint32(time.Now().Unix()),
		Source:      ed25519Public(priv),
		SourceADNL:  sourceADNL,
		Certificate: CertificateEmpty{},
		Data:        data,
		Extra:       []byte("extra"),
	}
	if err = msg.Sign(priv); err != nil {
		t.Fatalf("sign failed: %v", err)
	}

	var prechecks []bool
	o.SetBroadcastPrecheckHandler(func(info BroadcastPrecheckInfo) error {
		prechecks = append(prechecks, info.SignatureChecked)
		if !bytes.Equal(info.Extra, []byte("extra")) || !bytes.Equal(info.SourceADNL, sourceADNL) {
			t.Fatalf("unexpected precheck extra")
		}
		return nil
	})

	handled := 0
	var gotInfo BroadcastInfo
	o.SetBroadcastHandlerWithInfo(func(got tl.Serializable, info BroadcastInfo) BroadcastDisposition {
		handled++
		gotInfo = info
		gotMsg, ok := got.(Message)
		if !ok || !bytes.Equal(gotMsg.Overlay, payload.Overlay) {
			t.Fatalf("unexpected delivered payload %#v", got)
		}
		return BroadcastDispositionAcceptAndRelay
	})

	if err = m.customHandler(&adnl.MessageCustom{Data: WrapMessage(overlayID, *msg)}); err != nil {
		t.Fatalf("process simple failed: %v", err)
	}
	if handled != 1 {
		t.Fatalf("expected one delivery, got %d", handled)
	}
	if !gotInfo.Trusted || gotInfo.Delivery != BroadcastDeliveryTwoStepSimple || !bytes.Equal(gotInfo.Extra, []byte("extra")) || !bytes.Equal(gotInfo.SourceADNL, sourceADNL) || len(gotInfo.BroadcastID) != 32 {
		t.Fatalf("unexpected delivered info: %#v", gotInfo)
	}
	if len(prechecks) != 2 || prechecks[0] || !prechecks[1] {
		t.Fatalf("unexpected precheck calls: %v", prechecks)
	}
	select {
	case relayed := <-rebroadcasted:
		if _, ok := relayed.(*BroadcastTwoStepSimple); !ok {
			t.Fatalf("legacy peer received %T, want *BroadcastTwoStepSimple", relayed)
		}
	case <-time.After(time.Second):
		t.Fatal("expected rebroadcast to other peer")
	}
	if calls := otherPeer.calls.Load(); calls != 1 {
		t.Fatalf("expected rebroadcast to other peer, got %d", calls)
	}
	if len(sourcePeer.sent) != 0 || len(localPeer.sent) != 0 {
		t.Fatalf("source/local peers must be excluded from rebroadcast")
	}

	if err = m.customHandler(&adnl.MessageCustom{Data: WrapMessage(overlayID, *msg)}); err != nil {
		t.Fatalf("duplicate simple failed: %v", err)
	}
	if handled != 1 {
		t.Fatalf("duplicate should not deliver again, got %d deliveries", handled)
	}
}

func TestProcessBroadcastTwoStepReceiveDisabledByDefault(t *testing.T) {
	_, priv := keyPairFromSeed(81)
	overlayID := bytes.Repeat([]byte{0x52}, 32)
	sourceADNL := bytes.Repeat([]byte{0x53}, 32)

	m := newMockADNL()
	m.id = sourceADNL
	o := CreateExtendedADNL(m).CreateOverlayWithSettings(overlayID, 1024, true, true)

	payload := Message{Overlay: bytes.Repeat([]byte{0x54}, 32)}
	data, err := tl.Serialize(payload, true)
	if err != nil {
		t.Fatalf("payload serialize failed: %v", err)
	}
	msg := &BroadcastTwoStepSimple{
		Flags:       0,
		Date:        uint32(time.Now().Unix()),
		Source:      ed25519Public(priv),
		SourceADNL:  sourceADNL,
		Certificate: CertificateEmpty{},
		Data:        data,
	}
	if err = msg.Sign(priv); err != nil {
		t.Fatalf("sign failed: %v", err)
	}

	handled := false
	o.SetBroadcastHandlerWithInfo(func(msg tl.Serializable, info BroadcastInfo) BroadcastDisposition {
		handled = true
		return BroadcastDispositionAcceptAndRelay
	})

	if err = m.customHandler(&adnl.MessageCustom{Data: WrapMessage(overlayID, *msg)}); err != nil {
		t.Fatalf("disabled receive failed: %v", err)
	}
	if handled {
		t.Fatalf("disabled two-step receive should drop message")
	}

	o.EnableBroadcastTwoStep(sourceADNL, nil, NewBroadcastTwoStepState())
	o.DisableBroadcastTwoStep()
	if err = m.customHandler(&adnl.MessageCustom{Data: WrapMessage(overlayID, *msg)}); err != nil {
		t.Fatalf("disabled after enable receive failed: %v", err)
	}
	if handled {
		t.Fatalf("disabled two-step receive should drop message after disable")
	}
}

func TestProcessBroadcastTwoStepFEC(t *testing.T) {
	_, sourcePriv := keyPairFromSeed(75)
	_, payloadPriv := keyPairFromSeed(76)
	overlayID := bytes.Repeat([]byte{0x61}, 32)
	sourceADNL := bytes.Repeat([]byte{0x71}, 32)
	localID := bytes.Repeat([]byte{0x72}, 32)

	sourcePeers := mockBroadcastPeerSet{peers: make([]BroadcastPeer, 5)}
	for i := range sourcePeers.peers {
		sourcePeers.peers[i] = &mockBroadcastPeer{id: bytes.Repeat([]byte{byte(0x80 + i)}, 32)}
	}

	payload := Broadcast{
		Source:      keys.PublicKeyED25519{Key: payloadPriv.Public().(ed25519.PublicKey)},
		Certificate: CertificateEmpty{},
		Data:        bytes.Repeat([]byte{0xAB}, 700),
		Date:        777,
	}
	sendRes, err := SendBroadcastTwoStepFromTL(context.Background(), BroadcastTwoStepTLSendRequest{
		Key:         sourcePriv,
		Certificate: CertificateEmpty{},
		LocalADNLID: sourceADNL,
		Payload:     payload,
		Extra:       []byte("fec-extra"),
		PeerSet:     sourcePeers,
	},
		WithBroadcastTwoStepDate(uint32(time.Now().Unix())),
	)
	if err != nil {
		t.Fatalf("send fec failed: %v", err)
	}
	if sendRes.Mode != BroadcastTwoStepModeFEC {
		t.Fatalf("expected fec mode, got %#v", sendRes)
	}

	rebroadcasted := make(chan struct{}, 1)
	rebroadcastPeer := &twoStepRelayTestPeer{
		id: bytes.Repeat([]byte{0x91}, 32),
		send: func(context.Context, []byte) error {
			rebroadcasted <- struct{}{}
			return nil
		},
	}
	sourcePeer := &mockBroadcastPeer{id: sourceADNL}
	localPeer := &mockBroadcastPeer{id: localID}

	m := newMockADNL()
	w := CreateExtendedADNL(m)
	o := w.CreateOverlayWithSettings(overlayID, 4096, true, true)
	t.Cleanup(o.BroadcastReceiver.Close)
	o.EnableBroadcastTwoStep(localID, mockBroadcastPeerSet{peers: []BroadcastPeer{sourcePeer, rebroadcastPeer, localPeer}}, NewBroadcastTwoStepState())

	handled := 0
	var gotInfo BroadcastInfo
	o.SetBroadcastHandlerWithInfo(func(got tl.Serializable, info BroadcastInfo) BroadcastDisposition {
		handled++
		gotInfo = info
		gotBroadcast, ok := got.(Broadcast)
		if !ok || !bytes.Equal(gotBroadcast.Data, payload.Data) {
			t.Fatalf("unexpected decoded payload %#v", got)
		}
		return BroadcastDispositionAcceptAndRelay
	})

	first := sourcePeers.peers[0].(*mockBroadcastPeer).sent[0].(*BroadcastTwoStepFEC)
	second := sourcePeers.peers[1].(*mockBroadcastPeer).sent[0].(*BroadcastTwoStepFEC)
	if err = o.processBroadcastTwoStepFEC(first, sourceADNL); err != nil {
		t.Fatalf("first fec part failed: %v", err)
	}
	select {
	case <-rebroadcasted:
	case <-time.After(time.Second):
		t.Fatal("expected one rebroadcast from direct fec part")
	}
	if calls := rebroadcastPeer.calls.Load(); calls != 1 {
		t.Fatalf("expected one rebroadcast from direct fec part, got %d", calls)
	}
	if handled != 0 {
		t.Fatalf("first part should not decode yet")
	}

	if err = o.processBroadcastTwoStepFEC(second, sourcePeers.peers[1].ID()); err != nil {
		t.Fatalf("second fec part failed: %v", err)
	}
	if handled != 1 {
		t.Fatalf("expected decoded delivery, got %d", handled)
	}
	if !gotInfo.Trusted || gotInfo.Delivery != BroadcastDeliveryTwoStepFEC || !bytes.Equal(gotInfo.Extra, []byte("fec-extra")) || !bytes.Equal(gotInfo.SourceADNL, sourceADNL) || !bytes.Equal(gotInfo.BroadcastID, sendRes.BroadcastID) {
		t.Fatalf("unexpected fec info: %#v", gotInfo)
	}

	if err = o.processBroadcastTwoStepFEC(second, sourcePeers.peers[1].ID()); err != nil {
		t.Fatalf("duplicate fec part failed: %v", err)
	}
	if handled != 1 {
		t.Fatalf("duplicate should not deliver again, got %d deliveries", handled)
	}
}

func TestProcessBroadcastTwoStepFECSharedStateAcrossConnections(t *testing.T) {
	_, sourcePriv := keyPairFromSeed(78)
	overlayID := bytes.Repeat([]byte{0xD1}, 32)
	sourceADNL := bytes.Repeat([]byte{0xD2}, 32)

	sourcePeers := mockBroadcastPeerSet{peers: make([]BroadcastPeer, 5)}
	for i := range sourcePeers.peers {
		sourcePeers.peers[i] = &mockBroadcastPeer{id: bytes.Repeat([]byte{byte(0xD3 + i)}, 32)}
	}

	payload := Broadcast{
		Source:      ed25519Public(sourcePriv),
		Certificate: CertificateEmpty{},
		Data:        bytes.Repeat([]byte{0xE1}, 700),
		Date:        888,
	}
	_, err := SendBroadcastTwoStepFromTL(context.Background(), BroadcastTwoStepTLSendRequest{
		Key:         sourcePriv,
		Certificate: CertificateEmpty{},
		LocalADNLID: sourceADNL,
		Payload:     payload,
		PeerSet:     sourcePeers,
	},
		WithBroadcastTwoStepDate(uint32(time.Now().Unix())),
	)
	if err != nil {
		t.Fatalf("send fec failed: %v", err)
	}

	shared := NewBroadcastTwoStepState()
	firstADNL := newMockADNL()
	firstADNL.id = sourceADNL
	firstOverlay := CreateExtendedADNL(firstADNL).CreateOverlayWithSettings(overlayID, 4096, true, true)
	firstOverlay.EnableBroadcastTwoStep(firstADNL.id, nil, shared)

	secondADNL := newMockADNL()
	secondADNL.id = bytes.Repeat([]byte{0xE2}, 32)
	secondOverlay := CreateExtendedADNL(secondADNL).CreateOverlayWithSettings(overlayID, 4096, true, true)
	secondOverlay.EnableBroadcastTwoStep(secondADNL.id, nil, shared)

	handled := 0
	handler := func(got tl.Serializable, info BroadcastInfo) BroadcastDisposition {
		handled++
		return BroadcastDispositionAcceptAndRelay
	}
	firstOverlay.SetBroadcastHandlerWithInfo(handler)
	secondOverlay.SetBroadcastHandlerWithInfo(handler)

	first := sourcePeers.peers[0].(*mockBroadcastPeer).sent[0].(*BroadcastTwoStepFEC)
	second := sourcePeers.peers[1].(*mockBroadcastPeer).sent[0].(*BroadcastTwoStepFEC)
	if err = firstOverlay.processBroadcastTwoStepFEC(first, sourceADNL); err != nil {
		t.Fatalf("first shared part failed: %v", err)
	}
	if err = secondOverlay.processBroadcastTwoStepFEC(second, sourcePeers.peers[1].ID()); err != nil {
		t.Fatalf("second shared part failed: %v", err)
	}
	if handled != 1 {
		t.Fatalf("expected shared state decode, got %d deliveries", handled)
	}
}

func TestProcessBroadcastTwoStepFECDropsWhenBudgetTooSmall(t *testing.T) {
	_, priv := keyPairFromSeed(77)
	overlayID := bytes.Repeat([]byte{0xA7}, 32)
	sourceADNL := bytes.Repeat([]byte{0xB7}, 32)
	peers := mockBroadcastPeerSet{peers: make([]BroadcastPeer, 5)}
	for i := range peers.peers {
		peers.peers[i] = &mockBroadcastPeer{id: bytes.Repeat([]byte{byte(0xC0 + i)}, 32)}
	}

	res, err := SendBroadcastTwoStep(context.Background(), BroadcastTwoStepSendRequest{
		Key:         priv,
		Certificate: CertificateEmpty{},
		LocalADNLID: sourceADNL,
		Payload:     bytes.Repeat([]byte{0xDD}, 1024),
		PeerSet:     peers,
	},
		WithBroadcastTwoStepDate(uint32(time.Now().Unix())),
	)
	if err != nil || res.Mode != BroadcastTwoStepModeFEC {
		t.Fatalf("send fec failed, res=%#v err=%v", res, err)
	}

	m := newMockADNL()
	w := CreateExtendedADNL(m)
	o := w.CreateOverlayWithSettings(overlayID, 4096, true, true)
	o.SetBroadcastTwoStepLimits(1, 8)

	part := peers.peers[0].(*mockBroadcastPeer).sent[0].(*BroadcastTwoStepFEC)
	if err = o.processBroadcastTwoStepFEC(part, sourceADNL); err == nil {
		t.Fatalf("expected budget error")
	}
	stats := o.BroadcastTwoStepStats()
	if stats.DroppedTotal != 1 || stats.ActiveStreams != 0 {
		t.Fatalf("unexpected budget stats: %#v", stats)
	}
}

func TestBroadcastTwoStepCleanupDoesNotMarkStaleStreamDelivered(t *testing.T) {
	state := NewBroadcastTwoStepState()
	id := newBroadcastTwoStepIDKey(bytes.Repeat([]byte{0xA8}, 32))
	stream := &broadcastTwoStepStream{
		budgetBytes:   64,
		lastMessageAt: time.Now().Add(-broadcastTwoStepStreamTTL - time.Second),
	}

	state.streams[id] = stream
	state.activeBytes = stream.budgetBytes

	state.mx.Lock()
	state.cleanupLocked(time.Now(), true)
	state.mx.Unlock()

	stats := state.Stats()
	if stats.ActiveStreams != 0 || stats.ActiveBytes != 0 || stats.EvictedTotal != 1 {
		t.Fatalf("unexpected cleanup stats: %#v", stats)
	}
	if stats.DeliveredBroadcasts != 0 {
		t.Fatalf("stale partial stream must not be marked delivered, got %#v", stats)
	}
}

func TestProcessBroadcastTwoStepSimpleRetryThenAccept(t *testing.T) {
	o, state, msg := newTwoStepSimpleReceiveFixture(t, 91)

	var calls atomic.Int32
	o.SetBroadcastHandlerWithInfo(func(_ tl.Serializable, info BroadcastInfo) BroadcastDisposition {
		call := calls.Add(1)
		if !bytes.Equal(info.Payload, msg.Data) {
			t.Errorf("unexpected raw payload")
		}
		if len(info.Payload) > 0 && &info.Payload[0] != &msg.Data[0] {
			t.Errorf("two-step simple payload was copied")
		}
		if call == 1 {
			return BroadcastDispositionRetry
		}
		return BroadcastDispositionAcceptAndRelay
	})

	err := o.processBroadcastTwoStepSimple(msg, msg.SourceADNL)
	if !errors.Is(err, ErrBroadcastRejected) {
		t.Fatalf("expected retry rejection, got %v", err)
	}
	stats := state.Stats()
	if stats.DeliveredBroadcasts != 0 || stats.CompletedTotal != 0 {
		t.Fatalf("retry must not commit simple broadcast: %#v", stats)
	}

	if err = o.processBroadcastTwoStepSimple(msg, msg.SourceADNL); err != nil {
		t.Fatalf("retry delivery failed: %v", err)
	}
	stats = state.Stats()
	if calls.Load() != 2 || stats.DeliveredBroadcasts != 1 || stats.CompletedTotal != 1 {
		t.Fatalf("unexpected accepted simple state: calls=%d stats=%#v", calls.Load(), stats)
	}
}

func TestProcessBroadcastTwoStepSimpleIgnoreIsCommitted(t *testing.T) {
	o, state, msg := newTwoStepSimpleReceiveFixture(t, 92)

	var calls atomic.Int32
	o.SetBroadcastHandlerWithInfo(func(tl.Serializable, BroadcastInfo) BroadcastDisposition {
		calls.Add(1)
		return BroadcastDispositionIgnore
	})

	if err := o.processBroadcastTwoStepSimple(msg, msg.SourceADNL); err != nil {
		t.Fatalf("ignore delivery failed: %v", err)
	}
	if err := o.processBroadcastTwoStepSimple(msg, msg.SourceADNL); err != nil {
		t.Fatalf("ignored replay failed: %v", err)
	}
	stats := state.Stats()
	if calls.Load() != 1 || stats.DeliveredBroadcasts != 1 || stats.CompletedTotal != 1 {
		t.Fatalf("ignored simple broadcast was not committed: calls=%d stats=%#v", calls.Load(), stats)
	}
}

func TestProcessBroadcastTwoStepSimpleParseFailureIsCommitted(t *testing.T) {
	o, state, msg := newTwoStepSimpleReceiveFixture(t, 93)
	msg.Data = []byte{0xFF, 0xFF, 0xFF, 0xFF}
	_, priv := keyPairFromSeed(93)
	if err := msg.Sign(priv); err != nil {
		t.Fatalf("sign invalid payload failed: %v", err)
	}

	var calls atomic.Int32
	o.SetBroadcastHandlerWithInfo(func(tl.Serializable, BroadcastInfo) BroadcastDisposition {
		calls.Add(1)
		return BroadcastDispositionAcceptAndRelay
	})

	if err := o.processBroadcastTwoStepSimple(msg, msg.SourceADNL); err == nil {
		t.Fatalf("expected deterministic parse error")
	}
	if err := o.processBroadcastTwoStepSimple(msg, msg.SourceADNL); err != nil {
		t.Fatalf("committed parse failure replay failed: %v", err)
	}
	stats := state.Stats()
	if calls.Load() != 0 || stats.DeliveredBroadcasts != 1 || stats.CompletedTotal != 1 {
		t.Fatalf("parse failure was not committed as ignore: calls=%d stats=%#v", calls.Load(), stats)
	}
}

func TestProcessBroadcastTwoStepSimpleConcurrentWaiterRecontendsAfterRetry(t *testing.T) {
	o, state, msg := newTwoStepSimpleReceiveFixture(t, 94)

	firstHandlerStarted := make(chan struct{})
	releaseFirstHandler := make(chan struct{})
	var calls atomic.Int32
	o.SetBroadcastHandlerWithInfo(func(tl.Serializable, BroadcastInfo) BroadcastDisposition {
		if calls.Add(1) == 1 {
			close(firstHandlerStarted)
			<-releaseFirstHandler
			return BroadcastDispositionRetry
		}
		return BroadcastDispositionAcceptAndRelay
	})

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- o.processBroadcastTwoStepSimple(msg, msg.SourceADNL)
	}()
	<-firstHandlerStarted

	waiterStarted := make(chan struct{})
	waiterDone := make(chan error, 1)
	go func() {
		close(waiterStarted)
		waiterDone <- o.processBroadcastTwoStepSimple(msg, msg.SourceADNL)
	}()
	<-waiterStarted
	select {
	case err := <-waiterDone:
		t.Fatalf("waiter returned before retry completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseFirstHandler)
	if err := <-firstDone; !errors.Is(err, ErrBroadcastRejected) {
		t.Fatalf("expected first retry rejection, got %v", err)
	}
	if err := <-waiterDone; err != nil {
		t.Fatalf("waiter failed to re-contend: %v", err)
	}
	stats := state.Stats()
	if calls.Load() != 2 || stats.DeliveredBroadcasts != 1 || stats.CompletedTotal != 1 {
		t.Fatalf("unexpected concurrent admission state: calls=%d stats=%#v", calls.Load(), stats)
	}
}

func TestProcessBroadcastTwoStepFECRetryThenAccept(t *testing.T) {
	o, state, parts, sourceADNL := newTwoStepFECReceiveFixture(t, 95)

	var calls atomic.Int32
	o.SetBroadcastHandlerWithInfo(func(_ tl.Serializable, info BroadcastInfo) BroadcastDisposition {
		if len(info.Payload) == 0 {
			t.Errorf("expected raw decoded payload")
		}
		if calls.Add(1) == 1 {
			return BroadcastDispositionRetry
		}
		return BroadcastDispositionAcceptAndRelay
	})

	decodedAt := -1
	for i, part := range parts {
		err := o.processBroadcastTwoStepFEC(part, sourceADNL)
		if errors.Is(err, ErrBroadcastRejected) {
			decodedAt = i
			break
		}
		if err != nil {
			t.Fatalf("first fec attempt part %d failed: %v", i, err)
		}
	}
	if decodedAt < 0 {
		t.Fatalf("first fec attempt did not decode")
	}
	stats := state.Stats()
	if stats.ActiveStreams != 0 || stats.DeliveredBroadcasts != 0 || stats.CompletedTotal != 0 {
		t.Fatalf("fec retry must remove uncommitted stream: %#v", stats)
	}

	for i := 0; i <= decodedAt; i++ {
		if err := o.processBroadcastTwoStepFEC(parts[i], sourceADNL); err != nil {
			t.Fatalf("second fec attempt part %d failed: %v", i, err)
		}
	}
	stats = state.Stats()
	if calls.Load() != 2 || stats.ActiveStreams != 0 || stats.DeliveredBroadcasts != 1 || stats.CompletedTotal != 1 {
		t.Fatalf("unexpected accepted fec state: calls=%d stats=%#v", calls.Load(), stats)
	}
}

func TestProcessBroadcastTwoStepFECIgnoreIsCommitted(t *testing.T) {
	o, state, parts, sourceADNL := newTwoStepFECReceiveFixture(t, 96)

	var calls atomic.Int32
	o.SetBroadcastHandlerWithInfo(func(tl.Serializable, BroadcastInfo) BroadcastDisposition {
		calls.Add(1)
		return BroadcastDispositionIgnore
	})

	decodedAt := -1
	for i, part := range parts {
		if err := o.processBroadcastTwoStepFEC(part, sourceADNL); err != nil {
			t.Fatalf("fec ignore part %d failed: %v", i, err)
		}
		if calls.Load() == 1 {
			decodedAt = i
			break
		}
	}
	if decodedAt < 0 {
		t.Fatalf("fec ignore attempt did not decode")
	}
	for i := 0; i <= decodedAt; i++ {
		if err := o.processBroadcastTwoStepFEC(parts[i], sourceADNL); err != nil {
			t.Fatalf("ignored fec replay part %d failed: %v", i, err)
		}
	}
	stats := state.Stats()
	if calls.Load() != 1 || stats.ActiveStreams != 0 || stats.DeliveredBroadcasts != 1 || stats.CompletedTotal != 1 {
		t.Fatalf("ignored fec broadcast was not committed: calls=%d stats=%#v", calls.Load(), stats)
	}
}

func TestProcessBroadcastTwoStepFECConcurrentWaiterRecontendsAfterRetry(t *testing.T) {
	o, state, parts, sourceADNL := newTwoStepFECReceiveFixture(t, 97)
	if len(parts) < 2 {
		t.Fatalf("expected at least two fec parts")
	}

	firstHandlerStarted := make(chan struct{})
	releaseFirstHandler := make(chan struct{})
	var calls atomic.Int32
	o.SetBroadcastHandlerWithInfo(func(tl.Serializable, BroadcastInfo) BroadcastDisposition {
		if calls.Add(1) == 1 {
			close(firstHandlerStarted)
			<-releaseFirstHandler
			return BroadcastDispositionRetry
		}
		return BroadcastDispositionAcceptAndRelay
	})

	if err := o.processBroadcastTwoStepFEC(parts[0], sourceADNL); err != nil {
		t.Fatalf("first fec symbol failed: %v", err)
	}
	ownerDone := make(chan error, 1)
	go func() {
		ownerDone <- o.processBroadcastTwoStepFEC(parts[1], sourceADNL)
	}()
	<-firstHandlerStarted

	waiterDone := make(chan error, 1)
	go func() {
		waiterDone <- o.processBroadcastTwoStepFEC(parts[1], sourceADNL)
	}()
	select {
	case err := <-waiterDone:
		t.Fatalf("fec waiter returned before retry completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseFirstHandler)
	if err := <-ownerDone; !errors.Is(err, ErrBroadcastRejected) {
		t.Fatalf("expected fec owner retry rejection, got %v", err)
	}
	if err := <-waiterDone; err != nil {
		t.Fatalf("fec waiter failed to re-contend: %v", err)
	}
	if stats := state.Stats(); stats.ActiveStreams != 1 || stats.DeliveredBroadcasts != 0 || stats.CompletedTotal != 0 {
		t.Fatalf("waiter did not rebuild retryable fec stream: %#v", stats)
	}

	if err := o.processBroadcastTwoStepFEC(parts[0], sourceADNL); err != nil {
		t.Fatalf("fec retry completion failed: %v", err)
	}
	stats := state.Stats()
	if calls.Load() != 2 || stats.ActiveStreams != 0 || stats.DeliveredBroadcasts != 1 || stats.CompletedTotal != 1 {
		t.Fatalf("unexpected concurrent fec admission state: calls=%d stats=%#v", calls.Load(), stats)
	}
}

func TestBroadcastTwoStepRelayDispatcherBoundsQueueAndConcurrency(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	finished := make(chan struct{}, 2)
	var active atomic.Int32
	var maxActive atomic.Int32

	peer := &twoStepRelayTestPeer{
		id: bytes.Repeat([]byte{0xB1}, 32),
		send: func(ctx context.Context, _ []byte) error {
			current := active.Add(1)
			for {
				old := maxActive.Load()
				if current <= old || maxActive.CompareAndSwap(old, current) {
					break
				}
			}
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				active.Add(-1)
				return ctx.Err()
			}
			active.Add(-1)
			finished <- struct{}{}
			return nil
		},
	}
	dispatcher := newBroadcastTwoStepRelayDispatcher(1, 1, 1<<20, time.Minute)
	task := broadcastTwoStepRelayTask{
		peer:    peer,
		payload: mustReserveTwoStepRelayPayload(t, dispatcher, []byte("part"), 3),
	}

	if status := dispatcher.Submit(task); status != broadcastTwoStepRelayQueued {
		t.Fatalf("first submit status=%d, want queued", status)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		dispatcher.Close()
		t.Fatal("first relay did not start")
	}

	if status := dispatcher.Submit(task); status != broadcastTwoStepRelayQueued {
		t.Fatalf("second submit status=%d, want queued", status)
	}
	if status := dispatcher.Submit(task); status != broadcastTwoStepRelayQueueFull {
		t.Fatalf("third submit status=%d, want queue full", status)
	}

	stats := dispatcher.Stats()
	if stats.QueueDepth != 1 || stats.EnqueuedTotal != 2 || stats.QueueFullTotal != 1 {
		t.Fatalf("unexpected saturated relay stats: %#v", stats)
	}
	if max := maxActive.Load(); max != 1 {
		t.Fatalf("max active sends=%d, want 1", max)
	}

	close(release)
	for range 2 {
		select {
		case <-finished:
		case <-time.After(time.Second):
			dispatcher.Close()
			t.Fatal("accepted relay did not finish")
		}
	}
	dispatcher.Close()

	if status := dispatcher.Submit(broadcastTwoStepRelayTask{peer: peer}); status != broadcastTwoStepRelayClosed {
		t.Fatalf("submit after close status=%d, want closed", status)
	}
	stats = dispatcher.Stats()
	if stats.SentTotal != 2 || stats.ClosedTotal != 1 {
		t.Fatalf("unexpected completed relay stats: %#v", stats)
	}
}

func TestBroadcastTwoStepRelayDispatcherBoundsDistinctBodyBytes(t *testing.T) {
	started := make(chan struct{}, 2)
	finished := make(chan struct{}, 2)
	release := make(chan struct{})
	peer := &twoStepRelayTestPeer{
		id: bytes.Repeat([]byte{0xB7}, 32),
		send: func(ctx context.Context, _ []byte) error {
			started <- struct{}{}
			defer func() { finished <- struct{}{} }()
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	dispatcher := newBroadcastTwoStepRelayDispatcher(1, 1, 8, time.Minute)
	payload := mustReserveTwoStepRelayPayload(t, dispatcher, []byte("12345678"), 2)
	task := broadcastTwoStepRelayTask{peer: peer, payload: payload}

	if status := dispatcher.Submit(task); status != broadcastTwoStepRelayQueued {
		dispatcher.Close()
		t.Fatalf("first submit status=%d, want queued", status)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		dispatcher.Close()
		t.Fatal("first relay did not start")
	}
	if status := dispatcher.Submit(task); status != broadcastTwoStepRelayQueued {
		dispatcher.Close()
		t.Fatalf("second submit status=%d, want queued", status)
	}

	stats := dispatcher.Stats()
	if stats.ActiveBytes != 8 {
		close(release)
		dispatcher.Close()
		t.Fatalf("shared relay body charged %d bytes, want 8", stats.ActiveBytes)
	}
	if unexpected, ok := dispatcher.reservePayload(nil, NewPreparedBroadcastMessage([]byte("x")), 3); ok {
		for range 3 {
			dispatcher.releasePayload(unexpected)
		}
		close(release)
		dispatcher.Close()
		t.Fatal("distinct relay body exceeded byte budget but was reserved")
	}
	stats = dispatcher.Stats()
	if stats.ActiveBytes != 8 || stats.ByteFullTotal != 3 {
		close(release)
		dispatcher.Close()
		t.Fatalf("unexpected byte-full stats: %#v", stats)
	}

	close(release)
	for range 2 {
		select {
		case <-finished:
		case <-time.After(time.Second):
			dispatcher.Close()
			t.Fatal("accepted byte-budgeted relay did not finish")
		}
	}
	dispatcher.Close()
	stats = dispatcher.Stats()
	if stats.ActiveBytes != 0 || stats.SentTotal != 2 {
		t.Fatalf("relay body budget was not released: %#v", stats)
	}
}

func TestBroadcastReceiverTwoStepRelayConcurrentInitAndClose(t *testing.T) {
	t.Run("one dispatcher for concurrent init", func(t *testing.T) {
		receiver, err := NewBroadcastReceiver(bytes.Repeat([]byte{0xB8}, 32), 1024, true, true)
		if err != nil {
			t.Fatalf("create receiver: %v", err)
		}

		const callers = 32
		start := make(chan struct{})
		results := make(chan *broadcastTwoStepRelayDispatcher, callers)
		var wg sync.WaitGroup
		wg.Add(callers)
		for range callers {
			go func() {
				defer wg.Done()
				<-start
				results <- receiver.ensureBroadcastTwoStepRelayDispatcher()
			}()
		}
		close(start)
		wg.Wait()
		close(results)

		var first *broadcastTwoStepRelayDispatcher
		for relay := range results {
			if relay == nil {
				receiver.Close()
				t.Fatal("concurrent init returned nil dispatcher")
			}
			if first == nil {
				first = relay
			} else if relay != first {
				receiver.Close()
				t.Fatal("concurrent init created more than one dispatcher")
			}
		}
		receiver.Close()
		select {
		case <-first.ctx.Done():
		default:
			t.Fatal("receiver close left initialized dispatcher running")
		}
	})

	t.Run("init racing close", func(t *testing.T) {
		for round := range 8 {
			receiver, err := NewBroadcastReceiver(bytes.Repeat([]byte{byte(0xC0 + round)}, 32), 1024, true, true)
			if err != nil {
				t.Fatalf("round %d create receiver: %v", round, err)
			}

			const callers = 8
			start := make(chan struct{})
			results := make(chan *broadcastTwoStepRelayDispatcher, callers)
			var wg sync.WaitGroup
			wg.Add(callers + 1)
			for range callers {
				go func() {
					defer wg.Done()
					<-start
					results <- receiver.ensureBroadcastTwoStepRelayDispatcher()
				}()
			}
			go func() {
				defer wg.Done()
				<-start
				receiver.Close()
			}()
			close(start)
			wg.Wait()
			close(results)

			stored := receiver.twoStepRelay.Load()
			for relay := range results {
				if relay != nil && relay != stored {
					t.Fatalf("round %d returned unowned dispatcher", round)
				}
			}
			if stored != nil {
				select {
				case <-stored.ctx.Done():
				default:
					t.Fatalf("round %d close returned before dispatcher cancellation", round)
				}
			}
			if relay := receiver.ensureBroadcastTwoStepRelayDispatcher(); relay != nil {
				t.Fatalf("round %d initialized dispatcher after close", round)
			}
		}
	})
}

func TestBroadcastTwoStepRelayDispatcherAppliesPeerDeadline(t *testing.T) {
	deadlineErr := make(chan error, 1)
	peer := &twoStepRelayTestPeer{
		id: bytes.Repeat([]byte{0xB2}, 32),
		send: func(ctx context.Context, _ []byte) error {
			<-ctx.Done()
			deadlineErr <- ctx.Err()
			return ctx.Err()
		},
	}
	dispatcher := newBroadcastTwoStepRelayDispatcher(1, 1, 1<<20, 10*time.Millisecond)
	payload := mustReserveTwoStepRelayPayload(t, dispatcher, []byte("part"), 1)
	if status := dispatcher.Submit(broadcastTwoStepRelayTask{peer: peer, payload: payload}); status != broadcastTwoStepRelayQueued {
		dispatcher.Close()
		t.Fatalf("submit status=%d, want queued", status)
	}

	select {
	case err := <-deadlineErr:
		if !errors.Is(err, context.DeadlineExceeded) {
			dispatcher.Close()
			t.Fatalf("peer context error=%v, want deadline exceeded", err)
		}
	case <-time.After(time.Second):
		dispatcher.Close()
		t.Fatal("peer deadline did not fire")
	}
	dispatcher.Close()

	stats := dispatcher.Stats()
	if stats.FailedTotal != 1 || stats.TimedOutTotal != 1 || stats.SentTotal != 0 {
		t.Fatalf("unexpected deadline relay stats: %#v", stats)
	}
}

func TestBroadcastTwoStepRelayDispatcherCloseCancelsAndWaits(t *testing.T) {
	started := make(chan struct{})
	peerDone := make(chan error, 1)
	peer := &twoStepRelayTestPeer{
		id: bytes.Repeat([]byte{0xB3}, 32),
		send: func(ctx context.Context, _ []byte) error {
			close(started)
			<-ctx.Done()
			peerDone <- ctx.Err()
			return ctx.Err()
		},
	}
	dispatcher := newBroadcastTwoStepRelayDispatcher(1, 1, 1<<20, time.Hour)
	payload := mustReserveTwoStepRelayPayload(t, dispatcher, []byte("part"), 2)
	if status := dispatcher.Submit(broadcastTwoStepRelayTask{peer: peer, payload: payload}); status != broadcastTwoStepRelayQueued {
		dispatcher.Close()
		t.Fatalf("submit status=%d, want queued", status)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		dispatcher.Close()
		t.Fatal("relay did not start")
	}
	if status := dispatcher.Submit(broadcastTwoStepRelayTask{peer: peer, payload: payload}); status != broadcastTwoStepRelayQueued {
		dispatcher.Close()
		t.Fatalf("queued submit status=%d, want queued", status)
	}

	closed := make(chan struct{})
	go func() {
		dispatcher.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("dispatcher close did not wait for cancellation")
	}
	select {
	case err := <-peerDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("peer context error=%v, want canceled", err)
		}
	default:
		t.Fatal("dispatcher close returned before the peer send stopped")
	}
	stats := dispatcher.Stats()
	if stats.QueueDepth != 0 || stats.EnqueuedTotal != 2 || stats.CanceledTotal != 2 || peer.calls.Load() != 1 {
		t.Fatalf("unexpected shutdown state: stats=%#v calls=%d", stats, peer.calls.Load())
	}

	// Close is part of BroadcastReceiver.Close and must stay idempotent.
	dispatcher.Close()
}

func TestProcessBroadcastTwoStepSimpleRelayDoesNotDelayDelivery(t *testing.T) {
	o, state, msg := newTwoStepSimpleReceiveFixture(t, 99)
	localID := bytes.Repeat([]byte{102}, 32)
	slowStarted := make(chan struct{})
	slowDone := make(chan error, 1)
	slowPeer := &twoStepRelayTestPeer{
		id: bytes.Repeat([]byte{0xB6}, 32),
		send: func(ctx context.Context, _ []byte) error {
			close(slowStarted)
			<-ctx.Done()
			slowDone <- ctx.Err()
			return ctx.Err()
		},
	}
	dispatcher := newBroadcastTwoStepRelayDispatcher(4, 1, 1<<20, time.Hour)
	if !o.twoStepRelay.CompareAndSwap(nil, dispatcher) {
		dispatcher.Close()
		t.Fatal("fixture already initialized two-step relay dispatcher")
	}
	t.Cleanup(o.BroadcastReceiver.Close)
	o.EnableBroadcastTwoStep(localID, mockBroadcastPeerSet{peers: []BroadcastPeer{slowPeer}}, state)

	handled := make(chan struct{}, 1)
	var queuedBeforeDelivery atomic.Bool
	o.SetBroadcastHandlerWithInfo(func(tl.Serializable, BroadcastInfo) BroadcastDisposition {
		stats := o.BroadcastTwoStepRelayStats()
		queuedBeforeDelivery.Store(stats.EnqueuedTotal == 1)
		handled <- struct{}{}
		return BroadcastDispositionAcceptAndRelay
	})

	result := make(chan error, 1)
	go func() {
		result <- o.processBroadcastTwoStepSimple(msg, msg.SourceADNL)
	}()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("simple delivery failed because of relay: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("simple delivery waited for the slow relay peer")
	}
	select {
	case <-handled:
	case <-time.After(time.Second):
		t.Fatal("simple candidate was not delivered")
	}
	if !queuedBeforeDelivery.Load() {
		t.Fatal("simple relay was not accepted into the bounded queue before local delivery")
	}
	select {
	case <-slowStarted:
	case <-time.After(time.Second):
		t.Fatal("async simple relay did not start")
	}
	select {
	case err := <-slowDone:
		t.Fatalf("slow simple relay finished before receiver shutdown: %v", err)
	default:
	}

	o.BroadcastReceiver.Close()
	select {
	case err := <-slowDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("slow simple relay context error=%v, want canceled", err)
		}
	default:
		t.Fatal("receiver close returned before async simple relay stopped")
	}
	relayStats := o.BroadcastTwoStepRelayStats()
	if relayStats.FailedTotal != 1 || relayStats.CanceledTotal != 1 {
		t.Fatalf("unexpected failed simple relay stats: %#v", relayStats)
	}
}

func TestProcessBroadcastTwoStepFECRelayDoesNotDelayDecodedDelivery(t *testing.T) {
	o, state, parts, sourceADNL := newTwoStepFECReceiveFixture(t, 98)
	localID := bytes.Repeat([]byte{105}, 32)
	slowStarted := make(chan struct{})
	slowDone := make(chan error, 1)
	slowPeer := &twoStepRelayTestPeer{
		id: bytes.Repeat([]byte{0xB4}, 32),
		send: func(ctx context.Context, _ []byte) error {
			close(slowStarted)
			<-ctx.Done()
			slowDone <- ctx.Err()
			return ctx.Err()
		},
	}
	dispatcher := newBroadcastTwoStepRelayDispatcher(4, 1, 1<<20, time.Hour)
	if !o.twoStepRelay.CompareAndSwap(nil, dispatcher) {
		dispatcher.Close()
		t.Fatal("fixture already initialized two-step relay dispatcher")
	}
	t.Cleanup(o.BroadcastReceiver.Close)
	o.EnableBroadcastTwoStep(localID, mockBroadcastPeerSet{peers: []BroadcastPeer{slowPeer}}, state)

	handled := make(chan struct{}, 1)
	var queuedBeforeDelivery atomic.Bool
	o.SetBroadcastHandlerWithInfo(func(tl.Serializable, BroadcastInfo) BroadcastDisposition {
		stats := o.BroadcastTwoStepRelayStats()
		queuedBeforeDelivery.Store(stats.EnqueuedTotal == 1)
		handled <- struct{}{}
		return BroadcastDispositionAcceptAndRelay
	})

	indirectPeerID := bytes.Repeat([]byte{0xB5}, 32)
	if err := o.processBroadcastTwoStepFEC(parts[0], indirectPeerID); err != nil {
		t.Fatalf("indirect fec part failed: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		result <- o.processBroadcastTwoStepFEC(parts[1], sourceADNL)
	}()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("decoded fec delivery failed because of relay: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("decoded fec delivery waited for the slow relay peer")
	}
	select {
	case <-handled:
	case <-time.After(time.Second):
		t.Fatal("decoded candidate was not delivered")
	}
	if !queuedBeforeDelivery.Load() {
		t.Fatal("relay was not accepted into the bounded queue before local delivery")
	}
	select {
	case <-slowStarted:
	case <-time.After(time.Second):
		t.Fatal("async relay did not start")
	}
	select {
	case err := <-slowDone:
		t.Fatalf("slow relay finished before receiver shutdown: %v", err)
	default:
	}

	o.BroadcastReceiver.Close()
	select {
	case err := <-slowDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("slow relay context error=%v, want canceled", err)
		}
	default:
		t.Fatal("receiver close returned before async relay stopped")
	}
}

func newTwoStepSimpleReceiveFixture(t *testing.T, seed byte) (*ADNLOverlayWrapper, *BroadcastTwoStepState, *BroadcastTwoStepSimple) {
	t.Helper()

	_, priv := keyPairFromSeed(seed)
	sourceADNL := bytes.Repeat([]byte{seed}, 32)
	overlayID := bytes.Repeat([]byte{seed + 1}, 32)
	payload, err := tl.Serialize(Message{Overlay: bytes.Repeat([]byte{seed + 2}, 32)}, true)
	if err != nil {
		t.Fatalf("serialize simple fixture payload: %v", err)
	}
	msg := &BroadcastTwoStepSimple{
		Date:        uint32(time.Now().Unix()),
		Source:      ed25519Public(priv),
		SourceADNL:  sourceADNL,
		Certificate: CertificateEmpty{},
		Data:        payload,
	}
	if err = msg.Sign(priv); err != nil {
		t.Fatalf("sign simple fixture: %v", err)
	}

	state := NewBroadcastTwoStepState()
	o := CreateExtendedADNL(newMockADNL()).CreateOverlayWithSettings(overlayID, 4096, true, true)
	o.EnableBroadcastTwoStep(bytes.Repeat([]byte{seed + 3}, 32), nil, state)
	return o, state, msg
}

func newTwoStepFECReceiveFixture(t *testing.T, seed byte) (*ADNLOverlayWrapper, *BroadcastTwoStepState, []*BroadcastTwoStepFEC, []byte) {
	t.Helper()

	_, priv := keyPairFromSeed(seed)
	sourceADNL := bytes.Repeat([]byte{seed}, 32)
	overlayID := bytes.Repeat([]byte{seed + 1}, 32)
	peers := mockBroadcastPeerSet{peers: make([]BroadcastPeer, 5)}
	for i := range peers.peers {
		peers.peers[i] = &mockBroadcastPeer{id: bytes.Repeat([]byte{seed + byte(i) + 2}, 32)}
	}
	payload := Broadcast{
		Source:      ed25519Public(priv),
		Certificate: CertificateEmpty{},
		Data:        bytes.Repeat([]byte{seed}, 700),
		Date:        int32(seed),
	}
	res, err := SendBroadcastTwoStepFromTL(context.Background(), BroadcastTwoStepTLSendRequest{
		Key:         priv,
		Certificate: CertificateEmpty{},
		LocalADNLID: sourceADNL,
		Payload:     payload,
		PeerSet:     peers,
	}, WithBroadcastTwoStepDate(uint32(time.Now().Unix())))
	if err != nil {
		t.Fatalf("send fec fixture: %v", err)
	}
	if res.Mode != BroadcastTwoStepModeFEC {
		t.Fatalf("expected fec fixture mode, got %#v", res)
	}

	parts := make([]*BroadcastTwoStepFEC, 0, len(peers.peers))
	for _, peer := range peers.peers {
		parts = append(parts, peer.(*mockBroadcastPeer).sent[0].(*BroadcastTwoStepFEC))
	}

	state := NewBroadcastTwoStepState()
	o := CreateExtendedADNL(newMockADNL()).CreateOverlayWithSettings(overlayID, 4096, true, true)
	o.EnableBroadcastTwoStep(bytes.Repeat([]byte{seed + 7}, 32), nil, state)
	return o, state, parts, sourceADNL
}

// A fan-out released at quorum must still deliver to the peers it did not wait
// for, even though the caller cancels its context the moment the call returns.
// Before the stragglers were detached, exactly this cancelled them: on the test
// stand eleven of fourteen committee members received a symbol from the source
// and the other three had none to relay, which is what the second step of the
// broadcast is built out of.
func TestSendBroadcastTwoStepQuorumStragglersSurviveCallerCancel(t *testing.T) {
	_, priv := keyPairFromSeed(97)
	const peerCount = 9
	release := make(chan struct{})
	slowStarted := make(chan struct{}, 1)
	var delivered atomic.Int32

	peers := make([]BroadcastPeer, 0, peerCount)
	for i := 0; i < peerCount; i++ {
		peer := &mockBroadcastPeer{id: bytes.Repeat([]byte{byte(0x40 + i)}, 32)}
		if i == peerCount-1 {
			// The straggler: it has not finished when the quorum is reached.
			peer.sendFunc = func(ctx context.Context, _ tl.Serializable) error {
				select {
				case slowStarted <- struct{}{}:
				default:
				}
				select {
				case <-release:
					delivered.Add(1)

					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		} else {
			peer.sendFunc = func(context.Context, tl.Serializable) error {
				delivered.Add(1)

				return nil
			}
		}
		peers = append(peers, peer)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result, err := SendBroadcastTwoStep(ctx, BroadcastTwoStepSendRequest{
		Key:         priv,
		Certificate: CertificateEmpty{},
		LocalADNLID: bytes.Repeat([]byte{0x30}, 32),
		Payload:     bytes.Repeat([]byte("payload"), 400),
		PeerSet:     mockBroadcastPeerSet{peers: peers},
	}, WithBroadcastTwoStepDate(112), WithBroadcastTwoStepQuorumMargin(1))
	// What the caller does next, and the whole point of this test.
	cancel()
	if err != nil {
		close(release)
		t.Fatalf("fan-out failed: %v", err)
	}
	if result.Sent >= peerCount {
		close(release)
		t.Fatalf("sent = %d of %d: the fan-out waited for everyone and the quorum did nothing", result.Sent, peerCount)
	}
	if result.Pending != peerCount-len(result.Failed)-result.Sent {
		close(release)
		t.Fatalf("pending = %d, want %d unfinished recipients", result.Pending, peerCount-len(result.Failed)-result.Sent)
	}

	select {
	case <-slowStarted:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("the straggler was never called")
	}
	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if int(delivered.Load()) == peerCount {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("delivered to %d of %d peers: the straggler died with the caller's context",
		delivered.Load(), peerCount)
}
