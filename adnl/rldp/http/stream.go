package http

import (
	"io"
	"sync"
	"time"
)

type payloadStream struct {
	nextOffset int
	Data       *dataStreamer
	ValidTill  time.Time
}

type dataStreamer struct {
	buf []byte

	parts    chan []byte
	closer   chan bool
	finished bool
	closed   bool

	readerLock sync.Mutex
	writerLock sync.Mutex
	closerLock sync.Mutex
}

func newDataStreamer() *dataStreamer {
	return &dataStreamer{
		parts:  make(chan []byte, 1),
		closer: make(chan bool, 1),
	}
}

func (d *dataStreamer) Read(p []byte) (n int, err error) {
	d.readerLock.Lock()
	defer d.readerLock.Unlock()

	for {
		if len(d.buf) == 0 {
			select {
			case d.buf = <-d.parts:
				if d.buf == nil {
					if d.finished {
						return n, io.EOF
					}
					// flush
					return n, nil
				}
			case <-d.closer:
				return n, io.ErrUnexpectedEOF
			}
		}

		if n == len(p) {
			return n, nil
		}

		copied := copy(p[n:], d.buf)
		d.buf = d.buf[copied:]

		n += copied
	}
}

func (d *dataStreamer) Close() error {
	d.closerLock.Lock()
	defer d.closerLock.Unlock()

	if !d.closed {
		d.closed = true
		close(d.closer)
	}

	return nil
}

// FlushReader - forces Read to return current state
func (d *dataStreamer) FlushReader() {
	d.writerLock.Lock()
	defer d.writerLock.Unlock()

	select {
	case d.parts <- nil:
	case <-d.closer:
	}

	return
}

func (d *dataStreamer) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}

	d.writerLock.Lock()
	defer d.writerLock.Unlock()

	if d.finished {
		return 0, io.ErrClosedPipe
	}

	tmp := make([]byte, len(data))
	copy(tmp, data)

	select {
	case d.parts <- tmp:
	case <-d.closer:
		return 0, io.ErrClosedPipe
	}

	return len(data), nil
}

func (d *dataStreamer) Finish() {
	d.writerLock.Lock()
	defer d.writerLock.Unlock()

	if !d.finished {
		d.finished = true
		close(d.parts)
	}
}
