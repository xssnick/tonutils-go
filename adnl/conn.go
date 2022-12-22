package adnl

import (
	"fmt"
	"time"
)

type clientConn struct {
	closer  chan bool
	onClose func()
	writer  func(p []byte, deadline time.Time) (err error)
}

func newWriter(writer func(p []byte, deadline time.Time) (err error)) *clientConn {
	return &clientConn{
		closer: make(chan bool, 1),
		writer: writer,
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
	select {
	case <-c.closer:
		return nil
	default:
		close(c.closer)
	}
	return nil
}
