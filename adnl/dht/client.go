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
	RegisterClient(addr string, key ed25519.PublicKey) (adnl.Peer, error)
}

type knownNode struct {
	node *Node
	mx   sync.Mutex
}

type Client struct {
	activeNodes    map[string]*dhtNode
	knownNodesInfo map[string]*knownNode
	queryTimeout   time.Duration
	mx             sync.RWMutex
	minNodeMx      sync.Mutex

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

	return NewClientFromConfig(ctx, gateway, cfg)
}

func NewClientFromConfig(ctx context.Context, gateway Gateway, cfg *liteclient.GlobalConfig) (*Client, error) {
	dl, ok := ctx.Deadline()
	if !ok {
		dl = time.Now().Add(10 * time.Second)
	}

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

	return NewClient(dl.Sub(time.Now()), gateway, nodes)
}

func NewClient(connectTimeout time.Duration, gateway Gateway, nodes []*Node) (*Client, error) {
	globalCtx, cancel := context.WithCancel(context.Background())
	c := &Client{
		activeNodes:     map[string]*dhtNode{},
		knownNodesInfo:  map[string]*knownNode{},
		globalCtx:       globalCtx,
		globalCtxCancel: cancel,
		gateway:         gateway,
	}

	ch := make(chan bool, len(nodes))

	for _, node := range nodes {
		go func(node *Node) {
			ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
			defer cancel()

			_, err := c.addNode(ctx, node, false)
			if err != nil {
				Logger("failed to add DHT node", node.AddrList.Addresses[0].IP.String(), node.AddrList.Addresses[0].Port, " from config, err:", err.Error())
				return
			}

			ch <- true
		}(node)
	}

	select {
	case <-ch:
	case <-time.After(connectTimeout):
	}

	if len(c.activeNodes) == 0 {
		return nil, fmt.Errorf("no available nodes in the given list %v", nodes)
	}

	go c.nodesPinger()
	return c, nil
}

const _K = 10

func (c *Client) Close() {
	c.mx.Lock()
	var toClose []*dhtNode
	// doing this way to not get deadlock with nodeStateHandler
	for _, v := range c.activeNodes {
		toClose = append(toClose, v)
	}
	c.activeNodes = nil
	c.mx.Unlock()

	c.globalCtxCancel()

	for _, node := range toClose {
		node.Close()
	}
}

func (c *Client) nodeStateHandler(id string) func(node *dhtNode, state int) {
	return func(node *dhtNode, state int) {
		c.mx.Lock()
		defer c.mx.Unlock()

		if c.activeNodes == nil {
			return
		}

		switch state {
		case _StateFail:
			// delete(c.activeNodes, id)
		case _StateThrottle, _StateActive: // TODO: handle throttle in a diff list
			c.activeNodes[id] = node
		}
	}
}

func (c *Client) addNode(ctx context.Context, node *Node, waitConnection bool) (_ *dhtNode, err error) {
	pub, ok := node.ID.(adnl.PublicKeyED25519)
	if !ok {
		return nil, fmt.Errorf("unsupported id type %s", reflect.TypeOf(node.ID).String())
	}

	kid, err := adnl.ToKeyID(pub)
	if err != nil {
		return nil, err
	}

	keyID := hex.EncodeToString(kid)
	c.mx.RLock()
	kNode := c.knownNodesInfo[keyID]
	aNode := c.activeNodes[keyID]
	c.mx.RUnlock()

	if aNode != nil {
		// we already connected to this node, just return it
		return aNode, nil
	}

	if len(node.AddrList.Addresses) == 0 {
		return nil, fmt.Errorf("no addresses to connect to")
	} else if len(node.AddrList.Addresses) > 8 {
		// max 8 addresses to check
		node.AddrList.Addresses = node.AddrList.Addresses[:8]
	}

	if kNode == nil {
		c.mx.Lock()
		// check again under lock to guarantee that only one connection will be made
		if c.knownNodesInfo[keyID] == nil {
			kNode = &knownNode{node: node}
			c.knownNodesInfo[keyID] = kNode
		} else {
			kNode = c.knownNodesInfo[keyID]
		}
		c.mx.Unlock()
	}

	if waitConnection {
		for {
			if kNode.mx.TryLock() {
				break
			}

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				runtime.Gosched()
			}
		}
	} else {
		if !kNode.mx.TryLock() {
			return nil, fmt.Errorf("connection already in progress")
		}
	}

	defer kNode.mx.Unlock()

	c.mx.RLock()
	aNode = c.activeNodes[keyID]
	c.mx.RUnlock()

	if aNode != nil {
		// we already connected to this node, just return it
		return aNode, nil
	}

	// connect to first available address of node
	for _, udp := range node.AddrList.Addresses {
		addr := udp.IP.String() + ":" + fmt.Sprint(udp.Port)

		aNode, err = c.connectToNode(ctx, kid, addr, pub.Key, c.nodeStateHandler(keyID))
		if err != nil {
			// failed to connect, we will try next addr
			continue
		}
		// connected successfully
		break
	}

	if err != nil {
		c.mx.Lock()
		// connection was unsuccessful, so we remove node from known, to be able to retry later
		delete(c.knownNodesInfo, keyID)
		c.mx.Unlock()

		return nil, fmt.Errorf("failed to connect to node: %w", err)
	}

	return aNode, nil
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
	triedToAdd := map[string]bool{}

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

				strId := hex.EncodeToString(node.id)
				checkedMx.RLock()
				isChecked := checked[strId]
				checkedMx.RUnlock()

				if !isChecked {
					nodes, err := node.findNodes(storeCtx, kid, _K)
					if err != nil {
						continue
					}

					hasBetter := false
					currentPriority := leadingZeroBits(xor(kid, node.id))
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
							checkedMx.Lock()
							tried := triedToAdd[hex.EncodeToString(nid)]
							if !tried {
								triedToAdd[hex.EncodeToString(nid)] = true
							}
							checkedMx.Unlock()

							addCtx, cancel := context.WithTimeout(storeCtx, queryTimeout)
							dNode, err := c.addNode(addCtx, n, !tried)
							cancel()
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

	result := make(chan *foundResult)

	checked := map[string]bool{}
	checkedMx := sync.Mutex{}
	go func() {
		wg := sync.WaitGroup{}
		for {
			n, _ := plist.getNode()
			if n == nil {
				break
			}

			checkedMx.Lock()
			checked[string(n.id)] = true
			checkedMx.Unlock()

			wg.Add(1)
			go func() {
				c.searchVal(threadCtx, n, id, result, checked, &checkedMx)
				wg.Done()
			}()
		}
		wg.Wait()

		select {
		case <-threadCtx.Done():
		case result <- nil:
		}
	}()

	select {
	case val := <-result:
		if val == nil {
			return nil, cont, ErrDHTValueIsNotFound
		}
		cont.checkedNodes = append(cont.checkedNodes, val.node)
		return val.value, cont, nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

func (c *Client) searchVal(ctx context.Context, n *dhtNode, id []byte, result chan<- *foundResult, checked map[string]bool, mx *sync.Mutex) {
	findCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	val, err := n.findValue(findCtx, id, _K)
	cancel()
	if err != nil {
		return
	}

	switch v := val.(type) {
	case *Value:
		select {
		case <-ctx.Done():
		case result <- &foundResult{value: v, node: n}:
		}
		return
	case []*Node:
		if len(v) > 16 {
			// max 16 nodes to check
			v = v[:16]
		}

		wg := sync.WaitGroup{}
		wg.Add(len(v))

		for _, node := range v {
			nid, keyErr := adnl.ToKeyID(node.ID)
			if keyErr != nil {
				wg.Done()
				continue
			}

			mx.Lock()
			if checked[string(nid)] {
				mx.Unlock()
				wg.Done()
				continue
			}
			checked[string(nid)] = true
			mx.Unlock()

			go func(node *Node) {
				defer wg.Done()

				connectCtx, connectCancel := context.WithTimeout(ctx, queryTimeout)
				newNode, err := c.addNode(connectCtx, node, true)
				connectCancel()
				if err != nil {
					return
				}

				c.searchVal(ctx, newNode, id, result, checked, mx)
			}(node)
		}
		wg.Wait()
	}
}

func (c *Client) FindValueOld(ctx context.Context, key *Key, continuation ...*Continuation) (*Value, *Continuation, error) {
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

	var checkedMx sync.RWMutex
	triedToAdd := map[string]bool{}

	const threads = 12
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
						// max 24 nodes to check
						v = v[:24]
					}

					wg := sync.WaitGroup{}
					wg.Add(len(v))

					connectCtx, connectCancel := context.WithTimeout(threadCtx, queryTimeout)
					for _, n := range v {
						go func(n *Node) {
							defer wg.Done()

							nid, err := adnl.ToKeyID(n.ID)
							if err != nil {
								return
							}

							checkedMx.Lock()
							tried := triedToAdd[hex.EncodeToString(nid)]
							if !tried {
								triedToAdd[hex.EncodeToString(nid)] = true
							}
							checkedMx.Unlock()

							newNode, err := c.addNode(connectCtx, n, !tried)
							if err != nil {
								return
							}

							plist.addNode(newNode)
						}(n)
					}
					wg.Wait()
					connectCancel()
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

func (c *Client) nodesPinger() {
	for {
		select {
		case <-c.globalCtx.Done():
			return
		case <-time.After(1 * time.Second):
		}

		now := time.Now()
		c.mx.RLock()
		if len(c.activeNodes) == 0 {
			c.mx.RUnlock()
			continue
		}

		ch := make(chan *dhtNode, len(c.activeNodes)+1)
		for _, node := range c.activeNodes {
			// add check task for nodes that were not queried for > 8 seconds
			if atomic.LoadInt64(&node.lastQueryAt)+8 < now.Unix() {
				ch <- node
			}
		}
		close(ch)
		c.mx.RUnlock()

		var wg sync.WaitGroup
		wg.Add(8)
		for i := 0; i < 8; i++ {
			go func() {
				defer wg.Done()
				for {
					var node *dhtNode
					select {
					case <-c.globalCtx.Done():
						return
					case node = <-ch:
						if node == nil {
							// everything is checked
							return
						}
					}

					ctx, cancel := context.WithTimeout(c.globalCtx, queryTimeout)
					_ = node.checkPing(ctx) // we don't need the result, it will report new state to callback
					cancel()
				}
			}()
		}
		wg.Wait()
	}
}

func (c *Client) buildPriorityList(id []byte) *priorityList {
	plist := newPriorityList(_K*3, id)

	added := 0
	c.mx.RLock()
	// add fastest nodes first
	for _, node := range c.activeNodes {
		if node.getState() == _StateActive {
			plist.addNode(node)
			added++
		}
	}
	// if we have not enough fast nodes, add slow
	if added < 15 {
		for _, node := range c.activeNodes {
			if node.getState() == _StateThrottle {
				plist.addNode(node)
				added++
			}
		}
	}
	// if not enough active nodes, add failed, hope they will accept connection and become active
	// they may be failed due to our connection problems
	if added < 15 {
		for _, node := range c.activeNodes {
			if node.getState() == _StateFail {
				plist.addNode(node)
				added++
			}
		}
	}
	c.mx.RUnlock()

	return plist
}
