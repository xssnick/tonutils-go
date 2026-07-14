package adnl

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestPeerStatsTracksTrafficSequencesAndLifecycle(t *testing.T) {
	a, _ := testADNLWithPeer(t)
	writer := &capturePacketWriter{}
	a.writer = writer

	initial := a.Stats()
	if initial.CreatedAt.IsZero() || initial.CapturedAt.Before(initial.CreatedAt) {
		t.Fatalf("invalid initial timestamps: created=%v captured=%v", initial.CreatedAt, initial.CapturedAt)
	}
	if initial.Channel.State != PeerChannelStateNone {
		t.Fatalf("initial channel state = %d, want none", initial.Channel.State)
	}

	if err := a.SendNop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(writer.packets) != 1 {
		t.Fatalf("written packets = %d, want 1", len(writer.packets))
	}

	a.noteInboundPacket(321)
	peerSeqno := int64(7)
	peerConfirmSeqno := int64(1)
	peerReinit := int32(time.Now().Unix())
	if err := a.processPacket(&PacketContent{
		Messages:     []any{MessageNop{}},
		Seqno:        &peerSeqno,
		ConfirmSeqno: &peerConfirmSeqno,
		ReinitDate:   &peerReinit,
	}, false); err != nil {
		t.Fatal(err)
	}

	stats := a.Stats()
	if stats.Outbound.Packets != 1 || stats.Outbound.Bytes != uint64(len(writer.packets[0])) {
		t.Fatalf("unexpected outbound stats: %+v", stats.Outbound)
	}
	if stats.Inbound.Packets != 1 || stats.Inbound.Bytes != 321 || stats.Inbound.LastPacketAt.IsZero() {
		t.Fatalf("unexpected inbound stats: %+v", stats.Inbound)
	}
	if stats.Sequences.LocalSeqno != 1 || stats.Sequences.PeerSeqno != peerSeqno ||
		stats.Sequences.ConfirmedByPeerSeqno != peerConfirmSeqno {
		t.Fatalf("unexpected sequence stats: %+v", stats.Sequences)
	}
	if stats.Reinitialization.PeerEvents != 1 || stats.Reinitialization.LastPeerEventAt.IsZero() {
		t.Fatalf("unexpected reinitialization stats: %+v", stats.Reinitialization)
	}
	if stats.Channel.State != PeerChannelStatePending {
		t.Fatalf("channel state = %d, want pending", stats.Channel.State)
	}

	a.Reinit()
	stats = a.Stats()
	if stats.Sequences.LocalSeqno != 0 || stats.Sequences.PeerSeqno != 0 || stats.Sequences.ConfirmedByPeerSeqno != 0 {
		t.Fatalf("sequences were not reset by reinit: %+v", stats.Sequences)
	}
	if stats.Outbound.Packets != 1 || stats.Outbound.Bytes != uint64(len(writer.packets[0])) ||
		stats.Reinitialization.PeerEvents != 1 {
		t.Fatalf("cumulative counters were reset by reinit: %+v", stats)
	}

	a.Close()
	if stats = a.Stats(); !stats.Closed {
		t.Fatal("closed peer is reported as open")
	}
}

func TestPeerStatsTracksErrorsAndRootRecovery(t *testing.T) {
	a, _, ch := testADNLWithReadyChannel(t)
	a.writer = packetWriterFunc(func([]byte, time.Time) (int, error) {
		return 0, errors.New("i/o timeout")
	})

	if err := a.send([]byte{1}); err == nil {
		t.Fatal("expected write error")
	}
	a.noteInboundError(time.Now())

	now := time.Now()
	atomic.StoreInt64(&a.lastReceivedPacket, now.Add(-idleReinitSilence-time.Second).UnixNano())
	atomic.StoreInt64(&a.tryReinitAt, now.Add(-time.Second).UnixNano())
	forceRoot, _ := a.rootReinitOptions(now)
	if !forceRoot {
		t.Fatal("expected root recovery attempt")
	}

	stats := a.Stats()
	if stats.Channel.State != PeerChannelStateReady || !ch.ready.Load() {
		t.Fatalf("unexpected ready channel stats: %+v", stats.Channel)
	}
	if stats.Outbound.WriteErrors != 1 || stats.Outbound.LastErrorAt.IsZero() {
		t.Fatalf("unexpected outbound errors: %+v", stats.Outbound)
	}
	if stats.Inbound.ProcessingErrors != 1 || stats.Inbound.LastErrorAt.IsZero() {
		t.Fatalf("unexpected inbound errors: %+v", stats.Inbound)
	}
	if stats.Reinitialization.RootRecoveryAttempts != 1 || stats.Reinitialization.LastRootRecoveryAt.IsZero() {
		t.Fatalf("unexpected root recovery stats: %+v", stats.Reinitialization)
	}
}

type packetWriterFunc func([]byte, time.Time) (int, error)

func (f packetWriterFunc) Write(packet []byte, deadline time.Time) (int, error) {
	return f(packet, deadline)
}

func (f packetWriterFunc) Close() error {
	return nil
}
