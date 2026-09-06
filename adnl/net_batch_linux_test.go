//go:build linux

package adnl

import (
	"bytes"
	"errors"
	"net"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func listenBatchTestUDP(t *testing.T, network, address string) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenPacket(network, address)
	if err != nil {
		if network == "udp6" && (errors.Is(err, unix.EAFNOSUPPORT) || errors.Is(err, unix.EADDRNOTAVAIL)) {
			t.Skipf("IPv6 is unavailable: %v", err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return conn.(*net.UDPConn)
}

func batchTestReceiveSlots(count, size int) []*UDPPacket {
	packets := make([]*UDPPacket, count)
	for i := range packets {
		packets[i] = &UDPPacket{data: make([]byte, size)}
	}
	return packets
}

func TestUDPPacketBatchRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		network string
		address string
	}{
		{network: "udp4", address: "127.0.0.1:0"},
		{network: "udp6", address: "[::1]:0"},
	} {
		t.Run(tc.network, func(t *testing.T) {
			sender := listenBatchTestUDP(t, tc.network, tc.address)
			receiver := listenBatchTestUDP(t, tc.network, tc.address)
			tx := newPacketBatchConn(sender).(*udpPacketBatchConn)
			rx := newPacketBatchConn(receiver).(*udpPacketBatchConn)
			packets := make([]syncPacket, 12)
			for i := range packets {
				packets[i] = syncPacket{buf: bytes.Repeat([]byte{byte(i)}, 100), addr: receiver.LocalAddr()}
			}
			for sent := 0; sent < len(packets); {
				n, err := tx.writeBatch(packets[sent:])
				if err != nil || n == 0 {
					t.Fatalf("batch send: %d, %v", n, err)
				}
				sent += n
			}

			slots := batchTestReceiveSlots(udpBatchSize, 2048)
			for received := 0; received < len(packets); {
				n, err := rx.readBatch(slots)
				if err != nil || n == 0 {
					t.Fatalf("batch receive: %d, %v", n, err)
				}
				for _, p := range slots[:n] {
					if received >= len(packets) || !bytes.Equal(p.data[:p.n], packets[received].buf) {
						t.Fatalf("unexpected packet %d: %x", received, p.data[:p.n])
					}
					if p.from.String() != sender.LocalAddr().String() {
						t.Fatalf("source %v, want %v", p.from, sender.LocalAddr())
					}
					received++
				}
			}
			for i := range udpBatchSize {
				if tx.txBuffers[i][0] != nil || tx.tx[i].Addr != nil || rx.rxBuffers[i][0] != nil || rx.rx[i].Addr != nil {
					t.Fatal("batch descriptors retained packet storage")
				}
			}
		})
	}
}

func TestUDPPacketBatchDualStack(t *testing.T) {
	dual := listenBatchTestUDP(t, "udp", ":0")
	if dual.LocalAddr().(*net.UDPAddr).IP.To4() != nil {
		t.Skip("IPv6 dual-stack socket is unavailable")
	}
	v4 := listenBatchTestUDP(t, "udp4", "127.0.0.1:0")
	v6 := listenBatchTestUDP(t, "udp6", "[::1]:0")
	batch := newPacketBatchConn(dual)

	packets := []syncPacket{
		{buf: []byte{4}, addr: v4.LocalAddr()},
		{buf: []byte{6}, addr: v6.LocalAddr()},
	}
	for sent := 0; sent < len(packets); {
		n, err := batch.writeBatch(packets[sent:])
		if err != nil || n == 0 {
			t.Fatalf("dual-stack write: %d, %v", n, err)
		}
		sent += n
	}

	for i, conn := range []*net.UDPConn{v4, v6} {
		var data [8]byte
		n, from, err := conn.ReadFrom(data[:])
		if err != nil || n != 1 || data[0] != packets[i].buf[0] {
			t.Fatalf("dual-stack payload: %x, %v", data[:n], err)
		}
		if _, err := conn.WriteTo(packets[i].buf, from); err != nil {
			t.Fatal(err)
		}
	}

	slots := batchTestReceiveSlots(udpBatchSize, 100)
	received := map[byte]bool{}
	for len(received) < 2 {
		n, err := batch.readBatch(slots)
		if err != nil || n == 0 {
			t.Fatalf("dual-stack receive: %d, %v", n, err)
		}
		for _, p := range slots[:n] {
			if p.n != 1 {
				t.Fatalf("unexpected dual-stack packet size: %d", p.n)
			}
			received[p.data[0]] = true
		}
	}
	if !received[4] || !received[6] {
		t.Fatalf("missing address family: %v", received)
	}
}

func TestUDPPacketBatchTruncatedAndErrors(t *testing.T) {
	sender := listenBatchTestUDP(t, "udp4", "127.0.0.1:0")
	receiver := listenBatchTestUDP(t, "udp4", "127.0.0.1:0")
	rx := newPacketBatchConn(receiver)
	if _, err := sender.WriteTo(make([]byte, 200), receiver.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	slots := batchTestReceiveSlots(1, 64)
	if n, err := rx.readBatch(slots); n != 1 || err != nil || slots[0].n != 0 {
		t.Fatalf("truncated datagram was accepted: count=%d, size=%d, err=%v", n, slots[0].n, err)
	}

	if err := receiver.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if n, err := rx.readBatch(slots); n != 0 || err == nil {
		t.Fatalf("read deadline returned count=%d, err=%v", n, err)
	}

	tx := newPacketBatchConn(sender)
	if n, err := tx.writeBatch([]syncPacket{{buf: []byte{1}}}); n != 0 || err == nil {
		t.Fatalf("missing destination returned count=%d, err=%v", n, err)
	}
	if err := sender.Close(); err != nil {
		t.Fatal(err)
	}
	if n, err := tx.writeBatch([]syncPacket{{buf: []byte{1}, addr: receiver.LocalAddr()}}); n != 0 || !errors.Is(err, net.ErrClosed) {
		t.Fatalf("closed socket returned count=%d, err=%v", n, err)
	}
}
