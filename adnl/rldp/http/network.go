package http

import (
	"io"
	"net"
	"time"
)

type httpConn struct {
	r *dataStreamer
	w *dataStreamer
}

func (h *httpConn) Read(b []byte) (n int, err error) {
	return h.r.Read(b)
}

func (h *httpConn) Write(b []byte) (n int, err error) {
	return h.w.Write(b)
}

func (h *httpConn) Close() error {
	h.r.Finish()
	h.w.Finish()
	return nil
}

func (h *httpConn) LocalAddr() net.Addr {
	return &net.UDPAddr{}
}

func (h *httpConn) RemoteAddr() net.Addr {
	return &net.UDPAddr{}
}

func (h *httpConn) SetDeadline(t time.Time) error {
	return nil
}

func (h *httpConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (h *httpConn) SetWriteDeadline(t time.Time) error {
	return nil
}

type httpListener struct {
	connChan chan *httpConn
}

func newVirtualHttpListener() *httpListener {
	return &httpListener{
		connChan: make(chan *httpConn, 1),
	}
}

func (h *httpListener) Accept() (net.Conn, error) {
	conn, ok := <-h.connChan
	if !ok {
		return nil, io.ErrClosedPipe
	}
	return conn, nil
}

func (h *httpListener) Close() error {
	close(h.connChan)
	return nil
}

func (h *httpListener) addConn(r, w *dataStreamer) {
	h.connChan <- &httpConn{r: r, w: w}
}

func (h *httpListener) Addr() net.Addr {
	return &net.UDPAddr{}
}
