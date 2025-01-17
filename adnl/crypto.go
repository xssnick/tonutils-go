package adnl

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"github.com/oasisprotocol/curve25519-voi/curve"
	ed25519crv "github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/oasisprotocol/curve25519-voi/primitives/x25519"
)

// SharedKey - Generate encryption key based on our and server key, ECDH algorithm
func SharedKey(ourKey ed25519.PrivateKey, serverKey ed25519.PublicKey) ([]byte, error) {
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

func BuildSharedCipher(key []byte, checksum []byte) (cipher.Stream, error) {
	kiv := make([]byte, 48)
	// key
	copy(kiv, key[:16])
	copy(kiv[16:], checksum[16:])

	// iv
	copy(kiv[32:], checksum[:4])
	copy(kiv[36:], key[20:])

	ctr, err := NewCipherCtr(kiv[:32], kiv[32:])
	if err != nil {
		return nil, err
	}

	return ctr, nil
}

func NewCipherCtr(key, iv []byte) (cipher.Stream, error) {
	c, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	return cipher.NewCTR(c, iv), nil
}
