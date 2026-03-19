package rldp

import (
	"container/heap"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

var errTimeout timeoutError

type memPacket struct {
	data []byte
	addr net.Addr
}

type scheduledPacket struct {
	when time.Time
	pkt  memPacket
}

type packetQueue []scheduledPacket

func (pq packetQueue) Len() int { return len(pq) }

func (pq packetQueue) Less(i, j int) bool { return pq[i].when.Before(pq[j].when) }

func (pq packetQueue) Swap(i, j int) { pq[i], pq[j] = pq[j], pq[i] }

func (pq *packetQueue) Push(x interface{}) {
	*pq = append(*pq, x.(scheduledPacket))
}

func (pq *packetQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[:n-1]
	return item
}

type memPacketConn struct {
	name      string
	inbox     chan memPacket
	peer      *memPacketConn
	closeOnce sync.Once
	closed    atomic.Bool
	closeCh   chan struct{}

	baseDelay time.Duration
	jitter    time.Duration
	loss      float64

	rngMu sync.Mutex
	rng   *rand.Rand

	readDeadline  atomic.Int64
	writeDeadline atomic.Int64

	localAddr *net.UDPAddr

	dispatcherOnce sync.Once
	wakeCh         chan struct{}
	queueMu        sync.Mutex
	queue          packetQueue
}

func newMemPacketConnPair(loss float64, baseDelay, jitter time.Duration, buf int) (*memPacketConn, *memPacketConn) {
	if buf <= 0 {
		buf = 1024
	}
	now := time.Now().UnixNano()
	basePort := 20000 + int(now%10000)
	a := &memPacketConn{
		name:      "server",
		inbox:     make(chan memPacket, buf),
		closeCh:   make(chan struct{}),
		baseDelay: baseDelay,
		jitter:    jitter,
		loss:      loss,
		rng:       rand.New(rand.NewSource(now)),
		localAddr: &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: basePort},
		wakeCh:    make(chan struct{}, 1),
	}
	b := &memPacketConn{
		name:      "client",
		inbox:     make(chan memPacket, buf),
		closeCh:   make(chan struct{}),
		baseDelay: baseDelay,
		jitter:    jitter,
		loss:      loss,
		rng:       rand.New(rand.NewSource(now + 1)),
		localAddr: &net.UDPAddr{IP: net.IPv4(10, 0, 0, 2), Port: basePort + 1},
		wakeCh:    make(chan struct{}, 1),
	}
	a.peer = b
	b.peer = a
	a.startDispatcher()
	b.startDispatcher()
	return a, b
}

func (c *memPacketConn) randFloat() float64 {
	c.rngMu.Lock()
	f := c.rng.Float64()
	c.rngMu.Unlock()
	return f
}

func (c *memPacketConn) LocalAddr() net.Addr {
	return c.localAddr
}

func (c *memPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	if c.closed.Load() {
		return 0, nil, net.ErrClosed
	}
	var (
		timer   *time.Timer
		timerCh <-chan time.Time
	)
	if deadline := c.readDeadline.Load(); deadline > 0 {
		d := time.Until(time.Unix(0, deadline))
		if d <= 0 {
			return 0, nil, errTimeout
		}
		timer = time.NewTimer(d)
		timerCh = timer.C
		defer timer.Stop()
	}
	select {
	case pkt, ok := <-c.inbox:
		if !ok {
			return 0, nil, net.ErrClosed
		}
		n := copy(b, pkt.data)
		return n, pkt.addr, nil
	case <-c.closeCh:
		return 0, nil, net.ErrClosed
	case <-timerCh:
		return 0, nil, errTimeout
	}
}

func (c *memPacketConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	peer := c.peer
	if peer == nil || peer.closed.Load() {
		return 0, net.ErrClosed
	}
	if deadline := c.writeDeadline.Load(); deadline > 0 && time.Now().After(time.Unix(0, deadline)) {
		return 0, errTimeout
	}
	if len(b) == 0 {
		return 0, nil
	}
	if c.loss > 0 && c.randFloat() < c.loss {
		return len(b), nil
	}
	payload := make([]byte, len(b))
	copy(payload, b)
	delay := c.baseDelay
	if c.jitter > 0 {
		j := time.Duration((c.randFloat()*2 - 1) * float64(c.jitter))
		delay += j
		if delay < 0 {
			delay = 0
		}
	}
	peer.startDispatcher()
	pkt := memPacket{data: payload, addr: c.localAddr}
	peer.enqueuePacket(pkt, time.Now().Add(delay))
	return len(b), nil
}

func (c *memPacketConn) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		close(c.closeCh)
		close(c.inbox)
		c.wake()
	})
	return nil
}

func (c *memPacketConn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}

func (c *memPacketConn) SetReadDeadline(t time.Time) error {
	if t.IsZero() {
		c.readDeadline.Store(0)
	} else {
		c.readDeadline.Store(t.UnixNano())
	}
	return nil
}

func (c *memPacketConn) SetWriteDeadline(t time.Time) error {
	if t.IsZero() {
		c.writeDeadline.Store(0)
	} else {
		c.writeDeadline.Store(t.UnixNano())
	}
	return nil
}

func (c *memPacketConn) SetReadBuffer(int) error  { return nil }
func (c *memPacketConn) SetWriteBuffer(int) error { return nil }

func (c *memPacketConn) startDispatcher() {
	c.dispatcherOnce.Do(func() {
		if c.wakeCh == nil {
			c.wakeCh = make(chan struct{}, 1)
		}
		heap.Init(&c.queue)
		go c.dispatchLoop()
	})
}

func (c *memPacketConn) enqueuePacket(pkt memPacket, when time.Time) {
	if c.closed.Load() {
		return
	}
	c.queueMu.Lock()
	earliest := c.queue.Len() == 0
	if !earliest {
		earliest = when.Before(c.queue[0].when)
	}
	heap.Push(&c.queue, scheduledPacket{when: when, pkt: pkt})
	c.queueMu.Unlock()
	if earliest {
		c.wake()
	}
}

func (c *memPacketConn) dispatchLoop() {
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	for {
		c.queueMu.Lock()
		if c.queue.Len() == 0 {
			closed := c.closed.Load()
			c.queueMu.Unlock()
			if closed {
				return
			}
			select {
			case <-c.closeCh:
				return
			case <-c.wakeCh:
				continue
			}
		}
		next := c.queue[0]
		wait := time.Until(next.when)
		if wait <= 0 {
			heap.Pop(&c.queue)
			c.queueMu.Unlock()
			c.deliver(next.pkt)
			continue
		}
		c.queueMu.Unlock()

		resetTimer(timer, wait)
		select {
		case <-timer.C:
			continue
		case <-c.closeCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-c.wakeCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			continue
		}
	}
}

func (c *memPacketConn) deliver(pkt memPacket) {
	if c.closed.Load() {
		return
	}
	select {
	case c.inbox <- pkt:
	case <-c.closeCh:
	default:
	}
}

func (c *memPacketConn) wake() {
	if c.wakeCh == nil {
		return
	}
	select {
	case c.wakeCh <- struct{}{}:
	default:
	}
}

func resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}
