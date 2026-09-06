package adnl

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

type batchTestPacketConn struct {
	closed    chan struct{}
	closeOnce sync.Once
	read      func([]byte) (int, net.Addr, error)
	write     func([]byte, net.Addr) (int, error)
}

func newBatchTestPacketConn() *batchTestPacketConn {
	return &batchTestPacketConn{closed: make(chan struct{})}
}

func (c *batchTestPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	if c.read != nil {
		return c.read(p)
	}
	<-c.closed
	return 0, nil, net.ErrClosed
}

func (c *batchTestPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	if c.write != nil {
		return c.write(p, addr)
	}
	return len(p), nil
}

func (c *batchTestPacketConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (*batchTestPacketConn) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}
}

func (*batchTestPacketConn) SetDeadline(time.Time) error      { return nil }
func (*batchTestPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (*batchTestPacketConn) SetWriteDeadline(time.Time) error { return nil }

type batchTestConn struct {
	read  func([]*UDPPacket) (int, error)
	write func([]syncPacket) (int, error)
}

func (b *batchTestConn) readBatch(packets []*UDPPacket) (int, error) {
	return b.read(packets)
}

func (b *batchTestConn) writeBatch(packets []syncPacket) (int, error) {
	return b.write(packets)
}

func newBatchTestSyncConn(conn net.PacketConn, batch packetBatchConn, size int) *SyncConn {
	ctx, cancel := context.WithCancel(context.Background())
	return &SyncConn{
		conn:      conn,
		batch:     batch,
		chWrite:   make(chan syncPacket, size),
		closerCtx: ctx,
		closer:    cancel,
		writeDone: make(chan struct{}),
	}
}

func waitBatchTestDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("packet worker did not finish")
	}
}

func TestSyncConnBatchesAvailablePackets(t *testing.T) {
	for _, count := range []int{1, udpBatchSize + 3} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			conn := newBatchTestPacketConn()
			var sizes []int
			var payloads []byte
			var views [][]syncPacket
			written := make(chan struct{})
			batch := &batchTestConn{write: func(packets []syncPacket) (int, error) {
				sizes = append(sizes, len(packets))
				views = append(views, packets)
				for _, p := range packets {
					payloads = append(payloads, p.buf[0])
				}
				if len(payloads) == count {
					close(written)
				}
				return len(packets), nil
			}}
			s := newBatchTestSyncConn(conn, batch, count)
			for i := range count {
				if _, err := s.WriteTo([]byte{byte(i)}, conn.LocalAddr()); err != nil {
					t.Fatal(err)
				}
			}
			go s.writer()
			t.Cleanup(func() { _ = s.Close() })

			waitBatchTestDone(t, written)
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			for i, value := range payloads {
				if value != byte(i) {
					t.Fatalf("packet %d contains %d", i, value)
				}
			}
			if count == 1 && (len(sizes) != 1 || sizes[0] != 1) {
				t.Fatalf("sparse packet batches: %v", sizes)
			}
			if count > udpBatchSize && (len(sizes) != 2 || sizes[0] != udpBatchSize || sizes[1] != 3) {
				t.Fatalf("queued packet batches: %v", sizes)
			}
			for _, view := range views {
				for _, p := range view {
					if p.buf != nil || p.addr != nil {
						t.Fatal("writer retained completed batch storage")
					}
				}
			}
		})
	}
}

func TestSyncConnBatchPartialWrite(t *testing.T) {
	conn := newBatchTestPacketConn()
	var attempts [][]byte
	finished := make(chan struct{})
	batch := &batchTestConn{write: func(packets []syncPacket) (int, error) {
		values := make([]byte, len(packets))
		for i, p := range packets {
			values[i] = p.buf[0]
		}
		attempts = append(attempts, values)
		switch len(attempts) {
		case 1:
			return 2, nil
		case 2:
			return 0, errors.New("invalid destination")
		default:
			close(finished)
			return len(packets), nil
		}
	}}
	s := newBatchTestSyncConn(conn, batch, 4)
	for i := range 4 {
		_, _ = s.WriteTo([]byte{byte(i)}, conn.LocalAddr())
	}
	go s.writer()
	t.Cleanup(func() { _ = s.Close() })

	waitBatchTestDone(t, finished)
	_ = s.Close()
	want := [][]byte{{0, 1, 2, 3}, {2, 3}, {3}}
	if len(attempts) != len(want) {
		t.Fatalf("write attempts: %v", attempts)
	}
	for i := range want {
		if !bytes.Equal(attempts[i], want[i]) {
			t.Fatalf("write attempts: %v", attempts)
		}
	}
}

func TestSyncConnBatchCloseDropsPending(t *testing.T) {
	conn := newBatchTestPacketConn()
	attempts := 0
	batch := &batchTestConn{write: func(packets []syncPacket) (int, error) {
		attempts++
		return 0, net.ErrClosed
	}}
	s := newBatchTestSyncConn(conn, batch, udpBatchSize+4)
	for range udpBatchSize + 4 {
		_, _ = s.WriteTo(make([]byte, 100), conn.LocalAddr())
	}
	go s.writer()
	t.Cleanup(func() { _ = s.Close() })

	waitBatchTestDone(t, s.writeDone)
	if attempts != 1 || len(s.chWrite) != 0 {
		t.Fatalf("writes=%d, pending=%d after socket close", attempts, len(s.chWrite))
	}
	if _, err := s.WriteTo([]byte{1}, conn.LocalAddr()); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("write after close: %v", err)
	}
}

func TestSyncConnCloseUnblocksEnqueue(t *testing.T) {
	conn := newBatchTestPacketConn()
	started := make(chan struct{})
	conn.write = func(p []byte, addr net.Addr) (int, error) {
		close(started)
		<-conn.closed
		return 0, net.ErrClosed
	}
	s := NewSyncConn(conn, 1)
	t.Cleanup(func() { _ = s.Close() })
	if s.batch != nil {
		t.Fatal("custom PacketConn should use its WriteTo method")
	}
	_, _ = s.WriteTo([]byte{1}, conn.LocalAddr())
	waitBatchTestDone(t, started)
	_, _ = s.WriteTo([]byte{2}, conn.LocalAddr())

	result := make(chan error, 1)
	go func() {
		_, err := s.WriteTo([]byte{3}, conn.LocalAddr())
		result <- err
	}()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, net.ErrClosed) {
		t.Fatalf("blocked enqueue returned %v", err)
	}
	if len(s.chWrite) != 0 {
		t.Fatal("closed writer retained queued packets")
	}
	for range 100 {
		if _, err := s.WriteTo([]byte{4}, conn.LocalAddr()); !errors.Is(err, net.ErrClosed) {
			t.Fatalf("write after close returned %v", err)
		}
	}
}

func TestSyncConnNestedKeepsInnerWriter(t *testing.T) {
	conn := newBatchTestPacketConn()
	written := make(chan struct{})
	var received []byte
	batch := &batchTestConn{write: func(packets []syncPacket) (int, error) {
		for _, p := range packets {
			received = append(received, p.buf[0])
		}
		if len(received) == 2 {
			close(written)
		}
		return len(packets), nil
	}}
	inner := newBatchTestSyncConn(conn, batch, 2)
	go inner.writer()
	t.Cleanup(func() { _ = inner.Close() })
	outer := NewSyncConn(inner, 2)
	t.Cleanup(func() { _ = outer.Close() })
	if outer.batch != nil {
		t.Fatal("outer writer shares the inner writer's batch descriptors")
	}

	if _, err := outer.WriteTo([]byte{1}, conn.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if _, err := inner.WriteTo([]byte{2}, conn.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	waitBatchTestDone(t, written)
	_ = outer.Close()
	if len(received) != 2 || received[0]+received[1] != 3 {
		t.Fatalf("nested writer payloads: %v", received)
	}
}

func TestReadUDPPacketsBatchOwnership(t *testing.T) {
	pool := sync.Pool{New: func() any { return &UDPPacket{data: make([]byte, 2048)} }}
	var published []*UDPPacket
	var firstSlots []*UDPPacket
	reads := 0
	batch := &batchTestConn{read: func(packets []*UDPPacket) (int, error) {
		reads++
		if reads == 1 {
			firstSlots = append(firstSlots, packets...)
			for i := range 3 {
				packets[i].data[0] = byte(i + 1)
				packets[i].n = 64
			}
			packets[1].n = 63
			return 3, nil
		}
		if packets[0] == firstSlots[0] {
			t.Fatal("refilling a buffer still owned by the receiver")
		}
		if packets[1] != firstSlots[1] || packets[2] != firstSlots[2] {
			t.Fatal("discarded receive slots were not reused")
		}
		packets[0].data[0] = 4
		packets[0].n = 64
		return 1, net.ErrClosed
	}}

	readUDPPackets(context.Background(), nil, batch, &pool, func(p *UDPPacket) bool {
		if p.data[0] == 3 {
			return false
		}
		published = append(published, p)
		return true
	})
	if len(published) != 2 || published[0].data[0] != 1 || published[1].data[0] != 4 {
		t.Fatalf("received packets were overwritten or lost: %v", published)
	}
	for _, p := range published {
		pool.Put(p)
	}
}

func TestSingleNetManagerCustomPacketConn(t *testing.T) {
	conn := newBatchTestPacketConn()
	read := false
	conn.read = func(p []byte) (int, net.Addr, error) {
		if read {
			<-conn.closed
			return 0, nil, net.ErrClosed
		}
		read = true
		return copy(p, bytes.Repeat([]byte{0xAB}, 64)), conn.LocalAddr(), nil
	}
	m := NewSingleNetReader(func(string) (net.PacketConn, error) { return conn, nil })
	if err := m.InitConnection(nil, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)

	select {
	case p := <-m.GetReaderChan(nil):
		if p.n != 64 || p.data[0] != 0xAB || p.from.String() != conn.LocalAddr().String() {
			t.Fatal("custom PacketConn packet was changed")
		}
		m.Free(p)
	case <-time.After(5 * time.Second):
		t.Fatal("custom PacketConn did not deliver its packet")
	}
}
