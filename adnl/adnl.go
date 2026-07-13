package adnl

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/tl"
)

const (
	_FlagFrom                uint32 = 0x1
	_FlagFromShort           uint32 = 0x2
	_FlagOneMessage          uint32 = 0x4
	_FlagMultipleMessages    uint32 = 0x8
	_FlagAddress             uint32 = 0x10
	_FlagPriorityAddress     uint32 = 0x20
	_FlagSeqno               uint32 = 0x40
	_FlagConfirmSeqno        uint32 = 0x80
	_FlagRecvAddrListVer     uint32 = 0x100
	_FlagRecvPriorityAddrVer uint32 = 0x200
	_FlagReinitDate          uint32 = 0x400
	_FlagSignature           uint32 = 0x800
	_FlagPriority            uint32 = 0x1000
	_FlagAll                 uint32 = 0x1fff
)

const BasePayloadMTU = 1024
const HugePacketMaxSz = 1024*8 + 128
const maxIncompleteMultipartMessages = 512
const respondWithNopDelay = 1500 * time.Millisecond
const idleReinitTimeout = 120 * time.Second
const idleReinitSilence = 5 * time.Second
const idleReinitScheduleDelay = 10 * time.Second
const idleReinitRetryMinDelay = 500 * time.Millisecond
const idleReinitRetryJitter = time.Second
const staleAddressListSilence = 9 * time.Minute
const staleAddressListDropDelay = time.Minute

type CustomMessageHandler func(msg *MessageCustom) error
type QueryHandler func(msg *MessageQuery) error
type DisconnectHandler func(addr string, key ed25519.PublicKey)

type PacketWriter interface {
	Write(b []byte, deadline time.Time) (n int, err error)
	Close() error
}

type ADNL struct {
	writer             PacketWriter
	ourKey             ed25519.PrivateKey
	addr               string
	prevPacketHeaderSz uint32

	closerCtx context.Context
	closeFn   context.CancelFunc

	channelPtr unsafe.Pointer

	msgParts map[string]*partitionedMessage

	reinitTime                   int32
	seqno                        int64
	confirmSeqno                 int64
	dstReinit                    int32
	recvAddrVer                  int32
	recvPriorityAddrVer          int32
	ourAddrVerOnPeerSide         int32
	ourPriorityAddrVerOnPeerSide int32
	respondWithNopAfter          int64
	lastReceivedPacket           int64
	tryReinitAt                  int64
	dropAddrListAt               int64

	peerID        []byte
	peerKey       ed25519.PublicKey
	peerKeyX25519 []byte
	ourAddresses  unsafe.Pointer

	activeQueries map[queryID]chan tl.Serializable
	activePings   map[int64]chan MessagePong

	queryMx sync.Mutex
	pingMx  sync.RWMutex

	customMessageHandler unsafe.Pointer // CustomMessageHandler
	queryHandler         unsafe.Pointer // QueryHandler
	onDisconnect         unsafe.Pointer // DisconnectHandler
	onChannel            func(ch *Channel)

	mx sync.RWMutex

	stats *peerStats
}

var Logger func(v ...any)

func (g *Gateway) initADNL() *ADNL {
	now := time.Now()
	tm := int32(now.Unix())
	closerCtx, closeFn := context.WithCancel(context.Background())
	return &ADNL{
		ourAddresses: unsafe.Pointer(&address.List{
			Version:    tm,
			ReinitDate: tm,
		}),
		reinitTime:    tm,
		ourKey:        g.key,
		closerCtx:     closerCtx,
		closeFn:       closeFn,
		msgParts:      make(map[string]*partitionedMessage, 128),
		activePings:   make(map[int64]chan MessagePong),
		activeQueries: map[queryID]chan tl.Serializable{},
		stats:         newPeerStats(now),
	}
}

func (a *ADNL) Close() {
	trigger := false

	a.mx.Lock()
	select {
	case <-a.closerCtx.Done():
	default:
		a.closeFn()

		con := a.writer
		if con != nil {
			_ = con.Close()
		}

		trigger = true
	}
	a.mx.Unlock()

	if d := a.GetDisconnectHandler(); trigger && d != nil {
		d(a.addr, a.peerKey)
	}
}

func (a *ADNL) GetCloserCtx() context.Context {
	return a.closerCtx
}

func (c *Channel) process(buf []byte) error {
	if c.wantConfirm.Load() {
		// we got message in channel, no more confirmations required
		c.wantConfirm.Store(false)
	}

	data, err := c.decodePacket(buf)
	if err != nil {
		c.adnl.noteInboundError(time.Now())
		return fmt.Errorf("failed to decode packet: %w", err)
	}

	packet, err := parsePacket(data)
	if err != nil {
		c.adnl.noteInboundError(time.Now())
		return fmt.Errorf("failed to parse packet: %w", err)
	}
	c.adnl.noteInboundPacket(len(buf) + 32)

	err = c.adnl.processPacket(packet, true)
	if err != nil {
		c.adnl.noteInboundError(time.Now())
		return fmt.Errorf("failed to process packet: %w", err)
	}
	return nil
}

func (a *ADNL) processPacket(packet *PacketContent, fromChannel bool) (err error) {
	a.noteReceivedPacket()

	if !fromChannel && packet.From != nil {
		if err = a.learnPeerSource(packet.From.Key); err != nil {
			return err
		}
	}

	ourReinit := atomic.LoadInt32(&a.reinitTime)
	if packet.DstReinitDate != nil && *packet.DstReinitDate > ourReinit {
		if Logger != nil {
			Logger(
				"[ADNL DEBUG] drop packet with too new dst reinit",
				"packet_reinit", packet.ReinitDateValue(),
				"packet_dst_reinit", packet.DstReinitDateValue(),
				"our_reinit", ourReinit,
			)
		}
		return nil
	}
	if packet.ReinitDate != nil && *packet.ReinitDate > int32(time.Now().Unix()+60) {
		if Logger != nil {
			Logger(
				"[ADNL DEBUG] drop packet with too new peer reinit",
				"packet_reinit", packet.ReinitDateValue(),
				"packet_dst_reinit", packet.DstReinitDateValue(),
			)
		}
		return nil
	}
	if packet.ReinitDate != nil && *packet.ReinitDate > atomic.LoadInt32(&a.dstReinit) {
		a.applyPeerReinit(*packet.ReinitDate)
	}
	if packet.ReinitDate != nil && *packet.ReinitDate > 0 && *packet.ReinitDate < atomic.LoadInt32(&a.dstReinit) {
		if Logger != nil {
			Logger(
				"[ADNL DEBUG] drop packet with old peer reinit",
				"packet_reinit", packet.ReinitDateValue(),
				"peer_reinit", atomic.LoadInt32(&a.dstReinit),
			)
		}
		return nil
	}

	if packet.DstReinitDate != nil && *packet.DstReinitDate > 0 && *packet.DstReinitDate < atomic.LoadInt32(&a.reinitTime) {
		if Logger != nil {
			Logger(
				"[ADNL DEBUG] early reinit path",
				"packet_reinit", packet.ReinitDateValue(),
				"packet_dst_reinit", packet.DstReinitDateValue(),
				"our_reinit", atomic.LoadInt32(&a.reinitTime),
			)
		}
		if packet.ReinitDate != nil {
			atomic.StoreInt32(&a.dstReinit, *packet.ReinitDate)
		}
		a.notePeerAddressList(packet)

		seqno := atomic.AddInt64(&a.seqno, 1)
		buf, err := a.createPacketWithOptions(seqno, rootPacketOptions{
			forceAddress:          true,
			resetPeerAddrVersions: true,
		}, MessageNop{})
		if err != nil {
			return fmt.Errorf("failed to create packet: %w", err)
		}
		if err = a.send(buf); err != nil {
			return fmt.Errorf("failed to send ping reinit: %w", err)
		}

		return nil
	}

	var seqno int64
	if packet.Seqno != nil {
		seqno = *packet.Seqno
	}
	if packet.ConfirmSeqno != nil {
		atomicMaxInt64(&a.stats.peerConfirmSeqno, *packet.ConfirmSeqno)
	}

	for { // to guarantee swap to highest
		if conf := atomic.LoadInt64(&a.confirmSeqno); seqno > conf {
			if !atomic.CompareAndSwapInt64(&a.confirmSeqno, conf, seqno) {
				continue
			}
		}
		break
	}

	a.noteAddressListVersions(packet)
	a.notePeerAddressList(packet)

	for i, message := range packet.Messages {
		err = a.processMessage(message)
		if err != nil {
			return fmt.Errorf("failed to process message %d %s: %v", i, reflect.TypeOf(message), err)
		}
	}

	return nil
}

func (a *ADNL) processMessage(message any) error {
	switch ms := message.(type) {
	case MessagePong:
		a.pingMx.RLock()
		ch := a.activePings[ms.Value]
		a.pingMx.RUnlock()

		if ch != nil {
			select {
			case ch <- ms:
			default:
			}
		}
	case MessagePing:
		buf, err := a.buildRequest(MessagePong{Value: ms.Value})
		if err != nil {
			return fmt.Errorf("failed to build pong request: %w", err)
		}

		if err = a.send(buf); err != nil {
			return fmt.Errorf("failed to send pong: %w", err)
		}
	case MessageQuery:
		a.tryRespondWithNop()
		qh := atomic.LoadPointer(&a.queryHandler)
		if qh != nil {
			h := *(*QueryHandler)(qh)
			if err := h(&ms); err != nil {
				return fmt.Errorf("failed to handle query: %w", err)
			}
		} else if Logger != nil {
			Logger("[ADNL DEBUG] query handler is nil", hex.EncodeToString(ms.ID))
		}
	case MessageAnswer:
		a.tryRespondWithNop()
		a.processAnswer(ms.ID, ms.Data)
	case MessageCreateChannel:
		a.mx.Lock()
		defer a.mx.Unlock()

		ch := (*Channel)(atomic.LoadPointer(&a.channelPtr))

		var err error
		var key ed25519.PrivateKey
		initDate := int32(time.Now().Unix())
		if ch != nil {
			if bytes.Equal(ch.peerKey, ms.Key) {
				// already initialized on our side, but client missed confirmation,
				// channel is already known, so more confirmations will be sent in the next packets
				return nil
			}
			if ch.peerDate > 0 && ms.Date <= ch.peerDate {
				return nil
			}
			// looks like channel was lost on the other side, we will reinit it
			key = ch.key
			initDate = ch.initDate
		} else {
			_, key, err = ed25519.GenerateKey(nil)
			if err != nil {
				return err
			}
		}

		newChan := &Channel{
			adnl:     a,
			key:      key,
			initDate: initDate,
			peerDate: ms.Date,
		}
		newChan.wantConfirm.Store(true)

		if err = newChan.setup(ms.Key); err != nil {
			return fmt.Errorf("failed to setup channel: %w", err)
		}

		atomic.StorePointer(&a.channelPtr, unsafe.Pointer(newChan))
	case MessageConfirmChannel:
		a.mx.Lock()
		defer a.mx.Unlock()

		ch := (*Channel)(atomic.LoadPointer(&a.channelPtr))

		if ch == nil || !bytes.Equal(ch.key.Public().(ed25519.PublicKey), ms.PeerKey) {
			return fmt.Errorf("confirmation for unknown channel %s", hex.EncodeToString(ms.PeerKey))
		}

		if bytes.Equal(ch.peerKey, ms.Key) && ch.ready.Load() {
			// not required confirmation, skip it
			return nil
		}
		if !bytes.Equal(ch.peerKey, ms.Key) && ch.peerDate > 0 && ms.Date <= ch.peerDate {
			return nil
		}

		if !bytes.Equal(ch.peerKey, ms.Key) {
			newChan := &Channel{
				adnl:     a,
				key:      ch.key,
				initDate: ch.initDate,
				peerDate: ms.Date,
			}
			if err := newChan.setup(ms.Key); err != nil {
				return fmt.Errorf("failed to setup channel: %w", err)
			}
			atomic.StorePointer(&a.channelPtr, unsafe.Pointer(newChan))
			return nil
		}

		ch.peerDate = ms.Date
		if err := ch.setup(ms.Key); err != nil {
			return fmt.Errorf("failed to setup channel: %w", err)
		}
	case MessageReinit:
		a.applyPeerReinit(ms.Date)
	case MessagePart:
		a.tryRespondWithNop()
		msgID := string(ms.Hash)
		if ms.TotalSize <= 0 || ms.TotalSize > HugePacketMaxSz {
			return fmt.Errorf("skip invalid partitioned message with len %d bytes", ms.TotalSize)
		}

		a.mx.Lock()
		p, ok := a.msgParts[msgID]
		if !ok {
			if len(a.msgParts) >= maxIncompleteMultipartMessages {
				// cleanup old stuck messages
				tm := time.Now().Add(-7 * time.Second)
				for s, pt := range a.msgParts {
					if tm.After(pt.startedAt) {
						delete(a.msgParts, s)
					}
				}

				if len(a.msgParts) >= maxIncompleteMultipartMessages {
					a.mx.Unlock()
					return fmt.Errorf("too many incomplete messages")
				}
			}

			p = newPartitionedMessage(ms.TotalSize)
			a.msgParts[msgID] = p
		}
		a.mx.Unlock()

		ready, err := p.AddPart(ms.Offset, ms.Data)
		if err != nil {
			return fmt.Errorf("failed to add message part: %w", err)
		}

		if ready {
			a.mx.Lock()
			delete(a.msgParts, msgID)
			a.mx.Unlock()

			data, err := p.Build(ms.Hash)
			if err != nil {
				return fmt.Errorf("failed to build final message from parts: %w", err)
			}

			var msg any
			_, err = tl.ParseNoCopy(&msg, data, true)
			if err != nil {
				return fmt.Errorf("failed to parse message answer from parts: %w", err)
			}

			switch msg.(type) {
			case MessagePart:
				return fmt.Errorf("message part cant be inside another part")
			}

			err = a.processMessage(msg)
			if err != nil {
				return fmt.Errorf("failed to process message built from parts: %w", err)
			}
		}
	case MessageCustom:
		a.tryRespondWithNop()
		ch := atomic.LoadPointer(&a.customMessageHandler)
		if ch != nil {
			h := *(*CustomMessageHandler)(ch)
			if err := h(&ms); err != nil {
				return fmt.Errorf("failed to handle custom message: %w", err)
			}
		}
	case MessageNop:
	default:
		return fmt.Errorf("skipped unprocessable message of type %s", reflect.TypeOf(message).String())
	}

	return nil
}

func (a *ADNL) learnPeerSource(key ed25519.PublicKey) error {
	a.mx.Lock()
	defer a.mx.Unlock()

	if a.peerKey != nil {
		return nil
	}

	peerID, err := tl.Hash(keys.PublicKeyED25519{Key: key})
	if err != nil {
		return err
	}
	peerKeyX25519, err := keys.Ed25519PubToX25519(key)
	if err != nil {
		return err
	}

	a.peerID = peerID
	a.peerKey = key
	a.peerKeyX25519 = peerKeyX25519
	return nil
}

func (a *ADNL) applyPeerReinit(date int32) {
	if date <= 0 {
		return
	}

	for {
		current := atomic.LoadInt32(&a.dstReinit)
		if current >= date {
			return
		}
		if !atomic.CompareAndSwapInt32(&a.dstReinit, current, date) {
			continue
		}
		break
	}

	a.resetChannelAfterPeerReinit()
	atomic.StoreInt64(&a.confirmSeqno, 0)
	a.stats.peerConfirmSeqno.Store(0)
	atomic.StoreInt32(&a.ourAddrVerOnPeerSide, 0)
	atomic.StoreInt32(&a.ourPriorityAddrVerOnPeerSide, 0)
	a.stats.notePeerReinit(time.Now())
}

func (a *ADNL) resetChannelAfterPeerReinit() {
	ch := (*Channel)(atomic.LoadPointer(&a.channelPtr))
	if ch == nil {
		return
	}

	pending := &Channel{
		adnl:     a,
		key:      ch.key,
		initDate: ch.initDate,
	}
	atomic.StorePointer(&a.channelPtr, unsafe.Pointer(pending))
}

func (a *ADNL) noteAddressListVersions(packet *PacketContent) {
	if packet.RecvAddrListVersion != nil {
		atomicMaxInt32(&a.ourAddrVerOnPeerSide, *packet.RecvAddrListVersion)
	}
	if packet.RecvPriorityAddrListVersion != nil {
		atomicMaxInt32(&a.ourPriorityAddrVerOnPeerSide, *packet.RecvPriorityAddrListVersion)
	}
}

func (a *ADNL) notePeerAddressList(packet *PacketContent) {
	if packet.Address != nil {
		atomicMaxInt32(&a.recvAddrVer, packet.Address.Version)
	}
	if packet.PriorityAddress != nil {
		atomicMaxInt32(&a.recvPriorityAddrVer, packet.PriorityAddress.Version)
	}
}

func atomicMaxInt32(ptr *int32, value int32) {
	for {
		current := atomic.LoadInt32(ptr)
		if value <= current {
			return
		}
		if atomic.CompareAndSwapInt32(ptr, current, value) {
			return
		}
	}
}

func atomicMaxInt64(ptr *atomic.Int64, value int64) {
	for {
		current := ptr.Load()
		if value <= current {
			return
		}
		if ptr.CompareAndSwap(current, value) {
			return
		}
	}
}

func (a *ADNL) SetCustomMessageHandler(handler func(msg *MessageCustom) error) {
	atomic.StorePointer(&a.customMessageHandler, unsafe.Pointer(&handler))
}

func (a *ADNL) SetQueryHandler(handler func(msg *MessageQuery) error) {
	atomic.StorePointer(&a.queryHandler, unsafe.Pointer(&handler))
}

func (a *ADNL) GetQueryHandler() func(msg *MessageQuery) error {
	h := atomic.LoadPointer(&a.queryHandler)
	if h == nil {
		return nil
	}
	return *(*QueryHandler)(h)
}

func (a *ADNL) SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey)) {
	atomic.StorePointer(&a.onDisconnect, unsafe.Pointer(&handler))
}

func (a *ADNL) GetDisconnectHandler() func(addr string, key ed25519.PublicKey) {
	d := (*DisconnectHandler)(atomic.LoadPointer(&a.onDisconnect))
	if d == nil {
		return nil
	}
	return *d
}

func (a *ADNL) SetChannelReadyHandler(handler func(ch *Channel)) {
	a.onChannel = handler
}

func (a *ADNL) RemoteAddr() string {
	return a.addr
}

func (a *ADNL) processAnswer(id []byte, query any) {
	queryID, okID := encodeQueryID(id)
	if !okID {
		return
	}

	a.queryMx.Lock()
	res, ok := a.activeQueries[queryID]
	if ok {
		delete(a.activeQueries, queryID)
	}
	a.queryMx.Unlock()

	if ok {
		if res != nil { // nil means - we did query, but dont want response
			res <- query
		}
	} else {
		// Logger("unknown response with id", id, a.addr, reflect.TypeOf(query).String())
	}
}

func (a *ADNL) SendCustomMessage(_ context.Context, req tl.Serializable) error {
	baseMTU := false

reSplit:
	packet, packets, err := a.buildRequestMaySplit(&MessageCustom{
		Data: req,
	}, baseMTU)
	if err != nil {
		return fmt.Errorf("failed to send custom message: %w", err)
	}

	if packet != nil {
		if err = a.send(packet); err != nil {
			if !baseMTU && errors.Is(err, ErrPacketBiggerThanMTU) {
				baseMTU = true
				goto reSplit
			}
			return fmt.Errorf("failed to send custom packet: %w", err)
		}
	} else {
		for _, packet := range packets {
			if err = a.send(packet); err != nil {
				if !baseMTU && errors.Is(err, ErrPacketBiggerThanMTU) {
					baseMTU = true
					goto reSplit
				}
				return fmt.Errorf("failed to send custom packet: %w", err)
			}
		}
	}
	return nil
}

func (a *ADNL) SendNop(_ context.Context) error {
	packet, err := a.buildRequest(MessageNop{})
	if err != nil {
		return fmt.Errorf("failed to build nop packet: %w", err)
	}
	if err = a.send(packet); err != nil {
		return fmt.Errorf("failed to send nop packet: %w", err)
	}
	return nil
}

func (a *ADNL) tryRespondWithNop() {
	if err := a.respondWithNop(); err != nil && Logger != nil {
		Logger("failed to send nop response:", err.Error())
	}
}

func (a *ADNL) respondWithNop() error {
	now := time.Now()
	until := atomic.LoadInt64(&a.respondWithNopAfter)
	if until > now.UnixNano() {
		return nil
	}
	if !atomic.CompareAndSwapInt64(&a.respondWithNopAfter, until, now.Add(respondWithNopDelay).UnixNano()) {
		return nil
	}

	packet, err := a.buildRequest(MessageNop{})
	if err != nil {
		return fmt.Errorf("failed to build nop response packet: %w", err)
	}
	return a.send(packet)
}

func (a *ADNL) Ping(ctx context.Context) (time.Duration, error) {
	val := time.Now().UnixNano()
	ch := make(chan MessagePong, 1)
	a.pingMx.Lock()
	a.activePings[val] = ch
	a.pingMx.Unlock()

	defer func() {
		a.pingMx.Lock()
		delete(a.activePings, val)
		a.pingMx.Unlock()
	}()

	for {
		req, err := a.buildRequest(MessagePing{
			Value: val,
		})
		if err != nil {
			return 0, err
		}

		try, cancel := context.WithTimeout(ctx, 1*time.Second)

		if err = a.send(req); err != nil {
			cancel()
			return 0, fmt.Errorf("failed to send ping packet: %w", err)
		}

		select {
		case <-ctx.Done():
			cancel()
			return 0, ctx.Err()
		case <-ch:
			cancel()
			return time.Since(time.Unix(0, val)), nil
		case <-try.Done():
			continue
		}
	}
}

func (a *ADNL) Query(ctx context.Context, req, result tl.Serializable) error {
	q, err := createQueryMessage(req)
	if err != nil {
		return fmt.Errorf("failed to create query message: %w", err)
	}

	res := make(chan tl.Serializable, 1)
	reqID, okID := encodeQueryID(q.ID)
	if !okID {
		return fmt.Errorf("invalid query id size")
	}

	a.queryMx.Lock()
	a.activeQueries[reqID] = res
	a.queryMx.Unlock()

	defer func() {
		a.queryMx.Lock()
		delete(a.activeQueries, reqID)
		a.queryMx.Unlock()
	}()

	baseMTU := false
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()

retry:
	for {
		packet, packets, err := a.buildRequestMaySplit(q, baseMTU)
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}

		if packet != nil {
			if err = a.send(packet); err != nil {
				if !baseMTU && errors.Is(err, ErrPacketBiggerThanMTU) {
					baseMTU = true
					continue
				}
				return fmt.Errorf("failed to send query packet 0: %w", err)
			}
		} else {
			for i, packet := range packets {
				if err = a.send(packet); err != nil {
					if !baseMTU && errors.Is(err, ErrPacketBiggerThanMTU) {
						baseMTU = true
						continue retry
					}
					return fmt.Errorf("failed to send query packet %d: %w", i, err)
				}
			}
		}

		select {
		case resp := <-res:
			if err, ok := resp.(error); ok {
				return err
			}
			reflect.ValueOf(result).Elem().Set(reflect.ValueOf(resp))
			return nil
		case <-ctx.Done():
			return fmt.Errorf("deadline exceeded, addr %s %s, err: %w", a.addr, hex.EncodeToString(a.peerKey), ctx.Err())
		case <-timer.C:
			timer.Reset(250 * time.Millisecond)
		}
	}
}

func (a *ADNL) Answer(ctx context.Context, queryID []byte, result tl.Serializable) error {
	baseMTU := false
reSplit:
	packet, packets, err := a.buildRequestMaySplit(&MessageAnswer{
		ID:   queryID,
		Data: result,
	}, baseMTU)
	if err != nil {
		return fmt.Errorf("send answer  failed: %w", err)
	}

	if packet != nil {
		if err = a.send(packet); err != nil {
			if !baseMTU && errors.Is(err, ErrPacketBiggerThanMTU) {
				baseMTU = true
				goto reSplit
			}
			return fmt.Errorf("failed to send answer: %w", err)
		}
	} else {
		for _, packet := range packets {
			if err = a.send(packet); err != nil {
				if !baseMTU && errors.Is(err, ErrPacketBiggerThanMTU) {
					baseMTU = true
					goto reSplit
				}
				return fmt.Errorf("failed to send answer: %w", err)
			}
		}
	}
	return nil
}

const MaxMTU = 1500 - 40 - 8 // max is for ipv6 over ethernet

func (a *ADNL) buildRequestMaySplit(req tl.Serializable, useBase bool) (packet []byte, packets [][]byte, err error) {
	msg, err := tl.Serialize(req, true)
	if err != nil {
		return nil, nil, err
	}

	mtu := BasePayloadMTU
	if !useBase { // useBase is true when packet is oversize, and we rebuild it with lower MTU
		if sz := atomic.LoadUint32(&a.prevPacketHeaderSz); sz > 0 {
			// trying to extend mtu to send use all possible mtu
			mtu = (MaxMTU - 32 - 64) - int(sz)
		}
	}

	if len(msg) > mtu {
		if len(msg) > HugePacketMaxSz {
			return nil, nil, fmt.Errorf("too big message payload")
		}

		parts := splitMessage(msg, mtu)
		packets = make([][]byte, 0, len(parts))
		for i, part := range parts {
			buf, err := a.buildRequest(part)
			if err != nil {
				return nil, nil, fmt.Errorf("filed to build message part %d, err: %w", i, err)
			}
			packets = append(packets, buf)
		}
		return nil, packets, nil
	}

	buf, err := a.buildRequest(tl.Raw(msg))
	if err != nil {
		return nil, nil, fmt.Errorf("filed to build message, err: %w", err)
	}
	return buf, nil, nil
}

func (a *ADNL) buildRequest(req tl.Serializable) (buf []byte, err error) {
	ch := (*Channel)(atomic.LoadPointer(&a.channelPtr))
	seqno := atomic.AddInt64(&a.seqno, 1)
	channelReady := ch != nil && ch.ready.Load()
	forceRoot := false
	rootOpts := rootPacketOptions{}
	if channelReady {
		forceRoot, rootOpts = a.rootReinitOptions(time.Now())
	}

	if channelReady && !forceRoot {
		if ch.wantConfirm.Load() {
			// if client not yet received confirmation - we will send it till his first packet in channel
			chMsg := &MessageConfirmChannel{
				Key:     ch.key.Public().(ed25519.PublicKey),
				PeerKey: ch.peerKey,
				Date:    ch.initDate,
			}

			buf, err = a.createPacket(seqno, chMsg, req)
			if err != nil {
				return nil, fmt.Errorf("failed to create packet: %w", err)
			}
			return buf, nil
		}

		// channel is active
		buf, err = ch.createPacket(seqno, req)
		if err != nil {
			return nil, fmt.Errorf("failed to create packet: %w", err)
		}
		return buf, nil
	}
	if channelReady && forceRoot {
		buf, err = a.createPacketWithOptions(seqno, rootOpts, req)
		if err != nil {
			return nil, fmt.Errorf("failed to create reinit packet: %w", err)
		}
		return buf, nil
	}

	// if it is not exists, we will create it,
	if ch == nil {
		_, key, err := ed25519.GenerateKey(nil)
		if err != nil {
			return nil, err
		}

		a.mx.Lock()
		ch = &Channel{
			adnl:     a,
			key:      key,
			initDate: int32(time.Now().Unix()),
		}
		atomic.StorePointer(&a.channelPtr, unsafe.Pointer(ch))
		a.mx.Unlock()
	}

	// if it is pending, we will send creation (can be sent multiple times, it is ok)
	chMsg := &MessageCreateChannel{
		Key:  ch.key.Public().(ed25519.PublicKey),
		Date: ch.initDate,
	}

	buf, err = a.createPacket(seqno, chMsg, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create packet: %w", err)
	}

	return buf, nil
}

var ErrPacketBiggerThanMTU = fmt.Errorf("packet bigger than MTU")

func (a *ADNL) send(buf []byte) error {
	if len(buf) > MaxMTU {
		return ErrPacketBiggerThanMTU
	}

	if n, err := a.writer.Write(buf, time.Time{}); err != nil {
		a.stats.noteOutboundError(time.Now())
		// not close on io timeout because it can be triggered by network overload
		if !strings.Contains(err.Error(), "i/o timeout") {
			// it should trigger disconnect handler in read routine
			a.Close()
		}
		return err
	} else if n != len(buf) {
		a.stats.noteOutboundError(time.Now())
		return fmt.Errorf("not full packet was written")
	}

	now := time.Now()
	a.stats.noteOutboundPacket(len(buf), now)
	atomic.StoreInt64(&a.respondWithNopAfter, now.Add(respondWithNopDelay).UnixNano())
	return nil
}

func decodePacket(key ed25519.PrivateKey, packet []byte) ([]byte, error) {
	if len(packet) < 64 {
		return nil, ErrTooShortData
	}

	pub := packet[0:32]
	checksum := packet[32:64]
	data := packet[64:]

	key, err := keys.SharedKey(key, pub)
	if err != nil {
		return nil, err
	}

	ctr, err := keys.BuildSharedCipher(key, checksum)
	if err != nil {
		return nil, err
	}

	ctr.XORKeyStream(data, data)

	hash := sha256.Sum256(data)
	if !bytes.Equal(hash[:], checksum) {
		return nil, errors.New("invalid checksum of packet")
	}

	return data, nil
}

func (a *ADNL) SetAddresses(list address.List) {
	listCopy := address.CloneList(&list)
	if listCopy == nil {
		listCopy = &address.List{}
	}
	atomic.StoreInt32(&a.reinitTime, listCopy.ReinitDate)
	atomic.StorePointer(&a.ourAddresses, unsafe.Pointer(listCopy))
}

func (a *ADNL) GetAddressList() address.List {
	ourAddr := (*address.List)(atomic.LoadPointer(&a.ourAddresses))
	if ourAddr == nil {
		return address.List{}
	}
	return *address.CloneList(ourAddr)
}

func (a *ADNL) GetID() []byte {
	return append([]byte{}, a.peerID...)
}

func (a *ADNL) GetPubKey() ed25519.PublicKey {
	return append(ed25519.PublicKey{}, a.peerKey...)
}

func (a *ADNL) Reinit() {
	tm := int32(time.Now().Unix())

	// The advertised address list must carry the same reinit epoch as the
	// packets: C++ overwrites the list's reinit date with the local epoch on
	// every local list update (AdnlLocalId), and its update_addr_list drops
	// any received list whose reinit date is older than the epoch it already
	// learned from packet headers. Without this refresh a peer that saw our
	// new packet epoch would reject every address list we send afterwards.
	for {
		current := (*address.List)(atomic.LoadPointer(&a.ourAddresses))
		refreshed := address.CloneList(current)
		if refreshed == nil {
			refreshed = &address.List{}
		}
		refreshed.ReinitDate = tm
		if refreshed.Version < tm {
			refreshed.Version = tm
		}
		if atomic.CompareAndSwapPointer(&a.ourAddresses, unsafe.Pointer(current), unsafe.Pointer(refreshed)) {
			break
		}
	}

	atomic.StorePointer(&a.channelPtr, nil)
	atomic.StoreInt32(&a.reinitTime, tm)
	atomic.StoreInt32(&a.dstReinit, 0)
	atomic.StoreInt64(&a.seqno, 0)
	atomic.StoreInt64(&a.confirmSeqno, 0)
	a.stats.peerConfirmSeqno.Store(0)
	atomic.StoreInt64(&a.lastReceivedPacket, 0)
	atomic.StoreInt64(&a.tryReinitAt, 0)
	atomic.StoreInt64(&a.dropAddrListAt, 0)
	atomic.StoreInt32(&a.ourAddrVerOnPeerSide, 0)
	atomic.StoreInt32(&a.ourPriorityAddrVerOnPeerSide, 0)
}

type rootPacketOptions struct {
	forceAddress          bool
	resetPeerAddrVersions bool
}

func (a *ADNL) createPacket(seqno int64, msgs ...any) ([]byte, error) {
	return a.createPacketWithOptions(seqno, rootPacketOptions{}, msgs...)
}

func (a *ADNL) createPacketWithOptions(seqno int64, opts rootPacketOptions, msgs ...any) ([]byte, error) {
	if a.peerKey == nil {
		return nil, fmt.Errorf("unknown peer")
	}

	confSeq := atomic.LoadInt64(&a.confirmSeqno)
	reinit := atomic.LoadInt32(&a.reinitTime)
	dstReinit := atomic.LoadInt32(&a.dstReinit)
	addrVer := atomic.LoadInt32(&a.recvAddrVer)
	priorityAddrVer := atomic.LoadInt32(&a.recvPriorityAddrVer)
	var randData [32]byte
	_, err := rand.Read(randData[:])
	if err != nil {
		return nil, err
	}

	rand1, err := resizeRandForPacket(randData[:16])
	if err != nil {
		return nil, err
	}

	rand2, err := resizeRandForPacket(randData[16:])
	if err != nil {
		return nil, err
	}

	packet := &PacketContent{
		Rand1:         rand1,
		Messages:      msgs,
		Seqno:         &seqno,
		ConfirmSeqno:  &confSeq,
		ReinitDate:    &reinit,
		DstReinitDate: &dstReinit,
		Rand2:         rand2,
	}

	packet.From = &keys.PublicKeyED25519{Key: a.ourKey.Public().(ed25519.PublicKey)}
	if addrVer > 0 && !opts.resetPeerAddrVersions {
		packet.RecvAddrListVersion = &addrVer
	}
	if priorityAddrVer > 0 && !opts.resetPeerAddrVersions {
		packet.RecvPriorityAddrListVersion = &priorityAddrVer
	}

	ourAddr := (*address.List)(atomic.LoadPointer(&a.ourAddresses))
	if opts.forceAddress || atomic.LoadInt32(&a.ourAddrVerOnPeerSide) != ourAddr.Version {
		packet.Address = ourAddr
	}

	buf := bytes.NewBuffer(make([]byte, 96))
	buf.Grow(MaxMTU - 32)

	if _, err = packet.Serialize(buf); err != nil {
		return nil, err
	}

	packet.Signature = ed25519.Sign(a.ourKey, buf.Bytes()[96:])
	buf.Truncate(96)

	_, err = packet.Serialize(buf)
	if err != nil {
		return nil, err
	}
	bufData := buf.Bytes()
	packetData := bufData[96:]

	hash := sha256.Sum256(packetData)
	checksum := hash[:]

	_, outerPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, err
	}

	sharedKey, err := keys.SharedKeyWithPeerX25519(outerPriv, a.peerKeyX25519)
	if err != nil {
		return nil, err
	}

	ctr, err := keys.BuildSharedCipher(sharedKey, checksum)
	if err != nil {
		return nil, err
	}

	ctr.XORKeyStream(packetData, packetData)

	copy(bufData, a.peerID)
	copy(bufData[32:], outerPriv.Public().(ed25519.PublicKey))
	copy(bufData[64:], checksum)

	return bufData, nil
}

func (a *ADNL) noteReceivedPacket() {
	now := time.Now()
	atomic.StoreInt64(&a.lastReceivedPacket, now.UnixNano())
	atomic.StoreInt64(&a.tryReinitAt, now.Add(idleReinitTimeout).UnixNano())
	atomic.StoreInt64(&a.dropAddrListAt, 0)
}

func (a *ADNL) rootReinitOptions(now time.Time) (bool, rootPacketOptions) {
	last := atomic.LoadInt64(&a.lastReceivedPacket)
	if last <= 0 {
		return false, rootPacketOptions{}
	}

	lastAt := time.Unix(0, last)
	if now.Sub(lastAt) < idleReinitSilence {
		return false, rootPacketOptions{}
	}

	a.scheduleIdleReinit(now)
	a.scheduleStaleAddressListDrop(now, lastAt)

	tryAt := atomic.LoadInt64(&a.tryReinitAt)
	if tryAt <= 0 || now.UnixNano() < tryAt {
		return false, rootPacketOptions{}
	}

	next := now.Add(nextIdleReinitRetryDelay()).UnixNano()
	if !atomic.CompareAndSwapInt64(&a.tryReinitAt, tryAt, next) {
		return false, rootPacketOptions{}
	}
	a.stats.noteRootRecovery(now)

	return true, rootPacketOptions{
		forceAddress:          true,
		resetPeerAddrVersions: true,
	}
}

func (a *ADNL) scheduleIdleReinit(now time.Time) {
	candidate := now.Add(idleReinitScheduleDelay).UnixNano()
	for {
		current := atomic.LoadInt64(&a.tryReinitAt)
		if current > 0 && current <= candidate {
			return
		}
		if atomic.CompareAndSwapInt64(&a.tryReinitAt, current, candidate) {
			return
		}
	}
}

func (a *ADNL) scheduleStaleAddressListDrop(now time.Time, lastAt time.Time) {
	if now.Sub(lastAt) < staleAddressListSilence {
		return
	}

	dropAt := atomic.LoadInt64(&a.dropAddrListAt)
	if dropAt == 0 {
		atomic.CompareAndSwapInt64(&a.dropAddrListAt, 0, now.Add(staleAddressListDropDelay).UnixNano())
		return
	}
	if now.UnixNano() < dropAt {
		return
	}

	atomic.StoreInt64(&a.dropAddrListAt, 0)
	atomic.StoreInt32(&a.recvAddrVer, 0)
	atomic.StoreInt32(&a.recvPriorityAddrVer, 0)
}

func nextIdleReinitRetryDelay() time.Duration {
	return idleReinitRetryMinDelay + time.Duration(time.Now().UnixNano()%int64(idleReinitRetryJitter))
}

func createQueryMessage(query any) (*MessageQuery, error) {
	qid := make([]byte, 32)
	_, err := rand.Read(qid)
	if err != nil {
		return nil, err
	}

	return &MessageQuery{
		ID:   qid,
		Data: query,
	}, nil
}

type queryID [32]byte

func encodeQueryID(id []byte) (queryID, bool) {
	var qid queryID
	if len(id) != len(qid) {
		return qid, false
	}
	copy(qid[:], id)
	return qid, true
}
