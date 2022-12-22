package adnl

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"time"
)

type QueryServerHandler func(client *ADNL, msg *MessageQuery) error
type CustomServerHandler func(client *ADNL, msg *MessageCustom) error

type processor func(buf []byte) error

type Server struct {
	conn net.PacketConn

	key        ed25519.PrivateKey
	processors map[string]processor
	clients    map[string]*ADNL

	customMessageHandler CustomServerHandler
	queryHandler         QueryServerHandler
	disconnectHandler    DisconnectHandler

	mx sync.RWMutex
}

type connection struct {
	proc processor
	addr string
}

func NewServer(key ed25519.PrivateKey) *Server {
	return &Server{
		key:        key,
		processors: map[string]processor{},
	}
}

var RawListener = func(addr string) (net.PacketConn, error) {
	return net.ListenPacket("udp", addr)
}

func (s *Server) ListenAndServe(listenAddr string) (err error) {
	s.conn, err = RawListener(listenAddr)
	if err != nil {
		return err
	}

	for {
		buf := make([]byte, 4096)
		n, addr, err := s.conn.ReadFrom(buf)
		if err != nil {
			Logger("failed to accept client:", err)
			continue
		}

		if n < 64 {
			// too small packet
			continue
		}

		buf = buf[:n]
		id := buf[:32]
		buf = buf[32:]

		clientID := hex.EncodeToString(id)

		s.mx.RLock()
		proc := s.processors[clientID]
		s.mx.RUnlock()

		if proc == nil {
			a, err := initADNL(s.key)
			if err != nil {
				Logger("failed to init adnl:", err)
				continue
			}

			a.addr = addr.String()
			a.writer = newWriter(func(p []byte, deadline time.Time) (err error) {
				return s.write(deadline, addr, p)
			})

			a.SetQueryHandler(func(msg *MessageQuery) error {
				return s.queryHandler(a, msg)
			})
			a.SetDisconnectHandler(func(addr string, key ed25519.PublicKey) {
				s.mx.Lock()
				delete(s.processors, clientID)
				if a.channel != nil {
					delete(s.processors, hex.EncodeToString(a.channel.id))
				}
				s.mx.Unlock()

				if s.disconnectHandler != nil {
					s.disconnectHandler(addr, key)
				}
			})
			a.SetCustomMessageHandler(func(msg *MessageCustom) error {
				if s.customMessageHandler != nil {
					return s.customMessageHandler(a, msg)
				}
				return nil
			})
			a.SetChannelReadyHandler(func(ch *Channel) {
				s.mx.Lock()
				s.processors[hex.EncodeToString(ch.id)] = ch.process
				s.mx.Unlock()
			})

			proc = func(buf []byte) error {
				err := a.process(buf)
				if err != nil {
					// consider err on initialization as critical and drop connection
					a.Close()
				}
				return err
			}

			s.mx.Lock()
			s.processors[hex.EncodeToString(a.id)] = proc
			s.mx.Unlock()
		}

		go func() {
			defer func() {
				if r := recover(); r != nil {
					Logger("critical error while processing packet at server:", r)
				}
			}()

			err = proc(buf)
			if err != nil {
				Logger("failed to process packet at server:", err)
				return
			}
		}()
	}
}

func (s *Server) SetCustomMessageHandler(handler func(client *ADNL, msg *MessageCustom) error) {
	s.customMessageHandler = handler
}

func (s *Server) SetQueryHandler(handler func(client *ADNL, msg *MessageQuery) error) {
	s.queryHandler = handler
}

func (s *Server) SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey)) {
	s.disconnectHandler = handler
}

func (s *Server) write(deadline time.Time, addr net.Addr, buf []byte) error {
	s.mx.Lock()
	defer s.mx.Unlock()

	_ = s.conn.SetWriteDeadline(deadline)
	n, err := s.conn.WriteTo(buf, addr)
	if err != nil {
		return err
	}

	if n != len(buf) {
		return fmt.Errorf("too big packet")
	}

	return nil
}
