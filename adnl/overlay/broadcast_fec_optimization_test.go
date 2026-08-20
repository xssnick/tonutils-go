package overlay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"math"
	"sync"
	"testing"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
)

func legacyBroadcastFECID(source any, flags int32, dataHash []byte, dataSize uint32, fec any) ([]byte, error) {
	typeID, err := tl.Hash(fec)
	if err != nil {
		return nil, err
	}

	sourceID := make([]byte, 32)
	if flags&BroadcastFlagAnySender == 0 {
		sourceID, err = tl.Hash(source)
		if err != nil {
			return nil, err
		}
	}

	return tl.Hash(&BroadcastFECID{
		Source:   sourceID,
		Type:     typeID,
		DataHash: dataHash,
		Size:     dataSize,
		Flags:    flags,
	})
}

func TestBroadcastFECFixedEncodingMatchesTL(t *testing.T) {
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x31}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	source := keys.PublicKeyED25519{Key: publicKey}
	dataHash := bytes.Repeat([]byte{0xA5}, 32)
	fecTypes := []any{
		rldp.FECRaptorQ{DataSize: 1234, SymbolSize: 768, SymbolsCount: 2},
		&rldp.FECRaptorQ{DataSize: 1234, SymbolSize: 768, SymbolsCount: 2},
		rldp.FECRoundRobin{DataSize: 1234, SymbolSize: 768, SymbolsCount: 2},
		&rldp.FECRoundRobin{DataSize: 1234, SymbolSize: 768, SymbolsCount: 2},
		rldp.FECOnline{DataSize: 1234, SymbolSize: 768, SymbolsCount: 2},
		&rldp.FECOnline{DataSize: 1234, SymbolSize: 768, SymbolsCount: 2},
	}

	for _, flags := range []int32{0, BroadcastFlagAnySender} {
		for _, fec := range fecTypes {
			want, err := legacyBroadcastFECID(source, flags, dataHash, 1234, fec)
			if err != nil {
				t.Fatalf("legacy id: %v", err)
			}
			got, err := calcBroadcastFECID(source, flags, dataHash, 1234, fec)
			if err != nil {
				t.Fatalf("fixed id: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("id mismatch for %T flags=%d", fec, flags)
			}
		}
	}

	broadcastHash := bytes.Repeat([]byte{0x17}, 32)
	partHash := bytes.Repeat([]byte{0x29}, 32)
	wantPartID, err := tl.Hash(&BroadcastFECPartID{
		BroadcastHash: broadcastHash,
		DataHash:      partHash,
		Seqno:         117,
	})
	if err != nil {
		t.Fatalf("legacy part id: %v", err)
	}
	gotPartID, err := calcBroadcastFECPartID(broadcastHash, partHash, 117)
	if err != nil {
		t.Fatalf("fixed part id: %v", err)
	}
	if !bytes.Equal(gotPartID, wantPartID) {
		t.Fatal("part id mismatch")
	}

	wantToSign, err := tl.Serialize(&BroadcastToSign{Hash: partHash, Date: 123456}, true)
	if err != nil {
		t.Fatalf("legacy to-sign: %v", err)
	}
	gotToSign, err := serializeBroadcastFECToSign(partHash, 123456)
	if err != nil {
		t.Fatalf("fixed to-sign: %v", err)
	}
	if !bytes.Equal(gotToSign, wantToSign) {
		t.Fatal("to-sign mismatch")
	}
}

type recordingPreparedBroadcastPeer struct {
	id          []byte
	legacyCalls int
	bodies      [][]byte
	mx          sync.Mutex
}

func (p *recordingPreparedBroadcastPeer) ID() []byte {
	return p.id
}

func (p *recordingPreparedBroadcastPeer) SendCustomMessage(context.Context, tl.Serializable) error {
	p.mx.Lock()
	p.legacyCalls++
	p.mx.Unlock()
	return nil
}

func (p *recordingPreparedBroadcastPeer) SendPreparedCustomMessage(_ context.Context, body []byte) error {
	p.mx.Lock()
	p.bodies = append(p.bodies, body)
	p.mx.Unlock()
	return nil
}

func (p *recordingPreparedBroadcastPeer) snapshot() (int, [][]byte) {
	p.mx.Lock()
	defer p.mx.Unlock()
	return p.legacyCalls, append([][]byte(nil), p.bodies...)
}

func TestBroadcastFECSenderSharesPreparedPartAcrossPeers(t *testing.T) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x42}, ed25519.SeedSize))
	sender, err := NewBroadcastFECSender(
		privateKey,
		CertificateEmpty{},
		bytes.Repeat([]byte{0x5A}, 256),
		BroadcastFlagAnySender,
		WithBroadcastFECSymbolSize(64),
		WithBroadcastFECBurstSize(1),
		WithBroadcastFECDate(1234),
	)
	if err != nil {
		t.Fatalf("create sender: %v", err)
	}

	left := &recordingPreparedBroadcastPeer{id: bytes.Repeat([]byte{0x01}, 32)}
	right := &recordingPreparedBroadcastPeer{id: bytes.Repeat([]byte{0x02}, 32)}
	sent, err := sender.SendNow(t.Context(), StaticBroadcastPeerSet{left, right}, 1)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if sent != 1 {
		t.Fatalf("sent parts = %d, want 1", sent)
	}

	part, err := sender.Part(0)
	if err != nil {
		t.Fatalf("part: %v", err)
	}
	wantWire, err := tl.Serialize(part.Full, true)
	if err != nil {
		t.Fatalf("serialize expected part: %v", err)
	}

	leftLegacy, leftBodies := left.snapshot()
	rightLegacy, rightBodies := right.snapshot()
	if leftLegacy != 0 || rightLegacy != 0 {
		t.Fatalf("legacy sends = %d/%d, want 0/0", leftLegacy, rightLegacy)
	}
	if len(leftBodies) != 1 || len(rightBodies) != 1 {
		t.Fatalf("prepared sends = %d/%d, want 1/1", len(leftBodies), len(rightBodies))
	}
	if !bytes.Equal(leftBodies[0], wantWire) || !bytes.Equal(rightBodies[0], wantWire) {
		t.Fatal("prepared wire differs from canonical TL")
	}
	if &leftBodies[0][0] != &rightBodies[0][0] || &leftBodies[0][0] != &part.FullWire[0] {
		t.Fatal("fanout did not share the prepared wire backing array")
	}
}

func TestBroadcastFECRelaySharesPreparedPartAcrossPeers(t *testing.T) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x47}, ed25519.SeedSize))
	sender, err := NewBroadcastFECSender(
		privateKey,
		CertificateEmpty{},
		bytes.Repeat([]byte{0x68}, 128),
		BroadcastFlagAnySender,
		WithBroadcastFECSymbolSize(64),
		WithBroadcastFECDate(1234),
	)
	if err != nil {
		t.Fatalf("create sender: %v", err)
	}
	part, err := sender.Part(0)
	if err != nil {
		t.Fatalf("part: %v", err)
	}

	stream := fecBroadcastStream{
		parts:          map[uint32]broadcastFECRelayPart{},
		receivedPeers:  map[string]struct{}{},
		completedPeers: map[string]struct{}{},
	}
	broadcastHash, err := part.Full.CalcID()
	if err != nil {
		t.Fatalf("broadcast id: %v", err)
	}
	partDataHash := part.Full.PartDataHash()
	if err = stream.addRelayPart(0, part.Full, broadcastHash, partDataHash, bytes.Repeat([]byte{0x70}, 32)); err != nil {
		t.Fatalf("prepare relay part: %v", err)
	}

	left := &recordingPreparedBroadcastPeer{id: bytes.Repeat([]byte{0x21}, 32)}
	right := &recordingPreparedBroadcastPeer{id: bytes.Repeat([]byte{0x22}, 32)}
	ops := stream.relayPartOpsLocked(0, []BroadcastPeer{left, right}, bytes.Repeat([]byte{0x23}, 32), true)
	if err = sendBroadcastFECRelayOps(t.Context(), nil, ops); err != nil {
		t.Fatalf("relay: %v", err)
	}

	leftLegacy, leftBodies := left.snapshot()
	rightLegacy, rightBodies := right.snapshot()
	if leftLegacy != 0 || rightLegacy != 0 || len(leftBodies) != 1 || len(rightBodies) != 1 {
		t.Fatalf("legacy/prepared sends = %d/%d %d/%d", leftLegacy, rightLegacy, len(leftBodies), len(rightBodies))
	}
	if &leftBodies[0][0] != &rightBodies[0][0] {
		t.Fatal("relay did not share the prepared wire backing array")
	}
	want, err := tl.Serialize(part.Full, true)
	if err != nil {
		t.Fatalf("serialize expected relay: %v", err)
	}
	if !bytes.Equal(leftBodies[0], want) {
		t.Fatal("relay prepared wire differs from canonical TL")
	}
}

func TestBroadcastTwoStepSimpleSharesPreparedWireAcrossPeers(t *testing.T) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x53}, ed25519.SeedSize))
	left := &recordingPreparedBroadcastPeer{id: bytes.Repeat([]byte{0x11}, 32)}
	right := &recordingPreparedBroadcastPeer{id: bytes.Repeat([]byte{0x12}, 32)}

	result, err := SendBroadcastTwoStep(t.Context(), BroadcastTwoStepSendRequest{
		Key:         privateKey,
		Certificate: CertificateEmpty{},
		LocalADNLID: bytes.Repeat([]byte{0x13}, 32),
		Payload:     bytes.Repeat([]byte{0x61}, 128),
		PeerSet:     StaticBroadcastPeerSet{left, right},
	}, WithBroadcastTwoStepFECThreshold(math.MaxUint32, 1), WithBroadcastTwoStepDate(1234))
	if err != nil {
		t.Fatalf("send two-step: %v", err)
	}
	if result.Mode != BroadcastTwoStepModeSimple || result.Sent != 2 {
		t.Fatalf("result = %+v", result)
	}

	leftLegacy, leftBodies := left.snapshot()
	rightLegacy, rightBodies := right.snapshot()
	if leftLegacy != 0 || rightLegacy != 0 || len(leftBodies) != 1 || len(rightBodies) != 1 {
		t.Fatalf("legacy/prepared sends = %d/%d %d/%d", leftLegacy, rightLegacy, len(leftBodies), len(rightBodies))
	}
	if &leftBodies[0][0] != &rightBodies[0][0] {
		t.Fatal("two-step fanout did not share the prepared wire backing array")
	}
}

var benchmarkBroadcastFECID []byte
var benchmarkBroadcastWire []byte

func BenchmarkBroadcastFECIDEncoding(b *testing.B) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x71}, ed25519.SeedSize))
	source := keys.PublicKeyED25519{Key: privateKey.Public().(ed25519.PublicKey)}
	dataHash := bytes.Repeat([]byte{0x81}, 32)
	fec := rldp.FECRaptorQ{DataSize: 1 << 20, SymbolSize: 768, SymbolsCount: 1366}

	b.Run("fixed", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var err error
			benchmarkBroadcastFECID, err = calcBroadcastFECID(source, 0, dataHash, fec.DataSize, fec)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("reflective", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var err error
			benchmarkBroadcastFECID, err = legacyBroadcastFECID(source, 0, dataHash, fec.DataSize, fec)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkBroadcastFECToSignEncoding(b *testing.B) {
	partHash := bytes.Repeat([]byte{0x91}, 32)

	b.Run("fixed", func(b *testing.B) {
		var wire [4 + 32 + 4]byte
		b.ReportAllocs()
		for b.Loop() {
			if err := fillBroadcastFECToSign(&wire, partHash, 123456); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("reflective", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var err error
			benchmarkBroadcastWire, err = tl.Serialize(&BroadcastToSign{Hash: partHash, Date: 123456}, true)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

type benchmarkLegacyBroadcastPeer struct {
	id []byte
}

func (p benchmarkLegacyBroadcastPeer) ID() []byte {
	return p.id
}

func (benchmarkLegacyBroadcastPeer) SendCustomMessage(_ context.Context, message tl.Serializable) error {
	var err error
	benchmarkBroadcastWire, err = tl.Serialize(message, true)
	return err
}

type benchmarkPreparedBroadcastPeer struct {
	benchmarkLegacyBroadcastPeer
}

func (benchmarkPreparedBroadcastPeer) SendPreparedCustomMessage(_ context.Context, body []byte) error {
	benchmarkBroadcastWire = body
	return nil
}

func BenchmarkBroadcastFECRelayFanoutSerialization(b *testing.B) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x73}, ed25519.SeedSize))
	full := &BroadcastFEC{
		Source:      keys.PublicKeyED25519{Key: privateKey.Public().(ed25519.PublicKey)},
		Certificate: CertificateEmpty{},
		DataHash:    bytes.Repeat([]byte{0x82}, 32),
		DataSize:    1 << 20,
		Flags:       BroadcastFlagAnySender,
		Data:        bytes.Repeat([]byte{0x83}, 768),
		Seqno:       17,
		FEC:         rldp.FECRaptorQ{DataSize: 1 << 20, SymbolSize: 768, SymbolsCount: 1366},
		Date:        1234,
		Signature:   bytes.Repeat([]byte{0x84}, ed25519.SignatureSize),
	}

	for _, prepared := range []bool{true, false} {
		name := "legacy"
		if prepared {
			name = "prepared"
		}
		b.Run(name, func(b *testing.B) {
			ops := make([]broadcastFECRelayOp, 5)
			for i := range ops {
				legacy := benchmarkLegacyBroadcastPeer{id: bytes.Repeat([]byte{byte(i + 1)}, 32)}
				var peer BroadcastPeer = legacy
				if prepared {
					peer = benchmarkPreparedBroadcastPeer{benchmarkLegacyBroadcastPeer: legacy}
				}
				ops[i] = broadcastFECRelayOp{peer: peer, msg: full, seqno: full.Seqno}
			}

			b.ReportAllocs()
			for b.Loop() {
				if prepared {
					wire, err := prepareBroadcastMessage(full)
					if err != nil {
						b.Fatal(err)
					}
					for i := range ops {
						ops[i].wire = wire
					}
				}
				if err := sendBroadcastFECRelayOps(b.Context(), nil, ops); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
