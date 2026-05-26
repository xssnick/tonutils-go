package overlay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/xssnick/raptorq"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
)

const DefaultBroadcastFECSymbolSize uint32 = 768
const DefaultBroadcastFECBurstSize uint32 = 4
const DefaultBroadcastFECPartCacheSize uint32 = 256
const DefaultBroadcastFECTTL = 60 * time.Second
const DefaultBroadcastFECPace = 10 * time.Millisecond

type BroadcastPeer interface {
	ID() []byte
	SendCustomMessage(ctx context.Context, req tl.Serializable) error
}

type BroadcastPeerSet interface {
	Peers() []BroadcastPeer
}

type StaticBroadcastPeerSet []BroadcastPeer

func (s StaticBroadcastPeerSet) Peers() []BroadcastPeer {
	return []BroadcastPeer(s)
}

type BroadcastFECSenderOption func(cfg *broadcastFECSenderConfig)

type broadcastFECSenderConfig struct {
	symbolSize uint32
	burstSize  uint32
	partCache  uint32
	ttl        time.Duration
	pace       time.Duration
	date       uint32
	now        func() time.Time
}

type broadcastFECSendPeerState struct {
	received  bool
	completed bool
}

type broadcastFECSendPart struct {
	full  *BroadcastFEC
	short *BroadcastFECShort
}

// BroadcastFECPart contains both full and short wire forms for one FEC seqno.
type BroadcastFECPart struct {
	Full  *BroadcastFEC
	Short *BroadcastFECShort
}

// Message returns the short wire form after a peer sends FECReceived.
func (p BroadcastFECPart) Message(useShort bool) tl.Serializable {
	if useShort {
		return p.Short
	}
	return p.Full
}

type BroadcastFECSender struct {
	broadcastHash []byte
	dataHash      []byte
	source        keys.PublicKeyED25519
	certificate   any
	flags         int32
	date          uint32
	fec           rldp.FECRaptorQ
	encoder       *raptorq.Encoder
	nextSeqno     uint32
	totalParts    uint32
	burstSize     uint32
	partCacheSize uint32
	ttl           time.Duration
	pace          time.Duration
	nextSendAt    time.Time
	expiresAt     time.Time
	now           func() time.Time
	signKey       ed25519.PrivateKey

	parts     map[uint32]*broadcastFECSendPart
	partOrder []uint32
	peers     map[string]*broadcastFECSendPeerState
	mx        sync.Mutex
}

func WithBroadcastFECSymbolSize(symbolSize uint32) BroadcastFECSenderOption {
	return func(cfg *broadcastFECSenderConfig) {
		cfg.symbolSize = symbolSize
	}
}

func WithBroadcastFECBurstSize(burstSize uint32) BroadcastFECSenderOption {
	return func(cfg *broadcastFECSenderConfig) {
		cfg.burstSize = burstSize
	}
}

func WithBroadcastFECPartCacheSize(partCache uint32) BroadcastFECSenderOption {
	return func(cfg *broadcastFECSenderConfig) {
		cfg.partCache = partCache
	}
}

func WithBroadcastFECTTL(ttl time.Duration) BroadcastFECSenderOption {
	return func(cfg *broadcastFECSenderConfig) {
		cfg.ttl = ttl
	}
}

func WithBroadcastFECPace(pace time.Duration) BroadcastFECSenderOption {
	return func(cfg *broadcastFECSenderConfig) {
		cfg.pace = pace
	}
}

func WithBroadcastFECDate(date uint32) BroadcastFECSenderOption {
	return func(cfg *broadcastFECSenderConfig) {
		cfg.date = date
	}
}

func withBroadcastFECNow(now func() time.Time) BroadcastFECSenderOption {
	return func(cfg *broadcastFECSenderConfig) {
		cfg.now = now
	}
}

func NewBroadcastFECSender(key ed25519.PrivateKey, certificate any, payload []byte, flags int32, opts ...BroadcastFECSenderOption) (*BroadcastFECSender, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("payload is empty")
	}
	if uint64(len(payload)) > math.MaxUint32 {
		return nil, fmt.Errorf("payload is too large")
	}

	cfg := broadcastFECSenderConfig{
		symbolSize: DefaultBroadcastFECSymbolSize,
		burstSize:  DefaultBroadcastFECBurstSize,
		partCache:  DefaultBroadcastFECPartCacheSize,
		ttl:        DefaultBroadcastFECTTL,
		pace:       DefaultBroadcastFECPace,
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.symbolSize == 0 {
		return nil, fmt.Errorf("symbol size is zero")
	}
	if cfg.burstSize == 0 {
		return nil, fmt.Errorf("burst size is zero")
	}
	if cfg.ttl <= 0 {
		return nil, fmt.Errorf("ttl should be positive")
	}
	if cfg.pace < 0 {
		return nil, fmt.Errorf("pace should not be negative")
	}

	source := keys.PublicKeyED25519{Key: key.Public().(ed25519.PublicKey)}
	if certificate == nil {
		certificate = CertificateEmpty{}
	}

	enc, err := raptorq.NewRaptorQ(cfg.symbolSize).CreateEncoder(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to init raptorq encoder: %w", err)
	}

	now := cfg.now()
	date := cfg.date
	if date == 0 {
		date = uint32(now.Unix())
	}

	dataHash := sha256.Sum256(payload)
	fec := rldp.FECRaptorQ{
		DataSize:     uint32(len(payload)),
		SymbolSize:   cfg.symbolSize,
		SymbolsCount: enc.BaseSymbolsNum(),
	}

	base := &BroadcastFEC{
		Source:      source,
		Certificate: certificate,
		DataHash:    dataHash[:],
		DataSize:    uint32(len(payload)),
		Flags:       flags,
		FEC:         fec,
		Date:        date,
	}
	broadcastHash, err := base.CalcID()
	if err != nil {
		return nil, err
	}

	return &BroadcastFECSender{
		broadcastHash: broadcastHash,
		dataHash:      dataHash[:],
		source:        source,
		certificate:   certificate,
		flags:         flags,
		date:          date,
		fec:           fec,
		encoder:       enc,
		totalParts:    broadcastFECSeqnoLimit(enc.BaseSymbolsNum()),
		burstSize:     cfg.burstSize,
		partCacheSize: cfg.partCache,
		ttl:           cfg.ttl,
		pace:          cfg.pace,
		nextSendAt:    now,
		expiresAt:     now.Add(cfg.ttl),
		now:           cfg.now,
		signKey:       key,
		parts:         map[uint32]*broadcastFECSendPart{},
		partOrder:     make([]uint32, 0, min(uint32(64), cfg.partCache)),
		peers:         map[string]*broadcastFECSendPeerState{},
	}, nil
}

func NewBroadcastFECSenderFromTL(key ed25519.PrivateKey, certificate any, payload tl.Serializable, flags int32, opts ...BroadcastFECSenderOption) (*BroadcastFECSender, error) {
	data, err := tl.Serialize(payload, true)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize payload: %w", err)
	}
	return NewBroadcastFECSender(key, certificate, data, flags, opts...)
}

func (s *BroadcastFECSender) BroadcastHash() []byte {
	s.mx.Lock()
	defer s.mx.Unlock()

	return append([]byte(nil), s.broadcastHash...)
}

func (s *BroadcastFECSender) NextSendAt() time.Time {
	s.mx.Lock()
	defer s.mx.Unlock()

	return s.nextSendAt
}

func (s *BroadcastFECSender) Done() bool {
	s.mx.Lock()
	defer s.mx.Unlock()

	return s.nextSeqno >= s.totalParts
}

// TotalParts returns the exclusive upper bound for FEC part seqno.
func (s *BroadcastFECSender) TotalParts() uint32 {
	s.mx.Lock()
	defer s.mx.Unlock()

	return s.totalParts
}

func (s *BroadcastFECSender) Expired() bool {
	s.mx.Lock()
	defer s.mx.Unlock()

	return s.expiresAt.Before(s.now())
}

func (s *BroadcastFECSender) TrackControlMessage(peerID []byte, msg tl.Serializable) bool {
	s.mx.Lock()
	defer s.mx.Unlock()

	now := s.now()
	if s.expiresAt.Before(now) {
		return false
	}

	if !s.applyControlMessageLocked(peerID, msg) {
		return false
	}

	s.expiresAt = now.Add(s.ttl)
	return true
}

func (s *BroadcastFECSender) SendReady(ctx context.Context, peerSet BroadcastPeerSet) (uint32, error) {
	s.mx.Lock()
	now := s.now()
	ready := !s.nextSendAt.After(now)
	s.mx.Unlock()

	if !ready {
		return 0, nil
	}
	return s.SendNow(ctx, peerSet, s.burstSize)
}

func (s *BroadcastFECSender) SendNow(ctx context.Context, peerSet BroadcastPeerSet, parts uint32) (uint32, error) {
	if peerSet == nil {
		return 0, fmt.Errorf("peer set is nil")
	}
	if parts == 0 {
		return 0, nil
	}

	ops, sentParts, err := s.prepareSend(peerSet.Peers(), parts)
	if err != nil || sentParts == 0 {
		return sentParts, err
	}

	var sendErr error
	for _, op := range ops {
		if err = op.peer.SendCustomMessage(ctx, op.msg); err != nil && sendErr == nil {
			sendErr = fmt.Errorf("failed to send part %d to peer %x: %w", op.seqno, op.peer.ID(), err)
		}
	}
	return sentParts, sendErr
}

func (s *BroadcastFECSender) prepareSend(peers []BroadcastPeer, parts uint32) ([]broadcastFECSendOp, uint32, error) {
	s.mx.Lock()
	defer s.mx.Unlock()

	now := s.now()
	if s.expiresAt.Before(now) || s.nextSeqno >= s.totalParts {
		return nil, 0, nil
	}

	left := s.totalParts - s.nextSeqno
	if parts > left {
		parts = left
	}

	activePeers := make([]broadcastFECSendPeer, 0, len(peers))
	for _, peer := range peers {
		state := s.ensurePeerState(peer.ID())
		if state.completed {
			continue
		}
		activePeers = append(activePeers, broadcastFECSendPeer{
			peer:  peer,
			state: state,
		})
	}
	if len(activePeers) == 0 {
		return nil, 0, nil
	}

	ops := make([]broadcastFECSendOp, 0, int(parts)*len(activePeers))
	for i := uint32(0); i < parts; i++ {
		seqno := s.nextSeqno + i
		part, err := s.partLocked(seqno)
		if err != nil {
			return nil, 0, err
		}

		for _, peer := range activePeers {
			msg := tl.Serializable(part.full)
			if peer.state.received {
				msg = part.short
			}

			ops = append(ops, broadcastFECSendOp{
				peer:  peer.peer,
				msg:   msg,
				seqno: seqno,
			})
		}
	}

	s.nextSeqno += parts
	s.nextSendAt = now.Add(s.pace)
	s.expiresAt = now.Add(s.ttl)
	return ops, parts, nil
}

func (s *BroadcastFECSender) part(seqno uint32) (*broadcastFECSendPart, error) {
	s.mx.Lock()
	defer s.mx.Unlock()

	return s.partLocked(seqno)
}

// Part returns a cached full/short FEC part pair for custom peer queue schedulers.
// Callers should treat returned messages as immutable.
func (s *BroadcastFECSender) Part(seqno uint32) (BroadcastFECPart, error) {
	part, err := s.part(seqno)
	if err != nil {
		return BroadcastFECPart{}, err
	}

	return BroadcastFECPart{
		Full:  part.full,
		Short: part.short,
	}, nil
}

func (s *BroadcastFECSender) ensurePeerState(peerID []byte) *broadcastFECSendPeerState {
	id := string(peerID)
	state := s.peers[id]
	if state == nil {
		state = &broadcastFECSendPeerState{}
		s.peers[id] = state
	}
	return state
}

func (s *BroadcastFECSender) applyControlMessageLocked(peerID []byte, msg tl.Serializable) bool {
	state := s.ensurePeerState(peerID)
	switch t := msg.(type) {
	case FECReceived:
		if !bytes.Equal(t.Hash, s.broadcastHash) {
			return false
		}
		state.received = true
	case FECCompleted:
		if !bytes.Equal(t.Hash, s.broadcastHash) {
			return false
		}
		state.received = true
		state.completed = true
	default:
		return false
	}
	return true
}

func (s *BroadcastFECSender) peerState(peerID []byte) broadcastFECSendPeerState {
	s.mx.Lock()
	defer s.mx.Unlock()

	state := s.ensurePeerState(peerID)
	return *state
}

func (s *BroadcastFECSender) partLocked(seqno uint32) (*broadcastFECSendPart, error) {
	if seqno >= s.totalParts {
		return nil, fmt.Errorf("fec part seqno %d is out of range %d", seqno, s.totalParts)
	}

	if part := s.parts[seqno]; part != nil {
		return part, nil
	}

	data := s.encoder.GenSymbol(seqno)
	partHash, partDataHash, err := calcBroadcastFECPartData(s.broadcastHash, data, seqno)
	if err != nil {
		return nil, fmt.Errorf("failed to compute part %d ids: %w", seqno, err)
	}

	full := &BroadcastFEC{
		Source:      s.source,
		Certificate: s.certificate,
		DataHash:    append([]byte(nil), s.dataHash...),
		DataSize:    s.fec.DataSize,
		Flags:       s.flags,
		Data:        data,
		Seqno:       seqno,
		FEC:         s.fec,
		Date:        s.date,
	}
	signature, err := signBroadcastFECPart(s.signKey, partHash, s.date)
	if err != nil {
		return nil, fmt.Errorf("failed to sign broadcast part %d: %w", seqno, err)
	}
	full.Signature = signature

	short := &BroadcastFECShort{
		Source:        s.source,
		Certificate:   s.certificate,
		BroadcastHash: append([]byte(nil), s.broadcastHash...),
		PartDataHash:  partDataHash,
		Seqno:         int32(seqno),
		Signature:     append([]byte(nil), signature...),
	}

	part := &broadcastFECSendPart{
		full:  full,
		short: short,
	}
	s.cachePartLocked(seqno, part)
	return part, nil
}

func (s *BroadcastFECSender) cachePartLocked(seqno uint32, part *broadcastFECSendPart) {
	if s.partCacheSize == 0 {
		return
	}

	s.parts[seqno] = part
	s.partOrder = append(s.partOrder, seqno)

	for uint32(len(s.partOrder)) > s.partCacheSize {
		delete(s.parts, s.partOrder[0])
		copy(s.partOrder, s.partOrder[1:])
		s.partOrder = s.partOrder[:len(s.partOrder)-1]
	}
}

func (s *BroadcastFECSender) totalPartsLimit() uint32 {
	s.mx.Lock()
	defer s.mx.Unlock()

	return s.totalParts
}

type broadcastFECSendOp struct {
	peer  BroadcastPeer
	msg   tl.Serializable
	seqno uint32
}

type broadcastFECSendPeer struct {
	peer  BroadcastPeer
	state *broadcastFECSendPeerState
}

func (a *ADNLOverlayWrapper) ID() []byte {
	return a.GetID()
}
