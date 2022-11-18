package adnl

import (
	"crypto/ed25519"
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
	Key ed25519.PublicKey `tl:"int256"`
}

type PublicKeyAES struct {
	Key []byte `tl:"int256"`
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
