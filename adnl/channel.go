package adnl

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"github.com/xssnick/tonutils-go/tl"
	"math/big"
	"sync/atomic"
)

type Channel struct {
	adnl *ADNL

	key         ed25519.PrivateKey
	peerKey     ed25519.PublicKey
	ready       bool
	wantConfirm bool

	id     []byte
	idEnc  []byte
	encKey []byte
	decKey []byte

	initDate int32
}

func (c *Channel) SendCustomMessage(ctx context.Context, req tl.Serializable) error {
	return c.adnl.SendCustomMessage(ctx, req)
}

func (c *Channel) decodePacket(packet []byte) ([]byte, error) {
	checksum := packet[0:32]
	data := packet[32:]

	ctr, err := BuildSharedCipher(c.decKey, checksum)
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

func (c *Channel) setup(theirKey ed25519.PublicKey) (err error) {
	c.peerKey = theirKey
	c.decKey, err = SharedKey(c.key, c.peerKey)
	if err != nil {
		return err
	}

	c.encKey = make([]byte, len(c.decKey))
	for i := 0; i < len(c.decKey); i++ {
		c.encKey[(len(c.decKey)-1)-i] = c.decKey[i]
	}

	theirID, err := tl.Hash(PublicKeyED25519{c.adnl.peerKey})
	if err != nil {
		return err
	}

	ourID, err := tl.Hash(PublicKeyED25519{c.adnl.ourKey.Public().(ed25519.PublicKey)})
	if err != nil {
		return err
	}

	// if serverID < ourID, swap keys. if same -> copy enc key
	if eq := new(big.Int).SetBytes(theirID).Cmp(new(big.Int).SetBytes(ourID)); eq == -1 {
		c.encKey, c.decKey = c.decKey, c.encKey
	} else if eq == 0 {
		c.encKey = c.decKey
	}

	c.id, err = tl.Hash(PublicKeyAES{Key: c.decKey})
	if err != nil {
		return err
	}

	c.idEnc, err = tl.Hash(PublicKeyAES{Key: c.encKey})
	if err != nil {
		return err
	}

	h := c.adnl.onChannel
	if h != nil {
		h(c)
	}

	c.ready = true

	return nil
}

func (c *Channel) createPacket(seqno int64, msgs ...any) ([]byte, error) {
	data := make([]byte, 32)
	_, err := rand.Read(data)
	if err != nil {
		return nil, err
	}

	rand1, err := resizeRandForPacket(data[:16])
	if err != nil {
		return nil, err
	}

	rand2, err := resizeRandForPacket(data[16:])
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

	buf := bytes.NewBuffer(make([]byte, 64))
	buf.Grow(MaxMTU - 64)

	payloadSz, err := packet.Serialize(buf)
	if err != nil {
		return nil, err
	}

	bufBytes := buf.Bytes()
	packetBytes := bufBytes[64:]

	atomic.StoreUint32(&c.adnl.prevPacketHeaderSz, uint32(len(packetBytes)-payloadSz))

	hash := sha256.Sum256(packetBytes)
	checksum := hash[:]

	ctr, err := BuildSharedCipher(c.encKey, checksum)
	if err != nil {
		return nil, err
	}

	ctr.XORKeyStream(packetBytes, packetBytes)

	copy(bufBytes, c.idEnc)
	copy(bufBytes[32:], checksum)

	return bufBytes, nil
}
