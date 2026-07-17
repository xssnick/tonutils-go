package overlay

import (
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xssnick/tonutils-go/tl"
)

type overlayIDKey [32]byte

func newOverlayIDKey(id []byte) (overlayIDKey, error) {
	if len(id) != len(overlayIDKey{}) {
		return overlayIDKey{}, fmt.Errorf("overlay id must be 32 bytes, got %d", len(id))
	}

	var key overlayIDKey
	copy(key[:], id)
	return key, nil
}

type BroadcastDelivery uint8

const (
	BroadcastDeliveryUnknown BroadcastDelivery = iota
	BroadcastDeliverySimple
	BroadcastDeliveryFEC
	BroadcastDeliveryTwoStepSimple
	BroadcastDeliveryTwoStepFEC
)

// BroadcastDisposition tells the receiver whether an application-admitted
// broadcast should be remembered and relayed. Retry is the only outcome that
// leaves the broadcast uncommitted so a later delivery can run admission
// again.
type BroadcastDisposition uint8

const (
	BroadcastDispositionUnknown BroadcastDisposition = iota
	BroadcastDispositionAcceptAndRelay
	BroadcastDispositionIgnore
	BroadcastDispositionRetry
)

// BroadcastReceiver owns all receive, deduplication and relay state for one
// overlay. A receiver may be attached to any number of ADNL connections.
type BroadcastReceiver struct {
	overlayId  []byte
	overlayKey overlayIDKey

	maxUnauthSize     uint32
	allowFEC          bool
	trustUnauthorized bool

	fecState *BroadcastFECRelayState

	authorizedKeys atomic.Pointer[broadcastAuthorizedKeys]
	fecRelayConfig atomic.Pointer[broadcastFECRelayConfig]
	simpleRelay    atomic.Pointer[broadcastSimpleRelayConfig]
	twoStepState   atomic.Pointer[BroadcastTwoStepState]
	twoStepConfig  atomic.Pointer[broadcastTwoStepRelayConfig]
	handler        atomic.Pointer[broadcastHandler]
	precheck       atomic.Pointer[broadcastPrecheckHandler]

	active atomic.Bool
	closed atomic.Bool

	cleanupStop chan struct{}
	cleanupDone chan struct{}
	closeOnce   sync.Once
}

func NewBroadcastReceiver(id []byte, maxUnauthBroadcastSize uint32,
	allowBroadcastFEC bool, trustUnauthorizedBroadcast bool) (*BroadcastReceiver, error) {
	key, err := newOverlayIDKey(id)
	if err != nil {
		return nil, err
	}

	r := &BroadcastReceiver{
		overlayId:         append([]byte(nil), id...),
		overlayKey:        key,
		maxUnauthSize:     maxUnauthBroadcastSize,
		allowFEC:          allowBroadcastFEC,
		trustUnauthorized: trustUnauthorizedBroadcast,
		fecState:          NewBroadcastFECRelayState(),
		cleanupStop:       make(chan struct{}),
		cleanupDone:       make(chan struct{}),
	}
	r.active.Store(true)
	go r.runBroadcastCleanup()

	return r, nil
}

func (r *BroadcastReceiver) OverlayID() []byte {
	return append([]byte(nil), r.overlayId...)
}

func (r *BroadcastReceiver) SetActive(active bool) {
	if active && r.closed.Load() {
		return
	}
	r.active.Store(active)
	if r.closed.Load() {
		r.active.Store(false)
	}
}

func (r *BroadcastReceiver) IsActive() bool {
	return r.active.Load() && !r.closed.Load()
}

func (r *BroadcastReceiver) Close() {
	r.closeOnce.Do(func() {
		r.closed.Store(true)
		r.active.Store(false)
		close(r.cleanupStop)
		<-r.cleanupDone
	})
}

func (r *BroadcastReceiver) SetAuthorizedKeys(keysWithMaxLen map[string]uint32) {
	keys := make(map[string]uint32, len(keysWithMaxLen))
	maps.Copy(keys, keysWithMaxLen)
	r.authorizedKeys.Store(&broadcastAuthorizedKeys{keys: keys})
}

func (r *BroadcastReceiver) SetFECBroadcastLimits(maxActiveStreams int, maxActiveBytes int64) {
	r.fecState.SetLimits(maxActiveStreams, maxActiveBytes)
}

func (r *BroadcastReceiver) SetFECDeliveredCacheSize(max int) {
	r.fecState.SetDeliveredCacheSize(max)
}

func (r *BroadcastReceiver) SetSimpleDeliveredCacheSize(max int) {
	r.fecState.SetSimpleDeliveredCacheSize(max)
}

func (r *BroadcastReceiver) FECBroadcastStats() FECBroadcastStats {
	return r.fecState.Stats()
}

func (r *BroadcastReceiver) SetBroadcastHandlerWithInfo(handler func(msg tl.Serializable, info BroadcastInfo) BroadcastDisposition) {
	if handler == nil {
		r.handler.Store(nil)
		return
	}

	h := broadcastHandler(handler)
	r.handler.Store(&h)
}

func (r *BroadcastReceiver) SetBroadcastPrecheckHandler(handler func(info BroadcastPrecheckInfo) error) {
	if handler == nil {
		r.precheck.Store(nil)
		return
	}

	h := broadcastPrecheckHandler(handler)
	r.precheck.Store(&h)
}

func (r *BroadcastReceiver) EnableBroadcastFECRelay(localID []byte, peerSet BroadcastPeerSet) {
	r.fecRelayConfig.Store(&broadcastFECRelayConfig{
		enabled: peerSet != nil,
		localID: append([]byte(nil), localID...),
		peerSet: peerSet,
		state:   r.fecState,
	})
}

func (r *BroadcastReceiver) DisableBroadcastFECRelay() {
	r.fecRelayConfig.Store(nil)
}

func (r *BroadcastReceiver) EnableBroadcastSimpleRelay(localID []byte, peerSet BroadcastPeerSet) {
	r.simpleRelay.Store(&broadcastSimpleRelayConfig{
		localID: append([]byte(nil), localID...),
		peerSet: peerSet,
		state:   r.fecState,
	})
}

func (r *BroadcastReceiver) DisableBroadcastSimpleRelay() {
	r.simpleRelay.Store(nil)
}

func (r *BroadcastReceiver) EnableBroadcastTwoStep(localID []byte, peerSet BroadcastPeerSet) {
	r.ensureTwoStepState()
	r.twoStepConfig.Store(&broadcastTwoStepRelayConfig{
		localID: append([]byte(nil), localID...),
		peerSet: peerSet,
	})
}

func (r *BroadcastReceiver) DisableBroadcastTwoStep() {
	r.twoStepConfig.Store(nil)
}

func (r *BroadcastReceiver) SetBroadcastTwoStepLimits(maxActiveStreams int, maxActiveBytes int64) {
	r.ensureTwoStepState().SetLimits(maxActiveStreams, maxActiveBytes)
}

func (r *BroadcastReceiver) SetBroadcastTwoStepDeliveredCacheSize(max int) {
	r.ensureTwoStepState().SetDeliveredCacheSize(max)
}

func (r *BroadcastReceiver) BroadcastTwoStepStats() BroadcastTwoStepStats {
	state := r.twoStepState.Load()
	if state == nil {
		return BroadcastTwoStepStats{}
	}
	return state.Stats()
}

func (r *BroadcastReceiver) isBroadcastTwoStepEnabled() bool {
	return r.twoStepConfig.Load() != nil
}

type broadcastFECRelayConfig struct {
	enabled bool
	localID []byte
	peerSet BroadcastPeerSet
	state   *BroadcastFECRelayState
}

type broadcastSimpleRelayConfig struct {
	localID []byte
	peerSet BroadcastPeerSet
	state   *BroadcastFECRelayState
}

func (r *BroadcastReceiver) activeFECState() *BroadcastFECRelayState {
	return r.fecState
}

func (r *BroadcastReceiver) broadcastFECRelayConfig() broadcastFECRelayConfig {
	cfg := r.fecRelayConfig.Load()
	if cfg == nil {
		return broadcastFECRelayConfig{state: r.fecState}
	}
	return *cfg
}

func (r *BroadcastReceiver) broadcastSimpleRelayConfig() broadcastSimpleRelayConfig {
	cfg := r.simpleRelay.Load()
	if cfg == nil {
		return broadcastSimpleRelayConfig{}
	}
	return *cfg
}

func (r *BroadcastReceiver) trackBroadcastFECRelayControl(peerID []byte, control BroadcastFECControl) bool {
	cfg := r.fecRelayConfig.Load()
	if cfg == nil || !cfg.enabled {
		return false
	}
	return r.fecState.TrackControlMessage(peerID, control)
}

func (r *BroadcastReceiver) runBroadcastCleanup() {
	defer close(r.cleanupDone)

	ticker := time.NewTicker(fecBroadcastCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case now := <-ticker.C:
			r.fecState.mx.Lock()
			r.fecState.cleanupLocked(now, true)
			r.fecState.mx.Unlock()

			if state := r.twoStepState.Load(); state != nil {
				state.mx.Lock()
				state.cleanupLocked(now, true)
				state.mx.Unlock()
			}
		case <-r.cleanupStop:
			return
		}
	}
}

func (r *BroadcastReceiver) broadcastHandler() func(tl.Serializable, BroadcastInfo) BroadcastDisposition {
	handler := r.handler.Load()
	if handler == nil {
		return nil
	}
	return *handler
}

func (r *BroadcastReceiver) precheckHandler() func(BroadcastPrecheckInfo) error {
	handler := r.precheck.Load()
	if handler == nil {
		return nil
	}
	return *handler
}

func (r *BroadcastReceiver) authorizedKey(keyID string) (uint32, bool) {
	keys := r.authorizedKeys.Load()
	if keys == nil {
		return 0, false
	}
	maxSize, ok := keys.keys[keyID]
	return maxSize, ok
}

func (r *BroadcastReceiver) twoStepRelayConfig() (BroadcastPeerSet, []byte) {
	cfg := r.twoStepConfig.Load()
	if cfg == nil {
		return nil, nil
	}
	return cfg.peerSet, cfg.localID
}

func (r *BroadcastReceiver) ensureTwoStepState() *BroadcastTwoStepState {
	state := r.twoStepState.Load()
	if state != nil {
		return state
	}

	state = NewBroadcastTwoStepState()
	if r.twoStepState.CompareAndSwap(nil, state) {
		return state
	}
	return r.twoStepState.Load()
}

func (r *BroadcastReceiver) activeTwoStepState() *BroadcastTwoStepState {
	return r.ensureTwoStepState()
}

type broadcastAuthorizedKeys struct {
	keys map[string]uint32
}

type broadcastHandler func(msg tl.Serializable, info BroadcastInfo) BroadcastDisposition

type broadcastPrecheckHandler func(info BroadcastPrecheckInfo) error

type broadcastTwoStepRelayConfig struct {
	localID []byte
	peerSet BroadcastPeerSet
}
