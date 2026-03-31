package dht

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/liteclient"
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
	RegisterClient(addr string, key ed25519.PublicKey) (adnl.Peer, error)
}

type Client struct {
	knownNodes map[string]*dhtNode // unused, nodes are stored in buckets
	buckets    [256]*Bucket

	gateway   Gateway
	networkID int32

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

	return newClient(gateway, nodes, networkID)
}

func NewClient(gateway Gateway, nodes []*Node) (*Client, error) {
	return newClient(gateway, nodes, _UnknownNetworkID)
}

func newClient(gateway Gateway, nodes []*Node, networkID int32) (*Client, error) {
	globalCtx, cancel := context.WithCancel(context.Background())

	buckets := [256]*Bucket{}
	for i := 0; i < 256; i++ {
		buckets[i] = newBucket(_K)
	}

	c := &Client{
		knownNodes:      map[string]*dhtNode{},
		buckets:         buckets,
		globalCtx:       globalCtx,
		globalCtxCancel: cancel,
		gateway:         gateway,
		networkID:       networkID,
	}

	for _, node := range nodes {
		_, err := c.addNode(node)
		if err != nil {
			logAddr := "<unknown>"
			if node != nil && node.AddrList != nil && len(node.AddrList.Addresses) > 0 && node.AddrList.Addresses[0] != nil {
				logAddr = node.AddrList.Addresses[0].IP.String() + ":" + fmt.Sprint(node.AddrList.Addresses[0].Port)
			}
			Logger("failed to add DHT node", logAddr, " from config, err:", err.Error())
			continue
		}
	}

	return c, nil
}

const _K = 7

func (c *Client) Close() {
	c.globalCtxCancel()
	_ = c.gateway.Close()
}

func (c *Client) addNode(node *Node) (_ *dhtNode, err error) {
	if node == nil {
		return nil, fmt.Errorf("nil node")
	}

	pub, ok := node.ID.(keys.PublicKeyED25519)
	if !ok {
		return nil, fmt.Errorf("unsupported id type %s", reflect.TypeOf(node.ID).String())
	}

	kid, err := tl.Hash(pub)
	if err != nil {
		return nil, err
	}

	affinity := affinity(kid, c.gateway.GetID())
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

	// TODO: maybe use other addresses too
	addr := addresses[0].IP.String() + ":" + fmt.Sprint(addresses[0].Port)

	if hf := bucket.findNode(kid); hf != nil {
		if hf.addr == addr && hf.version == node.Version {
			return nil, fmt.Errorf("node already exists")
		}
		// updated address otherwise
	}

	kNode := c.initNode(kid, addr, pub.Key, node.Version)
	bucket.addNode(kNode)

	return kNode, nil
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
	_, err = tl.Parse(&nodes, vv.Data, true)
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

func (c *Client) StoreAddress(
	ctx context.Context,
	addresses address.List,
	ttl time.Duration,
	ownerKey ed25519.PrivateKey,
) (storedCount int, idKey []byte, err error) {
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
	return c.Store(ctx, id, []byte("address"), 0, data, UpdateRuleSignature{}, ttl, ownerKey)
}

func (c *Client) StoreOverlayNodes(
	ctx context.Context,
	overlayKey []byte,
	nodes *overlay.NodesList,
	ttl time.Duration,
) (storedCount int, idKey []byte, err error) {
	if nodes == nil || len(nodes.List) == 0 {
		return 0, nil, fmt.Errorf("0 nodes in list")
	}

	overlayID, err := tl.Hash(keys.PublicKeyOverlay{Key: overlayKey})
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

	id := keys.PublicKeyOverlay{Key: overlayKey}
	return c.Store(ctx, id, []byte("nodes"), 0, data, UpdateRuleOverlayNodes{}, ttl, nil)
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

	const threads = 3
	result := make(chan *foundResult, threads)

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

			findCtx, cancel := context.WithTimeout(threadCtx, queryTimeout)
			val, err := node.findValue(findCtx, id, _K)
			cancel()
			if err != nil {
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
