package dht

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/overlay"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tl"
)

const queryTimeout = 5000 * time.Millisecond

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
	mx         sync.RWMutex // unused, buckets has its own mutex

	gateway Gateway

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
	var nodes []*Node
	for _, node := range cfg.DHT.StaticNodes.Nodes {
		key, err := base64.StdEncoding.DecodeString(node.ID.Key)
		if err != nil {
			continue
		}

		sign, err := base64.StdEncoding.DecodeString(node.Signature)
		if err != nil {
			continue
		}

		n := &Node{
			ID: adnl.PublicKeyED25519{
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

	return NewClient(gateway, nodes)
}

func NewClient(gateway Gateway, nodes []*Node) (*Client, error) {
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
	}

	for _, node := range nodes {
		_, err := c.addNode(node)
		if err != nil {
			Logger("failed to add DHT node", node.AddrList.Addresses[0].IP.String(), node.AddrList.Addresses[0].Port, " from config, err:", err.Error())
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
	pub, ok := node.ID.(adnl.PublicKeyED25519)
	if !ok {
		return nil, fmt.Errorf("unsupported id type %s", reflect.TypeOf(node.ID).String())
	}

	kid, err := tl.Hash(pub)
	if err != nil {
		return nil, err
	}

	affinity := affinity(kid, c.gateway.GetID())
	bucket := c.buckets[affinity]

	if len(node.AddrList.Addresses) == 0 {
		return nil, fmt.Errorf("no addresses to connect to")
	} else if len(node.AddrList.Addresses) > 8 {
		// max 8 addresses to check
		node.AddrList.Addresses = node.AddrList.Addresses[:8]
	}

	// TODO: maybe use other addresses too
	addr := node.AddrList.Addresses[0].IP.String() + ":" + fmt.Sprint(node.AddrList.Addresses[0].Port)

	kNode := c.connectToNode(kid, addr, pub.Key)
	bucket.addNode(kNode)

	return kNode, nil
}

func (c *Client) FindOverlayNodes(ctx context.Context, overlayKey []byte, continuation ...*Continuation) (*overlay.NodesList, *Continuation, error) {
	keyHash, err := tl.Hash(adnl.PublicKeyOverlay{
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

	keyID, ok := val.KeyDescription.ID.(adnl.PublicKeyED25519)
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
	replicas int,
) (replicasMade int, idKey []byte, err error) {
	data, err := tl.Serialize(addresses, true)
	if err != nil {
		return 0, nil, err
	}

	id := adnl.PublicKeyED25519{Key: ownerKey.Public().(ed25519.PublicKey)}
	return c.Store(ctx, id, []byte("address"), 0, data, UpdateRuleSignature{}, ttl, ownerKey, replicas)
}

func (c *Client) StoreOverlayNodes(
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

	id := adnl.PublicKeyOverlay{Key: overlayKey}
	return c.Store(ctx, id, []byte("nodes"), 0, data, UpdateRuleOverlayNodes{}, ttl, nil, replicas)
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
	_ int, // unused, TON Whitepaper 3.2.7 - Store queries must be sent to all nodes in the K-sized bucket
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
	checkedMx := sync.RWMutex{}
	plist := c.buildPriorityList(keyId)

	const activeQueries = 2

	for {
		currentLen := len(checked)
		var wg sync.WaitGroup
		wg.Add(activeQueries)
		for i := 0; i < activeQueries; i++ {
			go func() {
				defer wg.Done()
				node, _ := plist.getNode()
				if node == nil {
					return
				}
				nodeId := node.id()
				checkedMx.RLock()
				isChecked := checked[nodeId]
				checkedMx.RUnlock()
				if isChecked {
					return
				}
				checkedMx.Lock()
				checked[nodeId] = true
				checkedMx.Unlock()
				fmt.Printf("Search nodes %s\n", nodeId)

				storeCallCtx, cancel := context.WithTimeout(ctx, queryTimeout)
				nodes, err := node.findNodes(storeCallCtx, keyId, _K)
				cancel()
				if err != nil {
					return
				} else {
					fmt.Printf("Adding nodes %d\n", len(nodes))
					for _, n := range nodes {
						_, err := c.addNode(n)
						if err != nil {
							continue
						}
					}
				}
			}()
		}
		wg.Wait()
		plist = c.buildPriorityList(keyId)
		if len(checked) == currentLen {
			fmt.Printf("S list stops growing: %d\n", len(checked))
			break
		} else {
			fmt.Printf("K iteration ends. Current size: %d\n", len(checked))
		}
	}

	stored := int32(0)

	for {
		var wg sync.WaitGroup
		wg.Add(activeQueries)
		for i := 0; i < activeQueries; i++ {
			go func() {
				defer wg.Done()
				for {
					node, _ := plist.getNode()
					if node == nil {
						return
					}
					storeCallCtx, cancel := context.WithTimeout(ctx, queryTimeout)
					err := node.storeValue(storeCallCtx, keyId, &val)
					cancel()
					if err == nil {
						fmt.Printf("Value stored on node %s - affinity %d\n", node.id(), affinity(keyId, node.adnlId))
						atomic.AddInt32(&stored, 1)
						return
					} else {
						fmt.Printf("Failed to store value on node %s - affinity %d - %s\n", node.id(), affinity(keyId, node.adnlId), err.Error())
					}
				}
			}()
		}
		wg.Wait()
		if atomic.LoadInt32(&stored) >= _K {
			break
		}
	}

	fmt.Println("Done.")

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

func (c *Client) FindValue(ctx context.Context, key *Key, continuation ...*Continuation) (*Value, *Continuation, error) {
	keyId, keyErr := tl.Hash(key)
	if keyErr != nil {
		return nil, nil, keyErr
	}

	plist := c.buildPriorityList(keyId)
	used := map[string]bool{}

	for {
		node, _ := plist.getNode()
		if node == nil {
			break
		}

		nodeId := node.id()

		isUsed := used[nodeId]
		if isUsed {
			continue
		}
		used[nodeId] = true

		findCtx, cancelFindCtx := context.WithTimeout(ctx, 10*time.Second)
		value, err := node.findValue(findCtx, keyId, _K)
		cancelFindCtx()
		if err != nil {
			fmt.Printf("Failed to find value on node %s - affinity %d - %s\n", node.id(), affinity(keyId, node.adnlId), err.Error())
			continue
		}

		switch v := value.(type) {
		case *Value:
			fmt.Printf("Value %s found on node %s - affinity %d\n", hex.EncodeToString(keyId), node.id(), affinity(keyId, node.adnlId))
			return value.(*Value), nil, nil
		case []*Node:
			for _, n := range v {
				dNode, err := c.addNode(n)
				if err != nil {
					continue
				}
				plist.addNode(dNode)
			}
			plist = c.buildPriorityList(keyId)
		}
	}

	return nil, nil, ErrDHTValueIsNotFound
}

func (c *Client) buildPriorityList(id []byte) *priorityList {
	plist := newPriorityList(_K*3, id)

	added := 0

loop:
	for i := 255; i >= 0; i-- {
		bucket := c.buckets[i]
		knownNodes := bucket.getNodes()
		for _, node := range knownNodes {
			if node != nil && node.badScore == 0 {
				if plist.addNode(node) {
					added++
				} else {
					break loop
				}
			}
		}
	}

	if added < _K {
	loop2:
		for i := 255; i >= 0; i-- {
			bucket := c.buckets[i]
			knownNodes := bucket.getNodes()
			for _, node := range knownNodes {
				if node != nil && node.badScore > 0 {
					if plist.addNode(node) {
						added++
					} else {
						break loop2
					}
				}
			}
		}
	}

	return plist
}
