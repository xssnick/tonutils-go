package adnl

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
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
	GetCloserCtx() context.Context
	SetAddresses(addresses address.List)
	RemoteAddr() string
	GetID() []byte
	GetPubKey() ed25519.PublicKey
	Reinit()
	Close()
}

type adnlClient interface {
	Peer
	processPacket(packet *PacketContent, fromChannel bool) (err error)
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

func (p *peerConn) GetCloserCtx() context.Context {
	return p.client.GetCloserCtx()
}

func (p *peerConn) SetAddresses(addresses address.List) {
	p.client.SetAddresses(addresses)
}

type srvProcessor struct {
	lastPacketAt time.Time
	processor    func(buf []byte) error
	closer       func()
}

type UDPPacket struct {
	from net.Addr
	data []byte
	n    int
}

type Gateway struct {
	addrList   unsafe.Pointer
	key        ed25519.PrivateKey
	processors map[string]*srvProcessor
	peers      map[string]*peerConn

	connHandler func(client Peer) error

	globalCtx       context.Context
	globalCtxCancel func()

	started bool
	mx      sync.RWMutex

	reader         NetManager
	onChannelOpen  func(ch *Channel)
	onChannelClose func(id string)

	lastTrack int64
	packets   uint64
	packetsSz uint64
}

func NewGateway(key ed25519.PrivateKey) *Gateway {
	return NewGatewayWithNetManager(key, NewSingleNetReader(DefaultListener))
}

func NewGatewayWithNetManager(key ed25519.PrivateKey, reader NetManager) *Gateway {
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
		reader:          reader,
	}
}

var DefaultListener = func(addr string) (net.PacketConn, error) {
	lp, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, err
	}
	return NewSyncConn(lp, 512*1024), nil
}

func (g *Gateway) GetAddressList() address.List {
	if g.addrList == nil {
		return address.List{}
	}
	return *(*address.List)(atomic.LoadPointer(&g.addrList))
}

func (g *Gateway) setupChannelCallbacks(onOpen func(ch *Channel), onClose func(id string)) {
	g.onChannelOpen = onOpen
	g.onChannelClose = onClose
}

func (g *Gateway) SetAddressList(addresses []*address.UDP) {
	g.mx.Lock()
	defer g.mx.Unlock()

	tm := int32(time.Now().Unix())
	list := address.List{
		Addresses:  addresses,
		Version:    tm,
		ReinitDate: tm,
		Priority:   0,
		ExpireAt:   0,
	}
	atomic.StorePointer(&g.addrList, unsafe.Pointer(&list))

	for _, conn := range g.peers {
		// update addr for all peers
		conn.client.SetAddresses(list)
	}
}

func (g *Gateway) StartServer(listenAddr string, listenThreads ...int) (err error) {
	g.mx.Lock()
	defer g.mx.Unlock()

	if g.started {
		return errors.New("gateway is already started")
	}

	threads := 1
	if len(listenThreads) > 0 && listenThreads[0] > 0 {
		threads = listenThreads[0]
	}

	adr := strings.Split(listenAddr, ":")
	if len(adr) != 2 {
		return fmt.Errorf("invalid listen address")
	}

	if g.addrList == nil {
		ip := net.ParseIP(adr[0])

		if ip = ip.To4(); !ip.Equal(net.IPv4zero) {
			port, err := strconv.ParseUint(adr[1], 10, 16)
			if err != nil {
				return fmt.Errorf("invalid listen port")
			}

			tm := int32(time.Now().Unix())
			g.addrList = unsafe.Pointer(&address.List{
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
			})
		}
	}

	rootId, err := tl.Hash(PublicKeyED25519{Key: g.key.Public().(ed25519.PublicKey)})
	if err != nil {
		return err
	}

	if err = g.reader.InitConnection(g, listenAddr); err != nil {
		return err
	}
	g.started = true

	go g.startOldPeersChecker()
	for i := 0; i < threads; i++ {
		go g.listen(rootId)
	}

	return nil
}

func (g *Gateway) StartClient(listenThreads ...int) (err error) {
	g.mx.Lock()
	defer g.mx.Unlock()

	if g.started {
		return errors.New("gateway is already started")
	}

	threads := 1
	if len(listenThreads) > 0 && listenThreads[0] > 0 {
		threads = listenThreads[0]
	}

	if g.addrList == nil {
		tm := int32(time.Now().Unix())
		g.addrList = unsafe.Pointer(&address.List{
			Addresses:  []*address.UDP{}, // no specified addresses, we are acting as a client
			Version:    tm,
			ReinitDate: tm,
			Priority:   0,
			ExpireAt:   0,
		})
	}

	rootId, err := tl.Hash(PublicKeyED25519{Key: g.key.Public().(ed25519.PublicKey)})
	if err != nil {
		return err
	}

	// listen all addresses, port will be auto-chosen
	if err = g.reader.InitConnection(g, ":"); err != nil {
		return err
	}
	g.started = true

	go g.startOldPeersChecker()
	for i := 0; i < threads; i++ {
		go g.listen(rootId)
	}

	return nil
}

func (g *Gateway) listen(rootId []byte) {
	ch := g.reader.GetReaderChan(g)
	var pk *UDPPacket
	for {
		if pk != nil {
			g.reader.Free(pk)
		}

		select {
		case <-g.globalCtx.Done():
			return
		case pk = <-ch:
		}

		buf := pk.data[:pk.n]
		id := buf[:32]
		buf = buf[32:]

		if bytes.Equal(rootId, id) {
			if len(buf) < 32 {
				// too small packet
				continue
			}

			data, err := decodePacket(g.key, buf)
			if err != nil {
				Logger("failed to decode packet:", err.Error())
				continue
			}

			packet, err := parsePacket(data)
			if err != nil {
				Logger("failed to parse packet:", err.Error())
				continue
			}

			peerId := packet.FromIDShort
			if peerId == nil {
				if packet.From == nil {
					Logger("invalid packet source")
					continue
				}

				peerId, err = tl.Hash(PublicKeyED25519{Key: packet.From.Key})
				if err != nil {
					Logger("invalid peer id, err:", err.Error())
					continue
				}
			}

			g.mx.RLock()
			cli := g.peers[string(peerId)]
			g.mx.RUnlock()

			if cli == nil {
				if packet.From == nil {
					Logger("invalid packet source")
					continue
				}

				cli, err = g.registerClient(pk.from, packet.From.Key, string(peerId))
				if err != nil {
					Logger("failed to register client:", err.Error())
					continue
				}
			} else {
				cli.checkUpdateAddr(pk.from)
			}

			err = cli.client.processPacket(packet, false)
			if err != nil {
				Logger("failed to process ADNL packet:", err.Error())
				continue
			}

			continue
		}

		g.mx.RLock()
		proc := g.processors[string(id)]
		g.mx.RUnlock()

		if proc == nil {
			continue
		}

		proc.lastPacketAt = time.Now()
		if err := proc.processor(buf); err != nil {
			Logger("failed to process packet at server:", err)
		}
	}
}

func (p *peerConn) checkUpdateAddr(addr net.Addr) {
	// we use atomic to safely swap address without lock, in case of change
	currentAddr := *(*net.Addr)(atomic.LoadPointer(&p.addr))
	if currentAddr.String() != addr.String() {
		atomic.StorePointer(&p.addr, unsafe.Pointer(&addr))
	}
}

func (g *Gateway) startOldPeersChecker() {
	t := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-g.globalCtx.Done():
			return
		case <-t.C:
		}

		now := time.Now()

		var prc []*srvProcessor
		g.mx.Lock()
		for k, pr := range g.processors {
			if now.Sub(pr.lastPacketAt) > 10*time.Minute {
				prc = append(prc, pr)
				delete(g.processors, k)

				if g.onChannelClose != nil {
					g.onChannelClose(k)
				}
			}
		}
		g.mx.Unlock()

		if len(prc) > 0 {
			for _, pr := range prc {
				pr.closer()
			}
		}
	}
}

func (g *Gateway) GetActivePeers() []Peer {
	g.mx.RLock()
	defer g.mx.RUnlock()

	peers := make([]Peer, 0, len(g.peers))
	for _, p := range g.peers {
		peers = append(peers, p)
	}

	return peers
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

	addrList := *(*address.List)(atomic.LoadPointer(&g.addrList))
	addrList.ReinitDate = int32(time.Now().Unix())
	addrList.Version = addrList.ReinitDate

	a := g.initADNL()
	a.SetAddresses(addrList)
	a.peerKey = key
	a.addr = addr.String()
	a.writer = newWriter(func(p []byte, deadline time.Time) (err error) {
		currentAddr := *(*net.Addr)(atomic.LoadPointer(&peer.addr))
		return g.write(currentAddr, p)
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

			if g.onChannelClose != nil {
				g.onChannelClose(oldId)
			}
		}
		g.processors[chID] = &srvProcessor{
			processor:    ch.process,
			lastPacketAt: time.Now(),
			closer:       ch.adnl.Close,
		}
		if g.onChannelOpen != nil {
			g.onChannelOpen(ch)
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

	g.reader.CloseConnection(g)
	return nil
}

func (g *Gateway) write(addr net.Addr, buf []byte) error {
	n, err := g.reader.WritePacket(g, buf, addr)
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

func (p *peerConn) Reinit() {
	p.client.Reinit()
}

func (p *peerConn) GetPubKey() ed25519.PublicKey {
	return p.client.GetPubKey()
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
		if p.server.onChannelClose != nil {
			p.server.onChannelClose(p.channelId)
		}
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

func (p *peerConn) processPacket(packet *PacketContent, fromChannel bool) (err error) {
	return p.client.processPacket(packet, fromChannel)
}
