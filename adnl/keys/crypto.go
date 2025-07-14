package keys

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha512"
	"filippo.io/edwards25519" // lib from core golang developer, based on go source with extended features
)

// SharedKey - Generate encryption key based on our and server key, ECDH algorithm
func SharedKey(ourKey ed25519.PrivateKey, serverKey ed25519.PublicKey) ([]byte, error) {
	privateKey, err := ecdh.X25519().NewPrivateKey(Ed25519PrivateToX25519(ourKey))
	if err != nil {
		return nil, err
	}

	pubX, err := Ed25519PubToX25519(serverKey)
	if err != nil {
		return nil, err
	}

	pubKey, err := ecdh.X25519().NewPublicKey(pubX)
	if err != nil {
		return nil, err
	}
	return privateKey.ECDH(pubKey)
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

func Ed25519PrivateToX25519(edPrivate ed25519.PrivateKey) []byte {
	h := sha512.Sum512(edPrivate.Seed())
	h[0] &= 248
	h[31] &= 127
	h[31] |= 64
	return h[:32]
}

func Ed25519PubToX25519(edPub ed25519.PublicKey) ([]byte, error) {
	// convert ed pub key to ec pub key
	point := new(edwards25519.Point)
	_, err := point.SetBytes(edPub)
	if err != nil {
		return nil, err
	}
	return point.BytesMontgomery(), nil
}
