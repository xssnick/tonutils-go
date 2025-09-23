package dht

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/liteclient"
	"io"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/overlay"
	"github.com/xssnick/tonutils-go/tl"
)

const queryTimeout = 3000 * time.Millisecond

type ADNL interface {
	Query(ctx context.Context, req, result tl.Serializable) error
	SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey))
	Close()
}

type Gateway interface {
	Close() error
	GetID() []byte
	GetKey() ed25519.PrivateKey
	RegisterClient(addr string, key ed25519.PublicKey) (adnl.Peer, error)
	SetConnectionHandler(handler func(client adnl.Peer) error)
	GetAddressList() address.List
}

// Deprecated: use DHT
type Client = DHT

type DHT struct {
	buckets [256]*Bucket

	gateway    Gateway
	serverMode bool

	values   map[string]*Value
	valuesMx sync.RWMutex

	selfCached atomic.Pointer[Node]

	globalCtx       context.Context
	globalCtxCancel func()
}

// Continuation allows to check value on the next nodes.
// Suitable for overlays, different DHT nodes may contain different node addresses.
// Can be used in case of everything is offline in single DHT node, to check another values.
type Continuation struct {
	checkedNodes []*dhtNode
}

type NodeInfo struct {
	Address string
	Key     ed25519.PublicKey
}

var Logger = func(v ...any) {}

func NewClientFromConfigUrl(ctx context.Context, gateway Gateway, cfgUrl string) (*DHT, error) {
	cfg, err := liteclient.GetConfigFromUrl(ctx, cfgUrl)
	if err != nil {
		return nil, err
	}

	return NewClientFromConfig(gateway, cfg)
}

func BootstrapNodesFromConfig(cfg *liteclient.GlobalConfig) ([]*Node, error) {
	var nodes []*Node
	for _, node := range cfg.DHT.StaticNodes.Nodes {
		key, err := base64.StdEncoding.DecodeString(node.ID.Key)
		if err != nil {
			return nil, err
		}

		sign, err := base64.StdEncoding.DecodeString(node.Signature)
		if err != nil {
			return nil, err
		}

		n := &Node{
			ID: keys.PublicKeyED25519{
				Key: key,
			},
			AddrList: &address.List{
				Version:    int32(node.AddrList.Version),
				ReinitDate: int32(node.AddrList.ReinitDate),
				Priority:   int32(node.AddrList.Priority),
				ExpireAt:   int32(node.AddrList.ExpireAt),
			},
			Version:   int32(node.Version),
			Signature: sign,
		}

		for _, addr := range node.AddrList.Addrs {
			ip := make(net.IP, 4)
			ii := int32(addr.IP)
			binary.BigEndian.PutUint32(ip, uint32(ii))
			n.AddrList.Addresses = append(n.AddrList.Addresses, &address.UDP{
				IP:   ip,
				Port: int32(addr.Port),
			})
		}

		nodes = append(nodes, n)
	}

	return nodes, nil
}

func NewClientFromConfig(gateway Gateway, cfg *liteclient.GlobalConfig) (*DHT, error) {
	list, err := BootstrapNodesFromConfig(cfg)
	if err != nil {
		return nil, err
	}

	return NewClient(gateway, list)
}

func New(gateway Gateway, nodes []*Node, serverMode bool) (*DHT, error) {
	if serverMode {
		if len(gateway.GetAddressList().Addresses) == 0 {
			return nil, fmt.Errorf("to run dht in server mode, at least one gateway address should be configured")
		}
	}

	buckets := [256]*Bucket{}
	for i := 0; i < 256; i++ {
		buckets[i] = newBucket(_K)
	}

	globalCtx, cancel := context.WithCancel(context.Background())

	c := &DHT{
		buckets:         buckets,
		globalCtx:       globalCtx,
		globalCtxCancel: cancel,
		gateway:         gateway,
		serverMode:      serverMode,
		values:          map[string]*Value{},
	}

	if serverMode {
		gateway.SetConnectionHandler(func(peer adnl.Peer) error {
			peer.SetQueryHandler(func(msg *adnl.MessageQuery) error {
				return c.processQuery(peer, msg.ID, msg.Data)
			})
			return nil
		})

		go c.cleaner()
	}

	for _, node := range nodes {
		if _, err := c.addNode(node); err != nil {
			Logger("failed to add DHT node", node.AddrList.Addresses[0].IP.String(), node.AddrList.Addresses[0].Port, " from config, err:", err.Error())
			continue
		}
	}

	return c, nil
}

func NewClient(gateway Gateway, nodes []*Node) (*DHT, error) {
	return New(gateway, nodes, false)
}

const _K = 7

func (c *DHT) Close() {
	c.globalCtxCancel()
	_ = c.gateway.Close()
}

func (c *DHT) addNode(node *Node) (_ *dhtNode, err error) {
	pub, ok := node.ID.(keys.PublicKeyED25519)
	if !ok {
		return nil, fmt.Errorf("unsupported id type %s", reflect.TypeOf(node.ID).String())
	}

	kid, err := tl.Hash(pub)
	if err != nil {
		return nil, err
	}

	affinity := affinity(kid, c.gateway.GetID())
	if affinity == 256 {
		return nil, fmt.Errorf("self node")
	}

	bucket := c.buckets[affinity]

	if len(node.AddrList.Addresses) == 0 {
		return nil, fmt.Errorf("no addresses to connect to")
	} else if len(node.AddrList.Addresses) > 5 {
		// max 5 addresses to check
		node.AddrList.Addresses = node.AddrList.Addresses[:5]
	}

	// TODO: maybe use other addresses too
	addr := node.AddrList.Addresses[0].IP.String() + ":" + fmt.Sprint(node.AddrList.Addresses[0].Port)

	if hf := bucket.findNode(kid); hf != nil {
		hf.updateEndpoint(addr, pub.Key)
		hf.setNode(node)
		return hf, nil
	}

	kNode := c.initNode(kid, addr, pub.Key, node)
	bucket.addNode(kNode)

	return kNode, nil
}

func (c *DHT) FindOverlayNodes(ctx context.Context, overlayKey []byte, continuation ...*Continuation) (*overlay.NodesList, *Continuation, error) {
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
	_, err = tl.Parse(&nodes, vv.Data, true)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse dht data for overlay nodes: %w", err)
	}
	return &nodes, cont, nil
}

func (c *DHT) FindAddresses(ctx context.Context, key []byte) (*address.List, ed25519.PublicKey, error) {
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
	_, err = tl.Parse(&list, val.Data, true)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse address list: %w", err)
	}

	keyID, ok := val.KeyDescription.ID.(keys.PublicKeyED25519)
	if !ok {
		return nil, nil, fmt.Errorf("unsupported key type %s", reflect.TypeOf(val.KeyDescription.ID))
	}

	return &list, keyID.Key, nil
}

var ErrDHTValueIsNotFound = errors.New("value is not found")

func (c *DHT) StoreAddress(
	ctx context.Context,
	addresses address.List,
	ttl time.Duration,
	ownerKey ed25519.PrivateKey,
	replicas int,
) (replicasMade int, idKey []byte, err error) {
	for i, udp := range addresses.Addresses {
		if udp.IP.Equal(net.IPv4zero) {
			return 0, nil, fmt.Errorf("address %d is zero", i)
		}
	}

	data, err := tl.Serialize(addresses, true)
	if err != nil {
		return 0, nil, err
	}

	id := keys.PublicKeyED25519{Key: ownerKey.Public().(ed25519.PublicKey)}
	return c.Store(ctx, id, []byte("address"), 0, data, UpdateRuleSignature{}, ttl, ownerKey, replicas)
}

func (c *DHT) StoreOverlayNodes(
	ctx context.Context,
	overlayKey []byte,
	nodes *overlay.NodesList,
	ttl time.Duration,
	replicas int,
) (replicasMade int, idKey []byte, err error) {
	if len(nodes.List) == 0 {
		return 0, nil, fmt.Errorf("0 nodes in list")
	}

	for _, node := range nodes.List {
		err := node.CheckSignature()
		if err != nil {
			return 0, nil, fmt.Errorf("untrusted overlay node in list: %w", err)
		}
	}

	data, err := tl.Serialize(nodes, true)
	if err != nil {
		return 0, nil, err
	}

	id := keys.PublicKeyOverlay{Key: overlayKey}
	return c.Store(ctx, id, []byte("nodes"), 0, data, UpdateRuleOverlayNodes{}, ttl, nil, replicas)
}

func (c *DHT) Store(
	ctx context.Context,
	id any,
	name []byte,
	index int32,
	value []byte,
	rule any,
	ttl time.Duration,
	ownerKey ed25519.PrivateKey,
	_ int,
) (_ int, idKey []byte, err error) {
	idKey, err = tl.Hash(id)
	if err != nil {
		return 0, nil, err
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
		val.KeyDescription.Signature, err = signTL(val.KeyDescription, ownerKey)
		if err != nil {
			return 0, nil, fmt.Errorf("failed to sign key description: %w", err)
		}
		val.Signature, err = signTL(val, ownerKey)
		if err != nil {
			return 0, nil, fmt.Errorf("failed to sign value: %w", err)
		}
	}

	keyId, err := tl.Hash(val.KeyDescription.Key)
	if err != nil {
		return 0, nil, err
	}

	checked := map[string]bool{}

	final := newPriorityList(_K, keyId)

	for {
		plist := c.buildPriorityList(keyId)

		var wg sync.WaitGroup
		var expansion int32
		for {
			node, _ := plist.Get()
			if node == nil {
				break
			}

			if _, ok := checked[string(node.adnlId)]; ok {
				continue
			}
			checked[string(node.adnlId)] = true

			wg.Add(1)
			go func(n *dhtNode) {
				defer wg.Done()

				ctxQuery, cancel := context.WithTimeout(ctx, queryTimeout)
				defer cancel()

				nodes, err := n.findNodes(ctxQuery, keyId, _K)
				if err != nil {
					return
				}

				// add responsive nodes
				final.Add(n)

				for _, newN := range nodes {
					if an, err := c.addNode(newN); err == nil && affinity(an.adnlId, keyId) >= uint(final.GetBestAffinity()) {
						atomic.StoreInt32(&expansion, 1)
					}
				}
			}(node)
		}
		wg.Wait()

		if atomic.LoadInt32(&expansion) == 0 {
			break
		}
	}

	var wg sync.WaitGroup
	var stored int32

	for {
		node, _ := final.Get()
		if node == nil {
			break
		}
		wg.Add(1)

		go func(n *dhtNode) {
			defer wg.Done()

			ctxStore, cancel := context.WithTimeout(ctx, queryTimeout)
			defer cancel()

			if err := n.storeValue(ctxStore, keyId, &val); err == nil {
				atomic.AddInt32(&stored, 1)
			}
		}(node)
	}
	wg.Wait()

	if stored == 0 {
		return 0, idKey, fmt.Errorf("no alive nodes found to store this key")
	}

	return int(stored), idKey, nil
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
func (c *DHT) FindValue(ctx context.Context, key *Key, continuation ...*Continuation) (*Value, *Continuation, error) {
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

	const threads = 3
	result := make(chan *foundResult, threads)

	cond := sync.NewCond(&sync.Mutex{})
	waitingThreads := 0

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
					cond.L.Unlock()
					result <- nil
					return
				}

				cond.Wait()
				node, _ = plist.Get()
				waitingThreads--
			}
			cond.L.Unlock()

			findCtx, cancel := context.WithTimeout(threadCtx, queryTimeout)
			val, err := node.findValue(findCtx, id, _K)
			cancel()
			if err != nil {
				continue
			}

			switch v := val.(type) {
			case *Value:
				result <- &foundResult{value: v, node: node}
				return
			case []*Node:
				added := false
				for _, n := range v {
					if newNode, err := c.addNode(n); err == nil {
						plist.Add(newNode)
						added = true
					}
				}

				if added {
					cond.Broadcast()
				}
			}
		}
	}

	for i := 0; i < threads; i++ {
		go launchWorker()
	}

	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case val := <-result:
		if val == nil {
			return nil, cont, ErrDHTValueIsNotFound
		}

		cont.checkedNodes = append(cont.checkedNodes, val.node)
		return val.value, cont, nil
	}
}

func (c *DHT) buildPriorityList(id []byte) *priorityList {
	plistGood := newPriorityList(_K+_K/2, id)
	plistBad := newPriorityList(_K/2, id)

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

type nodeAffinity struct {
	node     *Node
	affinity int
}

func (c *DHT) nearestNodeAffinities(id []byte, limit int) []nodeAffinity {
	if limit <= 0 {
		return nil
	}

	plist := c.buildPriorityList(id)
	result := make([]nodeAffinity, 0, limit)
	seen := map[string]bool{}

	for len(result) < limit {
		node, pr := plist.Get()
		if node == nil {
			break
		}

		key := node.id()
		if seen[key] {
			continue
		}
		seen[key] = true

		nn := node.getNode()
		if nn == nil {
			continue
		}

		result = append(result, nodeAffinity{node: nn, affinity: pr})
	}

	return result
}

func (c *DHT) nearestNodes(id []byte, limit int) ([]*Node, []int) {
	details := c.nearestNodeAffinities(id, limit)
	nodes := make([]*Node, 0, len(details))
	affinities := make([]int, 0, len(details))

	for _, d := range details {
		nodes = append(nodes, d.node)
		affinities = append(affinities, d.affinity)
	}

	return nodes, affinities
}

// Bootstrap ensures that the client knows at least minNodes peers by repeatedly
// performing findNode requests starting from the configured bootstrap nodes.
func (c *DHT) Bootstrap(ctx context.Context, minNodes int) error {
	if minNodes <= 0 {
		return nil
	}

	initial := c.knownNodeCount()
	switch {
	case initial >= minNodes:
		return nil
	case initial == 0:
		return fmt.Errorf("dht: bootstrap requires at least one known node")
	}

	queue := c.collectKnownNodes()
	if len(queue) == 0 {
		return fmt.Errorf("dht: bootstrap has no nodes to query")
	}

	visited := make(map[string]bool, len(queue))

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}

		if c.knownNodeCount() >= minNodes {
			return nil
		}

		node := queue[0]
		queue = queue[1:]

		id := node.id()
		if visited[id] {
			continue
		}
		visited[id] = true

		key, err := randomBootstrapKey(rand.Reader, c.gateway.GetID())
		if err != nil {
			return fmt.Errorf("dht: failed to generate random lookup key: %w", err)
		}

		ctxQuery, cancel := context.WithTimeout(ctx, queryTimeout)
		candidates, err := node.findNodes(ctxQuery, key, _K)
		cancel()
		if err != nil {
			continue
		}

		for _, candidate := range candidates {
			if candidate == nil {
				continue
			}
			if err := candidate.CheckSignature(); err != nil {
				continue
			}
			added, err := c.addNode(candidate)
			if err != nil {
				continue
			}

			addedID := added.id()
			if !visited[addedID] {
				queue = append(queue, added)
			}
		}
	}

	if c.knownNodeCount() >= minNodes {
		return nil
	}

	return fmt.Errorf("dht: bootstrap discovered %d nodes, want %d", c.knownNodeCount(), minNodes)
}

func (c *DHT) knownNodeCount() int {
	total := 0
	for i := range c.buckets {
		nodes := c.buckets[i].getNodes()
		for _, node := range nodes {
			if node != nil {
				total++
			}
		}
	}
	return total
}

func randomBootstrapKey(r io.Reader, selfID []byte) ([]byte, error) {
	if len(selfID) < 32 {
		return nil, fmt.Errorf("dht: gateway id too short: %d", len(selfID))
	}

	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, fmt.Errorf("read random key: %w", err)
	}

	var tBuf [1]byte
	if _, err := io.ReadFull(r, tBuf[:]); err != nil {
		return nil, fmt.Errorf("read bootstrap tier: %w", err)
	}
	t := int(tBuf[0] % 7)

	limit := 1 << uint(t)
	randVal, err := readRandBelowInclusive(r, limit)
	if err != nil {
		return nil, err
	}

	prefixBits := 64 - randVal
	if prefixBits < 0 {
		prefixBits = 0
	}

	applyPrefixBits(key, selfID, prefixBits)
	return key, nil
}

func readRandBelowInclusive(r io.Reader, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}

	base := limit + 1
	maxAccept := (256 / base) * base
	var b [1]byte

	for {
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, fmt.Errorf("read random byte: %w", err)
		}
		if int(b[0]) < maxAccept {
			return int(b[0]) % base, nil
		}
	}
}

func applyPrefixBits(dst, src []byte, bits int) {
	if bits <= 0 {
		return
	}

	maxBits := len(dst) * 8
	if srcBits := len(src) * 8; srcBits < maxBits {
		maxBits = srcBits
	}
	if bits > maxBits {
		bits = maxBits
	}

	for i := 0; i < bits; i++ {
		byteIdx := i / 8
		bitIdx := 7 - (i % 8)
		mask := byte(1 << uint(bitIdx))
		dst[byteIdx] &^= mask
		if src[byteIdx]&mask != 0 {
			dst[byteIdx] |= mask
		}
	}
}

func (c *DHT) collectKnownNodes() []*dhtNode {
	seen := map[string]struct{}{}
	nodes := make([]*dhtNode, 0)

	for i := range c.buckets {
		bucket := c.buckets[i]
		for _, node := range bucket.getNodes() {
			if node == nil {
				continue
			}
			id := node.id()
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			nodes = append(nodes, node)
		}
	}

	return nodes
}
