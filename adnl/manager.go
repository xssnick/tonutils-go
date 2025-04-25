package adnl

import (
	"context"
	"io"
	"net"
	"sync"
	"unsafe"
)

type NetManager interface {
	Free(p *UDPPacket)
	GetReaderChan(gate *Gateway) <-chan *UDPPacket
	InitConnection(gate *Gateway, addr string) error
	CloseConnection(gate *Gateway)
	WritePacket(gate *Gateway, p []byte, addr net.Addr) (n int, err error)
	Close()
}

type SingleNetManager struct {
	connector func(addr string) (net.PacketConn, error)
	conn      net.PacketConn
	bufPool   sync.Pool
	udpBuf    chan *UDPPacket

	globalCtx       context.Context
	globalCtxCancel context.CancelFunc
}

func NewSingleNetReader(connector func(addr string) (net.PacketConn, error)) *SingleNetManager {
	globalCtx, globalCtxCancel := context.WithCancel(context.Background())
	return &SingleNetManager{
		connector: connector,
		bufPool: sync.Pool{
			New: func() interface{} {
				return &UDPPacket{
					from: nil,
					data: make([]byte, 2048),
					n:    0,
				}
			},
		},
		udpBuf:          make(chan *UDPPacket, 1*1024*1024),
		globalCtx:       globalCtx,
		globalCtxCancel: globalCtxCancel,
	}
}

func (s *SingleNetManager) WritePacket(gate *Gateway, p []byte, addr net.Addr) (n int, err error) {
	if s.conn == nil {
		return 0, io.ErrClosedPipe
	}
	return s.conn.WriteTo(p, addr)
}

func (s *SingleNetManager) InitConnection(gate *Gateway, addr string) error {
	conn, err := s.connector(addr)
	if err != nil {
		return err
	}
	s.conn = conn

	go func() {
		for {
			p := s.bufPool.Get().(*UDPPacket)

			n, addr, err := s.conn.ReadFrom(p.data)
			if err != nil {
				s.bufPool.Put(p)

				select {
				case <-s.globalCtx.Done():
					return
				default:
				}

				Logger("failed to read packet:", err)
				continue
			}

			if n < 64 {
				s.bufPool.Put(p)

				// too small packet
				continue
			}

			p.from = addr
			p.n = n
			select {
			case s.udpBuf <- p:
			default:
				s.bufPool.Put(p)
			}
		}
	}()

	return nil
}

func (s *SingleNetManager) Close() {
	s.globalCtxCancel()
	_ = s.conn.Close()
}

func (s *SingleNetManager) Free(p *UDPPacket) {
	s.bufPool.Put(p)
}

func (s *SingleNetManager) CloseConnection(gate *Gateway) {
	s.globalCtxCancel()
	_ = s.conn.Close()
}

func (s *SingleNetManager) GetReaderChan(gate *Gateway) <-chan *UDPPacket {
	return s.udpBuf
}

type MultiNetManager struct {
	conn    net.PacketConn
	bufPool sync.Pool

	processors map[string]chan *UDPPacket
	src        map[*Gateway]chan *UDPPacket

	globalCtx       context.Context
	globalCtxCancel context.CancelFunc

	mx sync.RWMutex
}

func NewMultiNetReader(conn net.PacketConn) *MultiNetManager {
	globalCtx, globalCtxCancel := context.WithCancel(context.Background())

	m := &MultiNetManager{
		conn: conn,
		bufPool: sync.Pool{
			New: func() interface{} {
				return &UDPPacket{
					from: nil,
					data: make([]byte, 2048),
					n:    0,
				}
			},
		},
		processors:      map[string]chan *UDPPacket{},
		src:             make(map[*Gateway]chan *UDPPacket),
		globalCtx:       globalCtx,
		globalCtxCancel: globalCtxCancel,
	}

	go func() {
		for {
			p := m.bufPool.Get().(*UDPPacket)

			n, addr, err := m.conn.ReadFrom(p.data)
			if err != nil {
				m.bufPool.Put(p)

				select {
				case <-m.globalCtx.Done():
					return
				default:
				}

				Logger("failed to read packet from multi manager:", err)
				continue
			}

			if n < 64 {
				m.bufPool.Put(p)

				// too small packet
				continue
			}

			h := p.data[:32]
			m.mx.RLock()
			t := m.processors[*(*string)(unsafe.Pointer(&h))]
			m.mx.RUnlock()
			if t == nil {
				m.bufPool.Put(p)

				continue
			}

			p.from = addr
			p.n = n
			select {
			case t <- p:
			default:
				m.bufPool.Put(p)
			}
		}
	}()

	return m
}

func (m *MultiNetManager) Free(p *UDPPacket) {
	m.bufPool.Put(p)
}

func (m *MultiNetManager) Close() {
	m.globalCtxCancel()
	_ = m.conn.Close()
}

func (m *MultiNetManager) GetReaderChan(gate *Gateway) <-chan *UDPPacket {
	m.mx.Lock()
	defer m.mx.Unlock()

	return m.src[gate]
}

func (m *MultiNetManager) InitConnection(gate *Gateway, addr string) error {
	m.mx.Lock()
	defer m.mx.Unlock()

	chPackets := make(chan *UDPPacket, 1*1024*1024)
	m.src[gate] = chPackets
	m.processors[string(gate.GetID())] = chPackets

	gate.setupChannelCallbacks(func(ch *Channel) {
		m.mx.Lock()
		m.processors[string(ch.id)] = chPackets
		m.mx.Unlock()
	}, func(id string) {
		m.mx.Lock()
		delete(m.processors, id)
		m.mx.Unlock()
	})

	return nil
}

func (m *MultiNetManager) CloseConnection(gate *Gateway) {
	m.mx.Lock()
	defer m.mx.Unlock()

	delete(m.src, gate)
	if len(m.src) == 0 {
		m.globalCtxCancel()
		_ = m.conn.Close()
	}
}

func (m *MultiNetManager) WritePacket(gate *Gateway, p []byte, addr net.Addr) (n int, err error) {
	return m.conn.WriteTo(p, addr)
}
