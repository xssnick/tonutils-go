package adnl

import (
	"bytes"
	"container/list"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
	"net"
	"net/netip"
	"strconv"
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
	SendNop(ctx context.Context) error
	Query(ctx context.Context, req, result tl.Serializable) error
	Answer(ctx context.Context, queryID []byte, result tl.Serializable) error
	Ping(ctx context.Context) (time.Duration, error)
	GetQueryHandler() func(msg *MessageQuery) error
	GetCloserCtx() context.Context
	SetAddresses(addresses address.List)
	RemoteAddr() string
	GetID() []byte
	GetPubKey() ed25519.PublicKey
	Stats() PeerStats
	Reinit()
	Close()
}

type adnlClient interface {
	Peer
	processPacket(packet *PacketContent, fromChannel bool) (err error)
	noteInboundPacket(size int)
	noteInboundError(now time.Time)
}

type peerConn struct {
	addr         unsafe.Pointer
	channelId    string
	channel      *Channel
	clientId     string
	lastPacketAt int64
	pendingElem  *list.Element
	server       *Gateway
	client       adnlClient
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

func (p *peerConn) Ping(ctx context.Context) (time.Duration, error) {
	return p.client.Ping(ctx)
}

func (p *peerConn) SendNop(ctx context.Context) error {
	return p.client.SendNop(ctx)
}

type srvProcessor struct {
	lastPacketAt int64
	channel      *Channel
	processor    func(buf []byte) error
	closer       func()
}

type UDPPacket struct {
	from net.Addr
	data []byte
	n    int
}

type Gateway struct {
	addrList     unsafe.Pointer
	id           []byte
	key          ed25519.PrivateKey
	processors   map[string]*srvProcessor
	peers        map[string]*peerConn
	pendingPeers *list.List

	connHandler unsafe.Pointer // func(client Peer) error

	globalCtx       context.Context
	globalCtxCancel func()

	started bool
	mx      sync.RWMutex

	reader         NetManager
	onChannelOpen  func(ch *Channel)
	onChannelClose func(id string)

	maxPendingPeers int
	pendingPeerTTL  time.Duration
}

func NewGateway(key ed25519.PrivateKey) *Gateway {
	return NewGatewayWithNetManager(key, NewSingleNetReader(DefaultListener))
}

func NewGatewayWithNetManager(key ed25519.PrivateKey, reader NetManager) *Gateway {
	if key == nil {
		panic("key is nil")
	}

	id, err := tl.Hash(keys.PublicKeyED25519{Key: key.Public().(ed25519.PublicKey)})
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Gateway{
		id:              id,
		key:             key,
		processors:      map[string]*srvProcessor{},
		peers:           map[string]*peerConn{},
		pendingPeers:    list.New(),
		globalCtx:       ctx,
		globalCtxCancel: cancel,
		reader:          reader,
		maxPendingPeers: DefaultMaxPendingPeers,
		pendingPeerTTL:  DefaultPendingPeerTTL,
	}
}

var PacketsBufferSize = 128 * 1024
var DefaultUDPBufferSize = 32 << 20
var DefaultMaxPendingPeers = 8192
var DefaultPendingPeerTTL = 30 * time.Second

var DefaultListener = func(addr string) (net.PacketConn, error) {
	lp, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, err
	}

	if conn, ok := lp.(*net.UDPConn); ok {
		if err := conn.SetReadBuffer(DefaultUDPBufferSize); err != nil {
			if Logger != nil {
				Logger("[ADNL] failed to set read buffer:", err)
			}
		}
		if err := conn.SetWriteBuffer(DefaultUDPBufferSize); err != nil {
			if Logger != nil {
				Logger("[ADNL] failed to set write buffer:", err)
			}
		}
	}

	return NewSyncConn(lp, PacketsBufferSize), nil
}

func (g *Gateway) GetAddressList() address.List {
	ptr := (*address.List)(atomic.LoadPointer(&g.addrList))
	if ptr == nil {
		return address.List{}
	}
	return *address.CloneList(ptr)
}

func (g *Gateway) GetPublicKey() ed25519.PublicKey {
	return append(ed25519.PublicKey(nil), g.key.Public().(ed25519.PublicKey)...)
}

func (g *Gateway) setupChannelCallbacks(onOpen func(ch *Channel), onClose func(id string)) {
	g.onChannelOpen = onOpen
	g.onChannelClose = onClose
}

func (g *Gateway) SetAddressList(addresses []address.Address) {
	g.mx.Lock()
	defer g.mx.Unlock()

	tm := int32(time.Now().Unix())
	list := address.List{
		Addresses:  append([]address.Address(nil), addresses...),
		Version:    tm,
		ReinitDate: tm,
		Priority:   0,
		ExpireAt:   0,
	}
	list = *address.CloneList(&list)
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

	host, portStr, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return fmt.Errorf("invalid listen address")
	}

	if g.addrList == nil {
		tm := int32(time.Now().Unix())
		ip := net.ParseIP(host)

		if ip != nil && !ip.IsUnspecified() {
			port, err := strconv.ParseUint(portStr, 10, 16)
			if err != nil {
				return fmt.Errorf("invalid listen port")
			}

			addr, err := address.NewAddress(ip, int32(port))
			if err != nil {
				return err
			}

			g.addrList = unsafe.Pointer(&address.List{
				Addresses:  []address.Address{addr},
				Version:    tm,
				ReinitDate: tm,
				Priority:   0,
				ExpireAt:   0,
			})
		} else {
			// We may listen on 0.0.0.0/[::] or an ephemeral port before a public
			// address is known, but peer/client initialization still requires a
			// non-nil address list.
			g.addrList = unsafe.Pointer(&address.List{
				Addresses:  []address.Address{},
				Version:    tm,
				ReinitDate: tm,
				Priority:   0,
				ExpireAt:   0,
			})
		}
	}

	if err = g.reader.InitConnection(g, listenAddr); err != nil {
		return err
	}
	g.started = true

	go g.startOldPeersChecker()
	for i := 0; i < threads; i++ {
		go g.listen(g.GetID())
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
			Addresses:  []address.Address{}, // no specified addresses, we are acting as a client
			Version:    tm,
			ReinitDate: tm,
			Priority:   0,
			ExpireAt:   0,
		})
	}

	// listen all addresses, port will be auto-chosen
	if err = g.reader.InitConnection(g, ":"); err != nil {
		return err
	}
	g.started = true

	go g.startOldPeersChecker()
	for i := 0; i < threads; i++ {
		go g.listen(g.GetID())
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

		if pk.n < 32 {
			continue
		}

		buf := pk.data[:pk.n]
		id := buf[:32]
		buf = buf[32:]

		if bytes.Equal(rootId, id) {
			if len(buf) < 64 {
				// too small packet
				continue
			}

			if Logger != nil {
				Logger("[ADNL DEBUG] gateway root packet", "len", len(buf), "from", pk.from.String())
			}

			data, err := decodePacket(g.key, buf)
			if err != nil {
				if Logger != nil {
					Logger("failed to decode packet:", err.Error())
				}
				continue
			}

			packet, err := parsePacket(data)
			if err != nil {
				if Logger != nil {
					Logger("failed to parse packet:", err.Error())
				}
				continue
			}
			if Logger != nil {
				Logger(
					"[ADNL DEBUG] gateway parsed root packet",
					"from_full", packet.From != nil,
					"from_short", packet.FromIDShort != nil,
					"msgs", len(packet.Messages),
					"seqno", packet.SeqnoValue(),
					"confirm_seqno", packet.ConfirmSeqnoValue(),
					"reinit", packet.ReinitDateValue(),
					"dst_reinit", packet.DstReinitDateValue(),
				)
			}

			var (
				cli     *peerConn
				peerKey ed25519.PublicKey
				peerId  []byte
			)
			if packet.From != nil {
				peerKey = append(ed25519.PublicKey(nil), packet.From.Key...)
				peerId, err = tl.Hash(keys.PublicKeyED25519{Key: peerKey})
				if err != nil {
					if Logger != nil {
						Logger("invalid peer id, err:", err.Error())
					}
					continue
				}
			} else if packet.FromIDShort != nil {
				peerId = append([]byte(nil), packet.FromIDShort...)

				g.mx.RLock()
				cli = g.peers[*(*string)(unsafe.Pointer(&peerId))]
				g.mx.RUnlock()
				if cli == nil {
					continue
				}

				peerKey = cli.client.GetPubKey()
				if len(peerKey) != ed25519.PublicKeySize {
					continue
				}
			} else {
				continue
			}

			if err = packet.verifySignature(peerKey); err != nil {
				if Logger != nil {
					Logger("failed to verify packet signature:", err.Error())
				}
				continue
			}

			if cli == nil {
				cli, err = g.registerClient(pk.from, peerKey, string(peerId))
				if err != nil {
					if Logger != nil {
						Logger("failed to register client:", err.Error())
					}
					continue
				}
			} else {
				atomic.StoreInt64(&cli.lastPacketAt, time.Now().UnixNano())
				cli.checkUpdateAddr(pk.from)
			}
			cli.client.noteInboundPacket(pk.n)

			err = cli.client.processPacket(packet, false)
			if err != nil {
				cli.client.noteInboundError(time.Now())
				if Logger != nil {
					Logger("failed to process ADNL packet",
						"peer", hex.EncodeToString(peerId),
						"from", pk.from.String(),
						"error", err.Error())
				}
				continue
			}

			continue
		}

		g.mx.RLock()
		proc := g.processors[*(*string)(unsafe.Pointer(&id))]
		g.mx.RUnlock()

		if proc == nil {
			if Logger != nil {
				Logger(
					"failed to find ADNL channel processor",
					"id", hex.EncodeToString(id),
					"from", pk.from.String(),
				)
			}
			continue
		}

		atomic.StoreInt64(&proc.lastPacketAt, time.Now().Unix())
		if err := proc.processor(buf); err != nil {
			if Logger != nil {
				Logger(
					"failed to process ADNL channel packet",
					"id", hex.EncodeToString(id),
					"from", pk.from.String(),
					"error", err.Error(),
				)
			}
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

func (g *Gateway) touchPendingPeerLocked(peer *peerConn, now int64) {
	atomic.StoreInt64(&peer.lastPacketAt, now)
	if peer.pendingElem != nil {
		g.pendingPeers.MoveToBack(peer.pendingElem)
	}
}

func (g *Gateway) removePendingPeerLocked(peer *peerConn) {
	if peer.pendingElem == nil {
		return
	}

	g.pendingPeers.Remove(peer.pendingElem)
	peer.pendingElem = nil
}

func (g *Gateway) dropPeerLocked(peer *peerConn) bool {
	g.removePendingPeerLocked(peer)
	if g.peers[peer.clientId] != peer {
		return false
	}

	delete(g.peers, peer.clientId)
	return true
}

func (g *Gateway) evictPendingPeersLocked() []*peerConn {
	if g.maxPendingPeers <= 0 {
		return nil
	}

	var evicted []*peerConn
	for g.pendingPeers.Len() > g.maxPendingPeers {
		peer := g.pendingPeers.Front().Value.(*peerConn)
		if g.dropPeerLocked(peer) {
			evicted = append(evicted, peer)
		}
	}
	return evicted
}

func (g *Gateway) collectIdlePendingPeersLocked(now int64) []*peerConn {
	if g.pendingPeerTTL <= 0 {
		return nil
	}

	var idle []*peerConn
	ttl := g.pendingPeerTTL.Nanoseconds()
	for e := g.pendingPeers.Front(); e != nil; {
		next := e.Next()
		peer := e.Value.(*peerConn)
		if now-atomic.LoadInt64(&peer.lastPacketAt) > ttl && g.dropPeerLocked(peer) {
			idle = append(idle, peer)
		}
		e = next
	}
	return idle
}

func closePeers(peers []*peerConn) {
	for _, peer := range peers {
		peer.client.Close()
	}
}

func (g *Gateway) startOldPeersChecker() {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-g.globalCtx.Done():
			return
		case <-t.C:
		}

		now := time.Now()

		var prc []*srvProcessor
		var peers []*peerConn
		g.mx.Lock()
		for k, pr := range g.processors {
			if now.Unix()-atomic.LoadInt64(&pr.lastPacketAt) > 10*60 {
				prc = append(prc, pr)
				delete(g.processors, k)

				if g.onChannelClose != nil {
					g.onChannelClose(k)
				}
			}
		}
		peers = g.collectIdlePendingPeersLocked(now.UnixNano())
		g.mx.Unlock()

		if len(prc) > 0 {
			for _, pr := range prc {
				pr.closer()
			}
		}
		closePeers(peers)
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
	if a, ok := addr.(*net.UDPAddr); !ok {
		return nil, fmt.Errorf("invalid address type")
	} else if a.IP == nil || a.IP.IsUnspecified() {
		return nil, fmt.Errorf("zero address is invalid")
	}

	g.mx.Lock()
	now := time.Now().UnixNano()
	peer := g.peers[id]
	if peer != nil {
		g.touchPendingPeerLocked(peer, now)
		g.mx.Unlock()
		peer.checkUpdateAddr(addr)
		return peer, nil
	}

	peer = &peerConn{
		addr:         unsafe.Pointer(&addr),
		clientId:     id,
		lastPacketAt: now,
		server:       g,
	}

	addrList := *(*address.List)(atomic.LoadPointer(&g.addrList))

	a := g.initADNL()

	peerId, err := tl.Hash(keys.PublicKeyED25519{Key: key})
	if err != nil {
		g.mx.Unlock()
		return nil, err
	}

	a.peerID = peerId
	a.peerKey = key
	a.peerKeyX25519, err = keys.Ed25519PubToX25519(key)
	if err != nil {
		g.mx.Unlock()
		return nil, err
	}

	a.SetAddresses(addrList)

	a.addr = addr.String()
	a.writer = newWriter(func(p []byte, deadline time.Time) (err error) {
		currentAddr := *(*net.Addr)(atomic.LoadPointer(&peer.addr))
		return g.write(currentAddr, p)
	}, a.Close)
	peer.client = a

	g.peers[id] = peer
	peer.pendingElem = g.pendingPeers.PushBack(peer)
	evicted := g.evictPendingPeersLocked()

	// setup basic disconnect handler to auto-cleanup processors list
	peer.SetDisconnectHandler(nil)

	a.channelReady = func(ch *Channel) {
		chID := string(ch.id)

		g.mx.Lock()
		if g.peers[peer.clientId] != peer {
			g.mx.Unlock()
			return
		}

		oldId := peer.channelId
		oldChannel := peer.channel
		peer.channelId = chID
		peer.channel = ch
		g.removePendingPeerLocked(peer)

		closedOld := false
		if oldId != "" && oldChannel != ch {
			if processor := g.processors[oldId]; processor != nil && processor.channel == oldChannel {
				delete(g.processors, oldId)
				closedOld = true
			}
		}
		g.processors[chID] = &srvProcessor{
			processor:    ch.process,
			channel:      ch,
			lastPacketAt: time.Now().Unix(),
			closer:       ch.adnl.Close,
		}
		if closedOld && g.onChannelClose != nil {
			g.onChannelClose(oldId)
		}
		if g.onChannelOpen != nil {
			g.onChannelOpen(ch)
		}
		g.mx.Unlock()
	}
	a.channelClosed = func(ch *Channel) {
		chID := string(ch.id)

		g.mx.Lock()
		if g.peers[peer.clientId] != peer || peer.channel != ch {
			g.mx.Unlock()
			return
		}

		peer.channelId = ""
		peer.channel = nil
		processor := g.processors[chID]
		closed := processor != nil && processor.channel == ch
		if closed {
			delete(g.processors, chID)
		}
		if closed && g.onChannelClose != nil {
			g.onChannelClose(chID)
		}
		g.mx.Unlock()
	}

	connHandler := atomic.LoadPointer(&g.connHandler)
	g.mx.Unlock()
	closePeers(evicted)

	if connHandler != nil {
		type handlerFunc func(client Peer) error

		ch := *(*handlerFunc)(connHandler)
		if err := ch(peer); err != nil {
			// close connection if connection handler reports an error
			a.Close()
			return nil, err
		}
	}

	return peer, nil
}

func (g *Gateway) RegisterClient(addr string, key ed25519.PublicKey) (Peer, error) {
	pAddr, err := netip.ParseAddrPort(addr)
	if err != nil {
		return nil, err
	}
	udpAddr := net.UDPAddrFromAddrPort(pAddr)

	clientId, err := tl.Hash(keys.PublicKeyED25519{Key: key})
	if err != nil {
		return nil, err
	}

	return g.registerClient(udpAddr, key, string(clientId))
}

func (g *Gateway) SetConnectionHandler(handler func(client Peer) error) {
	atomic.StorePointer(&g.connHandler, unsafe.Pointer(&handler))
}

func (g *Gateway) Close() error {
	g.mx.Lock()
	g.globalCtxCancel()

	peers := make([]*peerConn, 0, len(g.peers))
	for _, peer := range g.peers {
		peers = append(peers, peer)
		g.removePendingPeerLocked(peer)
	}
	g.peers = map[string]*peerConn{}
	g.pendingPeers.Init()
	g.mx.Unlock()

	for _, peer := range peers {
		peer.client.Close()
	}

	g.mx.Lock()
	g.processors = map[string]*srvProcessor{}
	g.mx.Unlock()

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
	return append([]byte{}, g.id...)
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

func (p *peerConn) Stats() PeerStats {
	return p.client.Stats()
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
		p.server.removePendingPeerLocked(p)
		delete(p.server.peers, p.clientId)
		if p.channelId != "" && p.channel != nil {
			processor := p.server.processors[p.channelId]
			ok := processor != nil && processor.channel == p.channel
			if ok {
				delete(p.server.processors, p.channelId)
			}
			if ok && p.server.onChannelClose != nil {
				p.server.onChannelClose(p.channelId)
			}
		}
		p.server.mx.Unlock()

		if handler != nil {
			// run it async to avoid potential deadlock issues in user code
			// in case closed under lock, and the same lock is used in handler
			go handler(addr, key)
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
