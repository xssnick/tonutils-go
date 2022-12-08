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
	"math/big"
	"net"
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

type ADNL struct {
	reinitTime uint32
	conn       net.Conn
	pubKey     ed25519.PublicKey
	privKey    ed25519.PrivateKey
	addr       string

	channels        map[string]*Channel
	pendingChannels map[string]*Channel
	rootChannel     *Channel

	msgParts map[string]*partitionedMessage

	seqno        uint64
	confirmSeqno uint64
	loss         uint64

	serverPubKey ed25519.PublicKey

	activeQueries map[string]chan tl.Serializable

	customMessageHandler CustomMessageHandler
	onDisconnect         func(addr string, key ed25519.PublicKey)

	mx sync.RWMutex
}

type Channel struct {
	adnl *ADNL

	key   ed25519.PrivateKey
	ready bool

	encKey []byte
	decKey []byte

	initDate int32
}

func NewADNL(key ed25519.PublicKey) (*ADNL, error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, err
	}

	a := &ADNL{
		reinitTime:      uint32(time.Now().Unix()),
		pubKey:          pub,
		privKey:         priv,
		serverPubKey:    key,
		channels:        map[string]*Channel{},
		pendingChannels: map[string]*Channel{},
		msgParts:        map[string]*partitionedMessage{},
		activeQueries:   map[string]chan tl.Serializable{},
	}

	return a, nil
}

func (a *ADNL) Close() {
	con := a.conn
	if con != nil {
		con.Close()
	}
}

var Logger = log.Println

func (a *ADNL) Connect(ctx context.Context, addr string) (err error) {
	if a.conn != nil {
		return fmt.Errorf("already connected")
	}

	timeout := 30 * time.Second

	// add default timeout
	if till, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	} else {
		timeout = till.Sub(time.Now())
	}

	a.mx.Lock()
	a.addr = addr

	a.conn, err = net.DialTimeout("udp", addr, timeout)
	a.mx.Unlock()
	if err != nil {
		return err
	}

	rootID, err := ToKeyID(PublicKeyED25519{Key: a.privKey.Public().(ed25519.PublicKey)})
	if err != nil {
		return err
	}

	go func() {
		defer func() {
			a.mx.Lock()
			a.conn = nil
			a.mx.Unlock()

			if a.onDisconnect != nil {
				a.onDisconnect(addr, a.serverPubKey)
			}
		}()

		for {
			buf := make([]byte, 4096)
			n, err := a.conn.Read(buf)
			if err != nil {
				Logger("failed to read data", err)
				return
			}

			buf = buf[:n]
			id := buf[:32]
			buf = buf[32:]

			var packet *PacketContent
			var ch *Channel
			if bytes.Equal(id, rootID) { // message in root connection
				dec, err := a.decodePacket(buf)
				if err != nil {
					Logger("failed to decode packet", err)
					return
				}

				packet, err = a.parsePacket(dec)
				if err != nil {
					Logger("failed to parse packet", err)
					return
				}
			} else { // message in channel
				keyID := hex.EncodeToString(id)
				ch = a.channels[keyID]
				if ch == nil {
					Logger("unknown receiver id", keyID)
					continue
				}

				dec, err := ch.decodePacket(buf)
				if err != nil {
					Logger("failed to decode packet", err)
					continue
				}

				// println("PK", hex.EncodeToString(dec))

				packet, err = a.parsePacket(dec)
				if err != nil {
					Logger("failed to parse packet", err)
					continue
				}
			}

			seqno := uint64(*packet.Seqno)

			if a.confirmSeqno+1 < seqno {
				a.loss += seqno - (a.confirmSeqno + 1)
			}

			if seqno > a.confirmSeqno {
				a.confirmSeqno = uint64(*packet.Seqno)
			}

			for i, message := range packet.Messages {
				err = a.processMessage(message)
				if err != nil {
					log.Printf("failed to process message %d %s: %v", i, reflect.TypeOf(message), err)
					return
				}
			}
		}
	}()

	return nil
}

func (a *ADNL) processMessage(message any) error {
	switch ms := message.(type) {
	case MessageAnswer:
		a.processAnswer(hex.EncodeToString(ms.ID), ms.Data)
	case MessageConfirmChannel:
		ch := a.pendingChannels[hex.EncodeToString(ms.PeerKey[:])]
		if ch == nil {
			// not required confirmation, skip it
			return nil
		}

		chID, err := ch.setup(ms.Key[:])
		if err != nil {
			return fmt.Errorf("failed to setup channel: %w", err)
		}

		a.channels[hex.EncodeToString(chID)] = ch
		delete(a.pendingChannels, hex.EncodeToString(ms.PeerKey[:]))
	case MessagePart:
		msgID := hex.EncodeToString(ms.Hash)
		p, ok := a.msgParts[msgID]
		if !ok {
			// 1 MB max
			if ms.TotalSize > 1024*1000 {
				return fmt.Errorf("skip too big partitioned message with len %d bytes", ms.TotalSize)
			}

			p = newPartitionedMessage(ms.TotalSize)
			a.msgParts[msgID] = p
		}

		ready, err := p.AddPart(ms.Offset, ms.Data)
		if err != nil {
			return fmt.Errorf("failed to add message part: %w", err)
		}

		if ready {
			var msg any
			_, err = tl.Parse(&msg, p.Build(), true)
			if err != nil {
				return fmt.Errorf("failed to parse message answer from parts: %w", err)
			}

			err = a.processMessage(msg)
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
		panic(reflect.TypeOf(ms).String())
		return fmt.Errorf("skipped unprocessable message of type %s", reflect.TypeOf(message).String())
	}

	return nil
}

func (a *ADNL) SetCustomMessageHandler(handler func(msg *MessageCustom) error) {
	a.customMessageHandler = handler
}

func (a *ADNL) SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey)) {
	a.onDisconnect = handler
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

func (c *Channel) SendCustomMessage(ctx context.Context, req tl.Serializable) error {
	return c.adnl.sendCustomMessage(ctx, c, req)
}

func (a *ADNL) SendCustomMessage(ctx context.Context, req tl.Serializable) error {
	return a.sendCustomMessage(ctx, nil, req)
}

func (a *ADNL) sendCustomMessage(ctx context.Context, ch *Channel, req tl.Serializable) error {
	err := a.sendRequest(ctx, ch, &MessageCustom{
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

	if err = a.sendRequest(ctx, ch, q); err != nil {
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
		return fmt.Errorf("deadline exceeded, addr %s %s, err: %w", a.addr, hex.EncodeToString(a.serverPubKey), ctx.Err())
	}
}

func (a *ADNL) sendRequest(ctx context.Context, ch *Channel, req tl.Serializable) (err error) {
	a.mx.Lock()
	defer a.mx.Unlock()

	if a.conn == nil {
		return fmt.Errorf("ADNL connection is not active")
	}

	if ch != nil && !ch.ready {
		return fmt.Errorf("channel is not ready yet")
	}

	seqno := a.seqno + 1

	var buf []byte

	// if channel == nil, we will use root channel,
	if ch == nil {
		if a.rootChannel != nil && a.rootChannel.ready {
			// root channel is ready
			ch = a.rootChannel
		} else {
			// if it is not exists, we will create it,
			if a.rootChannel == nil {
				pubKey, key, err := ed25519.GenerateKey(nil)
				if err != nil {
					return err
				}

				a.rootChannel = &Channel{
					adnl:     a,
					key:      key,
					initDate: int32(time.Now().Unix()),
				}

				a.pendingChannels[hex.EncodeToString(pubKey)] = a.rootChannel
			}

			// if it is pending, we will send creation (can be sent multiple times, it is ok)
			chMsg := &MessageCreateChannel{
				Key:  a.rootChannel.key.Public().(ed25519.PublicKey),
				Date: a.rootChannel.initDate,
			}

			buf, err = a.createPacket(int64(seqno), chMsg, req)
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

	_ = a.conn.SetWriteDeadline(dl)
	for len(buf) > 0 {
		n, err := a.conn.Write(buf)
		if err != nil {
			// it should trigger disconnect handler in read routine
			a.conn.Close()
			return err
		}

		buf = buf[n:]
	}
	return nil
}

func (a *ADNL) decodePacket(packet []byte) ([]byte, error) {
	pub := packet[0:32]
	checksum := packet[32:64]
	data := packet[64:]

	key, err := SharedKey(a.privKey, pub)
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

func (c *Channel) decodePacket(packet []byte) ([]byte, error) {
	checksum := packet[0:32]
	data := packet[32:]

	ctr, err := BuildSharedCipher(c.decKey, checksum)
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

func (c *Channel) setup(serverKey ed25519.PublicKey) (_ []byte, err error) {
	c.decKey, err = SharedKey(c.key, serverKey)
	if err != nil {
		return nil, err
	}

	c.encKey = make([]byte, len(c.decKey))
	for i := 0; i < len(c.decKey); i++ {
		c.encKey[(len(c.decKey)-1)-i] = c.decKey[i]
	}

	theirID, err := ToKeyID(PublicKeyED25519{c.adnl.serverPubKey})
	if err != nil {
		return nil, err
	}

	ourID, err := ToKeyID(PublicKeyED25519{c.adnl.privKey.Public().(ed25519.PublicKey)})
	if err != nil {
		return nil, err
	}

	// if serverID < ourID, swap keys. if same -> copy enc key
	if eq := new(big.Int).SetBytes(theirID).Cmp(new(big.Int).SetBytes(ourID)); eq == -1 {
		c.encKey, c.decKey = c.decKey, c.encKey
	} else if eq == 0 {
		c.encKey = c.decKey
	}

	ourAESID, err := ToKeyID(PublicKeyAES{Key: c.decKey})
	if err != nil {
		return nil, err
	}

	c.ready = true
	return ourAESID, nil
}

func (c *Channel) createPacket(seqno int64, msgs ...any) ([]byte, error) {
	confSeq := int64(c.adnl.confirmSeqno)
	packet := &PacketContent{
		Messages:     msgs,
		Seqno:        &seqno,
		ConfirmSeqno: &confSeq,
	}

	packetData, err := packet.Serialize()
	if err != nil {
		return nil, err
	}

	hash := sha256.New()
	hash.Write(packetData)
	checksum := hash.Sum(nil)

	ctr, err := BuildSharedCipher(c.encKey, checksum)
	if err != nil {
		return nil, err
	}

	ctr.XORKeyStream(packetData, packetData)

	id, err := ToKeyID(PublicKeyAES{Key: c.encKey})
	if err != nil {
		return nil, err
	}

	enc := id
	enc = append(enc, checksum...)
	enc = append(enc, packetData...)

	return enc, nil
}

func (a *ADNL) createPacket(seqno int64, msgs ...any) ([]byte, error) {
	confSeq := int64(a.confirmSeqno)
	reinit := int32(a.reinitTime)
	dstReinit := int32(0)
	addr := &address.List{
		Version:    int32(a.reinitTime),
		ReinitDate: int32(a.reinitTime),
	}

	packet := &PacketContent{
		From:                &PublicKeyED25519{Key: a.pubKey},
		Address:             addr,
		Messages:            msgs,
		Seqno:               &seqno,
		ConfirmSeqno:        &confSeq,
		ReinitDate:          &reinit,
		DstReinitDate:       &dstReinit,
		RecvAddrListVersion: &reinit,
	}

	toSign, err := packet.Serialize()
	if err != nil {
		return nil, err
	}

	packet.Signature = ed25519.Sign(a.privKey, toSign)

	packetData, err := packet.Serialize()
	if err != nil {
		return nil, err
	}

	hash := sha256.New()
	hash.Write(packetData)
	checksum := hash.Sum(nil)

	key, err := SharedKey(a.privKey, a.serverPubKey)
	if err != nil {
		return nil, err
	}

	ctr, err := BuildSharedCipher(key, checksum)
	if err != nil {
		return nil, err
	}

	enc, err := ToKeyID(PublicKeyED25519{Key: a.serverPubKey})
	if err != nil {
		return nil, err
	}

	ctr.XORKeyStream(packetData, packetData)

	enc = append(enc, a.pubKey...)
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

	data, err := tl.Serialize(query, true)
	if err != nil {
		return nil, err
	}

	return &MessageQuery{
		ID:   qid,
		Data: data,
	}, nil
}
