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

	reinitTime           int32
	seqno                int64
	confirmSeqno         int64
	dstReinit            int32
	recvAddrVer          int32
	recvPriorityAddrVer  int32
	ourAddrVerOnPeerSide int32

	peerKey      ed25519.PublicKey
	ourAddresses unsafe.Pointer

	activeQueries map[string]chan tl.Serializable
	activePings   map[int64]chan MessagePong

	customMessageHandler unsafe.Pointer // CustomMessageHandler
	queryHandler         unsafe.Pointer // QueryHandler
	onDisconnect         unsafe.Pointer // DisconnectHandler
	onChannel            func(ch *Channel)

	mx sync.RWMutex
}

var Logger = func(v ...any) {}

func (g *Gateway) initADNL() *ADNL {
	tm := int32(time.Now().Unix())
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
		activeQueries: map[string]chan tl.Serializable{},
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
	if c.wantConfirm {
		// we got message in channel, no more confirmations required
		c.wantConfirm = false
	}

	data, err := c.decodePacket(buf)
	if err != nil {
		return fmt.Errorf("failed to decode packet: %w", err)
	}

	packet, err := parsePacket(data)
	if err != nil {
		return fmt.Errorf("failed to parse packet: %w", err)
	}

	err = c.adnl.processPacket(packet, true)
	if err != nil {
		return fmt.Errorf("failed to process packet: %w", err)
	}
	return nil
}

func (a *ADNL) processPacket(packet *PacketContent, fromChannel bool) (err error) {
	if packet.DstReinitDate != nil && *packet.DstReinitDate > 0 && *packet.DstReinitDate < atomic.LoadInt32(&a.reinitTime) {
		if packet.ReinitDate != nil {
			atomic.StoreInt32(&a.dstReinit, *packet.ReinitDate)
		}

		buf, err := a.buildRequest(MessageNop{})
		if err != nil {
			return fmt.Errorf("failed to create packet: %w", err)
		}
		if err = a.send(buf); err != nil {
			return fmt.Errorf("failed to send ping reinit: %w", err)
		}

		return nil
	}

	if !fromChannel && packet.From != nil {
		a.mx.Lock()
		if a.peerKey == nil {
			a.peerKey = packet.From.Key
		}
		a.mx.Unlock()
	}

	seqno := *packet.Seqno

	for { // to guarantee swap to highest
		if conf := atomic.LoadInt64(&a.confirmSeqno); seqno > conf {
			if !atomic.CompareAndSwapInt64(&a.confirmSeqno, conf, seqno) {
				continue
			}
		}
		break
	}

	if (packet.ReinitDate != nil && *packet.ReinitDate > atomic.LoadInt32(&a.dstReinit)) &&
		(packet.DstReinitDate != nil && *packet.DstReinitDate == atomic.LoadInt32(&a.reinitTime)) {
		// reset their seqno even if it is lower,
		// because other side could lose counter
		atomic.StoreInt64(&a.confirmSeqno, seqno)
		atomic.StoreInt32(&a.dstReinit, *packet.ReinitDate)
	}

	if packet.RecvPriorityAddrListVersion != nil {
		atomic.StoreInt32(&a.ourAddrVerOnPeerSide, *packet.RecvPriorityAddrListVersion)
	} else if packet.RecvAddrListVersion != nil {
		atomic.StoreInt32(&a.ourAddrVerOnPeerSide, *packet.RecvAddrListVersion)
	}

	if packet.Address != nil {
		// a.recvAddrVer = packet.Address.Version
		atomic.StoreInt32(&a.recvPriorityAddrVer, packet.Address.Version)
	}

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
		a.mx.RLock()
		ch := a.activePings[ms.Value]
		a.mx.RUnlock()

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
		qh := atomic.LoadPointer(&a.queryHandler)
		if qh != nil {
			h := *(*QueryHandler)(qh)
			if err := h(&ms); err != nil {
				return fmt.Errorf("failed to handle query: %w", err)
			}
		}
	case MessageAnswer:
		a.processAnswer(hex.EncodeToString(ms.ID), ms.Data)
	case MessageCreateChannel:
		a.mx.Lock()
		defer a.mx.Unlock()

		ch := (*Channel)(atomic.LoadPointer(&a.channelPtr))

		var err error
		var key ed25519.PrivateKey
		if ch != nil {
			if bytes.Equal(ch.peerKey, ms.Key) {
				// already initialized on our side, but client missed confirmation,
				// channel is already known, so more confirmations will be sent in the next packets
				return nil
			}
			// looks like channel was lost on the other side, we will reinit it
			key = ch.key
		} else {
			_, key, err = ed25519.GenerateKey(nil)
			if err != nil {
				return err
			}
		}

		newChan := &Channel{
			adnl:        a,
			key:         key,
			initDate:    int32(time.Now().Unix()),
			wantConfirm: true,
		}

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

		if ch.ready {
			// not required confirmation, skip it
			return nil
		}

		if err := ch.setup(ms.Key); err != nil {
			return fmt.Errorf("failed to setup channel: %w", err)
		}
	case MessagePart:
		msgID := string(ms.Hash)

		a.mx.Lock()
		p, ok := a.msgParts[msgID]
		if !ok {
			if ms.TotalSize > HugePacketMaxSz {
				a.mx.Unlock()
				return fmt.Errorf("skip too big partitioned message with len %d bytes", ms.TotalSize)
			}

			if len(a.msgParts) > 100 {
				// cleanup old stuck messages
				tm := time.Now().Add(-7 * time.Second)
				for s, pt := range a.msgParts {
					if tm.After(pt.startedAt) {
						delete(a.msgParts, s)
					}
				}

				if len(a.msgParts) > 16*1024 {
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
			_, err = tl.Parse(&msg, data, true)
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

func (a *ADNL) processAnswer(id string, query any) {
	a.mx.Lock()
	res, ok := a.activeQueries[id]
	if ok {
		delete(a.activeQueries, id)
	}
	a.mx.Unlock()

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
	packets, err := a.buildRequestMaySplit(&MessageCustom{
		Data: req,
	}, baseMTU)
	if err != nil {
		return fmt.Errorf("failed to send custom message: %w", err)
	}

	for _, packet := range packets {
		if err = a.send(packet); err != nil {
			if !baseMTU && errors.Is(err, ErrPacketBiggerThanMTU) {
				baseMTU = true
				goto reSplit
			}
			return fmt.Errorf("failed to send custom packet: %w", err)
		}
	}
	return nil
}

func (a *ADNL) Ping(ctx context.Context) (time.Duration, error) {
	val := time.Now().UnixNano()
	req, err := a.buildRequest(MessagePing{
		Value: val,
	})
	if err != nil {
		return 0, err
	}

	ch := make(chan MessagePong, 1)
	a.mx.Lock()
	a.activePings[val] = ch
	a.mx.Unlock()

	defer func() {
		a.mx.Lock()
		delete(a.activePings, val)
		a.mx.Unlock()
	}()

	for {
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
	reqID := hex.EncodeToString(q.ID)

	a.mx.Lock()
	a.activeQueries[reqID] = res
	a.mx.Unlock()

	baseMTU := false
reSplit:
	packets, err := a.buildRequestMaySplit(q, baseMTU)
	if err != nil {
		a.mx.Lock()
		delete(a.activeQueries, reqID)
		a.mx.Unlock()
		return fmt.Errorf("request failed: %w", err)
	}

	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()

	for {
		for i, packet := range packets {
			if err = a.send(packet); err != nil {
				if !baseMTU && errors.Is(err, ErrPacketBiggerThanMTU) {
					baseMTU = true
					goto reSplit
				}
				return fmt.Errorf("failed to send query packet %d: %w", i, err)
			}
		}

		timer.Reset(250 * time.Millisecond)

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
		}
	}
}

func (a *ADNL) Answer(ctx context.Context, queryID []byte, result tl.Serializable) error {
	baseMTU := false
reSplit:
	packets, err := a.buildRequestMaySplit(&MessageAnswer{
		ID:   queryID,
		Data: result,
	}, baseMTU)
	if err != nil {
		return fmt.Errorf("send answer  failed: %w", err)
	}

	for _, packet := range packets {
		if err = a.send(packet); err != nil {
			if !baseMTU && errors.Is(err, ErrPacketBiggerThanMTU) {
				baseMTU = true
				goto reSplit
			}
			return fmt.Errorf("failed to send answer: %w", err)
		}
	}
	return nil
}

const MaxMTU = 1500 - 40 - 8 // max is for ipv6 over ethernet

func (a *ADNL) buildRequestMaySplit(req tl.Serializable, useBase bool) (packets [][]byte, err error) {
	msg, err := tl.Serialize(req, true)
	if err != nil {
		return nil, err
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
			return nil, fmt.Errorf("too big message payload")
		}

		parts := splitMessage(msg, mtu)
		packets = make([][]byte, 0, len(parts))
		for i, part := range parts {
			buf, err := a.buildRequest(part)
			if err != nil {
				return nil, fmt.Errorf("filed to build message part %d, err: %w", i, err)
			}
			packets = append(packets, buf)
		}
		return packets, nil
	}

	buf, err := a.buildRequest(tl.Raw(msg))
	if err != nil {
		return nil, fmt.Errorf("filed to build message, err: %w", err)
	}
	return [][]byte{buf}, nil
}

func (a *ADNL) buildRequest(req tl.Serializable) (buf []byte, err error) {
	ch := (*Channel)(atomic.LoadPointer(&a.channelPtr))
	seqno := atomic.AddInt64(&a.seqno, 1)

	if ch != nil && ch.ready {
		if ch.wantConfirm {
			// if client not yet received confirmation - we will send it till his first packet in channel
			chMsg := &MessageConfirmChannel{
				Key:     ch.key.Public().(ed25519.PublicKey),
				PeerKey: ch.peerKey,
				Date:    ch.initDate,
			}

			buf, err = a.createPacket(seqno, true, chMsg, req)
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

	buf, err = a.createPacket(seqno, false, chMsg, req)
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
		// not close on io timeout because it can be triggered by network overload
		if !strings.Contains(err.Error(), "i/o timeout") {
			// it should trigger disconnect handler in read routine
			a.writer.Close()
		}
		return err
	} else if n != len(buf) {
		return fmt.Errorf("not full packet was written")
	}

	return nil
}

func decodePacket(key ed25519.PrivateKey, packet []byte) ([]byte, error) {
	pub := packet[0:32]
	checksum := packet[32:64]
	data := packet[64:]

	key, err := SharedKey(key, pub)
	if err != nil {
		return nil, err
	}

	ctr, err := BuildSharedCipher(key, checksum)
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
	atomic.StoreInt32(&a.reinitTime, list.ReinitDate)
	atomic.StorePointer(&a.ourAddresses, unsafe.Pointer(&list))
}

func (a *ADNL) GetAddressList() address.List {
	ourAddr := (*address.List)(atomic.LoadPointer(&a.ourAddresses))
	return *ourAddr
}

func (a *ADNL) GetID() []byte {
	id, _ := tl.Hash(PublicKeyED25519{Key: a.peerKey})
	return id
}

func (a *ADNL) GetPubKey() ed25519.PublicKey {
	return a.peerKey
}

func (a *ADNL) Reinit() {
	atomic.StorePointer(&a.channelPtr, nil)
	atomic.StoreInt32(&a.reinitTime, int32(time.Now().Unix()))
	atomic.StoreInt32(&a.dstReinit, 0)
	atomic.StoreInt64(&a.seqno, 0)
	atomic.StoreInt64(&a.confirmSeqno, 0)
}

func (a *ADNL) createPacket(seqno int64, isResp bool, msgs ...any) ([]byte, error) {
	if a.peerKey == nil {
		return nil, fmt.Errorf("unknown peer")
	}

	confSeq := atomic.LoadInt64(&a.confirmSeqno)
	reinit := atomic.LoadInt32(&a.reinitTime)
	dstReinit := atomic.LoadInt32(&a.dstReinit)

	// addrVer := a.recvAddrVer
	priorityAddrVer := atomic.LoadInt32(&a.recvPriorityAddrVer)

	randData := make([]byte, 32)
	_, err := rand.Read(randData)
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
		// RecvAddrListVersion:         &addrVer,         // if version is less, peer will send us his address,
		RecvPriorityAddrListVersion: &priorityAddrVer, // but we don't need it in current implementation
		Rand2:                       rand2,
	}

	if !isResp {
		packet.From = &PublicKeyED25519{Key: a.ourKey.Public().(ed25519.PublicKey)}
	} else {
		packet.FromIDShort, err = tl.Hash(PublicKeyED25519{Key: a.ourKey.Public().(ed25519.PublicKey)})
		if err != nil {
			return nil, err
		}
	}

	ourAddr := (*address.List)(atomic.LoadPointer(&a.ourAddresses))
	if atomic.LoadInt32(&a.ourAddrVerOnPeerSide) != ourAddr.Version {
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

	key, err := SharedKey(a.ourKey, a.peerKey)
	if err != nil {
		return nil, err
	}

	ctr, err := BuildSharedCipher(key, checksum)
	if err != nil {
		return nil, err
	}

	ctr.XORKeyStream(packetData, packetData)

	enc, err := tl.Hash(PublicKeyED25519{Key: a.peerKey})
	if err != nil {
		return nil, err
	}
	copy(bufData, enc)
	copy(bufData[32:], a.ourKey.Public().(ed25519.PublicKey))
	copy(bufData[64:], checksum)

	return bufData, nil
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
