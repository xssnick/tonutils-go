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
	"sync"
	"time"
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

type packetWriter interface {
	Write(b []byte, deadline time.Time) (n int, err error)
	Close() error
}

type ADNL struct {
	writer packetWriter
	ourKey ed25519.PrivateKey
	addr   string
	closer chan bool
	closed bool

	channel *Channel

	msgParts map[string]*partitionedMessage

	reinitTime           int32
	seqno                uint64
	confirmSeqno         uint64
	dstReinit            int32
	recvAddrVer          int32
	recvPriorityAddrVer  int32
	ourAddrVerOnPeerSide int32
	loss                 uint64

	peerKey      ed25519.PublicKey
	ourAddresses address.List

	activeQueries map[string]chan tl.Serializable

	customMessageHandler CustomMessageHandler
	queryHandler         QueryHandler
	onDisconnect         DisconnectHandler
	onChannel            func(ch *Channel)

	lastReceiveAt time.Time

	mx sync.RWMutex
}

var Logger = log.Println

func initADNL(key ed25519.PrivateKey) *ADNL {
	return &ADNL{
		ourAddresses: address.List{
			Version:    int32(time.Now().Unix()),
			ReinitDate: int32(time.Now().Unix()),
		},
		reinitTime: 0, //int32(time.Now().Unix()),
		ourKey:     key,
		closer:     make(chan bool, 1),

		msgParts:      map[string]*partitionedMessage{},
		activeQueries: map[string]chan tl.Serializable{},
	}
}

func (a *ADNL) Close() {
	a.mx.Lock()
	defer a.mx.Unlock()

	if !a.closed {
		a.closed = true

		close(a.closer)

		con := a.writer
		if con != nil {
			con.Close()
		}

		if a.onDisconnect != nil {
			a.onDisconnect(a.addr, a.peerKey)
		}
	}
}

func (a *ADNL) process(buf []byte) error {
	data, err := a.decodePacket(buf)
	if err != nil {
		return fmt.Errorf("failed to decode packet: %w", err)
	}

	err = a.processPacket(data, nil)
	if err != nil {
		return fmt.Errorf("failed to process packet: %w", err)
	}
	return nil
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

	err = c.adnl.processPacket(data, c)
	if err != nil {
		return fmt.Errorf("failed to process packet: %w", err)
	}
	return nil
}

func (a *ADNL) processPacket(data []byte, ch *Channel) (err error) {
	packet, err := a.parsePacket(data)
	if err != nil {
		return fmt.Errorf("failed to parse packet: %w", err)
	}

	a.mx.Lock()
	seqno := uint64(*packet.Seqno)
	a.lastReceiveAt = time.Now()

	if ch == nil && packet.From != nil {
		a.peerKey = packet.From.Key
	}

	if a.confirmSeqno+1 < seqno {
		a.loss += seqno - (a.confirmSeqno + 1)
	}

	if seqno > a.confirmSeqno {
		a.confirmSeqno = uint64(*packet.Seqno)
	}

	if packet.ReinitDate != nil {
		//	a.dstReinit = *packet.ReinitDate
		//	a.reinitTime = a.dstReinit
	}

	if packet.RecvPriorityAddrListVersion != nil {
		a.ourAddrVerOnPeerSide = *packet.RecvPriorityAddrListVersion
	}

	if packet.Address != nil {
		// a.recvAddrVer = packet.Address.Version
		a.recvPriorityAddrVer = packet.Address.Version
	}
	a.mx.Unlock()

	for i, message := range packet.Messages {
		err = a.processMessage(message, ch)
		if err != nil {
			return fmt.Errorf("failed to process message %d %s: %v", i, reflect.TypeOf(message), err)
		}
	}

	return nil
}

func (a *ADNL) processMessage(message any, ch *Channel) error {
	switch ms := message.(type) {
	case MessagePong:
		// TODO: record
	case MessagePing:
		err := a.sendRequest(context.Background(), ch, MessagePong{Value: ms.Value})
		if err != nil {
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

		if a.channel != nil {
			if !bytes.Equal(a.channel.peerKey, ms.Key) {
				return fmt.Errorf("another channel is already initialized")
			}
			// already initialized on our side, but client missed confirmation,
			// channel is already known, so more confirmations will be sent in the next packets
			return nil
		}

		_, key, err := ed25519.GenerateKey(nil)
		if err != nil {
			return err
		}

		newChan := &Channel{
			adnl:        a,
			key:         key,
			initDate:    int32(time.Now().Unix()),
			wantConfirm: true,
		}

		err = newChan.setup(ms.Key)
		if err != nil {
			return fmt.Errorf("failed to setup channel: %w", err)
		}

		if a.channel == nil {
			a.channel = newChan
		}
	case MessageConfirmChannel:
		a.mx.Lock()
		defer a.mx.Unlock()

		if a.channel == nil || !bytes.Equal(a.channel.key.Public().(ed25519.PublicKey), ms.PeerKey) {
			return fmt.Errorf("confirmation for unknown channel")
		}

		if a.channel.ready {
			// not required confirmation, skip it
			return nil
		}

		err := a.channel.setup(ms.Key)
		if err != nil {
			return fmt.Errorf("failed to setup channel: %w", err)
		}
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

			err = a.processMessage(msg, ch)
			if err != nil {
				return fmt.Errorf("failed to process message built from parts: %w", err)
			}
		}
	case MessageCustom:
		if a.customMessageHandler != nil {
			err := a.customMessageHandler(&ms)
			if err != nil {
				return fmt.Errorf("failed to handle custom message: %w", err)
			}
		}
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

func (a *ADNL) SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey)) {
	a.onDisconnect = handler
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
		Logger("unknown response with id", id, a.addr, reflect.TypeOf(query).String())
	}
}

func (a *ADNL) SendCustomMessage(ctx context.Context, req tl.Serializable) error {
	return a.sendCustomMessage(ctx, nil, req)
}

func (a *ADNL) sendCustomMessage(ctx context.Context, ch *Channel, req tl.Serializable) error {
	err := a.sendRequestMaySplit(ctx, ch, &MessageCustom{
		Data: req,
	})
	if err != nil {
		return fmt.Errorf("failed to send custom message: %w", err)
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

	if err = a.sendRequestMaySplit(ctx, ch, q); err != nil {
		a.mx.Lock()
		delete(a.activeQueries, reqID)
		a.mx.Unlock()

		return fmt.Errorf("request failed: %w", err)
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
	}
}

func (a *ADNL) Answer(ctx context.Context, queryID []byte, result tl.Serializable) error {
	if err := a.sendRequestMaySplit(ctx, nil, &MessageAnswer{
		ID:   queryID,
		Data: result,
	}); err != nil {
		return fmt.Errorf("send answer  failed: %w", err)
	}
	return nil
}

func (a *ADNL) sendRequestMaySplit(ctx context.Context, ch *Channel, req tl.Serializable) (err error) {
	msg, err := tl.Serialize(req, true)
	if err != nil {
		return err
	}

	if len(msg) > _MTU {
		parts := splitMessage(msg)
		if len(parts) > 8 {
			return fmt.Errorf("too big message, more than 8 parts")
		}

		for i, part := range parts {
			if err = a.sendRequest(ctx, ch, part); err != nil {
				return fmt.Errorf("filed to send message part %d, err: %w", i, err)
			}
		}
		return nil
	}

	return a.sendRequest(ctx, ch, tl.Raw(msg))
}

func (a *ADNL) sendRequest(ctx context.Context, ch *Channel, req tl.Serializable) (err error) {
	a.mx.Lock()
	defer a.mx.Unlock()

	if a.writer == nil {
		return fmt.Errorf("ADNL connection is not active")
	}

	if ch != nil && !ch.ready {
		return fmt.Errorf("channel is not ready yet")
	}

	seqno := a.seqno + 1

	var buf []byte
	// if channel == nil, we will use root channel,
	if ch == nil {
		if a.channel != nil && a.channel.ready {
			if a.channel.wantConfirm {
				// if client not yet received confirmation - we will send it till his first packet in channel
				chMsg := &MessageConfirmChannel{
					Key:     a.channel.key.Public().(ed25519.PublicKey),
					PeerKey: a.channel.peerKey,
					Date:    a.channel.initDate,
				}

				buf, err = a.createPacket(int64(seqno), true, chMsg, req)
				if err != nil {
					return fmt.Errorf("failed to create packet: %w", err)
				}
			} else {
				// channel is active
				ch = a.channel
			}
		} else {
			// if it is not exists, we will create it,
			if a.channel == nil {
				_, key, err := ed25519.GenerateKey(nil)
				if err != nil {
					return err
				}

				a.channel = &Channel{
					adnl:     a,
					key:      key,
					initDate: int32(time.Now().Unix()),
				}
			}

			// if it is pending, we will send creation (can be sent multiple times, it is ok)
			chMsg := &MessageCreateChannel{
				Key:  a.channel.key.Public().(ed25519.PublicKey),
				Date: a.channel.initDate,
			}

			buf, err = a.createPacket(int64(seqno), false, chMsg, req)
			if err != nil {
				return fmt.Errorf("failed to create packet: %w", err)
			}
		}
	}

	if ch != nil {
		buf, err = ch.createPacket(int64(seqno), req)
		if err != nil {
			return fmt.Errorf("failed to create packet: %w", err)
		}
	}

	err = a.send(ctx, buf)
	if err != nil {
		return fmt.Errorf("failed to send packet: %w", err)
	}

	a.seqno = seqno

	return nil
}

func (a *ADNL) send(ctx context.Context, buf []byte) error {
	dl, ok := ctx.Deadline()
	if !ok {
		dl = time.Now().Add(10 * time.Second)
	}

	n, err := a.writer.Write(buf, dl)
	if err != nil {
		// it should trigger disconnect handler in read routine
		a.writer.Close()
		return err
	}
	if n != len(buf) {
		return fmt.Errorf("too big packet")
	}

	return nil
}

func (a *ADNL) decodePacket(packet []byte) ([]byte, error) {
	pub := packet[0:32]
	checksum := packet[32:64]
	data := packet[64:]

	key, err := SharedKey(a.ourKey, pub)
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
	a.reinitTime = list.ReinitDate
	a.ourAddresses = list
}

func (a *ADNL) GetAddressList() address.List {
	return a.ourAddresses
}

func (a *ADNL) createPacket(seqno int64, isResp bool, msgs ...any) ([]byte, error) {
	if a.peerKey == nil {
		return nil, fmt.Errorf("unknown peer")
	}

	confSeq := int64(a.confirmSeqno)
	reinit := a.reinitTime
	dstReinit := a.dstReinit

	// addrVer := a.recvAddrVer
	priorityAddrVer := a.recvPriorityAddrVer

	rand1, err := randForPacket()
	if err != nil {
		return nil, err
	}

	rand2, err := randForPacket()
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
		packet.FromIDShort, err = ToKeyID(PublicKeyED25519{Key: a.ourKey.Public().(ed25519.PublicKey)})
		if err != nil {
			return nil, err
		}
	}

	if a.ourAddrVerOnPeerSide != a.ourAddresses.Version {
		packet.Address = &a.ourAddresses
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

	enc, err := ToKeyID(PublicKeyED25519{Key: a.peerKey})
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
