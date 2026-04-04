package dht

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"fmt"
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
	serverRepublishInterval   = 10 * time.Second
	serverReverseTTL          = 5 * time.Minute
	serverRefreshWorkers      = 16
	defaultMemoryStoreMaxKeys = 4096
)

type reverseConnection struct {
	addr     string
	key      ed25519.PublicKey
	keyID    []byte
	expireAt time.Time
}

type Server struct {
	*Client

	key ed25519.PrivateKey

	store ValueStore
	done  chan struct{}

	mx                 sync.RWMutex
	ourValues          map[string]*Value
	reverseConnections map[string]reverseConnection
	ourReverseClients  map[string]struct{}
}

func NewServer(gateway Gateway, key ed25519.PrivateKey, nodes []*Node, store ValueStore) (*Server, error) {
	k, a := normalizeKA(0, 0)
	return newServer(gateway, key, nodes, k, a, store)
}

func newServer(gateway Gateway, key ed25519.PrivateKey, nodes []*Node, k, a int, store ValueStore) (*Server, error) {
	if key == nil {
		return nil, fmt.Errorf("nil dht server key")
	}
	if gateway == nil {
		return nil, fmt.Errorf("nil gateway")
	}
	if pub := gateway.GetPublicKey(); len(pub) > 0 && !bytes.Equal(pub, key.Public().(ed25519.PublicKey)) {
		return nil, fmt.Errorf("dht key must match gateway key")
	}

	client, err := newClient(gateway, nodes, _UnknownNetworkID, k, a)
	if err != nil {
		return nil, err
	}

	if store == nil {
		store = NewMemoryValueStore(defaultMemoryStoreMaxKeys)
	}

	s := &Server{
		Client:             client,
		key:                key,
		store:              store,
		done:               make(chan struct{}),
		ourValues:          map[string]*Value{},
		reverseConnections: map[string]reverseConnection{},
		ourReverseClients:  map[string]struct{}{},
	}
	s.ourReverseClients[string(client.selfID)] = struct{}{}

	s.Client.queryPrefix = s.queryPrefix
	gateway.SetConnectionHandler(s.handlePeer)

	go s.maintenanceLoop()
	return s, nil
}

func NewServerFromConfig(gateway Gateway, key ed25519.PrivateKey, cfg *liteclient.GlobalConfig, store ValueStore) (*Server, error) {
	nodes, err := nodesFromConfig(cfg)
	if err != nil {
		return nil, err
	}

	k, a := normalizeKA(cfg.DHT.K, cfg.DHT.A)
	server, err := newServer(gateway, key, nodes, k, a, store)
	if err != nil {
		return nil, err
	}

	if cfg != nil && cfg.DHT.NetworkID != nil {
		server.networkID = *cfg.DHT.NetworkID
	}
	return server, nil
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
) (storedCount int, idKey []byte, err error) {
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
	return s.Store(ctx, id, []byte("address"), 0, data, UpdateRuleSignature{}, ttl, ownerKey)
}

func (s *Server) StoreOverlayNodes(
	ctx context.Context,
	overlayKey []byte,
	nodes *overlay.NodesList,
	ttl time.Duration,
) (storedCount int, idKey []byte, err error) {
	if len(nodes.List) == 0 {
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
	return s.Store(ctx, id, []byte("nodes"), 0, data, UpdateRuleOverlayNodes{}, ttl, nil)
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

	if err = s.storeIn(&val); err != nil {
		s.mx.Lock()
		delete(s.ourValues, string(idKey))
		s.mx.Unlock()
		return 0, nil, err
	}

	storedCount, err = s.Client.storePreparedValue(ctx, &val)
	return storedCount, idKey, err
}

func (s *Server) RegisterReverseConnection(ctx context.Context) error {
	pub := s.gateway.GetPublicKey()
	if len(pub) == 0 {
		return fmt.Errorf("gateway public key is not available")
	}

	clientID := append([]byte{}, s.selfID...)
	ttl := int32(time.Now().Add(serverReverseTTL).Unix())
	signature := ed25519.Sign(s.key, registerReverseConnectionToSign(clientID, s.selfID, ttl))
	req := RegisterReverseConnection{
		Node:      keys.PublicKeyED25519{Key: pub},
		TTL:       ttl,
		Signature: signature,
	}

	s.mx.Lock()
	s.ourReverseClients[string(clientID)] = struct{}{}
	s.mx.Unlock()

	keyID := reverseConnectionKeyID(clientID)
	nodes := s.collectNearestNodes(ctx, keyID)
	if len(nodes) == 0 {
		return fmt.Errorf("no alive nodes found to register reverse connection")
	}

	var stored int
	for _, node := range nodes {
		if node == nil {
			continue
		}

		res, err := s.queryNode(ctx, node, req)
		if err != nil {
			continue
		}
		if _, ok := res.(Stored); ok {
			stored++
		}
	}

	if stored == 0 {
		return fmt.Errorf("failed to register reverse connection")
	}
	return nil
}

func (s *Server) RequestReversePing(ctx context.Context, clientID []byte) error {
	target, err := s.selfADNLNode()
	if err != nil {
		return err
	}

	targetCopy := *target
	targetCopy.Signature = nil
	dataToSign, err := tl.Serialize(targetCopy, true)
	if err != nil {
		return err
	}

	req := &RequestReversePing{
		Target:    *target,
		Signature: ed25519.Sign(s.key, dataToSign),
		Client:    append([]byte{}, clientID...),
		K:         int32(s.k),
	}

	if res, err := s.requestReversePing(req); err == nil {
		if _, ok := res.(ReversePingOKResult); ok {
			return nil
		}
	}

	keyID := reverseConnectionKeyID(clientID)
	queue := s.collectNearestNodes(ctx, keyID)
	checked := map[string]struct{}{}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if node == nil {
			continue
		}
		if _, ok := checked[node.id()]; ok {
			continue
		}
		checked[node.id()] = struct{}{}

		res, err := s.queryNode(ctx, node, *req)
		if err != nil {
			continue
		}

		switch r := res.(type) {
		case ReversePingOKResult:
			return nil
		case ClientNotFoundResult:
			for _, newNode := range r.Nodes.List {
				an, err := s.addNode(newNode)
				if err == nil && an != nil {
					queue = append(queue, an)
				}
			}
		}
	}

	return fmt.Errorf("reverse ping path was not found")
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

func (s *Server) handleQuery(peer adnl.Peer, msg *adnl.MessageQuery) error {
	sender, payload := unwrapPrefixedPayload(msg.Data)
	s.absorbSenderNode(peer, sender)

	answer := func(result tl.Serializable) error {
		adnl.Logger("[DHT DEBUG] answering", reflect.TypeOf(result), "qid", fmt.Sprintf("%x", msg.ID))
		return peer.Answer(context.Background(), msg.ID, result)
	}

	switch q := payload.(type) {
	case Ping:
		return answer(Pong{ID: q.ID})
	case FindNode:
		return answer(NodesList{List: s.getNearestNodes(q.Key, s.limitK(q.K))})
	case FindValue:
		adnl.Logger("[DHT DEBUG] findValue query", fmt.Sprintf("%x", q.Key), "qid", fmt.Sprintf("%x", msg.ID))
		value, err := s.getStoredValue(q.Key)
		if err != nil {
			adnl.Logger("[DHT DEBUG] findValue getStoredValue err", err, "qid", fmt.Sprintf("%x", msg.ID))
			return err
		}
		if value != nil {
			adnl.Logger(
				"[DHT DEBUG] findValue hit",
				"name", string(value.KeyDescription.Key.Name),
				"idx", value.KeyDescription.Key.Index,
				"ttl", value.TTL,
				"data_len", len(value.Data),
				"qid", fmt.Sprintf("%x", msg.ID),
			)
			return answer(ValueFoundResult{Value: *value})
		}
		adnl.Logger("[DHT DEBUG] findValue miss", "qid", fmt.Sprintf("%x", msg.ID))
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
		if err = s.storeIn(q.Value); err != nil {
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
		if err := s.registerReverseConnection(peer, &q); err != nil {
			return err
		}
		return answer(Stored{})
	case RequestReversePing:
		result, err := s.requestReversePing(&q)
		if err != nil {
			return err
		}
		return answer(result)
	default:
		return fmt.Errorf("unsupported dht query type %s", reflect.TypeOf(payload))
	}
}

func (s *Server) handleMessage(peer adnl.Peer, msg *adnl.MessageCustom) error {
	sender, payload := unwrapPrefixedPayload(msg.Data)
	s.absorbSenderNode(peer, sender)

	switch q := payload.(type) {
	case RequestReversePingCont:
		return s.handleReversePingCont(&q)
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
	return buildSignedNode(
		keys.PublicKeyED25519{Key: s.key.Public().(ed25519.PublicKey)},
		cloneAddressList(&addrList),
		int32(time.Now().Unix()),
		s.networkID,
		s.key,
	)
}

func (s *Server) selfADNLNode() (*adnl.Node, error) {
	addrList := s.gateway.GetAddressList()
	node := &adnl.Node{
		ID:       keys.PublicKeyED25519{Key: s.key.Public().(ed25519.PublicKey)},
		AddrList: cloneAddressList(&addrList),
		Version:  int32(time.Now().Unix()),
	}

	nodeCopy := *node
	nodeCopy.Signature = nil
	data, err := tl.Serialize(nodeCopy, true)
	if err != nil {
		return nil, err
	}
	node.Signature = ed25519.Sign(s.key, data)
	return node, nil
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
	_, _ = s.addNodeWithStatus(node, true)
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

		sort.Slice(nodes, func(i, j int) bool {
			return compareDistance(nodes[i].adnlId, nodes[j].adnlId, key) < 0
		})

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

func (s *Server) storeIn(value *Value) error {
	if value == nil {
		return fmt.Errorf("nil value")
	}
	if int64(value.TTL) <= time.Now().Unix() {
		return nil
	}
	keyID, err := tl.Hash(value.KeyDescription.Key)
	if err != nil {
		return err
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

func (s *Server) registerReverseConnection(peer adnl.Peer, query *RegisterReverseConnection) error {
	now := time.Now()
	expiresAt := time.Unix(int64(query.TTL), 0)
	if !expiresAt.After(now) {
		return nil
	}

	clientID, err := tl.Hash(query.Node)
	if err != nil {
		return err
	}

	toSign := registerReverseConnectionToSign(clientID, s.selfID, query.TTL)
	pub, ok := query.Node.(keys.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("unsupported reverse connection node type %T", query.Node)
	}
	if !ed25519.Verify(pub.Key, toSign, query.Signature) {
		return fmt.Errorf("invalid reverse connection signature")
	}

	s.mx.Lock()
	s.reverseConnections[string(clientID)] = reverseConnection{
		addr:     peer.RemoteAddr(),
		key:      peer.GetPubKey(),
		keyID:    reverseConnectionKeyID(clientID),
		expireAt: expiresAt,
	}
	s.mx.Unlock()
	return nil
}

func (s *Server) requestReversePing(query *RequestReversePing) (any, error) {
	if err := query.Target.CheckSignature(); err != nil {
		return nil, err
	}

	s.mx.RLock()
	rc, ok := s.reverseConnections[string(query.Client)]
	s.mx.RUnlock()

	if ok && time.Now().Before(rc.expireAt) {
		peer, err := s.gateway.RegisterClient(rc.addr, rc.key)
		if err != nil {
			return nil, err
		}
		if err = peer.SendCustomMessage(context.Background(), RequestReversePingCont{
			Target:    query.Target,
			Signature: append([]byte{}, query.Signature...),
			Client:    append([]byte{}, query.Client...),
		}); err != nil {
			return nil, err
		}
		return ReversePingOKResult{}, nil
	}

	return ClientNotFoundResult{
		Nodes: NodesList{List: s.getNearestNodes(reverseConnectionKeyID(query.Client), s.limitK(query.K))},
	}, nil
}

func (s *Server) handleReversePingCont(query *RequestReversePingCont) error {
	if _, ok := s.ourReverseClients[string(query.Client)]; !ok {
		return nil
	}

	if err := query.Target.CheckSignature(); err != nil {
		return err
	}

	pub, ok := query.Target.ID.(keys.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("unsupported reverse ping target key %T", query.Target.ID)
	}
	if query.Target.AddrList == nil || len(query.Target.AddrList.Addresses) == 0 {
		return fmt.Errorf("reverse ping target has no addresses")
	}

	_, addr, err := firstDialAddress(query.Target.AddrList.Addresses)
	if err != nil {
		return fmt.Errorf("reverse ping target has no dialable addresses")
	}
	peer, err := s.gateway.RegisterClient(addr, pub.Key)
	if err != nil {
		return err
	}
	return peer.SendCustomMessage(context.Background(), tl.Raw(nil))
}

func (s *Server) maintenanceLoop() {
	defer close(s.done)

	nodeTicker := time.NewTicker(serverMaintenanceInterval)
	republishTicker := time.NewTicker(serverRepublishInterval)
	cleanupTicker := time.NewTicker(time.Second)
	defer nodeTicker.Stop()
	defer republishTicker.Stop()
	defer cleanupTicker.Stop()

	for {
		select {
		case <-s.globalCtx.Done():
			return
		case <-nodeTicker.C:
			s.refreshNodes()
		case <-republishTicker.C:
			s.republishValues()
		case <-cleanupTicker.C:
			s.cleanup()
		}
	}
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

	for i := range s.buckets {
		s.buckets[i].promoteReady()
	}
}

func (s *Server) republishValues() {
	s.mx.RLock()
	values := make([]*Value, 0, len(s.ourValues))
	for _, value := range s.ourValues {
		values = append(values, cloneValue(value))
	}
	s.mx.RUnlock()

	var forget [][]byte
	for _, value := range values {
		if value == nil || int64(value.TTL) <= time.Now().Unix()+60 {
			continue
		}

		keyID, err := tl.Hash(value.KeyDescription.Key)
		if err != nil {
			continue
		}

		dist := s.distance(keyID, s.k+10)
		if dist >= s.k+10 {
			forget = append(forget, append([]byte{}, keyID...))
			continue
		}
		if dist != 0 || !needRepublish(value) {
			continue
		}

		ctx, cancel := context.WithTimeout(s.globalCtx, queryTimeout)
		_, _ = s.Client.storePreparedValue(ctx, value)
		cancel()
	}

	if len(forget) > 0 {
		s.mx.Lock()
		for _, keyID := range forget {
			delete(s.ourValues, string(keyID))
		}
		s.mx.Unlock()
	}
}

func (s *Server) cleanup() {
	now := time.Now()
	var expiredKeys [][]byte

	_ = s.store.ForEach(func(keyID []byte, value *Value) error {
		if value != nil && int64(value.TTL) <= now.Unix() {
			expiredKeys = append(expiredKeys, append([]byte{}, keyID...))
		}
		return nil
	})
	for _, keyID := range expiredKeys {
		_ = s.store.Delete(keyID)
	}

	s.mx.Lock()
	for keyID, value := range s.ourValues {
		if value == nil || int64(value.TTL) <= now.Unix() {
			delete(s.ourValues, keyID)
		}
	}
	for clientID, conn := range s.reverseConnections {
		if !conn.expireAt.After(now) {
			delete(s.reverseConnections, clientID)
		}
	}
	s.mx.Unlock()
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
	if _, err := tl.Parse(&currentNodes, currentData, true); err != nil {
		return nil, err
	}

	var incomingNodes overlay.NodesList
	if _, err := tl.Parse(&incomingNodes, incomingData, true); err != nil {
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

func senderAddressList(node *Node) *address.List {
	if node == nil {
		return nil
	}
	return node.AddrList
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

func registerReverseConnectionToSign(clientID, dhtID []byte, ttl int32) []byte {
	buf := make([]byte, 32+32+4)
	copy(buf[:32], clientID)
	copy(buf[32:64], dhtID)
	binary.LittleEndian.PutUint32(buf[64:], uint32(ttl))
	return buf
}

func reverseConnectionKeyID(clientID []byte) []byte {
	keyID, _ := tl.Hash(Key{
		ID:    clientID,
		Name:  []byte("address"),
		Index: 0,
	})
	return keyID
}

func xorBit(a, b []byte, bit int) bool {
	if bit < 0 || bit >= len(a)*8 || bit >= len(b)*8 {
		return false
	}
	mask := byte(1 << uint(7-(bit%8)))
	return (a[bit/8] & mask) != (b[bit/8] & mask)
}
