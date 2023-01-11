package adnl

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"github.com/xssnick/tonutils-go/tl"
	"math/big"
)

type Channel struct {
	adnl *ADNL

	key         ed25519.PrivateKey
	peerKey     ed25519.PublicKey
	ready       bool
	wantConfirm bool

	id     []byte
	encKey []byte
	decKey []byte

	initDate int32
}

func (c *Channel) SendCustomMessage(ctx context.Context, req tl.Serializable) error {
	return c.adnl.sendCustomMessage(ctx, c, req)
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

	theirID, err := ToKeyID(PublicKeyED25519{c.adnl.peerKey})
	if err != nil {
		return err
	}

	ourID, err := ToKeyID(PublicKeyED25519{c.adnl.ourKey.Public().(ed25519.PublicKey)})
	if err != nil {
		return err
	}

	// if serverID < ourID, swap keys. if same -> copy enc key
	if eq := new(big.Int).SetBytes(theirID).Cmp(new(big.Int).SetBytes(ourID)); eq == -1 {
		c.encKey, c.decKey = c.decKey, c.encKey
	} else if eq == 0 {
		c.encKey = c.decKey
	}

	c.id, err = ToKeyID(PublicKeyAES{Key: c.decKey})
	if err != nil {
		return err
	}

	c.ready = true

	h := c.adnl.onChannel
	if h != nil {
		h(c)
	}

	return nil
}

func (c *Channel) createPacket(seqno int64, msgs ...any) ([]byte, error) {
	rand1, err := randForPacket()
	if err != nil {
		return nil, err
	}

	rand2, err := randForPacket()
	if err != nil {
		return nil, err
	}

	confSeq := int64(c.adnl.confirmSeqno)
	packet := &PacketContent{
		Rand1:        rand1,
		Messages:     msgs,
		Seqno:        &seqno,
		ConfirmSeqno: &confSeq,
		Rand2:        rand2,
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

	enc, err := ToKeyID(PublicKeyAES{Key: c.encKey})
	if err != nil {
		return nil, err
	}
	enc = append(enc, checksum...)
	enc = append(enc, packetData...)

	return enc, nil
}
