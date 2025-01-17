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
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/tl"
	"log"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
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

type CustomMessageHandler func(msg *MessageCustom) error
type QueryHandler func(msg *MessageQuery) error
type DisconnectHandler func(addr string, key ed25519.PublicKey)

type PacketWriter interface {
	Write(b []byte, deadline time.Time) (n int, err error)
	Close() error
}

type ADNL struct {
	writer PacketWriter
	ourKey ed25519.PrivateKey
	addr   string

	closerCtx context.Context
	closeFn   context.CancelFunc

	channel                  *Channel
	channelExchangeCompleted int32

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

	customMessageHandler CustomMessageHandler
	queryHandler         QueryHandler
	onDisconnect         DisconnectHandler
	onChannel            func(ch *Channel)

	mx sync.RWMutex

	lastTrack int64
	packets   uint64
	packetsSz uint64
}

var Logger = log.Println

func initADNL(key ed25519.PrivateKey) *ADNL {
	tm := int32(time.Now().Unix())
	closerCtx, closeFn := context.WithCancel(context.Background())
	return &ADNL{
		ourAddresses: unsafe.Pointer(&address.List{
			Version:    tm,
			ReinitDate: tm,
		}),
		reinitTime: tm,
		ourKey:     key,
		closerCtx:  closerCtx,
		closeFn:    closeFn,

		msgParts:      make(map[string]*partitionedMessage, 1024),
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

	if disc := a.onDisconnect; trigger && disc != nil {
		disc(a.addr, a.peerKey)
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
		if err = a.send(context.Background(), buf); err != nil {
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
		// TODO: record
	case MessagePing:
		buf, err := a.buildRequest(MessagePong{Value: ms.Value})
		if err != nil {
			return fmt.Errorf("failed to build pong request: %w", err)
		}

		if err = a.send(context.Background(), buf); err != nil {
			return fmt.Errorf("failed to send pong: %w", err)
		}
	case MessageQuery:
		if a.queryHandler != nil {
			if err := a.queryHandler(&ms); err != nil {
				return fmt.Errorf("failed to handle query: %w", err)
			}
		}
	case MessageAnswer:
		a.processAnswer(hex.EncodeToString(ms.ID), ms.Data)
	case MessageCreateChannel:
		a.mx.Lock()
		defer a.mx.Unlock()

		var err error
		var key ed25519.PrivateKey
		if a.channel != nil {
			if bytes.Equal(a.channel.peerKey, ms.Key) {
				// already initialized on our side, but client missed confirmation,
				// channel is already known, so more confirmations will be sent in the next packets
				return nil
			}
			// looks like channel was lost on the other side, we will reinit it
			println("REINIT CHANNEL")
			key = a.channel.key
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
		a.channel = newChan
		atomic.StoreInt32(&a.channelExchangeCompleted, 0)

	case MessageConfirmChannel:
		a.mx.Lock()
		defer a.mx.Unlock()

		if a.channel == nil || !bytes.Equal(a.channel.key.Public().(ed25519.PublicKey), ms.PeerKey) {
			return fmt.Errorf("confirmation for unknown channel %s", hex.EncodeToString(ms.PeerKey))
		}

		if a.channel.ready {
			// not required confirmation, skip it
			return nil
		}

		err := a.channel.setup(ms.Key)
		if err != nil {
			return fmt.Errorf("failed to setup channel: %w", err)
		}
		atomic.StoreInt32(&a.channelExchangeCompleted, 0)
	case MessagePart:
		msgID := hex.EncodeToString(ms.Hash)

		a.mx.Lock()
		p, ok := a.msgParts[msgID]
		if !ok {
			if ms.TotalSize > _HugePacketMaxSz {
				a.mx.Unlock()
				return fmt.Errorf("skip too big partitioned message with len %d bytes", ms.TotalSize)
			}

			if len(a.msgParts) > 100 {
				// cleanup old stuck messages
				for s, pt := range a.msgParts {
					if time.Since(pt.startedAt) > 10*time.Second {
						delete(a.msgParts, s)
					}
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
		if a.customMessageHandler != nil {
			if err := a.customMessageHandler(&ms); err != nil {
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
	a.customMessageHandler = handler
}

func (a *ADNL) SetQueryHandler(handler func(msg *MessageQuery) error) {
	a.queryHandler = handler
}

func (a *ADNL) GetQueryHandler() func(msg *MessageQuery) error {
	return a.queryHandler
}

func (a *ADNL) SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey)) {
	a.onDisconnect = handler
}

func (a *ADNL) GetDisconnectHandler() func(addr string, key ed25519.PublicKey) {
	return a.onDisconnect
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

func (a *ADNL) SendCustomMessage(ctx context.Context, req tl.Serializable) error {
	return a.sendCustomMessage(ctx, nil, req)
}

func (a *ADNL) sendCustomMessage(ctx context.Context, ch *Channel, req tl.Serializable) error {
	packets, err := a.buildRequestMaySplit(ch, &MessageCustom{
		Data: req,
	})
	if err != nil {
		return fmt.Errorf("failed to send custom message: %w", err)
	}

	for _, packet := range packets {
		if err = a.send(ctx, packet); err != nil {
			return fmt.Errorf("failed to send custom packet: %w", err)
		}
	}
	return nil
}

func (a *ADNL) Query(ctx context.Context, req, result tl.Serializable) error {
	return a.query(ctx, nil, req, result)
}

func (a *ADNL) query(ctx context.Context, ch *Channel, req, result tl.Serializable) error {
	q, err := createQueryMessage(req)
	if err != nil {
		return fmt.Errorf("failed to create query message: %w", err)
	}

	res := make(chan tl.Serializable, 1)

	reqID := hex.EncodeToString(q.ID)

	a.mx.Lock()
	a.activeQueries[reqID] = res
	a.mx.Unlock()

	packets, err := a.buildRequestMaySplit(ch, q)
	if err != nil {
		a.mx.Lock()
		delete(a.activeQueries, reqID)
		a.mx.Unlock()

		return fmt.Errorf("request failed: %w", err)
	}

	for {
		for i, packet := range packets {
			if err = a.send(ctx, packet); err != nil {
				return fmt.Errorf("failed to send query packet %d: %w", i, err)
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
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (a *ADNL) Answer(ctx context.Context, queryID []byte, result tl.Serializable) error {
	packets, err := a.buildRequestMaySplit(nil, &MessageAnswer{
		ID:   queryID,
		Data: result,
	})
	if err != nil {
		return fmt.Errorf("send answer  failed: %w", err)
	}

	for _, packet := range packets {
		if err = a.send(ctx, packet); err != nil {
			return fmt.Errorf("failed to send answer: %w", err)
		}
	}
	return nil
}

func (a *ADNL) buildRequestMaySplit(ch *Channel, req tl.Serializable) (packets [][]byte, err error) {
	msg, err := tl.Serialize(req, true)
	if err != nil {
		return nil, err
	}

	if len(msg) > _MTU {
		parts := splitMessage(msg)
		if len(parts) > 8 {
			return nil, fmt.Errorf("too big message, more than 8 parts")
		}

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
	seqno := atomic.AddInt64(&a.seqno, 1)

	if atomic.LoadInt32(&a.channelExchangeCompleted) != 0 {
		// channel is active, using fast flow
		buf, err = a.channel.createPacket(seqno, req)
		if err != nil {
			return nil, fmt.Errorf("failed to create packet: %w", err)
		}
		return buf, nil
	}

	// a.mx.Lock()
	// defer a.mx.Unlock()

	if a.channel != nil && a.channel.ready {
		if a.channel.wantConfirm {
			// if client not yet received confirmation - we will send it till his first packet in channel
			chMsg := &MessageConfirmChannel{
				Key:     a.channel.key.Public().(ed25519.PublicKey),
				PeerKey: a.channel.peerKey,
				Date:    a.channel.initDate,
			}

			buf, err = a.createPacket(seqno, true, chMsg, req)
			if err != nil {
				return nil, fmt.Errorf("failed to create packet: %w", err)
			}
			return buf, nil
		}

		atomic.StoreInt32(&a.channelExchangeCompleted, 1)

		// channel is active
		buf, err = a.channel.createPacket(seqno, req)
		if err != nil {
			return nil, fmt.Errorf("failed to create packet: %w", err)
		}
		return buf, nil
	}

	// if it is not exists, we will create it,
	if a.channel == nil {
		_, key, err := ed25519.GenerateKey(nil)
		if err != nil {
			return nil, err
		}

		a.channel = &Channel{
			adnl:     a,
			key:      key,
			initDate: int32(time.Now().Unix()),
		}
		atomic.StoreInt32(&a.channelExchangeCompleted, 0)

	}

	// if it is pending, we will send creation (can be sent multiple times, it is ok)
	chMsg := &MessageCreateChannel{
		Key:  a.channel.key.Public().(ed25519.PublicKey),
		Date: a.channel.initDate,
	}

	buf, err = a.createPacket(seqno, false, chMsg, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create packet: %w", err)
	}

	return buf, nil
}

func (a *ADNL) send(ctx context.Context, buf []byte) error {
	if err := ctx.Err(); err != nil {
		// check if context is failed to not try to write
		return err
	}

	dl, ok := ctx.Deadline()
	if !ok {
		dl = time.Now().Add(10 * time.Second)
	}

	n, err := a.writer.Write(buf, dl)
	if err != nil {
		// not close on io timeout because it can be triggered by network overload
		if !strings.Contains(err.Error(), "i/o timeout") {
			// it should trigger disconnect handler in read routine
			a.writer.Close()
		}
		return err
	}
	if n != len(buf) {
		return fmt.Errorf("too big packet")
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

	hash := sha256.New()
	hash.Write(data)
	if !bytes.Equal(hash.Sum(nil), checksum) {
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

	toSign, err := packet.Serialize()
	if err != nil {
		return nil, err
	}

	packet.Signature = ed25519.Sign(a.ourKey, toSign)

	packetData, err := packet.Serialize()
	if err != nil {
		return nil, err
	}

	hash := sha256.New()
	hash.Write(packetData)
	checksum := hash.Sum(nil)

	key, err := SharedKey(a.ourKey, a.peerKey)
	if err != nil {
		return nil, err
	}

	ctr, err := BuildSharedCipher(key, checksum)
	if err != nil {
		return nil, err
	}

	enc, err := tl.Hash(PublicKeyED25519{Key: a.peerKey})
	if err != nil {
		return nil, err
	}

	ctr.XORKeyStream(packetData, packetData)

	enc = append(enc, a.ourKey.Public().(ed25519.PublicKey)...)
	enc = append(enc, checksum...)
	enc = append(enc, packetData...)

	return enc, nil
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
