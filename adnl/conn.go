package adnl

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

type clientConn struct {
	closed  bool
	closer  chan bool
	onClose func()
	writer  func(p []byte, deadline time.Time) (err error)
	mx      sync.Mutex
}

func newWriter(writer func(p []byte, deadline time.Time) (err error), close func()) *clientConn {
	return &clientConn{
		onClose: close,
		closer:  make(chan bool, 1),
		writer:  writer,
	}
}

var ErrPeerConnClosed = errors.New("peer connection was closed")

func (c *clientConn) Write(b []byte, deadline time.Time) (n int, err error) {
	select {
	case <-c.closer:
		return 0, ErrPeerConnClosed
	default:
	}

	if err = c.writer(b, deadline); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *clientConn) Close() error {
	c.mx.Lock()
	defer c.mx.Unlock()

	if !c.closed {
		c.closed = true
		close(c.closer)
		if h := c.onClose; h != nil {
			go h() // to not lock
		}
	}

	return nil
}

type syncPacket struct {
	addr net.Addr
	buf  []byte
}

type SyncConn struct {
	conn      net.PacketConn
	batch     packetBatchConn
	chWrite   chan syncPacket
	closerCtx context.Context
	closer    context.CancelFunc
	writeMx   sync.RWMutex
	closeOnce sync.Once
	closeErr  error
	writeDone chan struct{}
}

func NewSyncConn(conn net.PacketConn, packetsBufSz int) *SyncConn {
	ctx, cancel := context.WithCancel(context.Background())
	sc := &SyncConn{
		conn:      conn,
		chWrite:   make(chan syncPacket, packetsBufSz),
		closer:    cancel,
		closerCtx: ctx,
		writeDone: make(chan struct{}),
	}
	if _, ok := conn.(*net.UDPConn); ok {
		// Each writer owns its descriptors. In particular, wrapping another
		// SyncConn must preserve its queue instead of sharing its TX batch.
		sc.batch = newPacketBatchConn(conn)
	}
	go sc.writer()
	return sc
}

func (s *SyncConn) writer() {
	var slots [udpBatchSize]syncPacket
	defer func() {
		_ = s.closeConn()
		clear(slots[:])

		// Cancelled enqueues leave before taking the exclusive lock. This
		// makes draining final: no sender can retain a new buffer after exit.
		s.writeMx.Lock()
		defer s.writeMx.Unlock()
		defer close(s.writeDone)
		for {
			select {
			case <-s.chWrite:
			default:
				return
			}
		}
	}()

	for {
		if s.closerCtx.Err() != nil {
			return
		}

		select {
		case p := <-s.chWrite:
			slots[0] = p
		case <-s.closerCtx.Done():
			return
		}

		n := 1
		if s.batch != nil {
		drain:
			for n < len(slots) {
				select {
				case p := <-s.chWrite:
					slots[n] = p
					n++
				default:
					break drain
				}
			}
		}

		for written := 0; written < n; {
			var sent int
			var err error
			if s.batch != nil {
				sent, err = s.batch.writeBatch(slots[written:n])
			} else {
				p := slots[written]
				_, err = s.conn.WriteTo(p.buf, p.addr)
				if err == nil {
					sent = 1
				}
			}

			clear(slots[written : written+sent])
			written += sent
			if err == nil && sent == 0 {
				err = io.ErrNoProgress
			}
			if err != nil {
				if errors.Is(err, net.ErrClosed) || s.closerCtx.Err() != nil {
					return
				}
				if Logger != nil {
					Logger("[CONN] Write error:", err.Error())
				}
				if written < n {
					// As with a single WriteTo error, discard the failing
					// datagram and keep later queued packets deliverable.
					slots[written] = syncPacket{}
					written++
				}
			}
		}
	}
}

func (s *SyncConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	return s.conn.ReadFrom(p)
}

// WriteTo queues p for asynchronous transmission. Its contents must remain
// immutable after return; ADNL sends independent packet buffers for this reason.
func (s *SyncConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	s.writeMx.RLock()
	defer s.writeMx.RUnlock()

	if s.closerCtx.Err() != nil {
		return 0, net.ErrClosed
	}
	select {
	case <-s.closerCtx.Done():
		return 0, net.ErrClosed
	case s.chWrite <- syncPacket{addr, p}:
		return len(p), nil
	}
}

func (s *SyncConn) Close() error {
	err := s.closeConn()
	<-s.writeDone
	return err
}

func (s *SyncConn) closeConn() error {
	s.closeOnce.Do(func() {
		s.closer()
		s.closeErr = s.conn.Close()
	})
	return s.closeErr
}

func (s *SyncConn) LocalAddr() net.Addr {
	return s.conn.LocalAddr()
}

func (s *SyncConn) SetDeadline(t time.Time) error {
	return s.conn.SetDeadline(t)
}

func (s *SyncConn) SetReadDeadline(t time.Time) error {
	return s.conn.SetReadDeadline(t)
}

func (s *SyncConn) SetWriteDeadline(t time.Time) error {
	return s.conn.SetWriteDeadline(t)
}
