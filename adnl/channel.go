package adnl

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
	"math/big"
	"sync/atomic"
)

type Channel struct {
	adnl *ADNL

	key         ed25519.PrivateKey
	peerKey     ed25519.PublicKey
	ready       atomic.Bool
	wantConfirm atomic.Bool

	id     []byte
	idEnc  []byte
	encKey []byte
	decKey []byte

	initDate int32
	peerDate int32
}

func (c *Channel) SendCustomMessage(ctx context.Context, req tl.Serializable) error {
	return c.adnl.SendCustomMessage(ctx, req)
}

func (c *Channel) decodePacket(packet []byte) ([]byte, error) {
	if len(packet) < 32 {
		return nil, ErrTooShortData
	}

	checksum := packet[0:32]
	data := packet[32:]

	if err := keys.XORSharedStream(data, data, c.decKey, checksum); err != nil {
		return nil, err
	}

	hash := sha256.Sum256(data)
	if !bytes.Equal(hash[:], checksum) {
		return nil, errors.New("invalid checksum of packet")
	}

	return data, nil
}

func (c *Channel) setup(theirKey ed25519.PublicKey) (err error) {
	c.peerKey = append(ed25519.PublicKey(nil), theirKey...)
	c.decKey, err = keys.SharedKey(c.key, c.peerKey)
	if err != nil {
		return err
	}

	c.encKey = make([]byte, len(c.decKey))
	for i := 0; i < len(c.decKey); i++ {
		c.encKey[(len(c.decKey)-1)-i] = c.decKey[i]
	}

	theirID, err := tl.Hash(keys.PublicKeyED25519{c.adnl.peerKey})
	if err != nil {
		return err
	}

	ourID, err := tl.Hash(keys.PublicKeyED25519{c.adnl.ourKey.Public().(ed25519.PublicKey)})
	if err != nil {
		return err
	}

	// if serverID < ourID, swap keys. if same -> copy enc key
	if eq := new(big.Int).SetBytes(theirID).Cmp(new(big.Int).SetBytes(ourID)); eq < 0 {
		c.encKey, c.decKey = c.decKey, c.encKey
	} else if eq == 0 {
		c.encKey = c.decKey
	}

	c.id, err = tl.Hash(keys.PublicKeyAES{Key: c.decKey})
	if err != nil {
		return err
	}

	c.idEnc, err = tl.Hash(keys.PublicKeyAES{Key: c.encKey})
	if err != nil {
		return err
	}

	c.ready.Store(true)
	return nil
}

func (c *Channel) createPacket(seqno int64, msgs ...any) ([]byte, error) {
	buf := getPacketBuild(64)
	defer putPacketBuild(buf)

	rand1, rand2, err := drawPacketRand(buf)
	if err != nil {
		return nil, err
	}

	confSeq := atomic.LoadInt64(&c.adnl.confirmSeqno)
	packet := &PacketContent{
		Rand1:        rand1,
		Messages:     msgs,
		Seqno:        &seqno,
		ConfirmSeqno: &confSeq,
		Rand2:        rand2,
	}

	payloadSz, err := packet.Serialize(buf)
	if err != nil {
		return nil, err
	}
	return c.sealPacket(buf, payloadSz)
}

// createPacketRaw builds a channel packet around one adnl.Message that is
// already in boxed wire form (see PreparedCustomMessage). The packet is
// written field by field, so the send costs no interface box, no reflective
// pass and no heap cell for the seqno fields; serializeChannelPacket is pinned
// byte-for-byte to PacketContent.Serialize by TestSerializeChannelPacketMatchesPacketContent.
func (c *Channel) createPacketRaw(seqno int64, message []byte) ([]byte, error) {
	buf := getPacketBuild(64)
	defer putPacketBuild(buf)

	rand1, rand2, err := drawPacketRand(buf)
	if err != nil {
		return nil, err
	}

	payloadSz := serializeChannelPacket(buf, rand1, rand2, seqno, atomic.LoadInt64(&c.adnl.confirmSeqno), message)
	return c.sealPacket(buf, payloadSz)
}

// drawPacketRand draws the packet's random padding into the header slot of
// the staging buffer, which holds zeros until sealPacket overwrites it with
// the channel id and checksum. A local array handed to crypto/rand is forced
// onto the heap for every packet; the slot is already in hand and is rewritten
// before the packet leaves.
func drawPacketRand(buf *bytes.Buffer) (rand1, rand2 []byte, err error) {
	data := buf.Bytes()[:32]
	if _, err = rand.Read(data); err != nil {
		return nil, nil, err
	}

	if rand1, err = resizeRandForPacket(data[:16]); err != nil {
		return nil, nil, err
	}
	if rand2, err = resizeRandForPacket(data[16:]); err != nil {
		return nil, nil, err
	}
	return rand1, rand2, nil
}

// serializeChannelPacket writes the adnl.packetContents every channel packet
// has: random padding, one message, seqno and confirm_seqno. It returns the
// size of the message section, which is what Serialize reports as payload.
func serializeChannelPacket(buf *bytes.Buffer, rand1, rand2 []byte, seqno, confirmSeqno int64, message []byte) int {
	var tmp [8]byte
	binary.LittleEndian.PutUint32(tmp[:4], _PacketContentID)
	buf.Write(tmp[:4])

	_ = tl.ToBytesToBuffer(buf, rand1)

	binary.LittleEndian.PutUint32(tmp[:4], _FlagOneMessage|_FlagSeqno|_FlagConfirmSeqno)
	buf.Write(tmp[:4])

	buf.Write(message)

	binary.LittleEndian.PutUint64(tmp[:8], uint64(seqno))
	buf.Write(tmp[:8])
	binary.LittleEndian.PutUint64(tmp[:8], uint64(confirmSeqno))
	buf.Write(tmp[:8])

	_ = tl.ToBytesToBuffer(buf, rand2)
	return len(message)
}

// sealPacket checksums and encrypts the serialized body staged after the
// 64-byte header, fills the header in and copies the datagram out of the
// pooled buffer.
func (c *Channel) sealPacket(buf *bytes.Buffer, payloadSz int) ([]byte, error) {
	bufBytes := buf.Bytes()
	packetBytes := bufBytes[64:]

	atomic.StoreUint32(&c.adnl.prevPacketHeaderSz, uint32(len(packetBytes)-payloadSz))

	hash := sha256.Sum256(packetBytes)
	checksum := hash[:]

	if err := keys.XORSharedStream(packetBytes, packetBytes, c.encKey, checksum); err != nil {
		return nil, err
	}

	copy(bufBytes, c.idEnc)
	copy(bufBytes[32:], checksum)

	out := make([]byte, len(bufBytes))
	copy(out, bufBytes)
	return out, nil
}
