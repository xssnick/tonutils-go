package overlay

import (
	"fmt"
	"sync"

	"github.com/xssnick/tonutils-go/tl"
)

// Ordinary broadcasts use an independent bounded dispatcher so their repair
// traffic cannot consume the two-step relay queue. Both are owned by Receiver.
func (r *BroadcastReceiver) ensureBroadcastRelayDispatcher() *broadcastTwoStepRelayDispatcher {
	r.twoStepRelayMx.Lock()
	defer r.twoStepRelayMx.Unlock()

	if r.closed.Load() {
		return nil
	}
	if relay := r.ordinaryRelay.Load(); relay != nil {
		return relay
	}

	relay := newBroadcastRelayDispatcher(
		DefaultTwoStepRelayQueueSize,
		DefaultTwoStepRelayConcurrency,
		DefaultTwoStepRelayMaxActiveBytes,
		DefaultTwoStepRelayPeerTimeout,
		true,
	)
	r.ordinaryRelay.Store(relay)
	return relay
}

type broadcastRelayGroup struct {
	message tl.Serializable
	payload *broadcastTwoStepRelayPayload
	refs    int
	legacy  bool
	framed  bool
}

type broadcastOrdinaryRelayPeer struct {
	id     broadcastExternalPeerIDKey
	head   int
	tail   int
	queued int
}

type broadcastOrdinaryRelaySlot struct {
	task broadcastTwoStepRelayTask
	next int
}

// A peer is present in ready exactly once until a worker takes it. That worker
// owns the peer until its send returns, then rotates remaining work to the end
// of ready. Slow peers therefore occupy one worker each, regardless of how many
// FEC symbols are pending for them.
// All pending tasks share fixed slots; drained peer queues retain no buffers.
type broadcastOrdinaryRelayScheduler struct {
	mx     sync.Mutex
	peers  map[broadcastExternalPeerIDKey]*broadcastOrdinaryRelayPeer
	ready  chan *broadcastOrdinaryRelayPeer
	queued int
	slots  []broadcastOrdinaryRelaySlot
	free   int
	// An already scheduled peer may retain only a fraction of the pending
	// slots, leaving room for other peers even while its send is blocked.
	// A one-slot queue permits no backlog for an already scheduled peer.
	perPeerLimit int
}

func (s *broadcastOrdinaryRelayScheduler) submit(task broadcastTwoStepRelayTask) bool {
	id := newBroadcastExternalPeerIDKey(task.peer.ID())
	s.mx.Lock()
	defer s.mx.Unlock()

	peer := s.peers[id]
	if s.free < 0 || (peer != nil && peer.queued >= s.perPeerLimit) {
		return false
	}

	index := s.free
	s.free = s.slots[index].next
	s.slots[index] = broadcastOrdinaryRelaySlot{task: task, next: -1}
	s.queued++
	if peer == nil {
		peer = &broadcastOrdinaryRelayPeer{id: id, head: index, tail: index, queued: 1}
		s.peers[id] = peer
		// Ready peers cannot outnumber queued tasks, so the bounded ready
		// channel always has space when a new peer is admitted.
		s.ready <- peer
		return true
	}

	if peer.tail < 0 {
		peer.head = index
	} else {
		s.slots[peer.tail].next = index
	}
	peer.tail = index
	peer.queued++
	return true
}

func (d *broadcastTwoStepRelayDispatcher) runOrdinaryWorker() {
	s := d.ordinary
	for {
		select {
		case <-d.ctx.Done():
			return
		default:
		}

		select {
		case <-d.ctx.Done():
			return
		case peer := <-s.ready:
			s.mx.Lock()
			index := peer.head
			task := s.slots[index].task
			peer.head = s.slots[index].next
			if peer.head < 0 {
				peer.tail = -1
			}
			s.slots[index] = broadcastOrdinaryRelaySlot{next: s.free}
			s.free = index
			s.queued--
			peer.queued--
			s.mx.Unlock()

			d.send(task)

			s.mx.Lock()
			if peer.head < 0 {
				delete(s.peers, peer.id)
			} else if d.ctx.Err() == nil {
				s.ready <- peer
			}
			s.mx.Unlock()
		}
	}
}

func (s *broadcastOrdinaryRelayScheduler) drain() []broadcastTwoStepRelayTask {
	s.mx.Lock()
	defer s.mx.Unlock()

	tasks := make([]broadcastTwoStepRelayTask, 0, s.queued)
	for _, peer := range s.peers {
		for index := peer.head; index >= 0; index = s.slots[index].next {
			tasks = append(tasks, s.slots[index].task)
		}
	}
	clear(s.peers)
	clear(s.slots)
	s.queued = 0
	for len(s.ready) > 0 {
		<-s.ready
	}
	return tasks
}

// enqueueBroadcastRelayOps runs only after the receive path has decided which
// parts may be relayed. It copies no peer payloads: one owned boxed body is
// shared until all its queued sends finish. A saturated queue drops relay work
// without blocking local admission or the FEC control response.
func (a *ADNLOverlayWrapper) enqueueBroadcastRelayOps(fec bool, ops []broadcastFECRelayOp) error {
	if len(ops) == 0 {
		return nil
	}
	relay := a.ensureBroadcastRelayDispatcher()
	if relay == nil {
		return nil
	}

	groups := make(map[*PreparedBroadcastMessage]*broadcastRelayGroup)
	for _, op := range ops {
		group := groups[op.wire]
		if group == nil {
			group = &broadcastRelayGroup{message: op.msg}
			groups[op.wire] = group
		}
		group.refs++
		switch op.peer.(type) {
		case PreparedBroadcastMessagePeer:
			group.framed = true
		case PreparedBroadcastPeer:
		default:
			group.legacy = true
		}
	}

	var prepareErr error
	for body, group := range groups {
		var message tl.Serializable
		var err error
		if group.legacy {
			// These values may originally alias the receive datagram. Parsing the
			// owned body without copies preserves the released pointer API while
			// retaining no pooled receive memory and no second payload allocation.
			var parsed any
			_, err = tl.ParseNoCopy(&parsed, body.Body(), true)
			if err == nil {
				switch group.message.(type) {
				case *Broadcast:
					value := parsed.(Broadcast)
					message = &value
				case *BroadcastFEC:
					value := parsed.(BroadcastFEC)
					message = &value
				case *BroadcastFECShort:
					value := parsed.(BroadcastFECShort)
					message = &value
				}
			}
		}
		if err == nil && group.framed {
			// Build once before the fanout; concurrent cold-frame creation would
			// allocate one full copy per worker. The dispatcher charges this frame
			// together with the boxed body against its retained-byte limit.
			_, err = body.ADNLMessage(a.overlayId)
		}
		if err != nil {
			a.fecState.addRelayStats(fec, 0, uint64(group.refs))
			if prepareErr == nil {
				prepareErr = fmt.Errorf("failed to prepare broadcast relay: %w", err)
			}
			continue
		}

		payload, ok := relay.reservePayload(message, body, group.refs)
		if !ok {
			a.fecState.addRelayStats(fec, 0, uint64(group.refs))
			continue
		}
		payload.state = a.fecState
		payload.fec = fec
		group.payload = payload
	}

	for _, op := range ops {
		if payload := groups[op.wire].payload; payload != nil {
			relay.Submit(broadcastTwoStepRelayTask{peer: op.peer, payload: payload})
		}
	}
	return prepareErr
}
