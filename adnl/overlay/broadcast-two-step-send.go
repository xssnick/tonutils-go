package overlay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/xssnick/raptorq"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

const DefaultBroadcastTwoStepFECMinBytes uint32 = 513
const DefaultBroadcastTwoStepFECMinPeers = 5

// DefaultBroadcastTwoStepSendConcurrency bounds how many peers one broadcast
// dispatches at the same time. Without a bound a large overlay would start a
// goroutine per peer on every broadcast.
const DefaultBroadcastTwoStepSendConcurrency = 64

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

// BroadcastSigner owns the key used to authenticate an overlay broadcast.
// Sign may be called concurrently by broadcast implementations that prepare
// independent FEC parts in parallel.
type BroadcastSigner interface {
	PublicKey() ed25519.PublicKey
	Sign(payload []byte) ([]byte, error)
}

type ed25519BroadcastSigner ed25519.PrivateKey

func (s ed25519BroadcastSigner) PublicKey() ed25519.PublicKey {
	return ed25519.PrivateKey(s).Public().(ed25519.PublicKey)
}

func (s ed25519BroadcastSigner) Sign(payload []byte) ([]byte, error) {
	return ed25519.Sign(ed25519.PrivateKey(s), payload), nil
}

type BroadcastTwoStepSendRequest struct {
	// Key is the original signing API, kept for compatibility. Exactly one of
	// Key and Signer must be set, otherwise the send is rejected.
	Key ed25519.PrivateKey
	// Signer signs on behalf of the source so a keyring can retain ownership of
	// the private material. Exactly one of Key and Signer must be set.
	Signer      BroadcastSigner
	Certificate any
	LocalADNLID []byte
	Payload     []byte
	Extra       []byte
	Flags       int32
	PeerSet     BroadcastPeerSet
}

type BroadcastTwoStepTLSendRequest struct {
	// Key is the original signing API, kept for compatibility. Exactly one of
	// Key and Signer must be set, otherwise the send is rejected.
	Key ed25519.PrivateKey
	// Signer signs on behalf of the source so a keyring can retain ownership of
	// the private material. Exactly one of Key and Signer must be set.
	Signer      BroadcastSigner
	Certificate any
	LocalADNLID []byte
	Payload     tl.Serializable
	Extra       []byte
	Flags       int32
	PeerSet     BroadcastPeerSet
}

type BroadcastTwoStepSenderOption func(cfg *broadcastTwoStepSenderConfig)

type broadcastTwoStepSenderConfig struct {
	date        uint32
	minBytes    uint32
	minPeers    int
	concurrency int
	now         func() time.Time
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

// WithBroadcastTwoStepSendConcurrency bounds how many peers of one broadcast
// are dispatched in parallel. Values below 1 select the default.
func WithBroadcastTwoStepSendConcurrency(concurrency int) BroadcastTwoStepSenderOption {
	return func(cfg *broadcastTwoStepSenderConfig) {
		cfg.concurrency = concurrency
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
	signer, publicKey, err := broadcastTwoStepSigner(req.Key, req.Signer)
	if err != nil {
		return BroadcastTwoStepSendResult{}, err
	}

	cfg := broadcastTwoStepSenderConfig{
		minBytes:    DefaultBroadcastTwoStepFECMinBytes,
		minPeers:    DefaultBroadcastTwoStepFECMinPeers,
		concurrency: DefaultBroadcastTwoStepSendConcurrency,
		now:         time.Now,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.minPeers < 1 {
		cfg.minPeers = 1
	}
	if cfg.concurrency < 1 {
		cfg.concurrency = DefaultBroadcastTwoStepSendConcurrency
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}

	source := keys.PublicKeyED25519{Key: publicKey}
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
		return sendBroadcastTwoStepFEC(ctx, signer, source, sourceID, req.Certificate, req.LocalADNLID, req.Payload, dataHash, req.Extra, flags, date, peers, cfg.concurrency)
	}
	return sendBroadcastTwoStepSimple(ctx, signer, source, sourceID, req.Certificate, req.LocalADNLID, req.Payload, dataHash, req.Extra, flags, date, peers, cfg.concurrency)
}

func broadcastTwoStepSigner(key ed25519.PrivateKey, signer BroadcastSigner) (BroadcastSigner, ed25519.PublicKey, error) {
	if signer != nil {
		if len(key) != 0 {
			return nil, nil, fmt.Errorf("key and signer are mutually exclusive")
		}
		publicKey := signer.PublicKey()
		if len(publicKey) != ed25519.PublicKeySize {
			return nil, nil, fmt.Errorf("signer public key should be %d bytes", ed25519.PublicKeySize)
		}

		return signer, publicKey, nil
	}

	// Key is the original public API. Keep it as a strict compatibility path
	// while the signer form lets keyrings retain ownership of private material.
	if len(key) != ed25519.PrivateKeySize {
		return nil, nil, fmt.Errorf("private key should be %d bytes", ed25519.PrivateKeySize)
	}

	localSigner := ed25519BroadcastSigner(key)
	return localSigner, localSigner.PublicKey(), nil
}

func SendBroadcastTwoStepFromTL(ctx context.Context, req BroadcastTwoStepTLSendRequest, opts ...BroadcastTwoStepSenderOption) (BroadcastTwoStepSendResult, error) {
	data, err := tl.Serialize(req.Payload, true)
	if err != nil {
		return BroadcastTwoStepSendResult{}, fmt.Errorf("failed to serialize payload: %w", err)
	}
	return SendBroadcastTwoStep(ctx, BroadcastTwoStepSendRequest{
		Key:         req.Key,
		Signer:      req.Signer,
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
		Signer:      req.Signer,
		Certificate: req.Certificate,
		LocalADNLID: req.LocalADNLID,
		Payload:     data,
		Extra:       req.Extra,
		Flags:       req.Flags,
		PeerSet:     req.PeerSet,
	}, opts...)
}

func sendBroadcastTwoStepSimple(ctx context.Context, signer BroadcastSigner, source keys.PublicKeyED25519, sourceID []byte, certificate any, localADNLID []byte, payload []byte, dataHash []byte, extra []byte, flags int32, date uint32, peers []BroadcastPeer, concurrency int) (BroadcastTwoStepSendResult, error) {
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
	msg.Signature, err = signBroadcastTwoStepSimpleWithSigner(signer, broadcastID, msg.Data)
	if err != nil {
		return BroadcastTwoStepSendResult{}, err
	}

	attempted, sent, failed, sendErr := sendBroadcastTwoStepMessage(ctx, peers, msg, concurrency)
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

func sendBroadcastTwoStepFEC(ctx context.Context, signer BroadcastSigner, source keys.PublicKeyED25519, sourceID []byte, certificate any, localADNLID []byte, payload []byte, dataHash []byte, extra []byte, flags int32, date uint32, peers []BroadcastPeer, concurrency int) (BroadcastTwoStepSendResult, error) {
	dataSize := uint32(len(payload))
	k := broadcastTwoStepFECBaseSymbols(len(peers))
	if k < 1 {
		return sendBroadcastTwoStepSimple(ctx, signer, source, sourceID, certificate, localADNLID, payload, dataHash, extra, flags, date, peers, concurrency)
	}

	partSize := uint32((len(payload) + k - 1) / k)
	if partSize == 0 || partSize >= dataSize {
		return sendBroadcastTwoStepSimple(ctx, signer, source, sourceID, certificate, localADNLID, payload, dataHash, extra, flags, date, peers, concurrency)
	}

	enc, err := raptorq.NewRaptorQ(partSize).CreateEncoder(payload)
	if err != nil {
		return BroadcastTwoStepSendResult{}, fmt.Errorf("failed to init raptorq encoder: %w", err)
	}

	broadcastID, err := calcBroadcastTwoStepIDFromSourceID(sourceID, flags, date, localADNLID, dataHash, dataSize, partSize, extra)
	if err != nil {
		return BroadcastTwoStepSendResult{}, err
	}

	messages := make([]*BroadcastTwoStepFEC, len(peers))
	for i := range peers {
		seqno := uint32(i)
		part := enc.GenSymbol(seqno)
		messages[i] = &BroadcastTwoStepFEC{
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
	}
	results := sendBroadcastTwoStepParallel(ctx, peers, concurrency, func(index int) (tl.Serializable, error) {
		message := messages[index]
		var signErr error
		message.Signature, signErr = signBroadcastTwoStepFECWithSigner(
			signer,
			broadcastID,
			message.Seqno,
			message.Part,
		)

		return message, signErr
	})
	sent, failed, sendErr := collectBroadcastTwoStepResults(results, "fec part")

	return BroadcastTwoStepSendResult{
		BroadcastID: broadcastID,
		DataHash:    append([]byte(nil), dataHash...),
		Mode:        BroadcastTwoStepModeFEC,
		Attempted:   len(peers),
		Sent:        sent,
		Failed:      failed,
		DataSize:    dataSize,
		PartSize:    partSize,
	}, sendErr
}

func broadcastTwoStepFECBaseSymbols(otherNodes int) int {
	return (otherNodes - 1) / 2
}

func sendBroadcastTwoStepMessage(ctx context.Context, peers []BroadcastPeer, msg tl.Serializable, concurrency int) (int, int, []BroadcastTwoStepPeerError, error) {
	results := sendBroadcastTwoStepParallel(ctx, peers, concurrency, func(int) (tl.Serializable, error) {
		return msg, nil
	})
	sent, failed, sendErr := collectBroadcastTwoStepResults(results, "broadcast")

	return len(peers), sent, failed, sendErr
}

type broadcastTwoStepPeerResult struct {
	peerID []byte
	err    error
}

// C++ dispatches every two-step recipient independently through the actor
// mailbox. A slow delivery receipt must not delay the first hop to another
// validator, especially in a small validator set where every vote is needed.
// Dispatch is bounded by concurrency, so a large overlay does not turn one
// broadcast into a goroutine per peer.
func sendBroadcastTwoStepParallel(
	ctx context.Context,
	peers []BroadcastPeer,
	concurrency int,
	message func(int) (tl.Serializable, error),
) []broadcastTwoStepPeerResult {
	results := make([]broadcastTwoStepPeerResult, len(peers))

	// A set that already fits into the bound needs no semaphore at all.
	var slots chan struct{}
	if len(peers) > concurrency {
		slots = make(chan struct{}, concurrency)
	}

	var wg sync.WaitGroup
	wg.Add(len(peers))
	for index, peer := range peers {
		if slots != nil {
			slots <- struct{}{}
		}

		go func(index int, peer BroadcastPeer) {
			defer wg.Done()

			peerID := append([]byte(nil), peer.ID()...)
			msg, err := message(index)
			if err == nil {
				err = peer.SendCustomMessage(ctx, msg)
			}
			results[index] = broadcastTwoStepPeerResult{peerID: peerID, err: err}

			if slots != nil {
				<-slots
			}
		}(index, peer)
	}
	wg.Wait()

	return results
}

func collectBroadcastTwoStepResults(
	results []broadcastTwoStepPeerResult,
	kind string,
) (int, []BroadcastTwoStepPeerError, error) {
	sent := 0
	var failed []BroadcastTwoStepPeerError
	var sendErr error
	for index := range results {
		result := results[index]
		if result.err == nil {
			sent++
			continue
		}
		failed = append(failed, BroadcastTwoStepPeerError{
			PeerID: result.peerID,
			Err:    result.err,
		})
		if sendErr == nil {
			sendErr = fmt.Errorf("failed to send two-step %s %d to peer %x: %w", kind, index, result.peerID, result.err)
		}
	}

	return sent, failed, sendErr
}
