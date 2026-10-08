package overlay

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/xssnick/tonutils-go/tl"
)

type benchmarkRelayLegacyPeer struct{ id []byte }

func (p benchmarkRelayLegacyPeer) ID() []byte { return p.id }

func (benchmarkRelayLegacyPeer) SendCustomMessage(_ context.Context, msg tl.Serializable) error {
	body, err := tl.Serialize(msg, true)
	runtime.KeepAlive(body)
	return err
}

type benchmarkRelayPreparedPeer struct{ benchmarkRelayLegacyPeer }

func (benchmarkRelayPreparedPeer) SendPreparedCustomMessage(_ context.Context, body []byte) error {
	runtime.KeepAlive(body)
	return nil
}

func BenchmarkBroadcastFECRelayOpAccumulation(b *testing.B) {
	peers := make([]BroadcastPeer, 15)
	for index := range peers {
		peers[index] = benchmarkRelayPreparedPeer{benchmarkRelayLegacyPeer{id: bytes.Repeat([]byte{byte(index + 1)}, 32)}}
	}
	for _, count := range []int{64, 1024} {
		stream := &fecBroadcastStream{parts: make(map[uint32]broadcastFECRelayPart, count)}
		for sequence := range count {
			stream.parts[uint32(sequence)] = broadcastFECRelayPart{full: &BroadcastFEC{Seqno: uint32(sequence)}}
		}
		for _, accumulated := range []bool{false, true} {
			name := "temporary_slices"
			if accumulated {
				name = "accumulator"
			}
			b.Run(fmt.Sprintf("parts_%d/%s", count, name), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					ops := make([]broadcastFECRelayOp, 0, count*len(peers))
					for sequence := range stream.parts {
						if accumulated {
							ops = stream.relayPartOpsLocked(ops, sequence, peers, nil, false)
						} else {
							partOps := make([]broadcastFECRelayOp, 0, len(peers))
							ops = append(ops, stream.relayPartOpsLocked(partOps, sequence, peers, nil, false)...)
						}
					}
					if len(ops) != count*len(peers) {
						b.Fatal("lost relay operations")
					}
				}
			})
		}
	}
}

func TestBroadcastRelayChargesPreparedFrame(t *testing.T) {
	body, err := PrepareBroadcastMessage(preparedTestBroadcastFEC())
	if err != nil {
		t.Fatal(err)
	}
	frame, err := body.ADNLMessage(bytes.Repeat([]byte{0x71}, 32))
	if err != nil {
		t.Fatal(err)
	}
	want := int64(cap(body.Body()) + cap(frame.Wire()) + 32)
	dispatcher := newBroadcastTwoStepRelayDispatcher(1, 1, want-1, DefaultTwoStepRelayPeerTimeout)
	t.Cleanup(dispatcher.Close)
	if _, ok := dispatcher.reservePayload(nil, body, 1); ok {
		t.Fatal("frame backing allocation was not charged")
	}
	dispatcher.maxActiveBytes = want
	payload, ok := dispatcher.reservePayload(nil, body, 1)
	if !ok || dispatcher.activeBytes.Load() != want {
		t.Fatalf("retained frame budget = %d, want %d", dispatcher.activeBytes.Load(), want)
	}
	dispatcher.releasePayload(payload)
}
