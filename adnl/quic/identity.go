package quic

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"strings"
)

type adnlID [32]byte

func (id adnlID) String() string { return hex.EncodeToString(id[:]) }

func (id adnlID) bytes() []byte {
	return append([]byte(nil), id[:]...)
}

// sni encodes an ADNL id as the SNI hostname used by TON QUIC:
// "<hex[0:32]>.<hex[32:64]>.adnl" (lower-case), matching the C++ node's
// ServerIdentity::sni.
func (id adnlID) sni() string {
	h := hex.EncodeToString(id[:])
	return h[:32] + "." + h[32:] + ".adnl"
}

// parseSNI decodes an "<hex>.<hex>.adnl" hostname back into an ADNL id.
func parseSNI(name string) (adnlID, error) {
	var id adnlID
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	rest, ok := strings.CutSuffix(name, ".adnl")
	if !ok {
		return id, fmt.Errorf("quic: SNI %q is not a .adnl name", name)
	}
	parts := strings.Split(rest, ".")
	if len(parts) != 2 || len(parts[0]) != 32 || len(parts[1]) != 32 {
		return id, fmt.Errorf("quic: malformed .adnl SNI %q", name)
	}
	raw, err := hex.DecodeString(parts[0] + parts[1])
	if err != nil {
		return id, fmt.Errorf("quic: SNI hex decode: %w", err)
	}
	copy(id[:], raw)
	return id, nil
}

func idFromPublicKey(key ed25519.PublicKey) (adnlID, error) {
	if len(key) != ed25519.PublicKeySize {
		return adnlID{}, fmt.Errorf("quic: invalid Ed25519 public key size %d", len(key))
	}
	return adnlIDFromKey(key), nil
}

// Identity is a local ADNL identity backed by an Ed25519 private key.
type Identity struct {
	key ed25519.PrivateKey
	id  adnlID
}

// NewIdentity builds an Identity from an Ed25519 private key.
func NewIdentity(key ed25519.PrivateKey) (Identity, error) {
	if len(key) != ed25519.PrivateKeySize {
		return Identity{}, fmt.Errorf("quic: invalid Ed25519 private key size %d", len(key))
	}
	pub, ok := key.Public().(ed25519.PublicKey)
	if !ok {
		return Identity{}, fmt.Errorf("quic: private key does not yield an Ed25519 public key")
	}
	return Identity{key: key, id: adnlIDFromKey(pub)}, nil
}

// ID returns a copy of the ADNL short id of this identity.
func (i Identity) ID() []byte { return i.id.bytes() }

// PublicKey returns the identity's Ed25519 public key.
func (i Identity) PublicKey() ed25519.PublicKey {
	return append(ed25519.PublicKey(nil), i.key.Public().(ed25519.PublicKey)...)
}
