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
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
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

type Peer interface {
	SetCustomMessageHandler(handler func(msg *MessageCustom) error)
	SetQueryHandler(handler func(msg *MessageQuery) error)
	SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey))
	SendCustomMessage(ctx context.Context, req tl.Serializable) error
	Query(ctx context.Context, req, result tl.Serializable) error
	Answer(ctx context.Context, queryID []byte, result tl.Serializable) error
	RemoteAddr() string
	Close()
}

type peerConn struct {
	addr      unsafe.Pointer
	channelId string
	clientId  string
	server    *Gateway
	client    Client
}

type srvProcessor struct {
	lastPacketAt time.Time
	processor    func(buf []byte) error
	closer       func()
}

type Gateway struct {
	conn net.PacketConn

	dhtIP      net.IP
	addrList   address.List
	key        ed25519.PrivateKey
	processors map[string]*srvProcessor
	peers      map[string]*peerConn

	connHandler func(client Client) error

	started bool
	mx      sync.RWMutex
}

func newGateway(key ed25519.PrivateKey) *Gateway {
	if key == nil {
		panic("key is nil")
	}

	return &Gateway{
		key:        key,
		processors: map[string]*srvProcessor{},
		peers:      map[string]*peerConn{},
	}
}

func StartClientGateway(key ed25519.PrivateKey) (*Gateway, error) {
	g := newGateway(key)
	if err := g.runAsClient(); err != nil {
		return nil, err
	}
	return g, nil
}

func StartServerGateway(key ed25519.PrivateKey, listenAddr string) (*Gateway, error) {
	g := newGateway(key)
	if err := g.runAsServer(listenAddr); err != nil {
		return nil, err
	}
	return g, nil
}

var RawListener = func(addr string) (net.PacketConn, error) {
	return net.ListenPacket("udp", addr)
}

func (s *Gateway) GetAddressList() address.List {
	return s.addrList
}

func (s *Gateway) SetExternalIP(ip net.IP) {
	s.dhtIP = ip
}

func (s *Gateway) runAsServer(listenAddr string) (err error) {
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

	rootId, err := ToKeyID(PublicKeyED25519{Key: s.key.Public().(ed25519.PublicKey)})
	if err != nil {
		return err
	}

	s.mx.Lock()
	defer s.mx.Unlock()

	if s.started {
		return fmt.Errorf("already started")
	}
	s.started = true

	go s.listen(rootId)

	return nil
}

func (s *Gateway) runAsClient() (err error) {
	tm := int32(time.Now().Unix())
	s.addrList = address.List{
		Addresses:  []*address.UDP{}, // no specified addresses, we are acting as a client
		Version:    tm,
		ReinitDate: tm,
		Priority:   0,
		ExpireAt:   0,
	}

	// listen all addresses, port will be auto-chosen
	s.conn, err = RawListener(":")
	if err != nil {
		return err
	}

	rootId, err := ToKeyID(PublicKeyED25519{Key: s.key.Public().(ed25519.PublicKey)})
	if err != nil {
		return err
	}

	s.mx.Lock()
	defer s.mx.Unlock()

	if s.started {
		return fmt.Errorf("already started")
	}
	s.started = true

	go s.listen(rootId)

	return nil
}

func (s *Gateway) listen(rootId []byte) {
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

		if bytes.Equal(rootId, id) {
			if len(buf) < 32 {
				// too small packet
				continue
			}

			data, err := decodePacket(s.key, buf)
			if err != nil {
				Logger("failed to decode packet:", err)
				continue
			}

			packet, err := parsePacket(data)
			if err != nil {
				Logger("failed to parse packet:", err)
				continue
			}

			peerId := packet.FromIDShort
			if peerId == nil {
				if packet.From == nil {
					// invalid packet
					continue
				}

				peerId, err = ToKeyID(PublicKeyED25519{Key: packet.From.Key})
				if err != nil {
					// invalid packet
					continue
				}
			}

			s.mx.RLock()
			cli := s.peers[string(peerId)]
			s.mx.RUnlock()

			if cli == nil {
				if packet.From == nil {
					// invalid packet
					continue
				}

				cli, err = s.registerClient(addr, packet.From.Key, string(peerId))
				if err != nil {
					continue
				}
			} else {
				cli.checkUpdateAddr(addr)
			}

			err = cli.client.processPacket(packet, nil)
			if err != nil {
				Logger("failed to process ADNL packet:", err)
				continue
			}
			continue
		}

		s.mx.RLock()
		proc := s.processors[string(id)]
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

func (p *peerConn) checkUpdateAddr(addr net.Addr) {
	// we use atomic to safely swap address without lock, in case of change
	currentAddr := *(*net.Addr)(atomic.LoadPointer(&p.addr))
	if currentAddr.String() != addr.String() {
		atomic.StorePointer(&p.addr, unsafe.Pointer(&addr))
	}
}

func (s *Gateway) registerClient(addr net.Addr, key ed25519.PublicKey, id string) (*peerConn, error) {
	s.mx.Lock()
	defer s.mx.Unlock()

	peer := s.peers[id]
	if peer != nil {
		peer.checkUpdateAddr(addr)
		return peer, nil
	}

	peer = &peerConn{
		addr:     unsafe.Pointer(&addr),
		clientId: id,
		server:   s,
	}

	addrList := s.addrList
	addrList.ReinitDate = int32(time.Now().Unix())
	addrList.Version = addrList.ReinitDate

	a := initADNL(s.key)
	a.SetAddresses(addrList)
	a.peerKey = key
	a.addr = addr.String()
	a.writer = newWriter(func(p []byte, deadline time.Time) (err error) {
		currentAddr := *(*net.Addr)(atomic.LoadPointer(&peer.addr))
		return s.write(deadline, currentAddr, p)
	})
	peer.client = a

	s.peers[id] = peer

	// setup basic disconnect handler to auto-cleanup processors list
	peer.SetDisconnectHandler(nil)

	a.SetChannelReadyHandler(func(ch *Channel) {
		oldId := peer.channelId

		chID := string(ch.id)
		peer.channelId = chID

		s.mx.Lock()
		if oldId != "" {
			delete(s.processors, oldId)
		}
		s.processors[chID] = &srvProcessor{
			processor:    ch.process,
			lastPacketAt: time.Now(),
			closer:       ch.adnl.Close,
		}
		s.mx.Unlock()

		if oldId == "" { // connection = first channel initialisation
			connHandler := s.connHandler
			if connHandler != nil {
				err := connHandler(peer)
				if err != nil {
					// close connection if connection handler reports an error
					ch.adnl.Close()
				}
			}
		}
	})

	return peer, nil
}

func (s *Gateway) RegisterClient(addr string, key ed25519.PublicKey) (Peer, error) {
	pAddr, err := netip.ParseAddrPort(addr)
	if err != nil {
		return nil, err
	}
	udpAddr := net.UDPAddrFromAddrPort(pAddr)

	clientId, err := ToKeyID(PublicKeyED25519{Key: key})
	if err != nil {
		return nil, err
	}

	return s.registerClient(udpAddr, key, string(clientId))
}

func (s *Gateway) SetConnectionHandler(handler func(client Client) error) {
	s.connHandler = handler
}

func (s *Gateway) Close() error {
	s.mx.Lock()
	defer s.mx.Unlock()

	if s.conn == nil {
		return nil
	}

	return s.conn.Close()
}

func (s *Gateway) write(deadline time.Time, addr net.Addr, buf []byte) error {
	s.mx.Lock()
	defer s.mx.Unlock()

	if s.conn == nil {
		return fmt.Errorf("no active socket connection")
	}

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

func (p *peerConn) SetCustomMessageHandler(handler func(msg *MessageCustom) error) {
	p.client.SetCustomMessageHandler(handler)
}

func (p *peerConn) SetQueryHandler(handler func(msg *MessageQuery) error) {
	p.client.SetQueryHandler(handler)
}

func (p *peerConn) SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey)) {
	p.client.SetDisconnectHandler(func(addr string, key ed25519.PublicKey) {
		p.server.mx.Lock()
		delete(p.server.processors, p.channelId)
		delete(p.server.peers, p.clientId)
		p.server.mx.Unlock()

		if handler != nil {
			handler(addr, key)
		}
	})
}

func (p *peerConn) SendCustomMessage(ctx context.Context, req tl.Serializable) error {
	return p.client.SendCustomMessage(ctx, req)
}

func (p *peerConn) Query(ctx context.Context, req, result tl.Serializable) error {
	return p.client.Query(ctx, req, result)
}

func (p *peerConn) Answer(ctx context.Context, queryID []byte, result tl.Serializable) error {
	return p.client.Answer(ctx, queryID, result)
}

func (p *peerConn) RemoteAddr() string {
	return p.client.RemoteAddr()
}

func (p *peerConn) Close() {
	p.client.Close()
}

func (p *peerConn) processPacket(packet *PacketContent, ch *Channel) (err error) {
	return p.client.processPacket(packet, ch)
}
