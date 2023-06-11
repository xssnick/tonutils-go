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
	"runtime"
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
	RegisterClient(addr string, key ed25519.PublicKey) (adnl.Peer, error)
}

type Client struct {
	knownNodes map[string]*dhtNode
	mx         sync.RWMutex

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
	c := &Client{
		knownNodes:      map[string]*dhtNode{},
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

	if len(c.knownNodes) == 0 {
		return nil, errors.New("0 nodes was added")
	}
	return c, nil
}

const _K = 10

func (c *Client) Close() {
	c.globalCtxCancel()
	_ = c.gateway.Close()
}

func (c *Client) addNode(node *Node) (_ *dhtNode, err error) {
	pub, ok := node.ID.(adnl.PublicKeyED25519)
	if !ok {
		return nil, fmt.Errorf("unsupported id type %s", reflect.TypeOf(node.ID).String())
	}

	kid, err := adnl.ToKeyID(pub)
	if err != nil {
		return nil, err
	}
	keyID := hex.EncodeToString(kid)

	c.mx.Lock()
	defer c.mx.Unlock()

	kNode := c.knownNodes[keyID]
	if kNode != nil {
		return kNode, nil
	}

	if len(node.AddrList.Addresses) == 0 {
		return nil, fmt.Errorf("no addresses to connect to")
	} else if len(node.AddrList.Addresses) > 8 {
		// max 8 addresses to check
		node.AddrList.Addresses = node.AddrList.Addresses[:8]
	}

	// TODO: maybe use other addresses too
	addr := node.AddrList.Addresses[0].IP.String() + ":" + fmt.Sprint(node.AddrList.Addresses[0].Port)

	kNode = c.connectToNode(kid, addr, pub.Key)
	c.knownNodes[keyID] = kNode

	return kNode, nil
}

func (c *Client) FindOverlayNodes(ctx context.Context, overlayKey []byte, continuation ...*Continuation) (*overlay.NodesList, *Continuation, error) {
	keyHash, err := adnl.ToKeyID(adnl.PublicKeyOverlay{
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

func (c *Client) StoreAddress(ctx context.Context, addresses address.List, ttl time.Duration, ownerKey ed25519.PrivateKey, copies int) (int, []byte, error) {
	data, err := tl.Serialize(addresses, true)
	if err != nil {
		return 0, nil, err
	}

	id := adnl.PublicKeyED25519{Key: ownerKey.Public().(ed25519.PublicKey)}
	return c.Store(ctx, id, []byte("address"), 0, data, UpdateRuleSignature{}, ttl, ownerKey, copies)
}

func (c *Client) StoreOverlayNodes(ctx context.Context, overlayKey []byte, nodes *overlay.NodesList, ttl time.Duration, copies int) (int, []byte, error) {
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
	return c.Store(ctx, id, []byte("nodes"), 0, data, UpdateRuleOverlayNodes{}, ttl, nil, copies)
}

func (c *Client) Store(ctx context.Context, id any, name []byte, index int32, value []byte, rule any, ttl time.Duration, ownerKey ed25519.PrivateKey, atLeastCopies int) (copiesMade int, idKey []byte, err error) {
	idKey, err = adnl.ToKeyID(id)
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

	kid, err := adnl.ToKeyID(val.KeyDescription.Key)
	if err != nil {
		return 0, nil, err
	}

	var checkedMx sync.RWMutex
	checked := map[string]bool{}

	plist := c.buildPriorityList(kid)

	var wg sync.WaitGroup
	wg.Add(atLeastCopies + 1)
	copiesLeft := int64(atLeastCopies)
	storeCtx, cancelStoreCtx := context.WithCancel(ctx)
	for i := 0; i < atLeastCopies+1; i++ {
		go func() {
			defer wg.Done()
			for atomic.LoadInt64(&copiesLeft) > 0 {
				node, _ := plist.getNode()
				if node == nil {
					break
				}

				strId := node.id()
				checkedMx.RLock()
				isChecked := checked[strId]
				checkedMx.RUnlock()

				if !isChecked {
					nodes, err := node.findNodes(storeCtx, kid, _K)
					if err != nil {
						continue
					}

					hasBetter := false
					currentPriority := leadingZeroBits(xor(kid, node.adnlId))
					for _, n := range nodes {
						var nid []byte
						nid, err = adnl.ToKeyID(n.ID)
						if err != nil {
							continue
						}

						checkedMx.RLock()
						isNChecked := checked[hex.EncodeToString(nid)]
						checkedMx.RUnlock()

						if isNChecked {
							continue
						}

						priority := leadingZeroBits(xor(kid, nid))
						if priority > currentPriority {
							dNode, err := c.addNode(n)
							if err != nil {
								continue
							}

							if plist.addNode(dNode) {
								hasBetter = true
							}
						}
					}

					if hasBetter {
						// push back as checked, to be able to use later for other copy
						checkedMx.Lock()
						if !checked[strId] {
							checked[strId] = true
							plist.markUsed(node, false)
						}
						checkedMx.Unlock()
						continue
					}
				}

				storeCallCtx, cancel := context.WithTimeout(storeCtx, queryTimeout)
				err := node.storeValue(storeCallCtx, kid, &val)
				cancel()
				if err != nil {
					continue
				}

				if atomic.AddInt64(&copiesLeft, -1) == 0 {
					cancelStoreCtx()
					break
				}
			}
		}()
	}
	wg.Wait()
	cancelStoreCtx()

	if copiesLeft == int64(atLeastCopies) {
		return 0, nil, fmt.Errorf("failed to store value: zero copies made")
	}

	return atLeastCopies - int(copiesLeft), idKey, nil
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
	id, keyErr := adnl.ToKeyID(key)
	if keyErr != nil {
		return nil, nil, keyErr
	}

	plist := c.buildPriorityList(id)

	cont := &Continuation{}
	if len(continuation) > 0 && continuation[0] != nil {
		cont = continuation[0]
		for _, n := range cont.checkedNodes {
			// mark nodes as used to not get a value from them again
			plist.markUsed(n, true)
		}
	}

	threadCtx, stopThreads := context.WithCancel(ctx)
	defer stopThreads()

	const threads = 8
	result := make(chan *foundResult, threads)
	var numNoTasks int64
	for i := 0; i < threads; i++ {
		go func() {
			noTasks := false
			for {
				select {
				case <-threadCtx.Done():
					return
				default:
				}

				// we get most prioritized node, priority depends on depth
				node, _ := plist.getNode()
				if node == nil {
					if !noTasks {
						noTasks = true
						atomic.AddInt64(&numNoTasks, 1)
					}

					if atomic.LoadInt64(&numNoTasks) < threads {
						// something is pending
						runtime.Gosched()
						continue
					}

					result <- nil
					return
				}

				if noTasks {
					noTasks = false
					atomic.AddInt64(&numNoTasks, -1)
				}

				findCtx, findCancel := context.WithTimeout(threadCtx, queryTimeout)

				val, err := node.findValue(findCtx, id, _K)
				findCancel()
				if err != nil {
					continue
				}

				switch v := val.(type) {
				case *Value:
					result <- &foundResult{value: v, node: node}
				case []*Node:
					if len(v) > 24 {
						// max 24 nodes to add
						v = v[:24]
					}

					for _, n := range v {
						newNode, err := c.addNode(n)
						if err != nil {
							continue
						}

						plist.addNode(newNode)
					}
				}
			}
		}()
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

func (c *Client) buildPriorityList(id []byte) *priorityList {
	plist := newPriorityList(_K*3, id)

	added := 0

	c.mx.RLock()
	defer c.mx.RUnlock()

	// add fastest nodes first
	for _, node := range c.knownNodes {
		if node.currentState == _StateActive {
			plist.addNode(node)
			added++
		}
	}
	// if we have not enough fast nodes, add slow
	if added < 15 {
		for _, node := range c.knownNodes {
			if node.currentState == _StateThrottle {
				plist.addNode(node)
				added++
			}
		}
	}
	// if not enough active nodes, add failed, hope they will accept connection and become active
	// they may be failed due to our connection problems
	if added < 15 {
		for _, node := range c.knownNodes {
			if node.currentState == _StateFail {
				plist.addNode(node)
				added++
			}
		}
	}

	return plist
}
