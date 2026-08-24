package overlay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type serialContractBroadcastSigner struct {
	key ed25519.PrivateKey

	calls      atomic.Int32
	active     atomic.Int32
	first      chan struct{}
	concurrent chan struct{}
	release    chan struct{}
	once       sync.Once
}

func (s *serialContractBroadcastSigner) PublicKey() ed25519.PublicKey {
	return s.key.Public().(ed25519.PublicKey)
}

func (s *serialContractBroadcastSigner) Sign(payload []byte) ([]byte, error) {
	if s.active.Add(1) > 1 {
		s.once.Do(func() { close(s.concurrent) })
	}
	defer s.active.Add(-1)

	if s.calls.Add(1) == 1 {
		close(s.first)
		<-s.release
	}

	return ed25519.Sign(s.key, payload), nil
}

func TestSendBroadcastTwoStepSerializesSignerCalls(t *testing.T) {
	_, privateKey := keyPairFromSeed(99)
	signer := &serialContractBroadcastSigner{
		key:        privateKey,
		first:      make(chan struct{}),
		concurrent: make(chan struct{}),
		release:    make(chan struct{}),
	}
	peers := mockBroadcastPeerSet{peers: make([]BroadcastPeer, 7)}
	for i := range peers.peers {
		peers.peers[i] = &mockBroadcastPeer{id: bytes.Repeat([]byte{byte(0xA0 + i)}, 32)}
	}

	done := make(chan error, 1)
	go func() {
		_, err := SendBroadcastTwoStep(context.Background(), BroadcastTwoStepSendRequest{
			Signer:      signer,
			Certificate: CertificateEmpty{},
			LocalADNLID: bytes.Repeat([]byte{0x9F}, 32),
			Payload:     bytes.Repeat([]byte{0xEC}, 4096),
			PeerSet:     peers,
		},
			WithBroadcastTwoStepDate(240),
			WithBroadcastTwoStepSendConcurrency(4),
		)
		done <- err
	}()

	select {
	case <-signer.first:
	case <-time.After(time.Second):
		t.Fatal("first signer call did not start")
	}
	select {
	case <-signer.concurrent:
		close(signer.release)
		t.Fatal("BroadcastSigner.Sign was called concurrently")
	case <-time.After(50 * time.Millisecond):
	}

	close(signer.release)
	if err := <-done; err != nil {
		t.Fatalf("two-step send failed: %v", err)
	}
	if got := signer.calls.Load(); got != int32(len(peers.peers)) {
		t.Fatalf("signer calls = %d, want %d", got, len(peers.peers))
	}
}
