package adnl

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/tl"
	"net"
	"strconv"
	"strings"
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

	processPacket(packet *PacketContent, ch *Channel) (err error)
}

type srvClient struct {
	channelId string
	clientId  string
	server    *Server
	client    Client
}

type srvProcessor struct {
	lastPacketAt time.Time
	processor    func(buf []byte) error
	closer       func()
}

type Server struct {
	conn net.PacketConn

	dhtIP      net.IP
	addrList   address.List
	key        ed25519.PrivateKey
	processors map[string]*srvProcessor
	clients    map[string]*srvClient

	connHandler func(client Client) error

	mx sync.RWMutex
}

func NewServer(key ed25519.PrivateKey) *Server {
	return &Server{
		key:        key,
		processors: map[string]*srvProcessor{},
		clients:    map[string]*srvClient{},
	}
}

var RawListener = func(addr string) (net.PacketConn, error) {
	return net.ListenPacket("udp", addr)
}

func (s *Server) GetAddressList() address.List {
	return s.addrList
}

func (s *Server) SetExternalIP(ip net.IP) {
	s.dhtIP = ip
}

func (s *Server) ListenAndServe(listenAddr string) (err error) {
	adr := strings.Split(listenAddr, ":")
	if len(adr) != 2 {
		return fmt.Errorf("invalid listen address")
	}

	ip := s.dhtIP
	if ip == nil {
		ip = net.ParseIP(adr[0])
	}
	ip = ip.To4()

	if ip.Equal(net.IPv4zero) {
		return fmt.Errorf("invalid external ip")
	}
	port, err := strconv.ParseUint(adr[1], 10, 16)
	if err != nil {
		return fmt.Errorf("invalid listen port")
	}

	tm := int32(time.Now().Unix())
	s.addrList = address.List{
		Addresses: []*address.UDP{
			{
				IP:   ip,
				Port: int32(port),
			},
		},
		Version:    tm,
		ReinitDate: tm,
		Priority:   0,
		ExpireAt:   0,
	}

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

		if bytes.Equal(rootID, id) {
			if len(buf) < 32 {
				// too small packet
				continue
			}

			data, err := decodePacket(s.key, buf)
			if err != nil {
				return fmt.Errorf("failed to decode packet: %w", err)
			}

			packet, err := parsePacket(data)
			if err != nil {
				Logger("failed to parse packet:", err)
				continue
			}

			if packet.From == nil {
				// invalid packet
				continue
			}

			clientId := hex.EncodeToString(packet.From.Key)

			s.mx.RLock()
			cli := s.clients[clientId]
			s.mx.RUnlock()

			if cli == nil {
				a := initADNL(s.key)
				a.SetAddresses(s.addrList)
				a.addr = addr.String()
				a.writer = newWriter(func(p []byte, deadline time.Time) (err error) {
					return s.write(deadline, addr, p)
				})

				cli = &srvClient{
					clientId: clientId,
					server:   s,
					client:   a,
				}
				// setup basic disconnect handler to auto-cleanup processors list
				cli.SetDisconnectHandler(nil)

				a.SetChannelReadyHandler(func(ch *Channel) {
					chID := hex.EncodeToString(ch.id)
					cli.channelId = chID

					s.mx.Lock()
					s.processors[chID] = &srvProcessor{
						processor:    ch.process,
						lastPacketAt: time.Now(),
						closer:       ch.adnl.Close,
					}
					s.mx.Unlock()

					connHandler := s.connHandler
					if connHandler != nil {
						err = connHandler(cli)
						if err != nil {
							// close connection if connection handler reports an error
							ch.adnl.Close()
						}
					}
				})

				s.mx.Lock()
				s.clients[clientId] = cli
				s.mx.Unlock()
			}

			err = cli.client.processPacket(packet, nil)
			if err != nil {
				Logger("failed to process ADNL packet:", err)
				continue
			}
			continue
		}

		s.mx.RLock()
		proc := s.processors[hex.EncodeToString(id)]
		s.mx.RUnlock()

		if proc == nil {
			Logger("unknown destination:", hex.EncodeToString(id))
			continue
		}

		now := time.Now()
		proc.lastPacketAt = now

		if now.Sub(lastListCheck) > 10*time.Second {
			lastListCheck = now
			var prc []*srvProcessor

			s.mx.Lock()
			for k, pr := range s.processors {
				if now.Sub(pr.lastPacketAt) > 30000000*time.Minute {
					prc = append(prc, pr)
					delete(s.processors, k)
				}
			}
			s.mx.Unlock()

			if len(prc) > 0 {
				go func() {
					for _, pr := range prc {
						pr.closer()
					}
				}()
			}
		}

		func() {
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
		delete(s.server.processors, s.channelId)
		delete(s.server.clients, s.clientId)
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

func (s *srvClient) processPacket(packet *PacketContent, ch *Channel) (err error) {
	return s.client.processPacket(packet, ch)
}
