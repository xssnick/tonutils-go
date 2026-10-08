package adnl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/tl"
)

func TestADNLQueryResponseTypes(t *testing.T) {
	for _, test := range []struct {
		name     string
		response any
		wantErr  bool
	}{
		{name: "expected", response: MessagePong{Value: 123}},
		{name: "unexpected", response: MessageNop{}, wantErr: true},
		{name: "empty", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, _ := testADNLWithPeer(t)
			a.writer = packetWriterFunc(func(packet []byte, _ time.Time) (int, error) {
				a.queryMx.Lock()
				for _, response := range a.activeQueries {
					response <- test.response
				}
				a.queryMx.Unlock()
				return len(packet), nil
			})

			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("Query panicked for response %T: %v", test.response, recovered)
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var result MessagePong
			err := a.Query(ctx, MessagePing{}, &result)
			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), "response type") {
					t.Fatalf("unexpected response returned %v", err)
				}
			} else if err != nil || result.Value != 123 {
				t.Fatalf("Query returned %+v, %v", result, err)
			}
		})
	}

	t.Run("interface", func(t *testing.T) {
		a, _ := testADNLWithPeer(t)
		a.writer = packetWriterFunc(func(packet []byte, _ time.Time) (int, error) {
			a.queryMx.Lock()
			for _, response := range a.activeQueries {
				response <- MessagePong{Value: 123}
			}
			a.queryMx.Unlock()
			return len(packet), nil
		})

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var result tl.Serializable
		if err := a.Query(ctx, MessagePing{}, &result); err != nil {
			t.Fatal(err)
		}
		if pong, ok := result.(MessagePong); !ok || pong.Value != 123 {
			t.Fatalf("unexpected interface response: %+v", result)
		}
	})
}

func TestADNLMultipartPreflightsMTU(t *testing.T) {
	p := newPreparedSendPeer(t, true)
	// A previous small header can underestimate the next packet's overhead.
	atomic.StoreUint32(&p.adnl.prevPacketHeaderSz, 1)
	packet, packets, err := p.adnl.buildRequestMaySplit(&MessageCustom{Data: TestMsg{Data: make([]byte, 2700)}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if packet != nil || len(packets) == 0 {
		t.Fatal("expected multipart packets")
	}
	for i, part := range packets {
		if len(part) > MaxMTU {
			t.Fatalf("packet %d exceeds MTU: %d > %d", i, len(part), MaxMTU)
		}
	}
	if len(p.writer.packets) != 0 {
		t.Fatal("packet preparation wrote to the transport")
	}
}

func TestADNLExpandedMultipartMTUBudgetsPadding(t *testing.T) {
	p := newPreparedSendPeer(t, true)
	for _, previousHeaderSize := range []uint32{40, 48, 56} {
		atomic.StoreUint32(&p.adnl.prevPacketHeaderSz, previousHeaderSize)
		if err := p.adnl.SendCustomMessage(context.Background(), TestMsg{Data: make([]byte, 3000)}); err != nil {
			t.Fatal(err)
		}
		_, packets := p.decodeSent(t)
		var offset int32
		for i, messages := range packets {
			part, ok := messages[0].(MessagePart)
			if !ok || len(messages) != 1 {
				t.Fatalf("unexpected multipart messages: %v", messages)
			}
			if part.Offset != offset {
				t.Fatalf("part %d starts at %d, want %d", i, part.Offset, offset)
			}
			if i == 0 && len(part.Data) <= BasePayloadMTU {
				t.Fatalf("expanded MTU rebuilt to base payload: %d", len(part.Data))
			}
			offset += int32(len(part.Data))

			wire, err := tl.Serialize(part, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, rand1Size := range []int{7, 15} {
				for _, rand2Size := range []int{7, 15} {
					buf := bytes.NewBuffer(make([]byte, 64))
					serializeChannelPacket(buf, make([]byte, rand1Size), make([]byte, rand2Size), 1, 0, wire)
					if buf.Len() > MaxMTU {
						t.Fatalf("previous header %d, part %d, padding %d/%d: packet size %d > %d",
							previousHeaderSize, i, rand1Size, rand2Size, buf.Len(), MaxMTU)
					}
				}
			}
		}
	}
}

func TestADNLDoesNotResplitAfterTransportMTUError(t *testing.T) {
	for _, operation := range []string{"custom", "prepared", "query", "answer"} {
		t.Run(operation, func(t *testing.T) {
			peer := newPreparedSendPeer(t, true)
			var attempts int
			peer.adnl.writer = packetWriterFunc(func(packet []byte, _ time.Time) (int, error) {
				attempts++
				if attempts == 2 {
					return 0, ErrPacketBiggerThanMTU
				}
				return len(packet), nil
			})
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			message := TestMsg{Data: make([]byte, 3000)}

			var err error
			switch operation {
			case "custom":
				err = peer.adnl.SendCustomMessage(ctx, message)
			case "prepared":
				var prepared *PreparedCustomMessage
				prepared, err = PrepareCustomMessage(message)
				if err == nil {
					err = peer.adnl.SendPreparedCustomMessage(ctx, prepared)
				}
			case "query":
				var result tl.Serializable
				err = peer.adnl.Query(ctx, message, &result)
			case "answer":
				err = peer.adnl.Answer(ctx, make([]byte, 32), message)
			}
			if !errors.Is(err, ErrPacketBiggerThanMTU) || attempts != 2 {
				t.Fatalf("transport MTU error triggered more writes: attempts=%d err=%v", attempts, err)
			}
		})
	}
}

func TestPartitionedMessageOverlappingParts(t *testing.T) {
	data := []byte("0123456789abcdefghij")
	hash := sha256.Sum256(data)
	for _, test := range []struct {
		name  string
		parts [][2]int
	}{
		{name: "overlap", parts: [][2]int{{0, 12}, {8, 20}}},
		{name: "same offset grows", parts: [][2]int{{0, 8}, {0, 12}, {12, 20}}},
		{name: "contained duplicate", parts: [][2]int{{5, 15}, {7, 12}, {0, 8}, {12, 20}}},
		{name: "bridges ranges", parts: [][2]int{{0, 5}, {15, 20}, {4, 16}}},
		{name: "reverse order", parts: [][2]int{{8, 20}, {0, 12}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			message := newPartitionedMessage(int32(len(data)))
			for i, part := range test.parts {
				ready, err := message.AddPart(int32(part[0]), data[part[0]:part[1]])
				if err != nil {
					t.Fatal(err)
				}
				if ready != (i == len(test.parts)-1) {
					t.Fatalf("part %d: ready=%v, covered=%d", i, ready, message.gotLen)
				}
			}
			built, err := message.Build(hash[:])
			if err != nil || !bytes.Equal(built, data) {
				t.Fatalf("Build returned %q, %v", built, err)
			}
			if ready, err := message.AddPart(0, data); ready || err != nil {
				t.Fatalf("completed duplicate: ready=%v err=%v", ready, err)
			}
		})
	}
}

func TestADNLPacketReplayWindow(t *testing.T) {
	a, _ := testADNLWithPeer(t)
	a.writer = &capturePacketWriter{}
	var delivered []int64
	a.SetCustomMessageHandler(func(message *MessageCustom) error {
		delivered = append(delivered, message.Data.(MessagePong).Value)
		return nil
	})

	send := func(seqno int64, epoch int32) {
		t.Helper()
		if err := a.processPacket(&PacketContent{
			Seqno:      &seqno,
			ReinitDate: &epoch,
			Messages:   []any{MessageCustom{Data: MessagePong{Value: seqno}}},
		}, false); err != nil {
			t.Fatal(err)
		}
	}

	const epoch = int32(100)
	for _, seqno := range []int64{10, 10, 8, 8, 9, 20, 19, 9, 1050, 25, 26, 27, 27, 1049, 1049, 1} {
		send(seqno, epoch)
	}
	want := []int64{10, 8, 9, 20, 19, 1050, 27, 1049}
	if len(delivered) != len(want) {
		t.Fatalf("delivered seqnos %v, want %v", delivered, want)
	}
	for i, value := range want {
		if delivered[i] != value {
			t.Fatalf("delivered seqnos %v, want %v", delivered, want)
		}
	}

	send(1, epoch+1)
	send(1, epoch+1)
	send(1051, epoch)
	if len(delivered) != len(want)+1 || delivered[len(want)] != 1 {
		t.Fatalf("peer reinit delivered seqnos %v", delivered)
	}
	if confirmed := atomic.LoadInt64(&a.confirmSeqno); confirmed != 1 {
		t.Fatalf("confirm seqno=%d, want 1", confirmed)
	}
}

func TestADNLPacketReplayWindowAcrossWraps(t *testing.T) {
	a, _ := testADNLWithPeer(t)
	atomic.StoreInt64(&a.respondWithNopAfter, int64(^uint64(0)>>1))
	var delivered bool
	a.SetCustomMessageHandler(func(*MessageCustom) error {
		delivered = true
		return nil
	})

	seen := make(map[int64]bool)
	var highest int64
	for i := range 5000 {
		seqno := highest + 1
		if i%7 == 0 {
			seqno = highest - int64((i*37)%1100)
		}
		if i%31 == 0 {
			seqno = highest + int64((i*53)%2000) + 1
		}
		highest = max(highest, seqno)
		want := seqno > 0 && highest-seqno < 1024 && !seen[seqno]
		delivered = false
		if err := a.processPacket(&PacketContent{
			Seqno:    &seqno,
			Messages: []any{MessageCustom{Data: MessageNop{}}},
		}, true); err != nil {
			t.Fatal(err)
		}
		if delivered != want {
			t.Fatalf("packet %d: seqno=%d highest=%d delivered=%v want=%v", i, seqno, highest, delivered, want)
		}
		if delivered {
			seen[seqno] = true
		}
	}
}

func TestADNLSetAddressesPreservesReinit(t *testing.T) {
	fixture := newChannelLifecycleFixture(t)
	a := fixture.adnl
	a.Reinit()
	a.Reinit()
	epoch := atomic.LoadInt32(&a.reinitTime)

	fixture.gateway.SetAddressList(nil)
	if got := atomic.LoadInt32(&a.reinitTime); got != epoch {
		t.Fatalf("address update changed reinit epoch: %d -> %d", epoch, got)
	}
	if list := a.GetAddressList(); list.ReinitDate != epoch || list.Version < epoch {
		t.Fatalf("address list is behind the packet epoch: %+v, epoch=%d", list, epoch)
	}
}

func TestGatewayAddressUpdateAfterReinitRefreshesAcknowledgedVersion(t *testing.T) {
	initial, peerPriv := testADNLWithPeer(t)
	gateway := NewGateway(initial.ourKey)
	gateway.SetAddressList(nil)
	peer, err := gateway.RegisterClient("127.0.0.1:30303", initial.peerKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gateway.Close() })
	a := peer.(*peerConn).client.(*ADNL)
	a.Reinit()
	acknowledged := a.GetAddressList()
	atomic.StoreInt32(&a.ourAddrVerOnPeerSide, acknowledged.Version)

	before, err := a.createPacket(1, MessageNop{})
	if err != nil {
		t.Fatal(err)
	}
	if packet := parseRootPacketFromBytes(t, peerPriv, before); packet.Address != nil {
		t.Fatal("already acknowledged address list was included in the packet")
	}

	addr, err := address.NewAddress(net.ParseIP("127.0.0.2"), 30304)
	if err != nil {
		t.Fatal(err)
	}
	gateway.SetAddressList([]address.Address{addr})
	updated := a.GetAddressList()
	if updated.ReinitDate != acknowledged.ReinitDate || updated.Version <= acknowledged.Version {
		t.Fatalf("address update did not retain epoch and advance Version: before=%+v after=%+v", acknowledged, updated)
	}

	after, err := a.createPacket(2, MessageNop{})
	if err != nil {
		t.Fatal(err)
	}
	packet := parseRootPacketFromBytes(t, peerPriv, after)
	if packet.Address == nil || packet.Address.Version != updated.Version || len(packet.Address.Addresses) != 1 {
		t.Fatalf("updated address list was not advertised: %+v", packet.Address)
	}
	if got := packet.Address.Addresses[0]; !address.IPValue(got).Equal(address.IPValue(addr)) || address.PortValue(got) != 30304 {
		t.Fatalf("packet advertised the wrong address: %+v", got)
	}

	forward := updated.Version + 10
	a.SetAddresses(address.List{Version: forward, ReinitDate: acknowledged.ReinitDate - 1})
	if list := a.GetAddressList(); list.Version != forward || list.ReinitDate != acknowledged.ReinitDate {
		t.Fatalf("explicit newer Version was changed: %+v, want Version=%d", list, forward)
	}
}

func TestADNLSetAddressesConcurrentReinit(t *testing.T) {
	a, _ := testADNLWithPeer(t)
	initial := atomic.LoadInt32(&a.reinitTime)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 100 {
			a.Reinit()
		}
	}()
	go func() {
		defer wg.Done()
		for range 100 {
			a.SetAddresses(address.List{Version: initial, ReinitDate: initial})
		}
	}()
	wg.Wait()

	epoch := atomic.LoadInt32(&a.reinitTime)
	if epoch < initial+100 {
		t.Fatalf("reinit epoch went backwards: initial=%d final=%d", initial, epoch)
	}
	if list := a.GetAddressList(); list.ReinitDate != epoch || list.Version < epoch {
		t.Fatalf("address list is inconsistent with reinit epoch: %+v, epoch=%d", list, epoch)
	}
}

func TestGatewayCloseAfterBindFailure(t *testing.T) {
	_, key := testADNLWithPeer(t)
	bindErr := errors.New("bind failed")
	manager := NewSingleNetReader(func(string) (net.PacketConn, error) {
		return nil, bindErr
	})
	gateway := NewGatewayWithNetManager(key, manager)
	if err := gateway.StartServer("127.0.0.1:12345"); !errors.Is(err, bindErr) {
		t.Fatalf("StartServer returned %v", err)
	}
	if err := gateway.Close(); err != nil {
		t.Fatal(err)
	}
	manager.Close()
	if _, err := manager.WritePacket(gateway, nil, &net.UDPAddr{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write after failed bind and close returned %v", err)
	}
}

func BenchmarkADNLProcessInboundPacket(b *testing.B) {
	a, _, _ := packetBuildPeer(b)
	atomic.StoreInt64(&a.respondWithNopAfter, int64(^uint64(0)>>1))
	a.SetCustomMessageHandler(func(*MessageCustom) error { return nil })
	var seqno int64
	packet := &PacketContent{
		Seqno:    &seqno,
		Messages: []any{MessageCustom{Data: TestMsg{Data: make([]byte, 1024)}}},
	}
	b.ReportAllocs()
	for b.Loop() {
		seqno++
		if err := a.processPacket(packet, true); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPartitionedMessageOrdered(b *testing.B) {
	data := make([]byte, 8192)
	parts := splitMessage(data, 1024)
	b.ReportAllocs()
	for b.Loop() {
		message := newPartitionedMessage(int32(len(data)))
		for _, part := range parts {
			if _, err := message.AddPart(part.Offset, part.Data); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkSendCustomMessageMultipartBuild(b *testing.B) {
	a, ch, _ := packetBuildPeer(b)
	atomic.StorePointer(&a.channelPtr, unsafe.Pointer(ch))
	message := &MessageCustom{Data: tl.Raw(make([]byte, 3000))}
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := a.buildRequestMaySplit(message, false); err != nil {
			b.Fatal(err)
		}
	}
}
