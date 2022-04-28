package liteclient

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"

	"github.com/oasisprotocol/curve25519-voi/curve"
	ed25519crv "github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/oasisprotocol/curve25519-voi/primitives/x25519"
)

func keyID(key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("key not 32 bytes")
	}

	// https://github.com/ton-blockchain/ton/blob/24dc184a2ea67f9c47042b4104bbb4d82289fac1/crypto/block/check-proof.cpp#L488
	magic := []byte{0xc6, 0xb4, 0x13, 0x48}
	hash := sha256.New()
	hash.Write(magic)
	hash.Write(key)
	s := hash.Sum(nil)

	return s, nil
}

// generate encryption key based on our and server key, ECDH algorithm
func sharedKey(ourKey ed25519.PrivateKey, serverKey ed25519.PublicKey) ([]byte, error) {
	comp, err := curve.NewCompressedEdwardsYFromBytes(serverKey)
	if err != nil {
		return nil, err
	}

	ep, err := curve.NewEdwardsPoint().SetCompressedY(comp)
	if err != nil {
		return nil, err
	}

	mp := curve.NewMontgomeryPoint().SetEdwards(ep)
	bb := x25519.EdPrivateKeyToX25519(ed25519crv.PrivateKey(ourKey))

	key, err := x25519.X25519(bb, mp[:])
	if err != nil {
		return nil, err
	}

	return key, nil
}

func newCipherCtr(key, iv []byte) (cipher.Stream, error) {
	c, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	return cipher.NewCTR(c, iv), nil
}

func (c *Client) validatePacket(data []byte, recvChecksum []byte) error {
	if len(data) < 32 {
		return errors.New("too small packet")
	}

	hash := sha256.New()
	hash.Write(data)
	checksum := hash.Sum(nil)

	if !bytes.Equal(recvChecksum, checksum) {
		return errors.New("checksum packet")
	}

	return nil
}
