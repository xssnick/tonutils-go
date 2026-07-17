package overlay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"math"
	"time"

	"github.com/xssnick/raptorq"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

const DefaultBroadcastTwoStepFECMinBytes uint32 = 513
const DefaultBroadcastTwoStepFECMinPeers = 5

type BroadcastTwoStepMode int

const (
	BroadcastTwoStepModeSimple BroadcastTwoStepMode = iota
	BroadcastTwoStepModeFEC
)

type BroadcastTwoStepSendResult struct {
	BroadcastID []byte
	DataHash    []byte
	Mode        BroadcastTwoStepMode
	Attempted   int
	Sent        int
	Failed      []BroadcastTwoStepPeerError
	DataSize    uint32
	PartSize    uint32
}

type BroadcastTwoStepPeerError struct {
	PeerID []byte
	Err    error
}

type BroadcastTwoStepSendRequest struct {
	Key         ed25519.PrivateKey
	Certificate any
	LocalADNLID []byte
	Payload     []byte
	Extra       []byte
	Flags       int32
	PeerSet     BroadcastPeerSet
}

type BroadcastTwoStepTLSendRequest struct {
	Key         ed25519.PrivateKey
	Certificate any
	LocalADNLID []byte
	Payload     tl.Serializable
	Extra       []byte
	Flags       int32
	PeerSet     BroadcastPeerSet
}

type BroadcastTwoStepSenderOption func(cfg *broadcastTwoStepSenderConfig)

type broadcastTwoStepSenderConfig struct {
	date     uint32
	minBytes uint32
	minPeers int
	now      func() time.Time
}

func WithBroadcastTwoStepDate(date uint32) BroadcastTwoStepSenderOption {
	return func(cfg *broadcastTwoStepSenderConfig) {
		cfg.date = date
	}
}

func WithBroadcastTwoStepFECThreshold(minBytes uint32, minPeers int) BroadcastTwoStepSenderOption {
	return func(cfg *broadcastTwoStepSenderConfig) {
		cfg.minBytes = minBytes
		cfg.minPeers = minPeers
	}
}

func SendBroadcastTwoStep(ctx context.Context, req BroadcastTwoStepSendRequest, opts ...BroadcastTwoStepSenderOption) (BroadcastTwoStepSendResult, error) {
	if len(req.Payload) == 0 {
		return BroadcastTwoStepSendResult{}, fmt.Errorf("payload is empty")
	}
	if uint64(len(req.Payload)) > math.MaxUint32 {
		return BroadcastTwoStepSendResult{}, fmt.Errorf("payload is too large")
	}
	if len(req.LocalADNLID) != 32 {
		return BroadcastTwoStepSendResult{}, fmt.Errorf("local ADNL id should be 32 bytes")
	}
	if req.PeerSet == nil {
		return BroadcastTwoStepSendResult{}, fmt.Errorf("peer set is nil")
	}

	cfg := broadcastTwoStepSenderConfig{
		minBytes: DefaultBroadcastTwoStepFECMinBytes,
		minPeers: DefaultBroadcastTwoStepFECMinPeers,
		now:      time.Now,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.minPeers < 1 {
		cfg.minPeers = 1
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}

	source := keys.PublicKeyED25519{Key: req.Key.Public().(ed25519.PublicKey)}
	if req.Certificate == nil {
		req.Certificate = CertificateEmpty{}
	}
	sourceID, err := tl.Hash(source)
	if err != nil {
		return BroadcastTwoStepSendResult{}, fmt.Errorf("failed to compute source key id: %w", err)
	}

	date := cfg.date
	if date == 0 {
		date = uint32(cfg.now().Unix())
	}
	flags := req.Flags &^ BroadcastFlagNoTwoStep

	peers := broadcastTwoStepSenderPeers(req.PeerSet.Peers(), req.LocalADNLID)
	dataSize := uint32(len(req.Payload))
	dataHash := calcBroadcastTwoStepDataHash(req.Payload)
	if dataSize >= cfg.minBytes && len(peers) >= cfg.minPeers {
		return sendBroadcastTwoStepFEC(ctx, req.Key, source, sourceID, req.Certificate, req.LocalADNLID, req.Payload, dataHash, req.Extra, flags, date, peers)
	}
	return sendBroadcastTwoStepSimple(ctx, req.Key, source, sourceID, req.Certificate, req.LocalADNLID, req.Payload, dataHash, req.Extra, flags, date, peers)
}

func SendBroadcastTwoStepFromTL(ctx context.Context, req BroadcastTwoStepTLSendRequest, opts ...BroadcastTwoStepSenderOption) (BroadcastTwoStepSendResult, error) {
	data, err := tl.Serialize(req.Payload, true)
	if err != nil {
		return BroadcastTwoStepSendResult{}, fmt.Errorf("failed to serialize payload: %w", err)
	}
	return SendBroadcastTwoStep(ctx, BroadcastTwoStepSendRequest{
		Key:         req.Key,
		Certificate: req.Certificate,
		LocalADNLID: req.LocalADNLID,
		Payload:     data,
		Extra:       req.Extra,
		Flags:       req.Flags,
		PeerSet:     req.PeerSet,
	}, opts...)
}

func (a *ADNLOverlayWrapper) SendBroadcastTwoStep(ctx context.Context, req BroadcastTwoStepSendRequest, opts ...BroadcastTwoStepSenderOption) (BroadcastTwoStepSendResult, error) {
	peerSet, localID := a.twoStepRelayConfig()
	if len(req.LocalADNLID) == 0 {
		req.LocalADNLID = localID
	}
	if req.PeerSet == nil {
		req.PeerSet = peerSet
	}
	return SendBroadcastTwoStep(ctx, req, opts...)
}

func broadcastTwoStepSenderPeers(peers []BroadcastPeer, localADNLID []byte) []BroadcastPeer {
	for i, peer := range peers {
		if !bytes.Equal(peer.ID(), localADNLID) {
			continue
		}

		filtered := make([]BroadcastPeer, 0, len(peers)-1)
		filtered = append(filtered, peers[:i]...)
		for _, candidate := range peers[i+1:] {
			if !bytes.Equal(candidate.ID(), localADNLID) {
				filtered = append(filtered, candidate)
			}
		}
		return filtered
	}
	return peers
}

func (a *ADNLOverlayWrapper) SendBroadcastTwoStepFromTL(ctx context.Context, req BroadcastTwoStepTLSendRequest, opts ...BroadcastTwoStepSenderOption) (BroadcastTwoStepSendResult, error) {
	data, err := tl.Serialize(req.Payload, true)
	if err != nil {
		return BroadcastTwoStepSendResult{}, fmt.Errorf("failed to serialize payload: %w", err)
	}
	return a.SendBroadcastTwoStep(ctx, BroadcastTwoStepSendRequest{
		Key:         req.Key,
		Certificate: req.Certificate,
		LocalADNLID: req.LocalADNLID,
		Payload:     data,
		Extra:       req.Extra,
		Flags:       req.Flags,
		PeerSet:     req.PeerSet,
	}, opts...)
}

func sendBroadcastTwoStepSimple(ctx context.Context, key ed25519.PrivateKey, source keys.PublicKeyED25519, sourceID []byte, certificate any, localADNLID []byte, payload []byte, dataHash []byte, extra []byte, flags int32, date uint32, peers []BroadcastPeer) (BroadcastTwoStepSendResult, error) {
	dataSize := uint32(len(payload))
	broadcastID, err := calcBroadcastTwoStepIDFromSourceID(sourceID, flags, date, localADNLID, dataHash, dataSize, dataSize, extra)
	if err != nil {
		return BroadcastTwoStepSendResult{}, err
	}

	msg := &BroadcastTwoStepSimple{
		Flags:       flags,
		Date:        date,
		Source:      source,
		SourceADNL:  append([]byte(nil), localADNLID...),
		Certificate: certificate,
		Data:        append([]byte(nil), payload...),
		Extra:       append([]byte(nil), extra...),
	}
	msg.Signature, err = signBroadcastTwoStepSimple(key, broadcastID, msg.Data)
	if err != nil {
		return BroadcastTwoStepSendResult{}, err
	}

	attempted, sent, failed, sendErr := sendBroadcastTwoStepMessage(ctx, peers, msg)
	return BroadcastTwoStepSendResult{
		BroadcastID: broadcastID,
		DataHash:    append([]byte(nil), dataHash...),
		Mode:        BroadcastTwoStepModeSimple,
		Attempted:   attempted,
		Sent:        sent,
		Failed:      failed,
		DataSize:    dataSize,
		PartSize:    dataSize,
	}, sendErr
}

func sendBroadcastTwoStepFEC(ctx context.Context, key ed25519.PrivateKey, source keys.PublicKeyED25519, sourceID []byte, certificate any, localADNLID []byte, payload []byte, dataHash []byte, extra []byte, flags int32, date uint32, peers []BroadcastPeer) (BroadcastTwoStepSendResult, error) {
	dataSize := uint32(len(payload))
	k := broadcastTwoStepFECBaseSymbols(len(peers))
	if k < 1 {
		return sendBroadcastTwoStepSimple(ctx, key, source, sourceID, certificate, localADNLID, payload, dataHash, extra, flags, date, peers)
	}

	partSize := uint32((len(payload) + k - 1) / k)
	if partSize == 0 || partSize >= dataSize {
		return sendBroadcastTwoStepSimple(ctx, key, source, sourceID, certificate, localADNLID, payload, dataHash, extra, flags, date, peers)
	}

	enc, err := raptorq.NewRaptorQ(partSize).CreateEncoder(payload)
	if err != nil {
		return BroadcastTwoStepSendResult{}, fmt.Errorf("failed to init raptorq encoder: %w", err)
	}

	broadcastID, err := calcBroadcastTwoStepIDFromSourceID(sourceID, flags, date, localADNLID, dataHash, dataSize, partSize, extra)
	if err != nil {
		return BroadcastTwoStepSendResult{}, err
	}

	var sendErr error
	attempted := 0
	sent := 0
	var failed []BroadcastTwoStepPeerError
	for i, peer := range peers {
		seqno := uint32(i)
		part := enc.GenSymbol(seqno)
		msg := &BroadcastTwoStepFEC{
			Flags:       flags,
			Date:        date,
			Source:      source,
			SourceADNL:  append([]byte(nil), localADNLID...),
			Certificate: certificate,
			DataHash:    append([]byte(nil), dataHash...),
			DataSize:    dataSize,
			Seqno:       seqno,
			Part:        part,
			Extra:       append([]byte(nil), extra...),
		}
		msg.Signature, err = signBroadcastTwoStepFEC(key, broadcastID, seqno, part)
		if err != nil {
			return BroadcastTwoStepSendResult{}, err
		}

		peerID := peer.ID()
		attempted++
		if err = peer.SendCustomMessage(ctx, msg); err != nil {
			failed = append(failed, BroadcastTwoStepPeerError{
				PeerID: append([]byte(nil), peerID...),
				Err:    err,
			})
			if sendErr == nil {
				sendErr = fmt.Errorf("failed to send two-step fec part %d to peer %x: %w", seqno, peerID, err)
			}
			continue
		}
		sent++
	}

	return BroadcastTwoStepSendResult{
		BroadcastID: broadcastID,
		DataHash:    append([]byte(nil), dataHash...),
		Mode:        BroadcastTwoStepModeFEC,
		Attempted:   attempted,
		Sent:        sent,
		Failed:      failed,
		DataSize:    dataSize,
		PartSize:    partSize,
	}, sendErr
}

func broadcastTwoStepFECBaseSymbols(otherNodes int) int {
	return (otherNodes - 1) / 2
}

func sendBroadcastTwoStepMessage(ctx context.Context, peers []BroadcastPeer, msg tl.Serializable) (int, int, []BroadcastTwoStepPeerError, error) {
	var sendErr error
	attempted := 0
	sent := 0
	var failed []BroadcastTwoStepPeerError
	for _, peer := range peers {
		peerID := peer.ID()
		attempted++
		if err := peer.SendCustomMessage(ctx, msg); err != nil {
			failed = append(failed, BroadcastTwoStepPeerError{
				PeerID: append([]byte(nil), peerID...),
				Err:    err,
			})
			if sendErr == nil {
				sendErr = fmt.Errorf("failed to send two-step broadcast to peer %x: %w", peerID, err)
			}
			continue
		}
		sent++
	}
	return attempted, sent, failed, sendErr
}
