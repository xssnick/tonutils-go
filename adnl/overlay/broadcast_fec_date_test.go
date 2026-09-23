package overlay

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tl"
)

func TestProcessFECBroadcastAcceptsPerPartDates(t *testing.T) {
	for _, flags := range []int32{0, BroadcastFlagAnySender} {
		t.Run(map[int32]string{0: "single sender", BroadcastFlagAnySender: "any sender"}[flags], func(t *testing.T) {
			o := CreateExtendedADNL(newMockADNL()).CreateOverlayWithSettings(bytes.Repeat([]byte{0xA2}, 32), 4096, true, true)
			t.Cleanup(o.Close)
			peer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0xA3}, 32)}
			state := NewBroadcastFECRelayState()
			o.EnableBroadcastFECRelay(bytes.Repeat([]byte{0xA4}, 32), mockBroadcastPeerSet{peers: []BroadcastPeer{peer}}, state)

			_, key := keyPairFromSeed(71)
			_, otherKey := keyPairFromSeed(72)
			date := uint32(time.Now().Unix()) - 2
			payload := Message{Overlay: bytes.Repeat([]byte{0xA5}, 32)}
			sender, err := NewBroadcastFECSenderFromTL(key, CertificateEmpty{}, payload, flags, WithBroadcastFECSymbolSize(8), WithBroadcastFECDate(date))
			if err != nil {
				t.Fatal(err)
			}
			deliveries := 0
			o.SetBroadcastHandlerWithInfo(func(msg tl.Serializable, info BroadcastInfo) BroadcastDisposition {
				deliveries++
				got, ok := msg.(Message)
				if !ok || !bytes.Equal(got.Overlay, payload.Overlay) {
					t.Errorf("unexpected decoded payload: %#v", msg)
				}
				return BroadcastDispositionAcceptAndRelay
			})

			for seqno := uint32(0); seqno < sender.fec.SymbolsCount; seqno++ {
				part, err := sender.part(seqno)
				if err != nil {
					t.Fatal(err)
				}
				full := *part.full
				if seqno > 0 {
					full.Date = date + 1
				}
				signer := key
				if flags == BroadcastFlagAnySender && seqno%2 == 1 {
					signer = otherKey
					full.Source = ed25519Public(signer)
				}
				if err = full.Sign(signer); err != nil {
					t.Fatal(err)
				}
				if err = o.processFECBroadcast(&full); err != nil {
					t.Fatalf("part %d, date %d: %v", seqno, full.Date, err)
				}
				waitOrdinaryBroadcastRelay(t, o.BroadcastReceiver)
				if len(peer.sent) != int(seqno)+1 {
					t.Fatalf("relayed %d parts after part %d", len(peer.sent), seqno)
				}
				relayed, ok := peer.sent[seqno].(*BroadcastFEC)
				if !ok || relayed.Date != full.Date || !bytes.Equal(relayed.Signature, full.Signature) {
					t.Fatalf("relay changed signed part: %#v", peer.sent[seqno])
				}
				if err = relayed.VerifySignature(); err != nil {
					t.Fatalf("relayed signature: %v", err)
				}
			}
			if deliveries != 1 {
				t.Fatalf("deliveries = %d, want 1", deliveries)
			}

			// Short parts have no date on the wire; C++ verifies them with the
			// first full part's date, even after receiving other full-part dates.
			part, err := sender.part(sender.fec.SymbolsCount + 1)
			if err != nil {
				t.Fatal(err)
			}
			if err = o.processFECBroadcastShort(part.short); err != nil {
				t.Fatalf("short part using initial date: %v", err)
			}
			waitOrdinaryBroadcastRelay(t, o.BroadcastReceiver)
		})
	}
}

func TestProcessFECBroadcastValidatesEachPartDateAndSignature(t *testing.T) {
	for _, tc := range []struct {
		name   string
		offset int64
		resign bool
		want   string
	}{
		{name: "changed date without signature", offset: -1, want: "signature"},
		{name: "expired", offset: -60, resign: true, want: "too old broadcast"},
		{name: "future", offset: 60, resign: true, want: "too new broadcast"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := CreateExtendedADNL(newMockADNL()).CreateOverlayWithSettings(bytes.Repeat([]byte{0xB1}, 32), 4096, true, true)
			t.Cleanup(o.Close)
			_, key := keyPairFromSeed(73)
			now := time.Now().Unix()
			sender, err := NewBroadcastFECSenderFromTL(key, CertificateEmpty{}, Message{Overlay: bytes.Repeat([]byte{0xB2}, 32)}, 0, WithBroadcastFECSymbolSize(8), WithBroadcastFECDate(uint32(now-2)))
			if err != nil {
				t.Fatal(err)
			}
			first, err := sender.part(0)
			if err != nil {
				t.Fatal(err)
			}
			if err = o.processFECBroadcast(first.full); err != nil {
				t.Fatal(err)
			}
			next, err := sender.part(1)
			if err != nil {
				t.Fatal(err)
			}
			invalid := *next.full
			invalid.Date = uint32(now + tc.offset)
			if tc.resign {
				if err = invalid.Sign(key); err != nil {
					t.Fatal(err)
				}
			}
			if err = o.processFECBroadcast(&invalid); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			valid := *next.full
			valid.Date = uint32(now - 1)
			if err = valid.Sign(key); err != nil {
				t.Fatal(err)
			}
			if err = o.processFECBroadcast(&valid); err != nil {
				t.Fatalf("valid part after rejection: %v", err)
			}
		})
	}
}
