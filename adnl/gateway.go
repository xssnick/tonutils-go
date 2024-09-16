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

type Peer interface {
	SetCustomMessageHandler(handler func(msg *MessageCustom) error)
	SetQueryHandler(handler func(msg *MessageQuery) error)
	GetDisconnectHandler() func(addr string, key ed25519.PublicKey)
	SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey))
	SendCustomMessage(ctx context.Context, req tl.Serializable) error
	Query(ctx context.Context, req, result tl.Serializable) error
	Answer(ctx context.Context, queryID []byte, result tl.Serializable) error
	GetQueryHandler() func(msg *MessageQuery) error
	RemoteAddr() string
	GetID() []byte
	Close()
}

type adnlClient interface {
	Peer
	processPacket(packet *PacketContent, ch *Channel) (err error)
}

type peerConn struct {
	addr      unsafe.Pointer
	channelId string
	clientId  string
	server    *Gateway
	client    adnlClient
}

func (p *peerConn) GetDisconnectHandler() func(addr string, key ed25519.PublicKey) {
	return p.client.GetDisconnectHandler()
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

	connHandler func(client Peer) error

	globalCtx       context.Context
	globalCtxCancel func()

	started bool
	mx      sync.RWMutex
}

func NewGateway(key ed25519.PrivateKey) *Gateway {
	if key == nil {
		panic("key is nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Gateway{
		key:             key,
		processors:      map[string]*srvProcessor{},
		peers:           map[string]*peerConn{},
		globalCtx:       ctx,
		globalCtxCancel: cancel,
	}
}

var RawListener = func(addr string) (net.PacketConn, error) {
	return net.ListenPacket("udp", addr)
}

func (g *Gateway) GetAddressList() address.List {
	return g.addrList
}

func (g *Gateway) SetExternalIP(ip net.IP) {
	g.dhtIP = ip
}

func (g *Gateway) StartServer(listenAddr string) (err error) {
	adr := strings.Split(listenAddr, ":")
	if len(adr) != 2 {
		return fmt.Errorf("invalid listen address")
	}

	ip := g.dhtIP
	if ip == nil {
		ip = net.ParseIP(adr[0])
	}
	ip = ip.To4()

	if ip.Equal(net.IPv4zero) {
		return fmt.Errorf("external ip cannot be 0.0.0.0, set it explicitly")
	}
	port, err := strconv.ParseUint(adr[1], 10, 16)
	if err != nil {
		return fmt.Errorf("invalid listen port")
	}

	tm := int32(time.Now().Unix())
	g.addrList = address.List{
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

	g.conn, err = RawListener(listenAddr)
	if err != nil {
		return err
	}

	rootId, err := tl.Hash(PublicKeyED25519{Key: g.key.Public().(ed25519.PublicKey)})
	if err != nil {
		return err
	}

	g.mx.Lock()
	defer g.mx.Unlock()

	if g.started {
		return fmt.Errorf("already started")
	}
	g.started = true

	go g.listen(rootId)

	return nil
}

func (g *Gateway) StartClient() (err error) {
	tm := int32(time.Now().Unix())
	g.addrList = address.List{
		Addresses:  []*address.UDP{}, // no specified addresses, we are acting as a client
		Version:    tm,
		ReinitDate: tm,
		Priority:   0,
		ExpireAt:   0,
	}

	// listen all addresses, port will be auto-chosen
	g.conn, err = RawListener(":")
	if err != nil {
		return err
	}

	rootId, err := tl.Hash(PublicKeyED25519{Key: g.key.Public().(ed25519.PublicKey)})
	if err != nil {
		return err
	}

	g.mx.Lock()
	defer g.mx.Unlock()

	if g.started {
		return fmt.Errorf("already started")
	}
	g.started = true

	go g.listen(rootId)

	return nil
}

func (g *Gateway) listen(rootId []byte) {
	lastListCheck := time.Now()
	for {
		buf := make([]byte, 4096)
		n, addr, err := g.conn.ReadFrom(buf)
		if err != nil {
			select {
			case <-g.globalCtx.Done():
				return
			default:
			}

			Logger("failed to read packet:", err)
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

			data, err := decodePacket(g.key, buf)
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

				peerId, err = tl.Hash(PublicKeyED25519{Key: packet.From.Key})
				if err != nil {
					// invalid packet
					continue
				}
			}

			g.mx.RLock()
			cli := g.peers[string(peerId)]
			g.mx.RUnlock()

			if cli == nil {
				if packet.From == nil {
					// invalid packet
					continue
				}

				cli, err = g.registerClient(addr, packet.From.Key, string(peerId))
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

		g.mx.RLock()
		proc := g.processors[string(id)]
		g.mx.RUnlock()

		if proc == nil {
			Logger("no processor for ADNL packet from", addr.String(), hex.EncodeToString(id))
			continue
		}

		now := time.Now()
		proc.lastPacketAt = now

		if now.Sub(lastListCheck) > 10*time.Second {
			lastListCheck = now
			var prc []*srvProcessor

			g.mx.Lock()
			for k, pr := range g.processors {
				if now.Sub(pr.lastPacketAt) > 180*time.Minute {
					prc = append(prc, pr)
					delete(g.processors, k)
				}
			}
			g.mx.Unlock()

			if len(prc) > 0 {
				go func() {
					for _, pr := range prc {
						pr.closer()
					}
				}()
			}
		}

		// TODO: processors pool
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

func (p *peerConn) checkUpdateAddr(addr net.Addr) {
	// we use atomic to safely swap address without lock, in case of change
	currentAddr := *(*net.Addr)(atomic.LoadPointer(&p.addr))
	if currentAddr.String() != addr.String() {
		atomic.StorePointer(&p.addr, unsafe.Pointer(&addr))
	}
}

func (g *Gateway) registerClient(addr net.Addr, key ed25519.PublicKey, id string) (*peerConn, error) {
	g.mx.Lock()
	defer g.mx.Unlock()

	peer := g.peers[id]
	if peer != nil {
		peer.checkUpdateAddr(addr)
		return peer, nil
	}

	peer = &peerConn{
		addr:     unsafe.Pointer(&addr),
		clientId: id,
		server:   g,
	}

	addrList := g.addrList
	addrList.ReinitDate = int32(time.Now().Unix())
	addrList.Version = addrList.ReinitDate

	a := initADNL(g.key)
	a.SetAddresses(addrList)
	a.peerKey = key
	a.addr = addr.String()
	a.writer = newWriter(func(p []byte, deadline time.Time) (err error) {
		currentAddr := *(*net.Addr)(atomic.LoadPointer(&peer.addr))
		return g.write(deadline, currentAddr, p)
	}, a.Close)
	peer.client = a

	g.peers[id] = peer

	// setup basic disconnect handler to auto-cleanup processors list
	peer.SetDisconnectHandler(nil)

	a.SetChannelReadyHandler(func(ch *Channel) {
		oldId := peer.channelId

		chID := string(ch.id)
		peer.channelId = chID

		g.mx.Lock()
		if oldId != "" {
			delete(g.processors, oldId)
		}
		g.processors[chID] = &srvProcessor{
			processor:    ch.process,
			lastPacketAt: time.Now(),
			closer:       ch.adnl.Close,
		}
		g.mx.Unlock()
	})

	connHandler := g.connHandler
	if connHandler != nil {
		go func() {
			if err := connHandler(peer); err != nil {
				// close connection if connection handler reports an error
				a.Close()
			}
		}()
	}

	return peer, nil
}

func (g *Gateway) RegisterClient(addr string, key ed25519.PublicKey) (Peer, error) {
	pAddr, err := netip.ParseAddrPort(addr)
	if err != nil {
		return nil, err
	}
	udpAddr := net.UDPAddrFromAddrPort(pAddr)

	clientId, err := tl.Hash(PublicKeyED25519{Key: key})
	if err != nil {
		return nil, err
	}

	return g.registerClient(udpAddr, key, string(clientId))
}

func (g *Gateway) SetConnectionHandler(handler func(client Peer) error) {
	g.connHandler = handler
}

func (g *Gateway) Close() error {
	g.mx.Lock()
	defer g.mx.Unlock()

	g.globalCtxCancel()

	if g.conn == nil {
		return nil
	}

	return g.conn.Close()
}

func (g *Gateway) write(deadline time.Time, addr net.Addr, buf []byte) error {
	g.mx.Lock()
	defer g.mx.Unlock()

	if g.conn == nil {
		return fmt.Errorf("no active socket connection")
	}

	_ = g.conn.SetWriteDeadline(deadline)
	n, err := g.conn.WriteTo(buf, addr)
	if err != nil {
		return err
	}

	if n != len(buf) {
		return fmt.Errorf("too big packet")
	}

	return nil
}

func (g *Gateway) GetID() []byte {
	id, _ := tl.Hash(PublicKeyED25519{Key: g.key.Public().(ed25519.PublicKey)})
	return id
}

func (p *peerConn) GetID() []byte {
	return p.client.GetID()
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

func (p *peerConn) GetQueryHandler() func(msg *MessageQuery) error {
	return p.client.GetQueryHandler()
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
