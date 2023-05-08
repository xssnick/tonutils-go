package adnl

import (
	"fmt"
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
