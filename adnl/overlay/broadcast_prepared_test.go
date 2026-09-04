package overlay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"testing"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
)

// preparedFrameADNL is a mock transport with the prepared send path; it
// records which prepared messages it was handed.
type preparedFrameADNL struct {
	*mockADNL
	prepared []*adnl.PreparedCustomMessage
}

func (m *preparedFrameADNL) SendPreparedCustomMessage(_ context.Context, msg *adnl.PreparedCustomMessage) error {
	m.prepared = append(m.prepared, msg)
	return nil
}

func preparedTestBroadcastFEC() *BroadcastFEC {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x31}, ed25519.SeedSize))
	return &BroadcastFEC{
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
}

// TestPreparedBroadcastMessageFrameMatchesSendCustomMessage pins the cached
// ADNL frame to the bytes ADNL.SendCustomMessage serializes for what
// ADNLOverlayWrapper.SendPreparedCustomMessage hands it, for every broadcast
// form the FEC paths send.
func TestPreparedBroadcastMessageFrameMatchesSendCustomMessage(t *testing.T) {
	full := preparedTestBroadcastFEC()
	short := &BroadcastFECShort{
		Source:        full.Source,
		Certificate:   full.Certificate,
		BroadcastHash: bytes.Repeat([]byte{0x85}, 32),
		PartDataHash:  bytes.Repeat([]byte{0x86}, 32),
		Seqno:         17,
		Signature:     full.Signature,
	}
	simple := &Broadcast{
		Source:      full.Source,
		Certificate: full.Certificate,
		Flags:       BroadcastFlagAnySender,
		Data:        bytes.Repeat([]byte{0x87}, 300),
		Date:        1234,
		Signature:   full.Signature,
	}

	for i, message := range []tl.Serializable{full, short, simple} {
		prepared, err := PrepareBroadcastMessage(message)
		if err != nil {
			t.Fatalf("message %d: prepare: %v", i, err)
		}
		wantBody, err := tl.Serialize(message, true)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(prepared.Body(), wantBody) {
			t.Fatalf("message %d: body differs from tl serialization", i)
		}

		for _, overlayID := range [][]byte{bytes.Repeat([]byte{0x01}, 32), bytes.Repeat([]byte{0x02}, 32)} {
			want, err := tl.Serialize(&adnl.MessageCustom{Data: []tl.Serializable{
				Message{Overlay: overlayID},
				tl.Raw(prepared.Body()),
			}}, true)
			if err != nil {
				t.Fatal(err)
			}

			framed, err := prepared.ADNLMessage(overlayID)
			if err != nil {
				t.Fatalf("message %d: frame: %v", i, err)
			}
			if !bytes.Equal(framed.Wire(), want) {
				t.Fatalf("message %d overlay %x: frame differs from SendCustomMessage serialization\n got %x\nwant %x", i, overlayID[:1], framed.Wire(), want)
			}

			again, err := prepared.ADNLMessage(overlayID)
			if err != nil {
				t.Fatal(err)
			}
			if again != framed {
				t.Fatalf("message %d: frame for the same overlay was rebuilt", i)
			}
		}
	}
}

func TestPreparedBroadcastMessageRejectsBadOverlayID(t *testing.T) {
	prepared := NewPreparedBroadcastMessage([]byte{1, 2, 3, 4})
	if _, err := prepared.ADNLMessage([]byte{1, 2, 3}); err == nil {
		t.Fatal("frame for a 3-byte overlay id was built")
	}
}

// TestBroadcastFECRelayFanoutSharesADNLFrame relays one part to several ADNL
// overlay peers of the same overlay and checks every transport received the
// very same prepared ADNL message, and that a transport without the prepared
// path still gets the body through the reflective wrapper path.
func TestBroadcastFECRelayFanoutSharesADNLFrame(t *testing.T) {
	overlayID := bytes.Repeat([]byte{0x77}, 32)
	full := preparedTestBroadcastFEC()
	prepared, err := PrepareBroadcastMessage(full)
	if err != nil {
		t.Fatal(err)
	}

	const fanout = 5
	transports := make([]*preparedFrameADNL, 0, fanout)
	peers := make([]BroadcastPeer, 0, fanout+1)
	for i := 0; i < fanout; i++ {
		base := newMockADNL()
		base.id = bytes.Repeat([]byte{byte(i + 1)}, 32)
		transport := &preparedFrameADNL{mockADNL: base}
		wrapper, err := CreateExtendedADNL(transport).AttachOverlay(newTestBroadcastReceiver(t, overlayID))
		if err != nil {
			t.Fatal(err)
		}
		transports = append(transports, transport)
		peers = append(peers, wrapper)
	}

	legacy := newMockADNL()
	legacy.id = bytes.Repeat([]byte{0x99}, 32)
	legacyWrapper, err := CreateExtendedADNL(legacy).AttachOverlay(newTestBroadcastReceiver(t, overlayID))
	if err != nil {
		t.Fatal(err)
	}
	peers = append(peers, legacyWrapper)

	ops := make([]broadcastFECRelayOp, 0, len(peers))
	for _, peer := range peers {
		ops = append(ops, broadcastFECRelayOp{peer: peer, msg: full, wire: prepared, seqno: full.Seqno})
	}
	if err = sendBroadcastFECRelayOps(context.Background(), nil, ops); err != nil {
		t.Fatal(err)
	}

	framed, err := prepared.ADNLMessage(overlayID)
	if err != nil {
		t.Fatal(err)
	}
	for i, transport := range transports {
		if len(transport.prepared) != 1 || len(transport.mockADNL.sendCustomCalls) != 0 {
			t.Fatalf("peer %d: prepared sends = %d, reflective sends = %d", i, len(transport.prepared), len(transport.mockADNL.sendCustomCalls))
		}
		if transport.prepared[0] != framed {
			t.Fatalf("peer %d: received a different prepared frame", i)
		}
	}

	if len(legacy.sendCustomCalls) != 1 {
		t.Fatalf("legacy transport sends = %d, want 1", len(legacy.sendCustomCalls))
	}
	legacyWire, err := tl.Serialize(&adnl.MessageCustom{Data: legacy.sendCustomCalls[0]}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(legacyWire, framed.Wire()) {
		t.Fatal("legacy transport path serializes differently from the prepared frame")
	}
}

// TestSendPreparedBroadcastPathSelection covers the three peer kinds
// SendPreparedBroadcast dispatches to.
func TestSendPreparedBroadcastPathSelection(t *testing.T) {
	full := preparedTestBroadcastFEC()
	prepared, err := PrepareBroadcastMessage(full)
	if err != nil {
		t.Fatal(err)
	}

	bodied := &recordingPreparedBroadcastPeer{id: bytes.Repeat([]byte{0x01}, 32)}
	if err = SendPreparedBroadcast(context.Background(), bodied, full, prepared); err != nil {
		t.Fatal(err)
	}
	if legacyCalls, bodies := bodied.snapshot(); legacyCalls != 0 || len(bodies) != 1 || &bodies[0][0] != &prepared.Body()[0] {
		t.Fatalf("body peer: legacy = %d, bodies = %d", legacyCalls, len(bodies))
	}

	legacy := &benchmarkLegacyBroadcastPeer{id: bytes.Repeat([]byte{0x02}, 32)}
	if err = SendPreparedBroadcast(context.Background(), legacy, full, prepared); err != nil {
		t.Fatal(err)
	}
	if err = SendPreparedBroadcast(context.Background(), legacy, full, nil); err != nil {
		t.Fatal(err)
	}

	overlayID := bytes.Repeat([]byte{0x03}, 32)
	transport := &preparedFrameADNL{mockADNL: newMockADNL()}
	wrapper, err := CreateExtendedADNL(transport).AttachOverlay(newTestBroadcastReceiver(t, overlayID))
	if err != nil {
		t.Fatal(err)
	}
	if err = SendPreparedBroadcast(context.Background(), wrapper, full, prepared); err != nil {
		t.Fatal(err)
	}
	framed, err := prepared.ADNLMessage(overlayID)
	if err != nil {
		t.Fatal(err)
	}
	if len(transport.prepared) != 1 || transport.prepared[0] != framed {
		t.Fatalf("framed peer: prepared sends = %d", len(transport.prepared))
	}
}

// BenchmarkBroadcastFECFanoutFrame compares what a five-peer fanout costs in
// framing alone: the wrapper's per-peer reflective path against the cached
// frame. The transports are mocks, so packet build and encryption are out.
func BenchmarkBroadcastFECFanoutFrame(b *testing.B) {
	overlayID := bytes.Repeat([]byte{0x55}, 32)
	full := preparedTestBroadcastFEC()
	prepared, err := PrepareBroadcastMessage(full)
	if err != nil {
		b.Fatal(err)
	}

	newWrappers := func(framed bool) []*ADNLOverlayWrapper {
		wrappers := make([]*ADNLOverlayWrapper, 0, 5)
		for i := 0; i < 5; i++ {
			var transport ADNL = newMockADNL()
			if framed {
				transport = &preparedFrameADNL{mockADNL: newMockADNL()}
			}
			wrapper, err := CreateExtendedADNL(transport).AttachOverlay(newTestBroadcastReceiver(b, overlayID))
			if err != nil {
				b.Fatal(err)
			}
			wrappers = append(wrappers, wrapper)
		}
		return wrappers
	}

	for _, framed := range []bool{false, true} {
		b.Run(fmt.Sprintf("framed=%t", framed), func(b *testing.B) {
			wrappers := newWrappers(framed)
			// tl.Serialize into a fresh buffer is what the reflective path
			// costs on a real ADNL transport too; the mock records it.
			b.ReportAllocs()
			for b.Loop() {
				for _, wrapper := range wrappers {
					if err := wrapper.SendPreparedBroadcastMessage(context.Background(), prepared); err != nil {
						b.Fatal(err)
					}
					if base, ok := wrapper.ADNLWrapper.ADNL.(*mockADNL); ok {
						if _, err := tl.Serialize(&adnl.MessageCustom{Data: base.sendCustomCalls[0]}, true); err != nil {
							b.Fatal(err)
						}
						base.sendCustomCalls = base.sendCustomCalls[:0]
					} else if t := wrapper.ADNLWrapper.ADNL.(*preparedFrameADNL); t != nil {
						t.prepared = t.prepared[:0]
					}
				}
			}
		})
	}
}
