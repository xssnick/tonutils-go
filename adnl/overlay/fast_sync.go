package overlay

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

const memberCertificateExpiryGrace = 3 * time.Second

func init() {
	tl.Register(MemberCertificateID{}, "overlay.memberCertificateId node:adnl.id.short flags:int slot:int expire_at:int = overlay.MemberCertificateId")
	tl.Register(MemberCertificate{}, "overlay.memberCertificate issued_by:PublicKey flags:int slot:int expire_at:int signature:bytes = overlay.MemberCertificate")
	tl.Register(EmptyMemberCertificate{}, "overlay.emptyMemberCertificate = overlay.MemberCertificate")
	tl.Register(NodeToSignEx{}, "overlay.node.toSignEx id:adnl.id.short overlay:int256 flags:int version:int = overlay.node.ToSign")
	tl.Register(NodeV2{}, "overlay.nodeV2 id:PublicKey overlay:int256 flags:int version:int signature:bytes certificate:overlay.MemberCertificate = overlay.NodeV2")
	tl.Register(NodesV2{}, "overlay.nodesV2 nodes:(vector overlay.nodeV2) = overlay.NodesV2")
	tl.Register(GetRandomPeersV2{}, "overlay.getRandomPeersV2 peers:overlay.nodesV2 = overlay.NodesV2")
	tl.Register(MessageExtra{}, "overlay.messageExtra flags:# certificate:flags.0?overlay.MemberCertificate = overlay.MessageExtra")
	tl.Register(MessageWithExtra{}, "overlay.messageWithExtra overlay:int256 extra:overlay.messageExtra = overlay.Message")
	tl.Register(QueryWithExtra{}, "overlay.queryWithExtra overlay:int256 extra:overlay.messageExtra = True")
}

type MemberCertificateID struct {
	Node     []byte `tl:"int256"`
	Flags    uint32 `tl:"int"`
	Slot     int32  `tl:"int"`
	ExpireAt int32  `tl:"int"`
}

type MemberCertificate struct {
	IssuedBy  any    `tl:"struct boxed [pub.ed25519]"`
	Flags     uint32 `tl:"int"`
	Slot      int32  `tl:"int"`
	ExpireAt  int32  `tl:"int"`
	Signature []byte `tl:"bytes"`
}

type EmptyMemberCertificate struct{}

// IssuerID returns the ADNL short ID of the certificate issuer.
func (c MemberCertificate) IssuerID() ([]byte, error) {
	issuer, err := c.issuerKey()
	if err != nil {
		return nil, err
	}

	id, err := tl.Hash(keys.PublicKeyED25519{Key: issuer})
	if err != nil {
		return nil, fmt.Errorf("calculating member certificate issuer id: %w", err)
	}

	return id, nil
}

// IsExpired applies the same three-second clock-skew allowance as cppnode.
func (c MemberCertificate) IsExpired(now time.Time) bool {
	deadline := time.Unix(int64(c.ExpireAt), 0).Add(memberCertificateExpiryGrace)
	return now.After(deadline)
}

// CheckSlot verifies that the certificate slot is in [0, maxSlots).
func (c MemberCertificate) CheckSlot(maxSlots int32) error {
	if c.Slot < 0 || c.Slot >= maxSlots {
		return fmt.Errorf("member certificate slot %d is outside [0,%d)", c.Slot, maxSlots)
	}

	return nil
}

// CheckSignature verifies that the issuer signed this certificate for nodeID.
func (c MemberCertificate) CheckSignature(nodeID []byte) error {
	if len(nodeID) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid member certificate node id size %d", len(nodeID))
	}
	if len(c.Signature) != ed25519.SignatureSize {
		return fmt.Errorf("invalid member certificate signature size %d", len(c.Signature))
	}

	issuer, err := c.issuerKey()
	if err != nil {
		return err
	}

	toSign, err := tl.Serialize(MemberCertificateID{
		Node:     nodeID,
		Flags:    c.Flags,
		Slot:     c.Slot,
		ExpireAt: c.ExpireAt,
	}, true)
	if err != nil {
		return fmt.Errorf("serializing member certificate id: %w", err)
	}

	if !ed25519.Verify(issuer, toSign, c.Signature) {
		return fmt.Errorf("invalid member certificate signature")
	}

	return nil
}

// Validate checks the stateless member-certificate invariants.
func (c MemberCertificate) Validate(nodeID []byte, now time.Time, maxSlots int32) error {
	if c.IsExpired(now) {
		return fmt.Errorf("member certificate is expired")
	}
	if err := c.CheckSlot(maxSlots); err != nil {
		return err
	}
	if err := c.CheckSignature(nodeID); err != nil {
		return err
	}

	return nil
}

func (c MemberCertificate) issuerKey() (ed25519.PublicKey, error) {
	issuer, ok := c.IssuedBy.(keys.PublicKeyED25519)
	if !ok {
		return nil, fmt.Errorf("unsupported member certificate issuer type %T", c.IssuedBy)
	}
	if len(issuer.Key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid member certificate issuer key size %d", len(issuer.Key))
	}

	return issuer.Key, nil
}

type NodeToSignEx struct {
	ID      []byte `tl:"int256"`
	Overlay []byte `tl:"int256"`
	Flags   uint32 `tl:"int"`
	Version int32  `tl:"int"`
}

type NodeV2 struct {
	ID          any    `tl:"struct boxed [pub.ed25519,pub.aes]"`
	Overlay     []byte `tl:"int256"`
	Flags       uint32 `tl:"int"`
	Version     int32  `tl:"int"`
	Signature   []byte `tl:"bytes"`
	Certificate any    `tl:"struct boxed [overlay.emptyMemberCertificate,overlay.memberCertificate]"`
}

// Sign signs the node descriptor with its advertised Ed25519 key.
func (n *NodeV2) Sign(key ed25519.PrivateKey) error {
	if len(key) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid node private key size %d", len(key))
	}

	publicKey, toSign, err := n.signingData()
	if err != nil {
		return err
	}
	if !bytes.Equal(publicKey, key[ed25519.SeedSize:]) {
		return fmt.Errorf("node private key does not match advertised id")
	}

	n.Signature = ed25519.Sign(key, toSign)
	return nil
}

// CheckSignature verifies the node descriptor signature.
func (n *NodeV2) CheckSignature() error {
	if len(n.Signature) != ed25519.SignatureSize {
		return fmt.Errorf("invalid node signature size %d", len(n.Signature))
	}

	publicKey, toSign, err := n.signingData()
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, toSign, n.Signature) {
		return fmt.Errorf("invalid node signature")
	}

	return nil
}

func (n *NodeV2) signingData() (ed25519.PublicKey, []byte, error) {
	publicKey, ok := n.ID.(keys.PublicKeyED25519)
	if !ok {
		return nil, nil, fmt.Errorf("unsupported node id type %T", n.ID)
	}
	if len(publicKey.Key) != ed25519.PublicKeySize {
		return nil, nil, fmt.Errorf("invalid node public key size %d", len(publicKey.Key))
	}
	if len(n.Overlay) != ed25519.PublicKeySize {
		return nil, nil, fmt.Errorf("invalid node overlay id size %d", len(n.Overlay))
	}

	shortID, err := tl.Hash(publicKey)
	if err != nil {
		return nil, nil, fmt.Errorf("calculating node id: %w", err)
	}

	if n.Flags == 0 {
		toSign, err := tl.Serialize(NodeToSign{
			ID:      shortID,
			Overlay: n.Overlay,
			Version: n.Version,
		}, true)
		if err != nil {
			return nil, nil, fmt.Errorf("serializing node signature data: %w", err)
		}

		return publicKey.Key, toSign, nil
	}

	toSign, err := tl.Serialize(NodeToSignEx{
		ID:      shortID,
		Overlay: n.Overlay,
		Flags:   n.Flags,
		Version: n.Version,
	}, true)
	if err != nil {
		return nil, nil, fmt.Errorf("serializing node signature data: %w", err)
	}

	return publicKey.Key, toSign, nil
}

type NodesV2 struct {
	Nodes []NodeV2 `tl:"vector struct"`
}

type GetRandomPeersV2 struct {
	Peers NodesV2 `tl:"struct"`
}

type MessageExtra struct {
	Flags       uint32 `tl:"flags"`
	Certificate any    `tl:"?0 struct boxed [overlay.emptyMemberCertificate,overlay.memberCertificate]"`
}

type MessageWithExtra struct {
	Overlay []byte       `tl:"int256"`
	Extra   MessageExtra `tl:"struct"`
}

type QueryWithExtra struct {
	Overlay []byte       `tl:"int256"`
	Extra   MessageExtra `tl:"struct"`
}
