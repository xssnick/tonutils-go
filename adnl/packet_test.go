package adnl

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

func TestParsePacketRejectsShortFields(t *testing.T) {
	buf := bytes.NewBuffer(nil)

	tmp := make([]byte, 4)
	binary.LittleEndian.PutUint32(tmp, _PacketContentID)
	buf.Write(tmp)
	_ = tl.ToBytesToBuffer(buf, []byte{1, 2, 3, 4, 5, 6, 7})

	binary.LittleEndian.PutUint32(tmp, _FlagSeqno)
	buf.Write(tmp)

	if _, err := parsePacket(buf.Bytes()); !errors.Is(err, ErrTooShortData) {
		t.Fatalf("expected ErrTooShortData, got %v", err)
	}
}

func TestDecodePacketRejectsTooShortPayload(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = decodePacket(priv, []byte{1, 2, 3}); !errors.Is(err, ErrTooShortData) {
		t.Fatalf("expected ErrTooShortData, got %v", err)
	}
}

func TestChannelDecodePacketRejectsTooShortPayload(t *testing.T) {
	var ch Channel
	if _, err := ch.decodePacket([]byte{1, 2, 3}); !errors.Is(err, ErrTooShortData) {
		t.Fatalf("expected ErrTooShortData, got %v", err)
	}
}

type capturePacketWriter struct {
	packets [][]byte
}

func (w *capturePacketWriter) Write(packet []byte, _ time.Time) (int, error) {
	w.packets = append(w.packets, append([]byte(nil), packet...))
	return len(packet), nil
}

func (w *capturePacketWriter) Close() error {
	return nil
}

func TestPacketVerifySignatureAcceptsRootPacketWithFullSource(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	raw := buildSignedPacketBytes(t, &PacketContent{
		Rand1:         []byte{1, 2, 3, 4, 5, 6, 7},
		From:          &keys.PublicKeyED25519{Key: pub},
		Messages:      []any{MessageNop{}},
		Seqno:         int64Ptr(1),
		ConfirmSeqno:  int64Ptr(1),
		ReinitDate:    int32Ptr(1),
		DstReinitDate: int32Ptr(1),
		Rand2:         []byte{8, 9, 10, 11, 12, 13, 14},
	}, priv)

	packet, err := parsePacket(raw)
	if err != nil {
		t.Fatal(err)
	}

	if err = packet.verifySignature(pub); err != nil {
		t.Fatalf("signature verification failed: %v", err)
	}
}

func TestCreatePacketUsesFullSourceAndRecvAddrVersion(t *testing.T) {
	_, ourPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	peerPub, peerPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	a := NewGateway(ourPriv).initADNL()
	a.peerKey = peerPub
	a.peerID, err = tl.Hash(keys.PublicKeyED25519{Key: peerPub})
	if err != nil {
		t.Fatal(err)
	}
	a.peerKeyX25519, err = keys.Ed25519PubToX25519(peerPub)
	if err != nil {
		t.Fatal(err)
	}
	a.recvAddrVer = 111
	a.recvPriorityAddrVer = 222

	packetBytes, err := a.createPacket(1, MessageNop{})
	if err != nil {
		t.Fatal(err)
	}

	data, err := decodePacket(peerPriv, packetBytes[32:])
	if err != nil {
		t.Fatal(err)
	}

	packet, err := parsePacket(data)
	if err != nil {
		t.Fatal(err)
	}

	if packet.From == nil || !bytes.Equal(packet.From.Key, ourPriv.Public().(ed25519.PublicKey)) {
		t.Fatalf("expected full source in root packet")
	}
	if packet.FromIDShort != nil {
		t.Fatalf("did not expect from_short in root packet")
	}
	if packet.RecvAddrListVersion == nil || *packet.RecvAddrListVersion != 111 {
		t.Fatalf("unexpected recv addr version: %+v", packet.RecvAddrListVersion)
	}
	if packet.RecvPriorityAddrListVersion == nil || *packet.RecvPriorityAddrListVersion != 222 {
		t.Fatalf("unexpected recv priority addr version: %+v", packet.RecvPriorityAddrListVersion)
	}
}

func TestInboundCustomRespondsWithRateLimitedNop(t *testing.T) {
	_, ourPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	peerPub, peerPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	a := NewGateway(ourPriv).initADNL()
	a.peerKey = peerPub
	a.peerID, err = tl.Hash(keys.PublicKeyED25519{Key: peerPub})
	if err != nil {
		t.Fatal(err)
	}
	a.peerKeyX25519, err = keys.Ed25519PubToX25519(peerPub)
	if err != nil {
		t.Fatal(err)
	}
	writer := &capturePacketWriter{}
	a.writer = writer

	if err = a.processMessage(MessageCustom{Data: TestMsg{Data: []byte{1}}}); err != nil {
		t.Fatal(err)
	}
	if len(writer.packets) != 1 {
		t.Fatalf("sent packets = %d, want 1", len(writer.packets))
	}

	data, err := decodePacket(peerPriv, writer.packets[0][32:])
	if err != nil {
		t.Fatal(err)
	}
	packet, err := parsePacket(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.Messages) != 2 {
		t.Fatalf("messages = %d, want createChannel+nop", len(packet.Messages))
	}
	if _, ok := packet.Messages[1].(MessageNop); !ok {
		t.Fatalf("second message = %T, want MessageNop", packet.Messages[1])
	}

	if err = a.processMessage(MessageCustom{Data: TestMsg{Data: []byte{2}}}); err != nil {
		t.Fatal(err)
	}
	if len(writer.packets) != 1 {
		t.Fatalf("rate limited packets = %d, want 1", len(writer.packets))
	}
}

func TestMessagePartRejectsTooManyIncompleteMessages(t *testing.T) {
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	a := NewGateway(key).initADNL()
	for i := 0; i < maxIncompleteMultipartMessages; i++ {
		hash := make([]byte, 32)
		binary.LittleEndian.PutUint32(hash, uint32(i))
		err = a.processMessage(MessagePart{
			Hash:      hash,
			TotalSize: 2,
			Offset:    0,
			Data:      []byte{1},
		})
		if err != nil {
			t.Fatalf("message part %d failed: %v", i, err)
		}
	}

	hash := make([]byte, 32)
	binary.LittleEndian.PutUint32(hash, uint32(maxIncompleteMultipartMessages))
	err = a.processMessage(MessagePart{
		Hash:      hash,
		TotalSize: 2,
		Offset:    0,
		Data:      []byte{1},
	})
	if err == nil || !strings.Contains(err.Error(), "too many incomplete messages") {
		t.Fatalf("expected too many incomplete messages error, got %v", err)
	}
}

func TestInboundCreateChannelCustomRespondsWithConfirmAndNop(t *testing.T) {
	_, ourPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	peerPub, peerPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	peerChannelPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	a := NewGateway(ourPriv).initADNL()
	a.peerKey = peerPub
	a.peerID, err = tl.Hash(keys.PublicKeyED25519{Key: peerPub})
	if err != nil {
		t.Fatal(err)
	}
	a.peerKeyX25519, err = keys.Ed25519PubToX25519(peerPub)
	if err != nil {
		t.Fatal(err)
	}
	writer := &capturePacketWriter{}
	a.writer = writer

	if err = a.processMessage(MessageCreateChannel{Key: peerChannelPub, Date: int32(time.Now().Unix())}); err != nil {
		t.Fatal(err)
	}
	if err = a.processMessage(MessageCustom{Data: TestMsg{Data: []byte{1}}}); err != nil {
		t.Fatal(err)
	}
	if len(writer.packets) != 1 {
		t.Fatalf("sent packets = %d, want 1", len(writer.packets))
	}

	data, err := decodePacket(peerPriv, writer.packets[0][32:])
	if err != nil {
		t.Fatal(err)
	}
	packet, err := parsePacket(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.Messages) != 2 {
		t.Fatalf("messages = %d, want confirmChannel+nop", len(packet.Messages))
	}
	confirm, ok := packet.Messages[0].(MessageConfirmChannel)
	if !ok {
		t.Fatalf("first message = %T, want MessageConfirmChannel", packet.Messages[0])
	}
	if !bytes.Equal(confirm.PeerKey, peerChannelPub) {
		t.Fatalf("confirm peer key mismatch")
	}
	if _, ok = packet.Messages[1].(MessageNop); !ok {
		t.Fatalf("second message = %T, want MessageNop", packet.Messages[1])
	}
}

func TestQueryRetryRebuildsPacketWithFreshSeqno(t *testing.T) {
	a, peerPriv := testADNLWithPeer(t)
	writer := &capturePacketWriter{}
	a.writer = writer

	ctx, cancel := context.WithTimeout(context.Background(), 620*time.Millisecond)
	defer cancel()

	var result TestMsg
	err := a.Query(ctx, TestMsg{Data: []byte{1}}, &result)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("query error = %v, want deadline exceeded", err)
	}
	if len(writer.packets) < 2 {
		t.Fatalf("sent packets = %d, want at least 2", len(writer.packets))
	}

	first := parseRootPacketFromBytes(t, peerPriv, writer.packets[0])
	second := parseRootPacketFromBytes(t, peerPriv, writer.packets[1])
	if second.SeqnoValue() <= first.SeqnoValue() {
		t.Fatalf("retry seqno = %d, want greater than first seqno %d", second.SeqnoValue(), first.SeqnoValue())
	}

	firstID := queryIDFromPacket(t, first)
	secondID := queryIDFromPacket(t, second)
	if !bytes.Equal(firstID, secondID) {
		t.Fatal("retry changed query id")
	}
}

func TestBuildRequestConcurrentChannelInitializationUsesSingleKey(t *testing.T) {
	const workers = 32

	a, peerPriv := testADNLWithPeer(t)
	packets := make([][]byte, workers)
	errs := make([]error, workers)

	a.mx.Lock()

	var wg sync.WaitGroup
	for i := range workers {
		i := i
		wg.Go(func() {
			packets[i], errs[i] = a.buildRequest(MessageNop{})
		})
	}

	allStarted := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&a.seqno) == workers {
			allStarted = true
			break
		}
		runtime.Gosched()
	}

	a.mx.Unlock()
	wg.Wait()

	if !allStarted {
		t.Fatalf("started %d of %d concurrent requests", atomic.LoadInt64(&a.seqno), workers)
	}

	var channelKey ed25519.PublicKey
	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}

		packet := parseRootPacketFromBytes(t, peerPriv, packets[i])
		var createChannel MessageCreateChannel
		found := false
		for _, message := range packet.Messages {
			if createChannel, found = message.(MessageCreateChannel); found {
				break
			}
		}
		if !found {
			t.Fatalf("request %d does not contain createChannel", i)
		}

		if channelKey == nil {
			channelKey = append(ed25519.PublicKey(nil), createChannel.Key...)
			continue
		}
		if !bytes.Equal(channelKey, createChannel.Key) {
			t.Fatalf("request %d uses a different channel key", i)
		}
	}

	ch := currentTestChannel(a)
	if ch == nil {
		t.Fatal("channel was not initialized")
	}
	if !bytes.Equal(channelKey, ch.key.Public().(ed25519.PublicKey)) {
		t.Fatal("stored channel key differs from the announced key")
	}
}

func TestReadyChannelUsesChannelPacketWhenPeerRecentlyReceived(t *testing.T) {
	a, peerPriv, ch := testADNLWithReadyChannel(t)
	a.noteReceivedPacket()

	packetBytes, err := a.buildRequest(MessageNop{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(packetBytes[:32], ch.idEnc) {
		t.Fatalf("packet destination is not channel id")
	}

	if _, err = decodePacket(peerPriv, packetBytes[32:]); err == nil {
		t.Fatal("channel packet decoded as root packet")
	}
}

func TestIdleReadyChannelSendsRootReinitPacket(t *testing.T) {
	a, peerPriv, ch := testADNLWithReadyChannel(t)
	now := time.Now()
	atomicStoreTime(&a.lastReceivedPacket, now.Add(-idleReinitSilence-time.Second))
	atomicStoreTime(&a.tryReinitAt, now.Add(-time.Millisecond))
	a.recvAddrVer = 111
	a.recvPriorityAddrVer = 222
	a.ourAddrVerOnPeerSide = a.GetAddressList().Version

	packetBytes, err := a.buildRequest(MessageNop{})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(packetBytes[:32], ch.idEnc) {
		t.Fatal("idle reinit packet used channel destination")
	}

	data, err := decodePacket(peerPriv, packetBytes[32:])
	if err != nil {
		t.Fatal(err)
	}
	packet, err := parsePacket(data)
	if err != nil {
		t.Fatal(err)
	}
	if packet.From == nil {
		t.Fatal("root reinit packet must include full source")
	}
	if packet.ReinitDate == nil || packet.DstReinitDate == nil {
		t.Fatalf("root reinit packet missing reinit dates")
	}
	if packet.Address == nil {
		t.Fatal("root reinit packet must refresh our address list")
	}
	if packet.RecvAddrListVersion != nil || packet.RecvPriorityAddrListVersion != nil {
		t.Fatalf("root reinit packet should reset peer addr versions, got %v/%v", packet.RecvAddrListVersion, packet.RecvPriorityAddrListVersion)
	}
	if len(packet.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(packet.Messages))
	}
	if _, ok := packet.Messages[0].(MessageNop); !ok {
		t.Fatalf("message = %T, want MessageNop", packet.Messages[0])
	}
}

func TestPeerPacketReinitResetsChannelAndKeepsOutboundSeqno(t *testing.T) {
	a, _, oldCh := testADNLWithReadyChannel(t)
	peerReinit := int32(time.Now().Unix())
	ourReinit := atomic.LoadInt32(&a.reinitTime)
	seqno := int64(8)
	outSeqno := int64(77)

	atomic.StoreInt32(&a.dstReinit, peerReinit-1)
	atomic.StoreInt64(&a.seqno, outSeqno)
	atomic.StoreInt64(&a.confirmSeqno, 55)
	atomic.StoreInt32(&a.ourAddrVerOnPeerSide, 12)
	atomic.StoreInt32(&a.ourPriorityAddrVerOnPeerSide, 13)

	if err := a.processPacket(&PacketContent{
		Messages:      []any{MessageNop{}},
		Seqno:         &seqno,
		ReinitDate:    &peerReinit,
		DstReinitDate: &ourReinit,
		Rand1:         []byte{1, 2, 3, 4, 5, 6, 7},
		Rand2:         []byte{8, 9, 10, 11, 12, 13, 14},
	}, false); err != nil {
		t.Fatal(err)
	}

	ch := currentTestChannel(a)
	if ch == nil {
		t.Fatal("peer reinit should keep local pending channel")
	}
	if !bytes.Equal(ch.key, oldCh.key) {
		t.Fatal("peer reinit changed local channel key")
	}
	if ch.initDate != oldCh.initDate {
		t.Fatalf("local channel date changed: got %d want %d", ch.initDate, oldCh.initDate)
	}
	if ch.ready.Load() {
		t.Fatal("peer reinit should clear ready channel state")
	}
	if len(ch.peerKey) != 0 {
		t.Fatal("peer reinit should clear peer channel key")
	}
	if ch.peerDate != 0 {
		t.Fatalf("peer channel date = %d, want 0", ch.peerDate)
	}
	if got := atomic.LoadInt64(&a.seqno); got != outSeqno {
		t.Fatalf("seqno = %d, want %d", got, outSeqno)
	}
	if got := atomic.LoadInt64(&a.confirmSeqno); got != seqno {
		t.Fatalf("confirm seqno = %d, want %d", got, seqno)
	}
	if got := atomic.LoadInt32(&a.dstReinit); got != peerReinit {
		t.Fatalf("peer reinit = %d, want %d", got, peerReinit)
	}
	if got := atomic.LoadInt32(&a.ourAddrVerOnPeerSide); got != 0 {
		t.Fatalf("addr ack version = %d, want 0", got)
	}
	if got := atomic.LoadInt32(&a.ourPriorityAddrVerOnPeerSide); got != 0 {
		t.Fatalf("priority addr ack version = %d, want 0", got)
	}
}

func TestMessageReinitResetsChannel(t *testing.T) {
	a, _, oldCh := testADNLWithReadyChannel(t)
	peerReinit := int32(time.Now().Unix())
	outSeqno := int64(10)
	atomic.StoreInt32(&a.dstReinit, peerReinit-1)
	atomic.StoreInt64(&a.seqno, outSeqno)
	atomic.StoreInt64(&a.confirmSeqno, 11)

	if err := a.processMessage(MessageReinit{Date: peerReinit}); err != nil {
		t.Fatal(err)
	}

	ch := currentTestChannel(a)
	if ch == nil {
		t.Fatal("message reinit should keep local pending channel")
	}
	if !bytes.Equal(ch.key, oldCh.key) {
		t.Fatal("message reinit changed local channel key")
	}
	if ch.ready.Load() {
		t.Fatal("message reinit should clear ready channel state")
	}
	if got := atomic.LoadInt32(&a.dstReinit); got != peerReinit {
		t.Fatalf("peer reinit = %d, want %d", got, peerReinit)
	}
	if got := atomic.LoadInt64(&a.seqno); got != outSeqno {
		t.Fatalf("seqno = %d, want %d", got, outSeqno)
	}
	if got := atomic.LoadInt64(&a.confirmSeqno); got != 0 {
		t.Fatalf("confirm seqno = %d, want 0", got)
	}
}

func TestPeerPacketReinitDoesNotRepeatOutboundSeqno(t *testing.T) {
	a, peerPriv, _ := testADNLWithReadyChannel(t)
	peerReinit := int32(time.Now().Unix())
	ourReinit := atomic.LoadInt32(&a.reinitTime)
	inSeqno := int64(1)

	first, err := a.buildRequest(MessageNop{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 {
		t.Fatal("empty first packet")
	}

	if err = a.processPacket(&PacketContent{
		Messages:      []any{MessageNop{}},
		Seqno:         &inSeqno,
		ReinitDate:    &peerReinit,
		DstReinitDate: &ourReinit,
		Rand1:         []byte{1, 2, 3, 4, 5, 6, 7},
		Rand2:         []byte{8, 9, 10, 11, 12, 13, 14},
	}, false); err != nil {
		t.Fatal(err)
	}

	second, err := a.buildRequest(MessageNop{})
	if err != nil {
		t.Fatal(err)
	}

	data, err := decodePacket(peerPriv, second[32:])
	if err != nil {
		t.Fatal(err)
	}
	packet, err := parsePacket(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := packet.SeqnoValue(); got != 2 {
		t.Fatalf("second outbound seqno = %d, want 2", got)
	}
}

func TestPeerPacketReinitKeepsPendingChannelForConfirm(t *testing.T) {
	a, _ := testADNLWithPeer(t)
	_, localChannelKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	peerChannelPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	pending := &Channel{
		adnl:     a,
		key:      localChannelKey,
		initDate: 100,
	}
	a.channelPtr = unsafe.Pointer(pending)

	peerReinit := int32(time.Now().Unix())
	ourReinit := atomic.LoadInt32(&a.reinitTime)
	seqno := int64(9)
	if err = a.processPacket(&PacketContent{
		Messages: []any{
			MessageConfirmChannel{
				Key:     peerChannelPub,
				PeerKey: localChannelKey.Public().(ed25519.PublicKey),
				Date:    200,
			},
		},
		Seqno:         &seqno,
		ReinitDate:    &peerReinit,
		DstReinitDate: &ourReinit,
		Rand1:         []byte{1, 2, 3, 4, 5, 6, 7},
		Rand2:         []byte{8, 9, 10, 11, 12, 13, 14},
	}, false); err != nil {
		t.Fatal(err)
	}

	ch := currentTestChannel(a)
	if ch == nil {
		t.Fatal("channel was lost")
	}
	if !ch.ready.Load() {
		t.Fatal("confirm after peer reinit did not make channel ready")
	}
	if !bytes.Equal(ch.peerKey, peerChannelPub) {
		t.Fatal("peer channel key mismatch")
	}
	if ch.peerDate != 200 {
		t.Fatalf("peer channel date = %d, want 200", ch.peerDate)
	}
	if !bytes.Equal(ch.key, localChannelKey) {
		t.Fatal("local channel key changed")
	}
}

func TestCreateChannelIgnoresStalePeerChannelDate(t *testing.T) {
	a, _ := testADNLWithPeer(t)
	peerChannelPub1, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	peerChannelPub2, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	if err = a.processMessage(MessageCreateChannel{Key: peerChannelPub1, Date: 100}); err != nil {
		t.Fatal(err)
	}
	first := currentTestChannel(a)
	if first == nil {
		t.Fatal("channel was not created")
	}
	initDate := first.initDate

	if err = a.processMessage(MessageCreateChannel{Key: peerChannelPub2, Date: 99}); err != nil {
		t.Fatal(err)
	}
	if ch := currentTestChannel(a); ch == nil || !bytes.Equal(ch.peerKey, peerChannelPub1) || ch.peerDate != 100 {
		t.Fatalf("stale createChannel replaced current channel: %+v", ch)
	}

	if err = a.processMessage(MessageCreateChannel{Key: peerChannelPub2, Date: 101}); err != nil {
		t.Fatal(err)
	}
	ch := currentTestChannel(a)
	if ch == nil {
		t.Fatal("channel was lost")
	}
	if !bytes.Equal(ch.peerKey, peerChannelPub2) {
		t.Fatal("newer createChannel did not replace peer key")
	}
	if ch.peerDate != 101 {
		t.Fatalf("peer channel date = %d, want 101", ch.peerDate)
	}
	if ch.initDate != initDate {
		t.Fatalf("local channel date changed: got %d want %d", ch.initDate, initDate)
	}
}

func TestCreateChannelCopiesPeerKey(t *testing.T) {
	a, _ := testADNLWithPeer(t)
	peerChannelPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte(nil), peerChannelPub...)

	if err = a.processMessage(MessageCreateChannel{Key: peerChannelPub, Date: 100}); err != nil {
		t.Fatal(err)
	}
	peerChannelPub[0] ^= 0xff

	ch := currentTestChannel(a)
	if ch == nil {
		t.Fatal("channel was not created")
	}
	if !bytes.Equal(ch.peerKey, want) {
		t.Fatal("peer channel key aliases source buffer")
	}
}

func TestPriorityAddrAckDoesNotSuppressRegularAddressRefresh(t *testing.T) {
	a, peerPriv := testADNLWithPeer(t)
	addr, err := address.NewAddress(net.ParseIP("127.0.0.1"), 30303)
	if err != nil {
		t.Fatal(err)
	}
	ourList := address.List{
		Addresses:  []address.Address{addr},
		Version:    123,
		ReinitDate: atomic.LoadInt32(&a.reinitTime),
	}
	a.SetAddresses(ourList)

	priorityAck := int32(123)
	seqno := int64(1)
	if err = a.processPacket(&PacketContent{
		Messages:                    []any{MessageNop{}},
		Seqno:                       &seqno,
		RecvPriorityAddrListVersion: &priorityAck,
		Rand1:                       []byte{1, 2, 3, 4, 5, 6, 7},
		Rand2:                       []byte{8, 9, 10, 11, 12, 13, 14},
	}, false); err != nil {
		t.Fatal(err)
	}

	if got := atomic.LoadInt32(&a.ourAddrVerOnPeerSide); got != 0 {
		t.Fatalf("regular addr ack version = %d, want 0", got)
	}
	if got := atomic.LoadInt32(&a.ourPriorityAddrVerOnPeerSide); got != priorityAck {
		t.Fatalf("priority addr ack version = %d, want %d", got, priorityAck)
	}

	packetBytes, err := a.createPacket(2, MessageNop{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := decodePacket(peerPriv, packetBytes[32:])
	if err != nil {
		t.Fatal(err)
	}
	packet, err := parsePacket(data)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Address == nil {
		t.Fatal("regular address list should still be sent")
	}
}

func testADNLWithPeer(t *testing.T) (*ADNL, ed25519.PrivateKey) {
	t.Helper()

	_, ourPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	peerPub, peerPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	a := NewGateway(ourPriv).initADNL()
	a.peerKey = peerPub
	a.peerID, err = tl.Hash(keys.PublicKeyED25519{Key: peerPub})
	if err != nil {
		t.Fatal(err)
	}
	a.peerKeyX25519, err = keys.Ed25519PubToX25519(peerPub)
	if err != nil {
		t.Fatal(err)
	}

	return a, peerPriv
}

func testADNLWithReadyChannel(t *testing.T) (*ADNL, ed25519.PrivateKey, *Channel) {
	t.Helper()

	a, peerPriv := testADNLWithPeer(t)
	peerChannelPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	_, chKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	ch := &Channel{
		adnl:     a,
		key:      chKey,
		initDate: int32(time.Now().Unix()),
	}
	if err = ch.setup(peerChannelPub); err != nil {
		t.Fatal(err)
	}
	ch.ready.Store(true)
	ch.wantConfirm.Store(false)
	a.channelPtr = unsafe.Pointer(ch)

	return a, peerPriv, ch
}

func currentTestChannel(a *ADNL) *Channel {
	return (*Channel)(atomic.LoadPointer(&a.channelPtr))
}

func parseRootPacketFromBytes(t *testing.T, peerPriv ed25519.PrivateKey, packetBytes []byte) *PacketContent {
	t.Helper()

	data, err := decodePacket(peerPriv, packetBytes[32:])
	if err != nil {
		t.Fatal(err)
	}
	packet, err := parsePacket(data)
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

func queryIDFromPacket(t *testing.T, packet *PacketContent) []byte {
	t.Helper()

	for _, message := range packet.Messages {
		if query, ok := message.(MessageQuery); ok {
			return query.ID
		}
	}
	t.Fatal("packet does not contain MessageQuery")
	return nil
}

func atomicStoreTime(ptr *int64, tm time.Time) {
	atomic.StoreInt64(ptr, tm.UnixNano())
}

func TestPacketVerifySignatureRejectsSpoofedSource(t *testing.T) {
	_, victimPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	victimPub := victimPriv.Public().(ed25519.PublicKey)

	attackerPub, attackerPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	raw := buildSignedPacketBytes(t, &PacketContent{
		Rand1: []byte{1, 2, 3, 4, 5, 6, 7},
		From:  &keys.PublicKeyED25519{Key: victimPub},
		Messages: []any{
			MessageNop{},
		},
		Seqno:         int64Ptr(1),
		ConfirmSeqno:  int64Ptr(1),
		ReinitDate:    int32Ptr(1),
		DstReinitDate: int32Ptr(1),
		Rand2:         []byte{8, 9, 10, 11, 12, 13, 14},
	}, attackerPriv)

	packet, err := parsePacket(raw)
	if err != nil {
		t.Fatal(err)
	}

	if err = packet.verifySignature(attackerPub); err == nil || !strings.Contains(err.Error(), "source mismatch") {
		t.Fatalf("expected source mismatch, got %v", err)
	}
}

func TestADNLProcessPacketAllowsPacketWithoutSeqno(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	a := NewGateway(priv).initADNL()
	if err = a.processPacket(&PacketContent{
		Messages: []any{MessageNop{}},
	}, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParsePacketWithoutSignatureDoesNotBuildToSign(t *testing.T) {
	raw := buildPacketBytes(t, &PacketContent{
		Rand1: []byte{1, 2, 3, 4, 5, 6, 7},
		Messages: []any{
			MessageNop{},
		},
		Seqno:        int64Ptr(1),
		ConfirmSeqno: int64Ptr(1),
		Rand2:        []byte{8, 9, 10, 11, 12, 13, 14},
	})

	packet, err := parsePacket(raw)
	if err != nil {
		t.Fatal(err)
	}
	if packet.toSign != nil {
		t.Fatalf("unsigned packet should not build signature payload")
	}
}

func TestGatewayAcceptsPacketSignedBySourceWithEphemeralOuterKey(t *testing.T) {
	_, serverPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := conn.LocalAddr().String()
	_ = conn.Close()

	gateway := NewGateway(serverPriv)
	defer gateway.Close()

	accepted := make(chan Peer, 1)
	gateway.SetConnectionHandler(func(client Peer) error {
		select {
		case accepted <- client:
		default:
		}
		return nil
	})

	if err = gateway.StartServer(addr); err != nil {
		t.Fatal(err)
	}

	senderPub, senderPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, ephPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	packet := buildEncryptedRootPacket(t, serverPriv, senderPriv, ephPriv, &PacketContent{
		Rand1: []byte{1, 2, 3, 4, 5, 6, 7},
		From:  &keys.PublicKeyED25519{Key: senderPub},
		Messages: []any{
			MessageNop{},
		},
		Seqno:        int64Ptr(1),
		ConfirmSeqno: int64Ptr(1),
		Rand2:        []byte{8, 9, 10, 11, 12, 13, 14},
	})

	udpConn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer udpConn.Close()

	if _, err = udpConn.Write(packet); err != nil {
		t.Fatal(err)
	}

	select {
	case peer := <-accepted:
		if got := peer.GetPubKey(); !bytes.Equal(got, senderPub) {
			t.Fatalf("unexpected peer key: %x != %x", got, senderPub)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expected gateway to accept packet")
	}
}

func buildPacketBytes(t *testing.T, packet *PacketContent) []byte {
	t.Helper()

	buf := bytes.NewBuffer(nil)
	if _, err := packet.Serialize(buf); err != nil {
		t.Fatalf("failed to serialize packet: %v", err)
	}
	return buf.Bytes()
}

func buildSignedPacketBytes(t *testing.T, packet *PacketContent, signer ed25519.PrivateKey) []byte {
	t.Helper()

	buf := bytes.NewBuffer(nil)
	if _, err := packet.Serialize(buf); err != nil {
		t.Fatalf("failed to serialize packet to sign: %v", err)
	}

	packet.Signature = ed25519.Sign(signer, buf.Bytes())
	buf.Reset()

	if _, err := packet.Serialize(buf); err != nil {
		t.Fatalf("failed to serialize signed packet: %v", err)
	}

	return buf.Bytes()
}

func int32Ptr(v int32) *int32 {
	return &v
}

func int64Ptr(v int64) *int64 {
	return &v
}

func buildEncryptedRootPacket(t *testing.T, dstPriv, signerPriv, outerPriv ed25519.PrivateKey, packet *PacketContent) []byte {
	t.Helper()

	buf := bytes.NewBuffer(make([]byte, 96))
	if _, err := packet.Serialize(buf); err != nil {
		t.Fatalf("failed to serialize packet to sign: %v", err)
	}

	packet.Signature = ed25519.Sign(signerPriv, buf.Bytes()[96:])
	buf.Truncate(96)

	if _, err := packet.Serialize(buf); err != nil {
		t.Fatalf("failed to serialize signed packet: %v", err)
	}

	data := buf.Bytes()
	payload := data[96:]

	sharedKey, err := keys.SharedKey(outerPriv, dstPriv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatalf("failed to compute shared key: %v", err)
	}

	checksum := sha256.Sum256(payload)
	stream, err := buildSharedCipherForTest(sharedKey, checksum[:])
	if err != nil {
		t.Fatalf("failed to build stream cipher: %v", err)
	}
	stream.XORKeyStream(payload, payload)

	dstID, err := tl.Hash(keys.PublicKeyED25519{Key: dstPriv.Public().(ed25519.PublicKey)})
	if err != nil {
		t.Fatalf("failed to compute dst id: %v", err)
	}

	copy(data, dstID)
	copy(data[32:], outerPriv.Public().(ed25519.PublicKey))
	copy(data[64:], checksum[:])

	return append([]byte(nil), data...)
}

func buildSharedCipherForTest(key, checksum []byte) (cipher.Stream, error) {
	kiv := make([]byte, 48)
	copy(kiv, key[:16])
	copy(kiv[16:], checksum[16:])
	copy(kiv[32:], checksum[:4])
	copy(kiv[36:], key[20:])

	block, err := aes.NewCipher(kiv[:32])
	if err != nil {
		return nil, err
	}
	return cipher.NewCTR(block, kiv[32:]), nil
}
