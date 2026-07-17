package overlay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

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
	otherPeer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0x43}, 32)}
	sourcePeer := &mockBroadcastPeer{id: sourceADNL}
	localPeer := &mockBroadcastPeer{id: localID}

	m := newMockADNL()
	m.id = sourceADNL
	w := CreateExtendedADNL(m)
	o := w.CreateOverlayWithSettings(overlayID, 1024, true, true)
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
		if !bytes.Equal(info.Extra, []byte("extra")) {
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
	if !gotInfo.Trusted || gotInfo.Delivery != BroadcastDeliveryTwoStepSimple || !bytes.Equal(gotInfo.Extra, []byte("extra")) || len(gotInfo.BroadcastID) != 32 {
		t.Fatalf("unexpected delivered info: %#v", gotInfo)
	}
	if len(prechecks) != 2 || prechecks[0] || !prechecks[1] {
		t.Fatalf("unexpected precheck calls: %v", prechecks)
	}
	if len(otherPeer.sent) != 1 {
		t.Fatalf("expected rebroadcast to other peer, got %d", len(otherPeer.sent))
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

	rebroadcastPeer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0x91}, 32)}
	sourcePeer := &mockBroadcastPeer{id: sourceADNL}
	localPeer := &mockBroadcastPeer{id: localID}

	m := newMockADNL()
	w := CreateExtendedADNL(m)
	o := w.CreateOverlayWithSettings(overlayID, 4096, true, true)
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
	if len(rebroadcastPeer.sent) != 1 {
		t.Fatalf("expected one rebroadcast from direct fec part, got %d", len(rebroadcastPeer.sent))
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
	if !gotInfo.Trusted || gotInfo.Delivery != BroadcastDeliveryTwoStepFEC || !bytes.Equal(gotInfo.Extra, []byte("fec-extra")) || !bytes.Equal(gotInfo.BroadcastID, sendRes.BroadcastID) {
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
