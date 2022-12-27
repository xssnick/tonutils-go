package adnl

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/tl"
	"net"
	"sync"
	"time"
)

type Client interface {
	SetCustomMessageHandler(handler func(msg *MessageCustom) error)
	SetQueryHandler(handler func(msg *MessageQuery) error)
	SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey))
	SendCustomMessage(ctx context.Context, req tl.Serializable) error
	Query(ctx context.Context, req, result tl.Serializable) error
	Answer(ctx context.Context, queryID []byte, result tl.Serializable) error
	RemoteAddr() string
	Close()
}

type srvClient struct {
	id     string
	server *Server
	client Client
}

type srvProcessor struct {
	lastPacketAt time.Time
	isChannel    bool
	processor    func(buf []byte) error
}

type Server struct {
	conn net.PacketConn

	key        ed25519.PrivateKey
	processors map[string]*srvProcessor

	connHandler func(client Client) error

	mx sync.RWMutex
}

func NewServer(key ed25519.PrivateKey) *Server {
	return &Server{
		key:        key,
		processors: map[string]*srvProcessor{},
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

	rootID, err := ToKeyID(PublicKeyED25519{Key: s.key.Public().(ed25519.PublicKey)})
	if err != nil {
		return err
	}

	lastListCheck := time.Now()
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

		var proc *srvProcessor
		if bytes.Equal(rootID, id) {
			if len(buf) < 32 {
				// too small packet
				continue
			}
			clientId := hex.EncodeToString(buf[:32])

			s.mx.RLock()
			proc = s.processors[clientId]
			s.mx.RUnlock()

			if proc == nil {
				a := initADNL(s.key)
				a.addr = addr.String()
				a.writer = newWriter(func(p []byte, deadline time.Time) (err error) {
					return s.write(deadline, addr, p)
				})

				a.SetChannelReadyHandler(func(ch *Channel) {
					chID := hex.EncodeToString(ch.id)

					s.mx.Lock()
					s.processors[chID] = &srvProcessor{
						isChannel:    true,
						processor:    ch.process,
						lastPacketAt: time.Now(),
					}
					s.mx.Unlock()

					cli := &srvClient{
						id:     chID,
						server: s,
						client: ch.adnl,
					}

					// setup basic disconnect handler to auto-cleanup processors list
					cli.SetDisconnectHandler(nil)

					connHandler := s.connHandler
					if connHandler != nil {
						err = connHandler(cli)
						if err != nil {
							// close connection if connection handler reports an error
							ch.adnl.Close()
						}
					}
				})

				cli := &srvClient{
					id:     clientId,
					server: s,
					client: a,
				}
				cli.SetDisconnectHandler(nil)

				// TODO: cleanup processors
				proc = &srvProcessor{
					isChannel: false,
					processor: func(buf []byte) error {
						err := a.process(buf)
						if err != nil {
							// consider err on initialization as critical and drop connection
							a.Close()
						}
						return err
					},
				}

				s.mx.Lock()
				s.processors[clientId] = proc
				s.mx.Unlock()
			}
		} else {
			s.mx.RLock()
			proc = s.processors[hex.EncodeToString(id)]
			s.mx.RUnlock()
		}

		if proc == nil {
			Logger("unknown destination:", hex.EncodeToString(id))
			continue
		}

		now := time.Now()
		proc.lastPacketAt = now

		if now.Sub(lastListCheck) > 5*time.Second {
			lastListCheck = now
			s.mx.Lock()
			for k, pr := range s.processors {
				if now.Sub(pr.lastPacketAt) > 5*time.Minute {
					delete(s.processors, k)
				}
			}
			s.mx.Unlock()
		}

		go func() {
			defer func() {
				if r := recover(); r != nil {
					Logger("critical error while processing packet at server:", r)
				}
			}()

			err = proc.processor(buf)
			if err != nil {
				Logger("failed to process packet at server:", err)
				return
			}
		}()
	}
}

func (s *Server) SetConnectionHandler(handler func(client Client) error) {
	s.connHandler = handler
}

func (s *Server) Close() error {
	s.mx.Lock()
	defer s.mx.Unlock()

	if s.conn == nil {
		return nil
	}

	return s.conn.Close()
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

func (s *srvClient) SetCustomMessageHandler(handler func(msg *MessageCustom) error) {
	s.client.SetCustomMessageHandler(handler)
}

func (s *srvClient) SetQueryHandler(handler func(msg *MessageQuery) error) {
	s.client.SetQueryHandler(handler)
}

func (s *srvClient) SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey)) {
	s.client.SetDisconnectHandler(func(addr string, key ed25519.PublicKey) {
		s.server.mx.Lock()
		delete(s.server.processors, s.id)
		s.server.mx.Unlock()

		if handler != nil {
			handler(addr, key)
		}
	})
}

func (s *srvClient) SendCustomMessage(ctx context.Context, req tl.Serializable) error {
	return s.client.SendCustomMessage(ctx, req)
}

func (s *srvClient) Query(ctx context.Context, req, result tl.Serializable) error {
	return s.client.Query(ctx, req, result)
}

func (s *srvClient) Answer(ctx context.Context, queryID []byte, result tl.Serializable) error {
	return s.client.Answer(ctx, queryID, result)
}

func (s *srvClient) RemoteAddr() string {
	return s.client.RemoteAddr()
}

func (s *srvClient) Close() {
	s.client.Close()
}
