package dht

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/liteclient"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/overlay"
	"github.com/xssnick/tonutils-go/tl"
)

const (
	queryTimeout           = 3000 * time.Millisecond
	lookupHedgeDelay       = 300 * time.Millisecond
	retryableQueryAttempts = 2
)

type ADNL interface {
	Query(ctx context.Context, req, result tl.Serializable) error
	SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey))
	Close()
}

type Gateway interface {
	Close() error
	GetID() []byte
	GetPublicKey() ed25519.PublicKey
	GetAddressList() address.List
	RegisterClient(addr string, key ed25519.PublicKey) (adnl.Peer, error)
	SetConnectionHandler(handler func(client adnl.Peer) error)
}

type Client struct {
	buckets [256]*Bucket

	gateway     Gateway
	selfID      []byte
	networkID   int32
	k           int
	a           int
	queryPrefix func() ([]byte, error)

	globalCtx       context.Context
	globalCtxCancel func()
}

// Continuation allows to check value on the next nodes.
// Suitable for overlays, different DHT nodes may contain different node addresses.
// Can be used in case of everything is offline in single DHT node, to check another values.
type Continuation struct {
	checkedNodes []*dhtNode
	checkedLocal bool
}

type NodeInfo struct {
	Address string
	Key     ed25519.PublicKey
}

var Logger func(v ...any)

func NewClientFromConfigUrl(ctx context.Context, gateway Gateway, cfgUrl string) (*Client, error) {
	cfg, err := liteclient.GetConfigFromUrl(ctx, cfgUrl)
	if err != nil {
		return nil, err
	}

	return NewClientFromConfig(gateway, cfg)
}

func NewClientFromConfig(gateway Gateway, cfg *liteclient.GlobalConfig) (*Client, error) {
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
	return newClient(gateway, nodes, networkID, k, a)
}

func NewClient(gateway Gateway, nodes []*Node) (*Client, error) {
	k, a := normalizeKA(0, 0)
	return newClient(gateway, nodes, _UnknownNetworkID, k, a)
}

func newClient(gateway Gateway, nodes []*Node, networkID int32, k, a int) (*Client, error) {
	globalCtx, cancel := context.WithCancel(context.Background())

	buckets := [256]*Bucket{}
	for i := 0; i < 256; i++ {
		buckets[i] = newBucket(k)
	}

	c := &Client{
		buckets:         buckets,
		globalCtx:       globalCtx,
		globalCtxCancel: cancel,
		gateway:         gateway,
		selfID:          gateway.GetID(),
		networkID:       networkID,
		k:               k,
		a:               a,
	}

	for _, node := range nodes {
		_, err := c.addNodeWithStatus(node, true)
		if err != nil {
			logAddr := "<unknown>"
			if node != nil {
				logAddr = describeNodeAddress(node.AddrList)
			}
			if Logger != nil {
				Logger("failed to add DHT node", logAddr, " from config, err:", err.Error())
			}
			continue
		}
	}

	return c, nil
}

const (
	_defaultK = 10
	_defaultA = 3
	_maxK     = 10
	_maxA     = 10
)

func normalizeKA(k, a int) (int, int) {
	if k <= 0 {
		k = _defaultK
	} else if k > _maxK {
		k = _maxK
	}

	if a <= 0 {
		a = _defaultA
	} else if a > _maxA {
		a = _maxA
	}
	return k, a
}

func configKA(k, a int) (int, int, error) {
	if k > _maxK {
		return 0, 0, fmt.Errorf("bad value k=%d", k)
	}
	if a > _maxA {
		return 0, 0, fmt.Errorf("bad value a=%d", a)
	}
	k, a = normalizeKA(k, a)
	return k, a, nil
}

func (c *Client) Close() {
	c.globalCtxCancel()
	_ = c.gateway.Close()
}

func (c *Client) addNode(node *Node) (_ *dhtNode, err error) {
	return c.addNodeWithStatus(node, false)
}

func (c *Client) addNodeWithStatus(node *Node, setActive bool) (_ *dhtNode, err error) {
	if node == nil {
		return nil, fmt.Errorf("nil node")
	}

	pub, ok := node.ID.(keys.PublicKeyED25519)
	if !ok {
		return nil, fmt.Errorf("unsupported id type %T", node.ID)
	}
	if len(pub.Key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid ed25519 public key")
	}

	kid, err := tl.Hash(pub)
	if err != nil {
		return nil, err
	}

	affinity := affinity(kid, c.selfID)
	if affinity >= uint(len(c.buckets)) {
		return nil, fmt.Errorf("self node")
	}
	bucket := c.buckets[affinity]

	var currentVersion int32
	if existing := bucket.findNode(kid); existing != nil {
		currentVersion = existing.version
	}
	if err := node.validate(currentVersion, c.networkID); err != nil {
		return nil, err
	}

	addresses := node.AddrList.Addresses
	if len(addresses) > 5 {
		// max 5 addresses to check
		addresses = addresses[:5]
	}

	_, addr, err := firstDialAddress(addresses)
	if err != nil {
		return nil, err
	}

	if hf := bucket.findNode(kid); hf != nil {
		if hf.addr == addr && hf.version == node.Version {
			return nil, fmt.Errorf("node already exists")
		}
		// updated address otherwise
	}

	kNode := c.initNode(kid, addr, pub.Key, node.Version)
	kNode.node = cloneNode(node)

	return bucket.addNode(kNode, setActive), nil
}

func (c *Client) FindOverlayNodes(ctx context.Context, overlayKey []byte, continuation ...*Continuation) (*overlay.NodesList, *Continuation, error) {
	keyHash, err := tl.Hash(keys.PublicKeyOverlay{
		Key: overlayKey,
	})

	if err != nil {
		return nil, nil, fmt.Errorf("failed to get key for overlay: %w", err)
	}

	vv, cont, err := c.FindValue(ctx, &Key{
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

func (c *Client) FindAddresses(ctx context.Context, key []byte) (*address.List, ed25519.PublicKey, error) {
	if len(key) != 32 {
		return nil, nil, fmt.Errorf("key should have 256 bits")
	}

	val, _, err := c.FindValue(ctx, &Key{
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

var ErrDHTValueIsNotFound = errors.New("value is not found")

func (c *Client) StoreAddress(
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

	storedCount, _, err = c.Store(ctx, id, []byte("address"), 0, data, UpdateRuleSignature{}, ttl, ownerKey)
	return storedCount, adnlID, err
}

func (c *Client) StoreOverlayNodes(
	ctx context.Context,
	overlayKey []byte,
	nodes *overlay.NodesList,
	ttl time.Duration,
) (storedCount int, overlayID []byte, err error) {
	if nodes == nil || len(nodes.List) == 0 {
		return 0, nil, fmt.Errorf("0 nodes in list")
	}

	id := keys.PublicKeyOverlay{Key: overlayKey}
	overlayID, err = tl.Hash(id)
	if err != nil {
		return 0, nil, err
	}

	for i := range nodes.List {
		err := checkOverlayNode(&nodes.List[i], overlayID, c.networkID)
		if err != nil {
			return 0, nil, fmt.Errorf("untrusted overlay node in list: %w", err)
		}
	}

	data, err := tl.Serialize(nodes, true)
	if err != nil {
		return 0, nil, err
	}

	storedCount, _, err = c.Store(ctx, id, []byte("nodes"), 0, data, UpdateRuleOverlayNodes{}, ttl, nil)
	return storedCount, overlayID, err
}

func (c *Client) Store(
	ctx context.Context,
	id any,
	name []byte,
	index int32,
	value []byte,
	rule any,
	ttl time.Duration,
	ownerKey ed25519.PrivateKey,
) (storedCount int, idKey []byte, err error) {
	val, keyId, err := buildStoreValue(id, name, index, value, rule, ttl, ownerKey)
	if err != nil {
		return 0, nil, err
	}
	storedCount, err = c.storePreparedValue(ctx, &val, keyId)
	return storedCount, keyId, err
}

func (c *Client) storePreparedValue(ctx context.Context, val *Value, keyId []byte) (storedCount int, err error) {
	if val == nil {
		return 0, fmt.Errorf("nil value")
	}

	nearest := c.collectNearestNodes(ctx, keyId)
	if len(nearest) == 0 {
		return 0, fmt.Errorf("no alive nodes found to store this key")
	}

	return c.storeValueToNodes(ctx, keyId, val, nearest)
}

func (c *Client) storeValueToNodes(ctx context.Context, keyId []byte, val *Value, nodes []*dhtNode) (storedCount int, err error) {
	payload, err := c.prepareStoreValuePayload(keyId, val)
	if err != nil {
		return 0, err
	}

	var wg sync.WaitGroup
	var stored int32

	for _, node := range nodes {
		if node == nil {
			continue
		}
		wg.Add(1)

		go func(n *dhtNode) {
			defer wg.Done()

			ctxStore, cancel := context.WithTimeout(ctx, queryTimeout)
			defer cancel()

			if err := n.storePayload(ctxStore, payload); err == nil {
				atomic.AddInt32(&stored, 1)
			}
		}(node)
	}
	wg.Wait()

	if stored == 0 {
		return 0, fmt.Errorf("no alive nodes found to store this key")
	}

	return int(stored), nil
}

func (c *Client) prepareStoreValuePayload(keyId []byte, val *Value) ([]byte, error) {
	if err := checkValueWithNetworkID(keyId, val, c.networkID); err != nil {
		return nil, fmt.Errorf("corrupted value: %w", err)
	}

	payload, err := tl.Serialize(Store{
		Value: val,
	}, true)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize dht query: %w", err)
	}
	payload, err = c.applyQueryPrefix(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to wrap dht query: %w", err)
	}
	return payload, nil
}

func (c *Client) collectNearestNodes(ctx context.Context, keyId []byte) []*dhtNode {
	baseActive := c.a
	if baseActive <= 0 {
		baseActive = 1
	}
	activeLimit := baseActive

	maxActive := activeLimit * 2
	if c.k > activeLimit && maxActive > c.k {
		maxActive = c.k
	}
	if maxActive < activeLimit {
		maxActive = activeLimit
	}

	attempts := map[string]int{}
	search := newNearestNodeSearch(keyId, c.k, c.k*2)
	c.seedNearestNodeSearch(search)

	type queryResult struct {
		node    *dhtNode
		nodes   []*Node
		err     error
		attempt int
	}

	searchCtx, stopSearch := context.WithCancel(ctx)
	defer stopSearch()

	result := make(chan queryResult, maxActive)
	active := 0

	launchQueries := func() {
		for active < activeLimit {
			node := search.Next()
			if node == nil {
				break
			}

			nodeID := string(node.adnlId)
			if attempts[nodeID] >= retryableQueryAttempts {
				search.Finish(node, false)
				continue
			}
			attempts[nodeID]++
			attempt := attempts[nodeID]
			active++

			go func(n *dhtNode) {
				ctxQuery, cancel := context.WithTimeout(searchCtx, queryTimeout)
				defer cancel()

				nodes, err := n.findNodes(ctxQuery, keyId, int32(c.k))
				select {
				case result <- queryResult{node: n, nodes: nodes, err: err, attempt: attempt}:
				case <-searchCtx.Done():
				}
			}(node)
		}
	}

	growActive := func() {
		if activeLimit >= maxActive {
			return
		}
		activeLimit += baseActive
		if activeLimit > maxActive {
			activeLimit = maxActive
		}
	}

	stopTimer := func(timer *time.Timer) {
		if timer == nil {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}

	for {
		launchQueries()
		if search.CanReturn() {
			stopSearch()
			return search.Results()
		}
		if active == 0 {
			break
		}

		var hedge *time.Timer
		var hedgeChan <-chan time.Time
		if activeLimit < maxActive {
			hedge = time.NewTimer(lookupHedgeDelay)
			hedgeChan = hedge.C
		}

		select {
		case res := <-result:
			stopTimer(hedge)
			active--

			if res.err != nil {
				search.Finish(res.node, false)
				if errors.Is(res.err, context.DeadlineExceeded) && res.attempt < retryableQueryAttempts {
					search.Retry(res.node)
				}
			} else {
				search.Finish(res.node, true)
				for _, newN := range res.nodes {
					if an, err := c.addNode(newN); err == nil {
						search.Add(an)
					}
				}
			}

			if search.ResultCount()+active < search.k && search.k > maxActive {
				growActive()
			}
		case <-hedgeChan:
			growActive()
		case <-ctx.Done():
			stopTimer(hedge)
			stopSearch()
			return search.Results()
		}
	}
	return search.Results()
}

func (c *Client) seedNearestNodeSearch(search *nearestNodeSearch) {
	var badNodes []*dhtNode

	for i := 255; i >= 0; i-- {
		for _, node := range c.buckets[i].getNodes() {
			if node == nil {
				continue
			}
			if atomic.LoadInt32(&node.badScore) == 0 {
				search.Add(node)
				continue
			}
			badNodes = append(badNodes, node)
		}
	}

	for _, node := range badNodes {
		search.Add(node)
	}
}

type nearestNodeState uint8

const (
	nearestNodePending nearestNodeState = iota
	nearestNodeInFlight
	nearestNodeDone
)

type nearestNodeItem struct {
	id     string
	node   *dhtNode
	state  nearestNodeState
	retry  bool
	result bool
}

type nearestNodeSearch struct {
	keyID      []byte
	k          int
	maxPending int
	items      []*nearestNodeItem
	byID       map[string]*nearestNodeItem
	results    []*nearestNodeItem
}

func newNearestNodeSearch(keyID []byte, k, maxPending int) *nearestNodeSearch {
	if maxPending < k {
		maxPending = k
	}
	return &nearestNodeSearch{
		keyID:      keyID,
		k:          k,
		maxPending: maxPending,
		byID:       map[string]*nearestNodeItem{},
	}
}

func (s *nearestNodeSearch) Add(node *dhtNode) bool {
	if node == nil {
		return false
	}

	id := node.id()
	if _, ok := s.byID[id]; ok {
		return false
	}

	item := &nearestNodeItem{
		id:    id,
		node:  node,
		state: nearestNodePending,
	}
	s.items = append(s.items, item)
	s.byID[id] = item
	s.sortItems()
	s.trimPending()
	return true
}

func (s *nearestNodeSearch) Next() *dhtNode {
	if node := s.next(false); node != nil {
		return node
	}
	return s.next(true)
}

func (s *nearestNodeSearch) next(retry bool) *dhtNode {
	worst := s.worstResult()
	for _, item := range s.items {
		if item.state != nearestNodePending {
			continue
		}
		if item.retry != retry {
			continue
		}
		if worst != nil && !xorDistanceLess(s.keyID, item.node.adnlId, worst.node.adnlId) {
			return nil
		}
		item.state = nearestNodeInFlight
		return item.node
	}
	return nil
}

func (s *nearestNodeSearch) Retry(node *dhtNode) {
	item := s.item(node)
	if item == nil || item.result {
		return
	}
	item.state = nearestNodePending
	item.retry = true
	s.sortItems()
	s.trimPending()
}

func (s *nearestNodeSearch) Finish(node *dhtNode, success bool) {
	item := s.item(node)
	if item == nil {
		return
	}
	item.state = nearestNodeDone
	if !success || item.result {
		return
	}

	item.result = true
	s.results = append(s.results, item)
	s.sortResults()
	if len(s.results) > s.k {
		s.results = s.results[:s.k]
	}
}

func (s *nearestNodeSearch) Results() []*dhtNode {
	res := make([]*dhtNode, 0, len(s.results))
	for _, item := range s.results {
		if item != nil && item.node != nil {
			res = append(res, item.node)
		}
	}
	return res
}

func (s *nearestNodeSearch) ResultCount() int {
	return len(s.results)
}

func (s *nearestNodeSearch) CanReturn() bool {
	worst := s.worstResult()
	if worst == nil {
		return false
	}

	for _, item := range s.items {
		if item.state != nearestNodePending {
			continue
		}
		return !xorDistanceLess(s.keyID, item.node.adnlId, worst.node.adnlId)
	}
	return true
}

func (s *nearestNodeSearch) item(node *dhtNode) *nearestNodeItem {
	if node == nil {
		return nil
	}
	return s.byID[node.id()]
}

func (s *nearestNodeSearch) worstResult() *nearestNodeItem {
	if len(s.results) < s.k {
		return nil
	}
	return s.results[len(s.results)-1]
}

func (s *nearestNodeSearch) sortItems() {
	sortNearestNodeItems(s.keyID, s.items)
}

func (s *nearestNodeSearch) sortResults() {
	sortNearestNodeItems(s.keyID, s.results)
}

func (s *nearestNodeSearch) trimPending() {
	pending := 0
	for _, item := range s.items {
		if item.state != nearestNodePending {
			continue
		}
		pending++
		if pending > s.maxPending {
			item.state = nearestNodeDone
		}
	}
}

func sortNearestNodeItems(keyID []byte, items []*nearestNodeItem) {
	sort.Slice(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		if left == nil || left.node == nil {
			return false
		}
		if right == nil || right.node == nil {
			return true
		}
		if xorDistanceLess(keyID, left.node.adnlId, right.node.adnlId) {
			return true
		}
		if xorDistanceLess(keyID, right.node.adnlId, left.node.adnlId) {
			return false
		}
		return left.id < right.id
	})
}

func buildStoreValue(
	id any,
	name []byte,
	index int32,
	value []byte,
	rule any,
	ttl time.Duration,
	ownerKey ed25519.PrivateKey,
) (Value, []byte, error) {
	if err := checkValuePublicKey(id); err != nil {
		return Value{}, nil, err
	}
	if err := checkValueUpdateRule(rule); err != nil {
		return Value{}, nil, err
	}

	idKey, err := tl.Hash(id)
	if err != nil {
		return Value{}, nil, err
	}

	val := Value{
		KeyDescription: KeyDescription{
			Key: Key{
				ID:    idKey,
				Name:  name,
				Index: index,
			},
			ID:         id,
			UpdateRule: rule,
		},
		Data: value,
		TTL:  int32(time.Now().Add(ttl).Unix()),
	}

	switch rule.(type) {
	case UpdateRuleSignature:
		if len(ownerKey) != ed25519.PrivateKeySize {
			return Value{}, nil, fmt.Errorf("invalid ed25519 private key")
		}
		val.KeyDescription.Signature, err = signTL(val.KeyDescription, ownerKey)
		if err != nil {
			return Value{}, nil, fmt.Errorf("failed to sign key description: %w", err)
		}
		val.Signature, err = signTL(val, ownerKey)
		if err != nil {
			return Value{}, nil, fmt.Errorf("failed to sign value: %w", err)
		}
	}

	keyId, err := tl.Hash(val.KeyDescription.Key)
	if err != nil {
		return Value{}, nil, err
	}
	if err = checkValue(keyId, &val); err != nil {
		return Value{}, nil, err
	}
	return val, keyId, nil
}

func signTL(obj tl.Serializable, key ed25519.PrivateKey) ([]byte, error) {
	data, err := tl.Serialize(obj, true)
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(key, data), nil
}

type foundResult struct {
	value *Value
	node  *dhtNode
}

// FindValue attempts to retrieve a value from the DHT based on the given key.
func (c *Client) FindValue(ctx context.Context, key *Key, continuation ...*Continuation) (*Value, *Continuation, error) {
	id, keyErr := tl.Hash(key)
	if keyErr != nil {
		return nil, nil, keyErr
	}

	plist := c.buildPriorityList(id)
	cont := &Continuation{}
	if len(continuation) > 0 && continuation[0] != nil {
		cont = continuation[0]
		for _, n := range cont.checkedNodes {
			plist.MarkUsed(n, true)
		}
	}

	threadCtx, stopThreads := context.WithCancel(ctx)
	defer stopThreads()

	threads := c.a
	result := make(chan *foundResult, threads)
	attempts := map[string]int{}

	cond := sync.NewCond(&sync.Mutex{})
	waitingThreads := 0
	stopped := false

	launchWorker := func() {
		for {
			select {
			case <-threadCtx.Done():
				return
			default:
			}

			var node *dhtNode
			cond.L.Lock()
			node, _ = plist.Get()
			for node == nil {
				waitingThreads++
				if waitingThreads == threads {
					stopped = true
					cond.Broadcast()
					cond.L.Unlock()
					result <- nil
					return
				}

				cond.Wait()
				if stopped {
					cond.L.Unlock()
					return
				}
				node, _ = plist.Get()
				waitingThreads--
			}
			cond.L.Unlock()

			nodeID := node.id()
			cond.L.Lock()
			attempts[nodeID]++
			attempt := attempts[nodeID]
			cond.L.Unlock()

			findCtx, cancel := context.WithTimeout(threadCtx, queryTimeout)
			val, err := node.findValue(findCtx, id, int32(c.k))
			cancel()
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) && attempt < retryableQueryAttempts {
					cond.L.Lock()
					plist.MarkUsed(node, false)
					cond.Broadcast()
					cond.L.Unlock()
				}
				continue
			}

			switch v := val.(type) {
			case *Value:
				cond.L.Lock()
				if !stopped {
					stopped = true
					cond.Broadcast()
				}
				cond.L.Unlock()
				result <- &foundResult{value: v, node: node}
				return
			case []*Node:
				added := false
				cond.L.Lock()
				for _, n := range v {
					if newNode, err := c.addNode(n); err == nil {
						plist.Add(newNode)
						added = true
					}
				}
				if added {
					cond.Broadcast()
				} else if attempt < retryableQueryAttempts {
					plist.MarkUsed(node, false)
					cond.Broadcast()
				}
				cond.L.Unlock()
			}
		}
	}

	for i := 0; i < threads; i++ {
		go launchWorker()
	}

	select {
	case <-ctx.Done():
		cond.L.Lock()
		if !stopped {
			stopped = true
			cond.Broadcast()
		}
		cond.L.Unlock()
		return nil, nil, ctx.Err()
	case val := <-result:
		cond.L.Lock()
		if !stopped {
			stopped = true
			cond.Broadcast()
		}
		cond.L.Unlock()
		if val == nil {
			return nil, cont, ErrDHTValueIsNotFound
		}

		cont.checkedNodes = append(cont.checkedNodes, val.node)
		return val.value, cont, nil
	}
}

func (c *Client) buildPriorityList(id []byte) *priorityList {
	plistGood := newPriorityList(c.k+c.k/2, id)
	plistBad := newPriorityList(c.k/2, id)

	for i := 255; i >= 0; i-- {
		bucket := c.buckets[i]
		knownNodes := bucket.getNodes()
		for _, node := range knownNodes {
			if node == nil {
				continue
			}

			if atomic.LoadInt32(&node.badScore) == 0 {
				plistGood.Add(node)
			} else {
				plistBad.Add(node)
			}
		}
	}

	// add K not good nodes to retry them if they can be better
	for {
		node, _ := plistBad.Get()
		if node == nil {
			break
		}
		plistGood.Add(node)
	}

	return plistGood
}
