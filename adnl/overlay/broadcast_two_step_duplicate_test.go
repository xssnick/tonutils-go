package overlay

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl/keys"
)

func TestTwoStepFECDuplicateRequiresExactVerifiedBytes(t *testing.T) {
	wrapper, state, parts, sourceID := newTwoStepFECReceiveFixture(t, 161)
	t.Cleanup(wrapper.BroadcastReceiver.Close)
	part := parts[0]
	if err := wrapper.processBroadcastTwoStepFEC(part, sourceID); err != nil {
		t.Fatal(err)
	}
	if err := wrapper.processBroadcastTwoStepFEC(part, sourceID); err != nil {
		t.Fatalf("exact duplicate failed: %v", err)
	}

	for _, test := range []struct {
		name   string
		change func(*BroadcastTwoStepFEC)
	}{
		{"part", func(m *BroadcastTwoStepFEC) { m.Part = bytes.Clone(m.Part); m.Part[0] ^= 1 }},
		{"signature", func(m *BroadcastTwoStepFEC) { m.Signature = bytes.Clone(m.Signature); m.Signature[0] ^= 1 }},
		{"seqno", func(m *BroadcastTwoStepFEC) { m.Seqno++ }},
		{"flags", func(m *BroadcastTwoStepFEC) { m.Flags ^= 2 }},
		{"date", func(m *BroadcastTwoStepFEC) { m.Date++ }},
		{"source ADNL", func(m *BroadcastTwoStepFEC) { m.SourceADNL = bytes.Clone(m.SourceADNL); m.SourceADNL[0] ^= 1 }},
		{"hash", func(m *BroadcastTwoStepFEC) { m.DataHash = bytes.Clone(m.DataHash); m.DataHash[0] ^= 1 }},
		{"data size", func(m *BroadcastTwoStepFEC) { m.DataSize++ }},
		{"extra", func(m *BroadcastTwoStepFEC) { m.Extra = []byte("changed") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := *part
			test.change(&changed)
			if err := wrapper.processBroadcastTwoStepFEC(&changed, sourceID); err == nil {
				t.Fatal("changed signed message reused a verified duplicate")
			}
		})
	}

	id, err := part.CalcID()
	if err != nil {
		t.Fatal(err)
	}
	state.mx.Lock()
	stream := state.streams[newBroadcastTwoStepIDKey(id)]
	state.mx.Unlock()
	stream.mx.Lock()
	if len(stream.seenParts) != 1 {
		t.Fatalf("invalid duplicates retained %d parts", len(stream.seenParts))
	}
	stream.mx.Unlock()
}

func TestTwoStepFECConcurrentExactDuplicates(t *testing.T) {
	wrapper, state, parts, sourceID := newTwoStepFECReceiveFixture(t, 171)
	t.Cleanup(wrapper.BroadcastReceiver.Close)
	start := make(chan struct{})
	var workers sync.WaitGroup
	errors := make(chan error, 32)
	for range cap(errors) {
		workers.Go(func() {
			<-start
			errors <- wrapper.processBroadcastTwoStepFEC(parts[0], sourceID)
		})
	}
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if stats := state.Stats(); stats.ActiveStreams != 1 || stats.CompletedTotal != 0 {
		t.Fatalf("concurrent duplicate created unexpected state: %+v", stats)
	}
	for _, part := range parts[1:] {
		if err := wrapper.processBroadcastTwoStepFEC(part, sourceID); err != nil {
			t.Fatal(err)
		}
	}
	if stats := state.Stats(); stats.ActiveStreams != 0 || stats.CompletedTotal != 1 {
		t.Fatalf("duplicates prevented subsequent decode: %+v", stats)
	}
}

func TestTwoStepFECVerifiedDigestBudget(t *testing.T) {
	for _, size := range []uint32{32, 1024, 1 << 20} {
		for _, partSize := range []uint32{1, 32, 768} {
			limit := broadcastTwoStepSeqnoLimit(size, partSize)
			budget := estimateTwoStepBroadcastBudgetBytes(size, partSize)
			if budget < int64(limit)*128+int64(size) {
				t.Fatalf("size %d part %d: budget %d omits digest capacity for %d parts", size, partSize, budget, limit)
			}
		}
	}
}

func BenchmarkTwoStepFECDuplicateVerification(b *testing.B) {
	_, key := keyPairFromSeed(181)
	for _, size := range []int{768, 256 << 10} {
		part := bytes.Repeat([]byte{0x31}, size)
		id := bytes.Repeat([]byte{0x32}, 32)
		signature, err := signBroadcastTwoStepFEC(key, id, 1, part)
		if err != nil {
			b.Fatal(err)
		}
		public := key.Public().(ed25519.PublicKey)
		verified := broadcastTwoStepPartFingerprint(id, public, 1, part, signature)
		b.Run(fmt.Sprintf("size_%d/full_verify", size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := verifyBroadcastTwoStepFECSignature(keys.PublicKeyED25519{Key: public}, id, 1, part, signature); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("size_%d/verified_digest", size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if broadcastTwoStepPartFingerprint(id, public, 1, part, signature) != verified {
					b.Fatal("digest mismatch")
				}
			}
		})
	}
}

func BenchmarkTwoStepFECDuplicateReceive(b *testing.B) {
	_, key := keyPairFromSeed(191)
	for _, size := range []int{768, 256 << 10} {
		for _, cached := range []bool{false, true} {
			name := "uncached"
			if cached {
				name = "verified_digest"
			}
			b.Run(fmt.Sprintf("size_%d/%s", size, name), func(b *testing.B) {
				receiver := newTestBroadcastReceiver(b, bytes.Repeat([]byte{0x41}, 32))
				receiver.EnableBroadcastTwoStep(bytes.Repeat([]byte{0x42}, 32), nil)
				wrapper := &ADNLOverlayWrapper{BroadcastReceiver: receiver}
				part := &BroadcastTwoStepFEC{
					Source:      keys.PublicKeyED25519{Key: key.Public().(ed25519.PublicKey)},
					SourceADNL:  bytes.Repeat([]byte{0x43}, 32),
					Certificate: CertificateEmpty{},
					DataHash:    bytes.Repeat([]byte{0x44}, 32),
					DataSize:    uint32(size * 2),
					Part:        bytes.Repeat([]byte{0x45}, size),
					Date:        uint32(time.Now().Unix()),
				}
				if err := part.Sign(key); err != nil {
					b.Fatal(err)
				}
				if err := wrapper.processBroadcastTwoStepFEC(part, part.SourceADNL); err != nil {
					b.Fatal(err)
				}
				if !cached {
					id, err := part.CalcID()
					if err != nil {
						b.Fatal(err)
					}
					state := receiver.activeTwoStepState()
					state.mx.Lock()
					stream := state.streams[newBroadcastTwoStepIDKey(id)]
					state.mx.Unlock()
					stream.mx.Lock()
					// Preserve membership and decoder state while forcing the full
					// verification path, as before verified digests were retained.
					stream.seenParts[part.Seqno] = [32]byte{}
					stream.mx.Unlock()
				}
				b.ReportAllocs()
				for b.Loop() {
					if err := wrapper.processBroadcastTwoStepFEC(part, part.SourceADNL); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
