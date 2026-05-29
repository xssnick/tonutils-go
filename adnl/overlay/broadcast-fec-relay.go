package overlay

import (
	"bytes"
	"container/list"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xssnick/tonutils-go/tl"
)

const broadcastFECDateSkew = 20 * time.Second
const broadcastFECRetainedBudgetBytes int64 = 4096
const broadcastSimpleDeliveredCacheSize = 100

// Keep delivered IDs for the same late-part window as retained relay streams.
const broadcastFECDeliveredTTL = fecBroadcastFinishedTTL

type broadcastFECRelayPart struct {
	full  *BroadcastFEC
	short *BroadcastFECShort
}

type broadcastFECDeliveredEntry struct {
	id        string
	expiresAt time.Time
}

type broadcastSimpleDeliveredEntry struct {
	id string
}

type broadcastFECRelayOp struct {
	peer  BroadcastPeer
	msg   tl.Serializable
	seqno uint32
}

// BroadcastFECRelayState stores ordinary FEC receive and relay state shared by
// overlay wrappers that participate in the same overlay.
type BroadcastFECRelayState struct {
	streams       map[string]*fecBroadcastStream
	delivered     map[string]*list.Element
	deliveredList *list.List
	simple        map[string]*list.Element
	simpleList    *list.List

	maxActiveStreams int
	maxActiveBytes   int64
	deliveredMax     int
	simpleMax        int
	activeBytes      int64
	nextCleanupAt    time.Time

	dropped            uint64
	evicted            uint64
	completed          uint64
	deliveredCacheHits uint64
	simpleRelaySent    uint64
	simpleRelayFailed  uint64
	fecRelaySent       uint64
	fecRelayFailed     uint64

	mx sync.RWMutex
}

func NewBroadcastFECRelayState() *BroadcastFECRelayState {
	return &BroadcastFECRelayState{
		streams:          map[string]*fecBroadcastStream{},
		delivered:        map[string]*list.Element{},
		deliveredList:    list.New(),
		simple:           map[string]*list.Element{},
		simpleList:       list.New(),
		maxActiveStreams: DefaultFECBroadcastMaxActiveStreams,
		maxActiveBytes:   DefaultFECBroadcastMaxActiveBytes,
		deliveredMax:     DefaultFECDeliveredCacheSize,
		simpleMax:        broadcastSimpleDeliveredCacheSize,
	}
}

func (s *BroadcastFECRelayState) SetLimits(maxActiveStreams int, maxActiveBytes int64) {
	if maxActiveStreams < 1 {
		maxActiveStreams = 1
	}
	if maxActiveBytes < 1 {
		maxActiveBytes = 1
	}

	now := time.Now()
	s.mx.Lock()
	s.maxActiveStreams = maxActiveStreams
	s.maxActiveBytes = maxActiveBytes
	s.cleanupLocked(now, true)
	s.mx.Unlock()
}

func (s *BroadcastFECRelayState) SetDeliveredCacheSize(max int) {
	if max < 0 {
		max = 0
	}

	s.mx.Lock()
	s.deliveredMax = max
	s.cleanupDeliveredLocked(time.Now())
	s.trimDeliveredLocked()
	s.mx.Unlock()
}

func (s *BroadcastFECRelayState) Stats() FECBroadcastStats {
	s.mx.RLock()
	stats := FECBroadcastStats{
		ActiveStreams:           len(s.streams),
		ActiveBytes:             s.activeBytes,
		DeliveredBroadcasts:     len(s.delivered),
		DroppedTotal:            s.dropped,
		EvictedTotal:            s.evicted,
		CompletedTotal:          s.completed,
		DeliveredCacheHitsTotal: s.deliveredCacheHits,
		SimpleRelaySentTotal:    s.simpleRelaySent,
		SimpleRelayFailedTotal:  s.simpleRelayFailed,
		FECRelaySentTotal:       s.fecRelaySent,
		FECRelayFailedTotal:     s.fecRelayFailed,
	}
	s.mx.RUnlock()

	return stats
}

func (s *BroadcastFECRelayState) addRelayStats(fec bool, sent, failed uint64) {
	if sent == 0 && failed == 0 {
		return
	}

	s.mx.Lock()
	if fec {
		s.fecRelaySent += sent
		s.fecRelayFailed += failed
	} else {
		s.simpleRelaySent += sent
		s.simpleRelayFailed += failed
	}
	s.mx.Unlock()
}

func (s *BroadcastFECRelayState) cleanupLocked(now time.Time, force bool) {
	if !force && !s.nextCleanupAt.IsZero() && now.Before(s.nextCleanupAt) {
		return
	}
	s.nextCleanupAt = now.Add(fecBroadcastCleanupInterval)

	for id, stream := range s.streams {
		stream.mx.Lock()
		completed := stream.completedAt != nil
		stale := stream.lastMessageAt.Add(fecBroadcastStreamIdleTTL).Before(now)
		expiredFinished := stream.finishedAt != nil && stream.finishedAt.Add(fecBroadcastFinishedTTL).Before(now)
		stream.mx.Unlock()
		if !stale && !expiredFinished {
			continue
		}

		s.removeStreamLocked(id, stream, completed, now)
		s.evicted++
	}

	s.cleanupDeliveredLocked(now)
}

func (s *BroadcastFECRelayState) reserveLocked(now time.Time, budgetBytes int64) bool {
	s.cleanupLocked(now, false)
	if !s.hasBudgetLocked(1, budgetBytes) {
		s.dropped++
		return false
	}

	s.activeBytes += budgetBytes
	return true
}

func (s *BroadcastFECRelayState) releaseLocked(budgetBytes int64) {
	s.activeBytes -= budgetBytes
	if s.activeBytes < 0 {
		s.activeBytes = 0
	}
}

func (s *BroadcastFECRelayState) hasBudgetLocked(incomingStreams int, incomingBytes int64) bool {
	if incomingBytes > s.maxActiveBytes {
		return false
	}

	return len(s.streams)+incomingStreams <= s.maxActiveStreams && s.activeBytes+incomingBytes <= s.maxActiveBytes
}

func (s *BroadcastFECRelayState) removeStreamLocked(id string, stream *fecBroadcastStream, delivered bool, now time.Time) {
	if s.streams[id] != stream {
		return
	}

	delete(s.streams, id)
	s.releaseLocked(stream.budgetBytes)
	if delivered {
		s.registerDeliveredLocked(id, now)
	}
}

func (s *BroadcastFECRelayState) reduceStreamBudgetLocked(id string, stream *fecBroadcastStream, budgetBytes int64) {
	if s.streams[id] != stream || stream.budgetBytes <= budgetBytes {
		return
	}

	s.activeBytes -= stream.budgetBytes - budgetBytes
	if s.activeBytes < 0 {
		s.activeBytes = 0
	}
	stream.budgetBytes = budgetBytes
}

func (s *BroadcastFECRelayState) registerDeliveredLocked(id string, now time.Time) {
	if s.deliveredMax == 0 {
		return
	}

	s.cleanupDeliveredLocked(now)
	entry := broadcastFECDeliveredEntry{
		id:        id,
		expiresAt: now.Add(broadcastFECDeliveredTTL),
	}
	if elem := s.delivered[id]; elem != nil {
		elem.Value = entry
		s.deliveredList.MoveToBack(elem)
		return
	}

	s.delivered[id] = s.deliveredList.PushBack(entry)
	s.trimDeliveredLocked()
}

func (s *BroadcastFECRelayState) isDeliveredLocked(id string, now time.Time) bool {
	elem := s.delivered[id]
	if elem == nil {
		return false
	}

	entry := elem.Value.(broadcastFECDeliveredEntry)
	if !entry.expiresAt.After(now) {
		delete(s.delivered, id)
		s.deliveredList.Remove(elem)
		return false
	}

	entry.expiresAt = now.Add(broadcastFECDeliveredTTL)
	elem.Value = entry
	s.deliveredList.MoveToBack(elem)
	return true
}

func (s *BroadcastFECRelayState) cleanupDeliveredLocked(now time.Time) {
	for {
		elem := s.deliveredList.Front()
		if elem == nil {
			return
		}

		entry := elem.Value.(broadcastFECDeliveredEntry)
		if entry.expiresAt.After(now) {
			return
		}

		delete(s.delivered, entry.id)
		s.deliveredList.Remove(elem)
	}
}

func (s *BroadcastFECRelayState) trimDeliveredLocked() {
	for len(s.delivered) > s.deliveredMax {
		elem := s.deliveredList.Front()
		if elem == nil {
			return
		}

		entry := elem.Value.(broadcastFECDeliveredEntry)
		delete(s.delivered, entry.id)
		s.deliveredList.Remove(elem)
	}
}

func (s *BroadcastFECRelayState) isSimpleDeliveredLocked(id string) bool {
	elem := s.simple[id]
	if elem == nil {
		return false
	}

	s.simpleList.MoveToBack(elem)
	return true
}

func (s *BroadcastFECRelayState) registerSimpleDeliveredLocked(id string) {
	if s.simpleMax == 0 {
		return
	}

	if elem := s.simple[id]; elem != nil {
		s.simpleList.MoveToBack(elem)
		return
	}

	s.simple[id] = s.simpleList.PushBack(broadcastSimpleDeliveredEntry{id: id})
	for len(s.simple) > s.simpleMax {
		elem := s.simpleList.Front()
		if elem == nil {
			return
		}

		entry := elem.Value.(broadcastSimpleDeliveredEntry)
		delete(s.simple, entry.id)
		s.simpleList.Remove(elem)
	}
}

func (s *BroadcastFECRelayState) TrackControlMessage(peerID []byte, control BroadcastFECControl) bool {
	s.mx.RLock()
	stream := s.streams[string(control.Hash)]
	s.mx.RUnlock()
	if stream == nil {
		return false
	}

	stream.mx.Lock()
	if stream.completedPeers == nil {
		stream.completedPeers = map[string]struct{}{}
	}
	if stream.receivedPeers == nil {
		stream.receivedPeers = map[string]struct{}{}
	}
	id := string(peerID)
	stream.receivedPeers[id] = struct{}{}
	if control.Completed {
		stream.completedPeers[id] = struct{}{}
	}
	stream.mx.Unlock()
	return true
}

func (s *fecBroadcastStream) receivedPart(seqno uint32) bool {
	if s.nextSeqno >= 64 && seqno < s.nextSeqno-64 {
		return true
	}
	if seqno >= s.nextSeqno {
		return false
	}
	return s.receivedParts&(1<<(s.nextSeqno-seqno-1)) != 0
}

func (s *fecBroadcastStream) addReceivedPart(seqno uint32) {
	if seqno < s.nextSeqno {
		s.receivedParts |= 1 << (s.nextSeqno - seqno - 1)
		return
	}

	old := s.nextSeqno
	s.nextSeqno = seqno + 1
	if s.nextSeqno-old >= 64 {
		s.receivedParts = 1
		return
	}
	s.receivedParts <<= s.nextSeqno - old
	s.receivedParts |= 1
}

func checkBroadcastFECDate(date uint32, now time.Time) error {
	unix := now.Unix()
	if int64(date) < unix-int64(broadcastFECDateSkew/time.Second) {
		return fmt.Errorf("too old broadcast")
	}
	if int64(date) > unix+int64(broadcastFECDateSkew/time.Second) {
		return fmt.Errorf("too new broadcast")
	}
	return nil
}

func (s *fecBroadcastStream) addRelayPart(seqno uint32, full *BroadcastFEC, broadcastHash, partDataHash []byte) {
	if s.parts == nil {
		s.parts = map[uint32]broadcastFECRelayPart{}
	}
	s.parts[seqno] = broadcastFECRelayPart{
		full: full,
		short: &BroadcastFECShort{
			Source:        full.Source,
			Certificate:   full.Certificate,
			BroadcastHash: broadcastHash,
			PartDataHash:  partDataHash,
			Seqno:         int32(seqno),
			Signature:     full.Signature,
		},
	}
}

func (s *fecBroadcastStream) relayPartOpsLocked(seqno uint32, peers []BroadcastPeer, localID, sourcePeerID []byte, erase bool) []broadcastFECRelayOp {
	part, ok := s.parts[seqno]
	if !ok {
		return nil
	}
	if erase {
		delete(s.parts, seqno)
	}

	ops := make([]broadcastFECRelayOp, 0, len(peers))
	for _, peer := range peers {
		if peer == nil {
			continue
		}
		peerID := peer.ID()
		if len(peerID) == 0 || bytes.Equal(peerID, localID) || bytes.Equal(peerID, sourcePeerID) {
			continue
		}

		id := string(peerID)
		if _, ok = s.completedPeers[id]; ok {
			continue
		}

		msg := tl.Serializable(part.full)
		if _, ok = s.receivedPeers[id]; ok {
			msg = part.short
		}
		ops = append(ops, broadcastFECRelayOp{
			peer:  peer,
			msg:   msg,
			seqno: seqno,
		})
	}
	return ops
}

func (s *fecBroadcastStream) drainRelayPartOpsLocked(peers []BroadcastPeer, localID, sourcePeerID []byte) []broadcastFECRelayOp {
	if len(s.parts) == 0 {
		return nil
	}

	ops := make([]broadcastFECRelayOp, 0, len(s.parts)*len(peers))
	for seqno := range s.parts {
		ops = append(ops, s.relayPartOpsLocked(seqno, peers, localID, sourcePeerID, true)...)
	}
	return ops
}

func sendBroadcastFECRelayOps(ctx context.Context, state *BroadcastFECRelayState, ops []broadcastFECRelayOp) error {
	var sendErr error
	var sent, failed uint64
	for _, op := range ops {
		if err := op.peer.SendCustomMessage(ctx, op.msg); err != nil {
			failed++
			if sendErr == nil {
				sendErr = fmt.Errorf("failed to relay FEC part %d to peer %x: %w", op.seqno, op.peer.ID(), err)
			}
			continue
		}
		sent++
	}
	if state != nil {
		state.addRelayStats(true, sent, failed)
	}
	return sendErr
}
