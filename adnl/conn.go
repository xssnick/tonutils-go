package adnl

import (
	"context"
	"errors"
	"fmt"
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

func (c *clientConn) Write(b []byte, deadline time.Time) (n int, err error) {
	select {
	case <-c.closer:
		return 0, fmt.Errorf("connection was closed")
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
	chWrite   chan syncPacket
	chRead    chan syncPacket
	closerCtx context.Context
	closer    context.CancelFunc
}

func NewSyncConn(conn net.PacketConn, packetsBufSz int) *SyncConn {
	ctx, cancel := context.WithCancel(context.Background())
	sc := &SyncConn{
		conn:      conn,
		chWrite:   make(chan syncPacket, packetsBufSz),
		closer:    cancel,
		closerCtx: ctx,
	}
	go sc.writer()
	return sc
}

func (s *SyncConn) writer() {
	defer s.Close()

	for {
		select {
		case p := <-s.chWrite:
			if _, err := s.conn.WriteTo(p.buf, p.addr); err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				// should not happen, but if will we want to see
				Logger("[CONN] Write error:", err.Error())
			}
		case <-s.closerCtx.Done():
			return
		}
	}
}

func (s *SyncConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	return s.conn.ReadFrom(p)
}

func (s *SyncConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	select {
	case <-s.closerCtx.Done():
		return 0, fmt.Errorf("connection was closed")
	case s.chWrite <- syncPacket{addr, p}:
		return len(p), nil
	}
}

func (s *SyncConn) Close() error {
	s.closer()
	return s.conn.Close()
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
