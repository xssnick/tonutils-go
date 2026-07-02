package dht

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	rand "math/rand/v2"
	"net"
	"net/netip"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/adnl/overlay"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tl"
)

const (
	serverMaintenanceInterval = 10 * time.Second
	serverMaintenanceJitter   = 10 * time.Second
	serverRepublishInterval   = time.Second
	serverRepublishJitter     = time.Second
	serverCleanupInterval     = time.Second
	serverCleanupJitter       = time.Second
	serverBucketFillInterval  = 10 * time.Second
	serverBucketFillJitter    = 10 * time.Second
	serverBucketFillTargets   = 4
	serverBucketFillWorkers   = 2
	serverStartupFillTargets  = 24
	serverStartupFillWorkers  = 4
	serverRefreshWorkers      = 16
	defaultMemoryStoreMaxKeys = 100000
	serverRepublishScanLimit  = 64
)

var errReverseConnectionsDisabled = errors.New("dht reverse connections are disabled")

type localStoreError struct {
	err error
}

func (l *localStoreError) Error() string {
	return l.err.Error()
}

func (l *localStoreError) Unwrap() error {
	return l.err
}

// StoreHook is called before an incoming dht.store value is saved locally.
type StoreHook func(peer adnl.Peer, keyID []byte, value *Value) error

// QueryHook is called after an incoming DHT query is handled.
type QueryHook func(method string, err error)

// QueryAdmissionHook is called before an incoming DHT query is handled.
type QueryAdmissionHook func(peer adnl.Peer, method string) error

type Server struct {
	*Client

	key ed25519.PrivateKey

	store ValueStore
	done  chan struct{}

	mx        sync.RWMutex
	ourValues map[string]*Value

	storeHookMx sync.RWMutex
	storeHook   StoreHook

	queryHookMx sync.RWMutex
	queryHook   QueryHook

	queryAdmissionHookMx sync.RWMutex
	queryAdmissionHook   QueryAdmissionHook

	fillBucketCursor int

	republishMx         sync.Mutex
	republishOwnKeys    [][]byte
	republishOwnIndex   int
	republishStoreKeys  [][]byte
	republishStoreIndex int
}

func NewServer(gateway Gateway, key ed25519.PrivateKey, nodes []*Node, store ValueStore) (*Server, error) {
	k, a := normalizeKA(0, 0)
	return newServer(gateway, key, nodes, _UnknownNetworkID, k, a, store)
}

func newServer(gateway Gateway, key ed25519.PrivateKey, nodes []*Node, networkID int32, k, a int, store ValueStore) (*Server, error) {
	if key == nil {
		return nil, fmt.Errorf("nil dht server key")
	}
	if gateway == nil {
		return nil, fmt.Errorf("nil gateway")
	}
	if pub := gateway.GetPublicKey(); len(pub) > 0 && !bytes.Equal(pub, key.Public().(ed25519.PublicKey)) {
		return nil, fmt.Errorf("dht key must match gateway key")
	}

	client, err := newClient(gateway, nodes, networkID, k, a)
	if err != nil {
		return nil, err
	}

	if store == nil {
		store = NewMemoryValueStore(defaultMemoryStoreMaxKeys)
	}

	s := &Server{
		Client:    client,
		key:       key,
		store:     store,
		done:      make(chan struct{}),
		ourValues: map[string]*Value{},
	}

	s.Client.queryPrefix = s.queryPrefix
	gateway.SetConnectionHandler(s.handlePeer)

	go s.maintenanceLoop()
	return s, nil
}

func NewServerFromConfig(gateway Gateway, key ed25519.PrivateKey, cfg *liteclient.GlobalConfig, store ValueStore) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}

	nodes, err := nodesFromConfig(cfg)
	if err != nil {
		return nil, err
	}

	networkID := _UnknownNetworkID
	if cfg.DHT.NetworkID != nil {
		networkID = *cfg.DHT.NetworkID
	}

	k, a, err := configKA(cfg.DHT.K, cfg.DHT.A)
	if err != nil {
		return nil, err
	}
	server, err := newServer(gateway, key, nodes, networkID, k, a, store)
	if err != nil {
		return nil, err
	}
	return server, nil
}

// SetStoreHook sets a hook for incoming dht.store queries. Passing nil disables it.
func (s *Server) SetStoreHook(hook StoreHook) {
	s.storeHookMx.Lock()
	s.storeHook = hook
	s.storeHookMx.Unlock()
}

// SetQueryHook sets a hook for incoming DHT queries. Passing nil disables it.
func (s *Server) SetQueryHook(hook QueryHook) {
	s.queryHookMx.Lock()
	s.queryHook = hook
	s.queryHookMx.Unlock()
}

// SetQueryAdmissionHook sets a hook called before incoming DHT queries are handled.
// Returning an error rejects the query. Passing nil disables it.
func (s *Server) SetQueryAdmissionHook(hook QueryAdmissionHook) {
	s.queryAdmissionHookMx.Lock()
	s.queryAdmissionHook = hook
	s.queryAdmissionHookMx.Unlock()
}

func (s *Server) Close() error {
	s.Client.Close()
	if s.done != nil {
		<-s.done
	}
	if s.store != nil {
		_ = s.store.Close()
	}
	return nil
}

func (s *Server) StoreAddress(
	ctx context.Context,
	addresses address.List,
	ttl time.Duration,
	ownerKey ed25519.PrivateKey,
) (storedCount int, adnlID []byte, err error) {
	for i, addr := range addresses.Addresses {
		if address.IsZero(addr) {
			return 0, nil, fmt.Errorf("address %d is zero", i)
		}
	}

	data, err := tl.Serialize(addresses, true)
	if err != nil {
		return 0, nil, err
	}

	id := keys.PublicKeyED25519{Key: ownerKey.Public().(ed25519.PublicKey)}
	adnlID, err = tl.Hash(id)
	if err != nil {
		return 0, nil, err
	}

	storedCount, _, err = s.Store(ctx, id, []byte("address"), 0, data, UpdateRuleSignature{}, ttl, ownerKey)
	return storedCount, adnlID, err
}

func (s *Server) StoreOverlayNodes(
	ctx context.Context,
	overlayKey []byte,
	nodes *overlay.NodesList,
	ttl time.Duration,
) (storedCount int, overlayID []byte, err error) {
	if nodes == nil || len(nodes.List) == 0 {
		return 0, nil, fmt.Errorf("0 nodes in list")
	}

	for i := range nodes.List {
		if err = nodes.List[i].CheckSignature(); err != nil {
			return 0, nil, fmt.Errorf("untrusted overlay node in list: %w", err)
		}
	}

	data, err := tl.Serialize(nodes, true)
	if err != nil {
		return 0, nil, err
	}

	id := keys.PublicKeyOverlay{Key: overlayKey}
	overlayID, err = tl.Hash(id)
	if err != nil {
		return 0, nil, err
	}

	storedCount, _, err = s.Store(ctx, id, []byte("nodes"), 0, data, UpdateRuleOverlayNodes{}, ttl, nil)
	return storedCount, overlayID, err
}

func (s *Server) FindOverlayNodes(ctx context.Context, overlayKey []byte, continuation ...*Continuation) (*overlay.NodesList, *Continuation, error) {
	keyHash, err := tl.Hash(keys.PublicKeyOverlay{
		Key: overlayKey,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get key for overlay: %w", err)
	}

	vv, cont, err := s.FindValue(ctx, &Key{
		ID:    keyHash,
		Name:  []byte("nodes"),
		Index: 0,
	}, continuation...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to find dht key for overlay: %w", err)
	}

	var nodes overlay.NodesList
	_, err = tl.ParseNoCopy(&nodes, vv.Data, true)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse dht data for overlay nodes: %w", err)
	}
	return &nodes, cont, nil
}

func (s *Server) FindAddresses(ctx context.Context, key []byte) (*address.List, ed25519.PublicKey, error) {
	if len(key) != 32 {
		return nil, nil, fmt.Errorf("key should have 256 bits")
	}

	val, _, err := s.FindValue(ctx, &Key{
		ID:    key,
		Name:  []byte("address"),
		Index: 0,
	})
	if err != nil {
		return nil, nil, err
	}

	var list address.List
	_, err = tl.ParseNoCopy(&list, val.Data, true)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse address list: %w", err)
	}

	keyID, ok := val.KeyDescription.ID.(keys.PublicKeyED25519)
	if !ok {
		return nil, nil, fmt.Errorf("unsupported key type %T", val.KeyDescription.ID)
	}

	return &list, keyID.Key, nil
}

func (s *Server) FindValue(ctx context.Context, key *Key, continuation ...*Continuation) (*Value, *Continuation, error) {
	id, err := tl.Hash(key)
	if err != nil {
		return nil, nil, err
	}

	cont := &Continuation{}
	if len(continuation) > 0 && continuation[0] != nil {
		cont = continuation[0]
	}

	if !cont.checkedLocal {
		cont.checkedLocal = true
		value, err := s.getStoredValue(id)
		if err != nil {
			return nil, nil, err
		}
		if value != nil && isValueAcceptable(value) {
			return value, cont, nil
		}
	}

	return s.Client.FindValue(ctx, key, cont)
}

func (s *Server) Store(
	ctx context.Context,
	id any,
	name []byte,
	index int32,
	value []byte,
	rule any,
	ttl time.Duration,
	ownerKey ed25519.PrivateKey,
) (storedCount int, idKey []byte, err error) {
	val, idKey, err := buildStoreValue(id, name, index, value, rule, ttl, ownerKey)
	if err != nil {
		return 0, nil, err
	}

	s.mx.Lock()
	s.ourValues[string(idKey)] = cloneValue(&val)
	s.mx.Unlock()

	storedCount, err = s.storePreparedValue(ctx, &val, idKey)
	if err != nil {
		var localErr *localStoreError
		if errors.As(err, &localErr) {
			s.mx.Lock()
			delete(s.ourValues, string(idKey))
			s.mx.Unlock()
			return 0, nil, err
		}
	}
	return storedCount, idKey, err
}

func (s *Server) storePreparedValue(ctx context.Context, val *Value, keyID []byte) (storedCount int, err error) {
	if val == nil {
		return 0, fmt.Errorf("nil value")
	}

	nearest := s.collectNearestNodes(ctx, keyID)

	storedLocally := false
	if s.shouldStoreLocally(keyID, nearest) {
		if err = s.storeIn(keyID, val); err != nil {
			return 0, &localStoreError{err: err}
		}
		storedLocally = true
	}
	if len(nearest) == 0 {
		if storedLocally {
			return 1, nil
		}
		return 0, fmt.Errorf("no alive nodes found to store this key")
	}

	return s.Client.storeValueToNodes(ctx, keyID, val, nearest)
}

func (s *Server) shouldStoreLocally(keyID []byte, nearest []*dhtNode) bool {
	if len(nearest) < s.k {
		return true
	}

	var worst *dhtNode
	for _, node := range nearest {
		if node == nil {
			continue
		}
		if worst == nil || xorDistanceLess(keyID, worst.adnlId, node.adnlId) {
			worst = node
		}
	}
	if worst == nil {
		return true
	}

	return xorDistanceLess(keyID, s.selfID, worst.adnlId)
}

func (s *Server) RegisterReverseConnection(ctx context.Context) error {
	return errReverseConnectionsDisabled
}

func (s *Server) RequestReversePing(ctx context.Context, clientID []byte) error {
	return errReverseConnectionsDisabled
}

func (s *Server) handlePeer(peer adnl.Peer) error {
	peer.SetQueryHandler(func(msg *adnl.MessageQuery) error {
		return s.handleQuery(peer, msg)
	})
	peer.SetCustomMessageHandler(func(msg *adnl.MessageCustom) error {
		return s.handleMessage(peer, msg)
	})
	return nil
}

func (s *Server) queryNode(ctx context.Context, node *dhtNode, req tl.Serializable) (any, error) {
	if node == nil {
		return nil, fmt.Errorf("nil node")
	}

	payload, err := tl.Serialize(req, true)
	if err != nil {
		return nil, err
	}
	payload, err = s.applyQueryPrefix(payload)
	if err != nil {
		return nil, err
	}

	var res any
	ctxQuery, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	if err = node.query(ctxQuery, tl.Raw(payload), &res); err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Server) handleQuery(peer adnl.Peer, msg *adnl.MessageQuery) (err error) {
	sender, payload := unwrapPrefixedPayload(msg.Data)
	method := queryMethod(payload)
	defer func() {
		s.callQueryHook(method, err)
	}()
	if err = s.callQueryAdmissionHook(peer, method); err != nil {
		return err
	}
	s.absorbSenderNode(peer, sender)

	clientIP := peerRemoteIP(peer)
	queryType := reflect.TypeOf(payload)
	answer := func(result tl.Serializable) error {
		if Logger != nil {
			Logger(
				"[DHT DEBUG] answering",
				reflect.TypeOf(result),
				"to", queryType,
				"client_ip", clientIP,
				"qid", fmt.Sprintf("%x", msg.ID),
			)
		}
		return peer.Answer(context.Background(), msg.ID, result)
	}

	switch q := payload.(type) {
	case Ping:
		return answer(Pong{ID: q.ID})
	case FindNode:
		return answer(NodesList{List: s.getNearestNodes(q.Key, s.limitK(q.K))})
	case FindValue:
		if Logger != nil {
			Logger(
				"[DHT DEBUG] findValue query",
				fmt.Sprintf("%x", q.Key),
				"client_ip", clientIP,
				"qid", fmt.Sprintf("%x", msg.ID),
			)
		}
		value, err := s.getStoredValue(q.Key)
		if err != nil {
			if Logger != nil {
				Logger(
					"[DHT DEBUG] findValue getStoredValue err",
					err,
					"client_ip", clientIP,
					"qid", fmt.Sprintf("%x", msg.ID),
				)
			}
			return err
		}
		if value != nil {
			if Logger != nil {
				Logger(
					"[DHT DEBUG] findValue hit",
					"name", string(value.KeyDescription.Key.Name),
					"idx", value.KeyDescription.Key.Index,
					"ttl", value.TTL,
					"data_len", len(value.Data),
					"client_ip", clientIP,
					"qid", fmt.Sprintf("%x", msg.ID),
				)
			}
			return answer(ValueFoundResult{Value: *value})
		}
		if Logger != nil {
			Logger(
				"[DHT DEBUG] findValue miss",
				"client_ip", clientIP,
				"qid", fmt.Sprintf("%x", msg.ID),
			)
		}
		return answer(ValueNotFoundResult{
			Nodes: NodesList{List: s.getNearestNodes(q.Key, s.limitK(q.K))},
		})
	case Store:
		keyID, err := tl.Hash(q.Value.KeyDescription.Key)
		if err != nil {
			return err
		}
		if err = checkValueWithNetworkID(keyID, q.Value, s.networkID); err != nil {
			return err
		}
		if err = s.callStoreHook(peer, keyID, q.Value); err != nil {
			return err
		}
		if err = s.storeIn(keyID, q.Value); err != nil {
			return err
		}
		return answer(Stored{})
	case SignedAddressListQuery:
		node, err := s.selfNode()
		if err != nil {
			return err
		}
		return answer(*node)
	case RegisterReverseConnection:
		return errReverseConnectionsDisabled
	case RequestReversePing:
		return errReverseConnectionsDisabled
	default:
		return fmt.Errorf("unsupported dht query type %s", reflect.TypeOf(payload))
	}
}

func peerRemoteIP(peer adnl.Peer) string {
	remote := peer.RemoteAddr()
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return remote
	}
	return host
}

func sameDialAddress(left, right string) bool {
	leftAddr, leftErr := netip.ParseAddrPort(left)
	rightAddr, rightErr := netip.ParseAddrPort(right)
	if leftErr == nil && rightErr == nil {
		return leftAddr.Addr().Unmap() == rightAddr.Addr().Unmap() && leftAddr.Port() == rightAddr.Port()
	}
	return left == right
}

func queryMethod(payload any) string {
	switch payload.(type) {
	case Ping:
		return "ping"
	case FindNode:
		return "findNode"
	case FindValue:
		return "findValue"
	case Store:
		return "store"
	case SignedAddressListQuery:
		return "getSignedAddressList"
	case RegisterReverseConnection:
		return "registerReverseConnection"
	case RequestReversePing:
		return "requestReversePing"
	default:
		return "unknown"
	}
}

func (s *Server) handleMessage(peer adnl.Peer, msg *adnl.MessageCustom) error {
	sender, payload := unwrapPrefixedPayload(msg.Data)
	s.absorbSenderNode(peer, sender)

	switch payload.(type) {
	case RequestReversePingCont:
		return nil
	default:
		return fmt.Errorf("unsupported dht message type %s", reflect.TypeOf(payload))
	}
}

func (s *Server) queryPrefix() ([]byte, error) {
	node, err := s.selfNode()
	if err != nil {
		return nil, err
	}
	return tl.Serialize(Query{Node: node}, true)
}

func (s *Server) selfNode() (*Node, error) {
	addrList := s.gateway.GetAddressList()
	return BuildSignedNode(
		keys.PublicKeyED25519{Key: s.key.Public().(ed25519.PublicKey)},
		cloneAddressList(&addrList),
		int32(time.Now().Unix()),
		s.networkID,
		s.key,
	)
}

func (s *Server) absorbSenderNode(peer adnl.Peer, node *Node) {
	if node == nil {
		return
	}
	if err := node.validate(0, s.networkID); err != nil {
		return
	}

	id, err := tl.Hash(node.ID)
	if err != nil || !bytes.Equal(id, peer.GetID()) {
		return
	}

	_, addr, err := firstDialAddressFromList(node.AddrList)
	if err != nil || !sameDialAddress(addr, peer.RemoteAddr()) {
		return
	}

	_, _ = s.addNodeWithStatus(node, true)
}

func (s *Server) callStoreHook(peer adnl.Peer, keyID []byte, value *Value) error {
	s.storeHookMx.RLock()
	hook := s.storeHook
	s.storeHookMx.RUnlock()
	if hook == nil {
		return nil
	}
	return hook(peer, append([]byte{}, keyID...), cloneValue(value))
}

func (s *Server) callQueryAdmissionHook(peer adnl.Peer, method string) error {
	s.queryAdmissionHookMx.RLock()
	hook := s.queryAdmissionHook
	s.queryAdmissionHookMx.RUnlock()
	if hook == nil {
		return nil
	}
	return hook(peer, method)
}

func (s *Server) callQueryHook(method string, err error) {
	s.queryHookMx.RLock()
	hook := s.queryHook
	s.queryHookMx.RUnlock()
	if hook == nil {
		return
	}
	hook(method, err)
}

func (s *Server) getNearestNodes(key []byte, k int) []*Node {
	if k <= 0 {
		return nil
	}

	result := make([]*Node, 0, k)
	selfID := s.selfID
	addBucket := func(idx int) {
		nodes := s.buckets[idx].getActiveNodes()
		if len(nodes) == 0 {
			return
		}

		if len(nodes) > 1 {
			sort.Slice(nodes, func(i, j int) bool {
				return compareDistance(nodes[i].adnlId, nodes[j].adnlId, key) < 0
			})
		}

		for _, node := range nodes {
			if node == nil || node.node == nil {
				continue
			}
			result = append(result, node.asNode())
			if len(result) >= k {
				return
			}
		}
	}

	for bit := 0; bit < 256 && len(result) < k; bit++ {
		if xorBit(key, selfID, bit) {
			addBucket(bit)
		}
	}

	for bit := 255; bit >= 0 && len(result) < k; bit-- {
		if !xorBit(key, selfID, bit) {
			addBucket(bit)
		}
	}

	if len(result) > k {
		result = result[:k]
	}
	return result
}

func (s *Server) getStoredValue(keyID []byte) (*Value, error) {
	value, err := s.store.Get(keyID)
	if err != nil || value == nil {
		return value, err
	}
	if int64(value.TTL) <= time.Now().Unix() {
		_ = s.store.Delete(keyID)
		return nil, nil
	}
	return value, nil
}

func (s *Server) storeIn(keyID []byte, value *Value) error {
	if value == nil {
		return fmt.Errorf("nil value")
	}
	if err := checkValueWithNetworkID(keyID, value, s.networkID); err != nil {
		return err
	}
	now := time.Now().Unix()
	if err := checkValueTTLAt(value.TTL, now); err != nil {
		return err
	}
	if int64(value.TTL) <= now {
		return nil
	}
	if s.distance(keyID, s.k+10) >= s.k+10 {
		return nil
	}

	current, err := s.store.Get(keyID)
	if err != nil {
		return err
	}
	if current == nil {
		return s.store.Put(keyID, value)
	}

	merged, changed, err := mergeValue(current, value)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return s.store.Put(keyID, merged)
}

func (s *Server) distance(keyID []byte, max int) int {
	if max <= 0 {
		return 0
	}

	selfID := s.selfID
	count := 0
	for bit := 0; bit < 256; bit++ {
		if xorBit(keyID, selfID, bit) {
			count += s.buckets[bit].activeCount()
			if count >= max {
				return max
			}
		}
	}
	return count
}

func (s *Server) maintenanceLoop() {
	defer close(s.done)

	s.startupFillBuckets()

	now := time.Now()
	nextRefresh := now.Add(jitteredInterval(serverMaintenanceInterval, serverMaintenanceJitter))
	nextRepublish := now.Add(jitteredInterval(serverRepublishInterval, serverRepublishJitter))
	nextCleanup := now.Add(jitteredInterval(serverCleanupInterval, serverCleanupJitter))
	nextFill := now.Add(jitteredInterval(serverBucketFillInterval, serverBucketFillJitter))

	timer := time.NewTimer(time.Until(earliestTime(nextRefresh, nextRepublish, nextCleanup, nextFill)))
	defer timer.Stop()

	for {
		resetTimer(timer, time.Until(earliestTime(nextRefresh, nextRepublish, nextCleanup, nextFill)))

		select {
		case <-s.globalCtx.Done():
			return
		case <-timer.C:
		}

		now = time.Now()
		if !now.Before(nextRefresh) {
			s.refreshNodes()
			nextRefresh = time.Now().Add(jitteredInterval(serverMaintenanceInterval, serverMaintenanceJitter))
		}
		now = time.Now()
		if !now.Before(nextRepublish) {
			s.republishValues()
			nextRepublish = time.Now().Add(jitteredInterval(serverRepublishInterval, serverRepublishJitter))
		}
		now = time.Now()
		if !now.Before(nextCleanup) {
			s.cleanup()
			nextCleanup = time.Now().Add(jitteredInterval(serverCleanupInterval, serverCleanupJitter))
		}
		now = time.Now()
		if !now.Before(nextFill) {
			s.fillBuckets()
			nextFill = time.Now().Add(jitteredInterval(serverBucketFillInterval, serverBucketFillJitter))
		}
	}
}

func resetTimer(timer *time.Timer, d time.Duration) {
	if d < 0 {
		d = 0
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(d)
}

func earliestTime(first time.Time, rest ...time.Time) time.Time {
	earliest := first
	for _, t := range rest {
		if t.Before(earliest) {
			earliest = t
		}
	}
	return earliest
}

func jitteredInterval(base, jitter time.Duration) time.Duration {
	if jitter <= 0 {
		return base
	}
	return base + time.Duration(rand.Int64N(int64(jitter)))
}

func (s *Server) refreshNodes() {
	now := time.Now()
	workers := serverRefreshWorkers
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan *dhtNode, workers*2)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for node := range jobs {
				if node == nil {
					continue
				}

				ctx, cancel := context.WithTimeout(s.globalCtx, queryTimeout)
				updated, err := node.getSignedAddressList(ctx)
				cancel()
				if err == nil && updated != nil {
					node.markPingSuccess()
					_, _ = s.addNodeWithStatus(updated, true)
					continue
				}
				node.markPingFailure()
			}
		}()
	}

	for i := range s.buckets {
		for _, node := range s.buckets[i].getNodes() {
			if node == nil {
				continue
			}
			if !node.shouldPing(now) {
				continue
			}
			node.markPingAttempt(now)
			jobs <- node
		}
	}

	close(jobs)
	wg.Wait()

	s.promoteReadyNodes()
}

func (s *Server) promoteReadyNodes() {
	for i := range s.buckets {
		s.buckets[i].promoteReady()
	}
}

func (s *Server) startupFillBuckets() {
	targets := serverStartupFillTargets
	if targets <= 0 {
		return
	}

	workers := serverStartupFillWorkers
	if workers < 1 {
		workers = 1
	}
	if workers > targets {
		workers = targets
	}

	jobs := make(chan []byte, workers)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for keyID := range jobs {
				ctx, cancel := context.WithTimeout(s.globalCtx, queryTimeout)
				s.collectNearestNodes(ctx, keyID)
				cancel()
			}
		}()
	}

	for i := 0; i < targets; i++ {
		keyID := bucketFillKey(s.selfID, startupFillBucketBit(i, targets))
		select {
		case <-s.globalCtx.Done():
			close(jobs)
			wg.Wait()
			return
		case jobs <- keyID:
		}
	}

	close(jobs)
	wg.Wait()
	s.promoteReadyNodes()
}

func (s *Server) fillBuckets() {
	s.promoteReadyNodes()
	keys := s.bucketFillKeys(serverBucketFillTargets)
	if len(keys) == 0 {
		return
	}

	workers := serverBucketFillWorkers
	if workers < 1 {
		workers = 1
	}
	if workers > len(keys) {
		workers = len(keys)
	}

	jobs := make(chan []byte, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for keyID := range jobs {
				ctx, cancel := context.WithTimeout(s.globalCtx, queryTimeout)
				s.collectNearestNodes(ctx, keyID)
				cancel()
			}
		}()
	}

	for _, keyID := range keys {
		select {
		case <-s.globalCtx.Done():
			close(jobs)
			wg.Wait()
			return
		case jobs <- keyID:
		}
	}

	close(jobs)
	wg.Wait()
	s.promoteReadyNodes()
}

func (s *Server) bucketFillKeys(max int) [][]byte {
	if max <= 0 {
		return nil
	}

	keys := make([][]byte, 0, max)
	used := make(map[int]struct{}, max)
	for len(keys) < max {
		bit, ok := s.nextFillBucket(used)
		if !ok {
			break
		}
		used[bit] = struct{}{}
		keys = append(keys, bucketFillKey(s.selfID, bit))
	}
	if len(keys) == 0 {
		keys = append(keys, randomBucketFillKey(s.selfID))
	}
	return keys
}

func (s *Server) nextFillBucket(skip map[int]struct{}) (int, bool) {
	start := s.fillBucketCursor % len(s.buckets)
	if bit, ok := s.findBucketFrom(start, skip, func(active, backup int) bool {
		return active == 0
	}); ok {
		s.fillBucketCursor = (bit + 1) % len(s.buckets)
		return bit, true
	}
	if bit, ok := s.findBucketFrom(start, skip, func(active, backup int) bool {
		return active < s.k
	}); ok {
		s.fillBucketCursor = (bit + 1) % len(s.buckets)
		return bit, true
	}
	return 0, false
}

func (s *Server) findBucketFrom(start int, skip map[int]struct{}, match func(active, backup int) bool) (int, bool) {
	for offset := 0; offset < len(s.buckets); offset++ {
		bit := (start + offset) % len(s.buckets)
		if _, ok := skip[bit]; ok {
			continue
		}
		active, backup := s.buckets[bit].nodeCounts()
		if match(active, backup) {
			return bit, true
		}
	}
	return 0, false
}

func startupFillBucketBit(idx, total int) int {
	if total <= 1 {
		return 0
	}
	bit := idx * 255 / (total - 1)
	if bit < 0 {
		return 0
	}
	if bit > 255 {
		return 255
	}
	return bit
}

func bucketFillKey(selfID []byte, bucketBit int) []byte {
	keyID := make([]byte, 32)
	for i := range keyID {
		keyID[i] = byte(rand.UintN(256))
	}

	maxBits := len(selfID) * 8
	if maxBits > len(keyID)*8 {
		maxBits = len(keyID) * 8
	}
	if maxBits == 0 {
		return keyID
	}
	if bucketBit < 0 {
		bucketBit = 0
	} else if bucketBit >= maxBits {
		bucketBit = maxBits - 1
	}

	for bit := 0; bit < bucketBit; bit++ {
		setBit(keyID, bit, bitAt(selfID, bit))
	}
	setBit(keyID, bucketBit, !bitAt(selfID, bucketBit))
	return keyID
}

func randomBucketFillKey(selfID []byte) []byte {
	keyID := make([]byte, 32)
	for i := range keyID {
		keyID[i] = byte(rand.UintN(256))
	}

	sameBits := 64 - rand.IntN(1<<uint(rand.IntN(7)))
	if max := len(selfID) * 8; sameBits > max {
		sameBits = max
	}
	for bit := 0; bit < sameBits; bit++ {
		setBit(keyID, bit, bitAt(selfID, bit))
	}
	return keyID
}

func (s *Server) republishValues() {
	s.republishMx.Lock()
	defer s.republishMx.Unlock()

	now := time.Now().Unix()
	ownedKey := s.republishOwnedValue(now)
	s.republishStoredValue(now, ownedKey)
}

func (s *Server) republishOwnedValue(now int64) []byte {
	s.ensureOwnedRepublishKeys()
	for i := 0; i < serverRepublishScanLimit && s.republishOwnIndex < len(s.republishOwnKeys); i++ {
		keyID, value := s.nextOwnedRepublishValue(now)
		if value == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(s.globalCtx, queryTimeout)
		_, _ = s.storePreparedValue(ctx, value, keyID)
		cancel()
		return keyID
	}
	return nil
}

func (s *Server) ensureOwnedRepublishKeys() {
	if s.republishOwnIndex >= len(s.republishOwnKeys) {
		s.republishOwnKeys = s.ownedValueKeys()
		s.republishOwnIndex = 0
	}
}

func (s *Server) nextOwnedRepublishValue(now int64) ([]byte, *Value) {
	keyID := s.republishOwnKeys[s.republishOwnIndex]
	s.republishOwnIndex++

	s.mx.RLock()
	value := cloneValue(s.ourValues[string(keyID)])
	s.mx.RUnlock()
	if value == nil || int64(value.TTL) <= now+60 {
		return keyID, nil
	}
	return keyID, value
}

func (s *Server) ownedValueKeys() [][]byte {
	s.mx.RLock()
	keys := make([][]byte, 0, len(s.ourValues))
	for keyID := range s.ourValues {
		keys = append(keys, []byte(keyID))
	}
	s.mx.RUnlock()

	sortKeyBytes(keys)
	return keys
}

func (s *Server) republishStoredValue(now int64, ownedKey []byte) {
	if !s.ensureStoredRepublishKeys() {
		return
	}

	for i := 0; i < serverRepublishScanLimit && s.republishStoreIndex < len(s.republishStoreKeys); i++ {
		keyID, value := s.nextStoredRepublishValue(now)
		if keyID == nil {
			return
		}
		if value == nil {
			continue
		}
		dist := s.distance(keyID, s.k+10)
		if dist >= s.k+10 {
			_ = s.store.Delete(keyID)
			continue
		}
		if dist != 0 || !needRepublish(value) {
			continue
		}
		if ownedKey != nil && bytes.Equal(ownedKey, keyID) {
			continue
		}

		ctx, cancel := context.WithTimeout(s.globalCtx, queryTimeout)
		_, _ = s.storePreparedValue(ctx, value, keyID)
		cancel()
		return
	}
}

func (s *Server) ensureStoredRepublishKeys() bool {
	if s.republishStoreIndex >= len(s.republishStoreKeys) {
		keys, err := s.storedValueKeys()
		if err != nil || len(keys) == 0 {
			return false
		}
		s.republishStoreKeys = keys
		s.republishStoreIndex = 0
	}
	return true
}

func (s *Server) nextStoredRepublishValue(now int64) ([]byte, *Value) {
	if s.republishStoreIndex >= len(s.republishStoreKeys) {
		return nil, nil
	}
	keyID := s.republishStoreKeys[s.republishStoreIndex]
	s.republishStoreIndex++

	value, err := s.store.Get(keyID)
	if err != nil || value == nil || int64(value.TTL) <= now+60 {
		return keyID, nil
	}
	return keyID, value
}

func (s *Server) storedValueKeys() ([][]byte, error) {
	if lister, ok := s.store.(valueStoreKeyLister); ok {
		keys, err := lister.Keys()
		if err != nil {
			return nil, err
		}
		sortKeyBytes(keys)
		return keys, nil
	}

	var keys [][]byte
	err := s.store.ForEach(func(keyID []byte, value *Value) error {
		keys = append(keys, append([]byte{}, keyID...))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sortKeyBytes(keys)
	return keys, nil
}

func sortKeyBytes(keys [][]byte) {
	sort.Slice(keys, func(i, j int) bool {
		return bytes.Compare(keys[i], keys[j]) < 0
	})
}

func (s *Server) cleanup() {
	now := time.Now().Unix()
	s.cleanupStore(now)

	s.mx.Lock()
	for keyID, value := range s.ourValues {
		if value == nil || int64(value.TTL) <= now {
			delete(s.ourValues, keyID)
		}
	}
	s.mx.Unlock()
}

func (s *Server) cleanupStore(now int64) {
	if cleaner, ok := s.store.(valueStoreExpiredCleaner); ok {
		_ = cleaner.DeleteExpired(now)
		return
	}

	var expiredKeys [][]byte
	_ = s.store.ForEach(func(keyID []byte, value *Value) error {
		if value != nil && int64(value.TTL) <= now {
			expiredKeys = append(expiredKeys, append([]byte{}, keyID...))
		}
		return nil
	})
	for _, keyID := range expiredKeys {
		_ = s.store.Delete(keyID)
	}
}

func mergeValue(current, incoming *Value) (*Value, bool, error) {
	switch current.KeyDescription.UpdateRule.(type) {
	case UpdateRuleSignature:
		if incoming.TTL <= current.TTL {
			return current, false, nil
		}
		return cloneValue(incoming), true, nil
	case UpdateRuleAnybody:
		return cloneValue(incoming), true, nil
	case UpdateRuleOverlayNodes:
		data, err := mergeOverlayNodesData(current.Data, incoming.Data)
		if err != nil {
			return nil, false, err
		}
		merged := cloneValue(current)
		merged.Data = data
		if incoming.TTL > merged.TTL {
			merged.TTL = incoming.TTL
		}
		merged.Signature = nil
		return merged, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported update rule %T", current.KeyDescription.UpdateRule)
	}
}

func needRepublish(value *Value) bool {
	if value == nil {
		return false
	}

	switch value.KeyDescription.UpdateRule.(type) {
	case UpdateRuleSignature:
		return true
	default:
		return false
	}
}

func mergeOverlayNodesData(currentData, incomingData []byte) ([]byte, error) {
	var currentNodes overlay.NodesList
	if _, err := tl.ParseNoCopy(&currentNodes, currentData, true); err != nil {
		return nil, err
	}

	var incomingNodes overlay.NodesList
	if _, err := tl.ParseNoCopy(&incomingNodes, incomingData, true); err != nil {
		return nil, err
	}

	type nodeWithID struct {
		id   string
		node overlay.Node
	}

	merged := map[string]overlay.Node{}
	for _, list := range [][]overlay.Node{currentNodes.List, incomingNodes.List} {
		for _, node := range list {
			id, err := tl.Hash(node.ID)
			if err != nil {
				return nil, err
			}
			key := string(id)
			if prev, ok := merged[key]; !ok || prev.Version < node.Version {
				merged[key] = node
			}
		}
	}

	nodes := make([]nodeWithID, 0, len(merged))
	for id, node := range merged {
		nodes = append(nodes, nodeWithID{id: id, node: node})
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].node.Version != nodes[j].node.Version {
			return nodes[i].node.Version > nodes[j].node.Version
		}
		return nodes[i].id < nodes[j].id
	})

	result := overlay.NodesList{List: make([]overlay.Node, 0, len(nodes))}
	for _, node := range nodes {
		result.List = append(result.List, node.node)
	}

	data, err := tl.Serialize(result, true)
	if err != nil {
		return nil, err
	}
	for len(data) > _MaxValueSize && len(result.List) > 0 {
		result.List = result.List[:len(result.List)-1]
		data, err = tl.Serialize(result, true)
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}

func unwrapPrefixedPayload(data any) (*Node, any) {
	switch v := data.(type) {
	case []tl.Serializable:
		if len(v) == 2 {
			switch prefix := v[0].(type) {
			case Query:
				return cloneNode(prefix.Node), v[1]
			case Message:
				return cloneNode(prefix.Node), v[1]
			}
		}
	case []any:
		if len(v) == 2 {
			switch prefix := v[0].(type) {
			case Query:
				return cloneNode(prefix.Node), v[1]
			case Message:
				return cloneNode(prefix.Node), v[1]
			}
		}
	}
	return nil, data
}

func compareDistance(a, b, target []byte) int {
	for i := 0; i < len(target) && i < len(a) && i < len(b); i++ {
		ax := a[i] ^ target[i]
		bx := b[i] ^ target[i]
		if ax < bx {
			return -1
		}
		if ax > bx {
			return 1
		}
	}
	return 0
}

func (s *Server) limitK(k int32) int {
	if k <= 0 {
		return 0
	}
	if k > int32(s.k) {
		return s.k
	}
	return int(k)
}

func xorBit(a, b []byte, bit int) bool {
	if bit < 0 || bit >= len(a)*8 || bit >= len(b)*8 {
		return false
	}
	mask := byte(1 << uint(7-(bit%8)))
	return (a[bit/8] & mask) != (b[bit/8] & mask)
}

func bitAt(data []byte, bit int) bool {
	if bit < 0 || bit >= len(data)*8 {
		return false
	}
	mask := byte(1 << uint(7-(bit%8)))
	return data[bit/8]&mask != 0
}

func setBit(data []byte, bit int, value bool) {
	if bit < 0 || bit >= len(data)*8 {
		return
	}
	mask := byte(1 << uint(7-(bit%8)))
	if value {
		data[bit/8] |= mask
		return
	}
	data[bit/8] &^= mask
}

func xorDistanceLess(key, a, b []byte) bool {
	limit := len(key)
	if len(a) < limit {
		limit = len(a)
	}
	if len(b) < limit {
		limit = len(b)
	}

	for i := 0; i < limit; i++ {
		ax := key[i] ^ a[i]
		bx := key[i] ^ b[i]
		if ax != bx {
			return ax < bx
		}
	}
	return false
}
