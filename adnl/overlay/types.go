package overlay

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
	"reflect"
	"time"
)

func init() {
	tl.Register(Node{}, "overlay.node id:PublicKey overlay:int256 version:int signature:bytes = overlay.Node")
	tl.Register(NodeToSign{}, "overlay.node.toSign id:adnl.id.short overlay:int256 version:int = overlay.node.ToSign")
	tl.Register(NodesList{}, "overlay.nodes nodes:(vector overlay.node) = overlay.Nodes")
	tl.Register(GetRandomPeers{}, "overlay.getRandomPeers peers:overlay.nodes = overlay.Nodes")
	tl.Register(Query{}, "overlay.query overlay:int256 = True")
	tl.Register(Message{}, "overlay.message overlay:int256 = overlay.Message")
	tl.Register(Certificate{}, "overlay.certificate issued_by:PublicKey expire_at:int max_size:int signature:bytes = overlay.Certificate")
	tl.Register(CertificateV2{}, "overlay.certificateV2 issued_by:PublicKey expire_at:int max_size:int flags:int signature:bytes = overlay.Certificate")
	tl.Register(CertificateEmpty{}, "overlay.emptyCertificate = overlay.Certificate")
	tl.Register(CertificateId{}, "overlay.certificateId overlay_id:int256 node:int256 expire_at:int max_size:int = overlay.CertificateId")
	tl.Register(CertificateIdV2{}, "overlay.certificateIdV2 overlay_id:int256 node:int256 expire_at:int max_size:int flags:int = overlay.CertificateId")
	tl.Register(Broadcast{}, "overlay.broadcast src:PublicKey certificate:overlay.Certificate flags:int data:bytes date:int signature:bytes = overlay.Broadcast")
	tl.Register(BroadcastFEC{}, "overlay.broadcastFec src:PublicKey certificate:overlay.Certificate data_hash:int256 data_size:int flags:int data:bytes seqno:int fec:fec.Type date:int signature:bytes = overlay.Broadcast")
	tl.Register(BroadcastFECShort{}, "overlay.broadcastFecShort src:PublicKey certificate:overlay.Certificate broadcast_hash:int256 part_data_hash:int256 seqno:int signature:bytes = overlay.Broadcast")
	tl.Register(BroadcastFECID{}, "overlay.broadcastFec.id src:int256 type:int256 data_hash:int256 size:int flags:int = overlay.broadcastFec.Id")
	tl.Register(BroadcastFECPartID{}, "overlay.broadcastFec.partId broadcast_hash:int256 data_hash:int256 seqno:int = overlay.broadcastFec.PartId")
	tl.Register(BroadcastToSign{}, "overlay.broadcast.toSign hash:int256 date:int = overlay.broadcast.ToSign")
	tl.Register(FECReceived{}, "overlay.fec.received hash:int256 = overlay.Broadcast")
	tl.Register(FECCompleted{}, "overlay.fec.completed hash:int256 = overlay.Broadcast")
}

type CheckableCert interface {
	Check(issuedToId []byte, overlayId []byte, dataSize int32, isFEC bool) (CertCheckResult, error)
}

type CertificateEmpty struct{}

type Certificate struct {
	IssuedBy  any    `tl:"struct boxed [pub.ed25519]"`
	ExpireAt  int32  `tl:"int"`
	MaxSize   int32  `tl:"int"`
	Signature []byte `tl:"bytes"`
}

func (c Certificate) Check(issuedToId []byte, overlayId []byte, dataSize int32, isFEC bool) (CertCheckResult, error) {
	if dataSize > c.MaxSize {
		return CertCheckResultForbidden, nil
	}
	if time.Now().UTC().Unix() > int64(c.ExpireAt) {
		return CertCheckResultForbidden, nil
	}

	toSign, err := tl.Serialize(CertificateId{
		OverlayID: overlayId,
		Node:      issuedToId,
		ExpireAt:  c.ExpireAt,
		MaxSize:   c.MaxSize,
	}, true)
	if err != nil {
		return CertCheckResultForbidden, err
	}

	issuer, ok := c.IssuedBy.(adnl.PublicKeyED25519)
	if !ok {
		return CertCheckResultForbidden, fmt.Errorf("unsupported issuer key format")
	}

	if !ed25519.Verify(issuer.Key, toSign, c.Signature) {
		return CertCheckResultForbidden, fmt.Errorf("incorrect cert signature")
	}
	return CertCheckResultTrusted, nil
}

type CertificateId struct {
	OverlayID []byte `tl:"int256"`
	Node      []byte `tl:"int256"`
	ExpireAt  int32  `tl:"int"`
	MaxSize   int32  `tl:"int"`
}

type CertificateIdV2 struct {
	OverlayID []byte `tl:"int256"`
	Node      []byte `tl:"int256"`
	ExpireAt  int32  `tl:"int"`
	MaxSize   int32  `tl:"int"`
	Flags     int32  `tl:"int"`
}

type CertificateV2 struct {
	IssuedBy  any    `tl:"struct boxed [pub.ed25519]"`
	ExpireAt  int32  `tl:"int"`
	MaxSize   int32  `tl:"int"`
	Flags     int32  `tl:"int"`
	Signature []byte `tl:"bytes"`
}

func (c CertificateV2) Check(issuedToId []byte, overlayId []byte, dataSize int32, isFEC bool) (CertCheckResult, error) {
	if dataSize > c.MaxSize {
		return CertCheckResultForbidden, nil
	}
	if time.Now().UTC().Unix() > int64(c.ExpireAt) {
		return CertCheckResultForbidden, nil
	}
	if isFEC && (c.Flags&_CertFlagAllowFEC) == 0 {
		return CertCheckResultForbidden, nil
	}

	toSign, err := tl.Serialize(CertificateIdV2{
		OverlayID: overlayId,
		Node:      issuedToId,
		ExpireAt:  c.ExpireAt,
		MaxSize:   c.MaxSize,
		Flags:     c.Flags,
	}, true)
	if err != nil {
		return CertCheckResultForbidden, err
	}

	issuer, ok := c.IssuedBy.(adnl.PublicKeyED25519)
	if !ok {
		return CertCheckResultForbidden, fmt.Errorf("unsupported issuer key format")
	}

	if !ed25519.Verify(issuer.Key, toSign, c.Signature) {
		return CertCheckResultForbidden, fmt.Errorf("incorrect cert signature")
	}
	if (c.Flags & _CertFlagTrusted) == 0 {
		return CertCheckResultNeedCheck, nil
	}
	return CertCheckResultTrusted, nil
}

type Broadcast struct {
	Source      any    `tl:"struct boxed [pub.ed25519]"`
	Certificate any    `tl:"struct boxed [overlay.emptyCertificate,overlay.certificate,overlay.certificateV2]"`
	Flags       int32  `tl:"int"`
	Data        []byte `tl:"bytes"`
	Date        int32  `tl:"int"`
	Signature   []byte `tl:"bytes"`
}

type BroadcastFEC struct {
	Source      any    `tl:"struct boxed [pub.ed25519]"`
	Certificate any    `tl:"struct boxed [overlay.emptyCertificate,overlay.certificate,overlay.certificateV2]"`
	DataHash    []byte `tl:"int256"`
	DataSize    int32  `tl:"int"`
	Flags       int32  `tl:"int"`
	Data        []byte `tl:"bytes"`
	Seqno       int32  `tl:"int"`
	FEC         any    `tl:"struct boxed [fec.roundRobin,fec.raptorQ,fec.online]"`
	Date        int32  `tl:"int"`
	Signature   []byte `tl:"bytes"`
}

func (t *BroadcastFEC) CalcID() ([]byte, error) {
	typeId, err := adnl.ToKeyID(t.FEC)
	if err != nil {
		return nil, fmt.Errorf("failed to compute fec type id: %w", err)
	}

	var src = make([]byte, 32)
	if t.Flags&_BroadcastFlagAnySender == 0 {
		src, err = adnl.ToKeyID(t.Source)
		if err != nil {
			return nil, fmt.Errorf("failed to compute source key id: %w", err)
		}
	}

	broadcastHash, err := adnl.ToKeyID(&BroadcastFECID{
		Source:   src,
		Type:     typeId,
		DataHash: t.DataHash,
		Size:     t.DataSize,
		Flags:    t.Flags,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to compute hash id of the broadcast: %w", err)
	}
	return broadcastHash, nil
}

type BroadcastFECShort struct {
	Source        any    `tl:"struct boxed [pub.ed25519]"`
	Certificate   any    `tl:"struct boxed [overlay.emptyCertificate,overlay.certificate,overlay.certificateV2]"`
	BroadcastHash []byte `tl:"int256"`
	PartDataHash  []byte `tl:"int256"`
	Seqno         int32  `tl:"int"`
	Signature     []byte `tl:"bytes"`
}

type BroadcastFECID struct {
	Source   []byte `tl:"int256"`
	Type     []byte `tl:"int256"`
	DataHash []byte `tl:"int256"`
	Size     int32  `tl:"int"`
	Flags    int32  `tl:"int"`
}

type BroadcastFECPartID struct {
	BroadcastHash []byte `tl:"int256"`
	DataHash      []byte `tl:"int256"`
	Seqno         int32  `tl:"int"`
}

type BroadcastToSign struct {
	Hash []byte `tl:"int256"`
	Date int32  `tl:"int"`
}

type Node struct {
	ID        any    `tl:"struct boxed [pub.ed25519,pub.aes]"`
	Overlay   []byte `tl:"int256"`
	Version   int32  `tl:"int"`
	Signature []byte `tl:"bytes"`
}

type NodeToSign struct {
	ID      []byte `tl:"int256"`
	Overlay []byte `tl:"int256"`
	Version int32  `tl:"int"`
}

type NodesList struct {
	List []Node `tl:"vector struct"`
}

type GetRandomPeers struct {
	List NodesList `tl:"struct"`
}

type Query struct {
	Overlay []byte `tl:"int256"`
}

type Message struct {
	Overlay []byte `tl:"int256"`
}

type FECReceived struct {
	Hash []byte `tl:"int256"`
}

type FECCompleted struct {
	Hash []byte `tl:"int256"`
}

func (n *Node) CheckSignature() error {
	pub, ok := n.ID.(adnl.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("unsupported id type %s", reflect.TypeOf(n.ID).String())
	}

	id, err := adnl.ToKeyID(n.ID)
	if err != nil {
		return fmt.Errorf("failed to calc id: %w", err)
	}

	toVerify, err := tl.Serialize(NodeToSign{
		ID:      id,
		Overlay: n.Overlay,
		Version: n.Version,
	}, true)
	if err != nil {
		return fmt.Errorf("failed to serialize node: %w", err)
	}
	if !ed25519.Verify(pub.Key, toVerify, n.Signature) {
		return fmt.Errorf("bad signature for node: %s", hex.EncodeToString(pub.Key))
	}
	return nil
}

func (n *Node) Sign(key ed25519.PrivateKey) error {
	pub, ok := n.ID.(adnl.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("unsupported id type %s", reflect.TypeOf(n.ID).String())
	}

	if !pub.Key.Equal(key.Public()) {
		return fmt.Errorf("incorrect private key")
	}

	id, err := adnl.ToKeyID(n.ID)
	if err != nil {
		return fmt.Errorf("failed to calc id: %w", err)
	}

	toVerify, err := tl.Serialize(NodeToSign{
		ID:      id,
		Overlay: n.Overlay,
		Version: n.Version,
	}, true)
	if err != nil {
		return fmt.Errorf("failed to serialize node: %w", err)
	}
	n.Signature = ed25519.Sign(key, toVerify)

	return nil
}
