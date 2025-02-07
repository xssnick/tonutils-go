package adnl

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"github.com/xssnick/tonutils-go/tl"
)

func init() {
	tl.Register(PublicKeyED25519{}, "pub.ed25519 key:int256 = PublicKey")
	tl.Register(PublicKeyAES{}, "pub.aes key:int256 = PublicKey")
	tl.Register(PublicKeyOverlay{}, "pub.overlay name:bytes = PublicKey")
	tl.Register(PublicKeyUnEnc{}, "pub.unenc data:bytes = PublicKey")

	tl.Register(PrivateKeyAES{}, "pk.aes key:int256 = PrivateKey")
}

type PublicKeyED25519 struct {
	Key ed25519.PublicKey // `tl:"int256"`
}

func (p *PublicKeyED25519) Parse(data []byte) ([]byte, error) {
	if len(data) < ed25519.PublicKeySize {
		return nil, errors.New("too short ed25519 public key")
	}

	p.Key = make([]byte, ed25519.PublicKeySize)
	copy(p.Key, data)

	return data[ed25519.PublicKeySize:], nil
}

func (p *PublicKeyED25519) Serialize(buf *bytes.Buffer) error {
	if len(p.Key) != ed25519.PublicKeySize {
		return errors.New("invalid public key")
	}
	buf.Write(p.Key)
	return nil
}

type PublicKeyAES struct {
	Key []byte // `tl:"int256"`
}

func (p *PublicKeyAES) Parse(data []byte) ([]byte, error) {
	if len(data) < ed25519.PublicKeySize {
		return nil, errors.New("too short aes key")
	}

	p.Key = make([]byte, ed25519.PublicKeySize)
	copy(p.Key, data)

	return data[ed25519.PublicKeySize:], nil
}

func (p *PublicKeyAES) Serialize(buf *bytes.Buffer) error {
	if len(p.Key) != ed25519.PublicKeySize {
		return errors.New("invalid aes key")
	}
	buf.Write(p.Key)
	return nil
}

type PublicKeyUnEnc struct {
	Key []byte `tl:"bytes"`
}

type PublicKeyOverlay struct {
	Key []byte `tl:"bytes"`
}

type PrivateKeyAES struct {
	Key []byte `tl:"int256"`
}
