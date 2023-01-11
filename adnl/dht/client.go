package dht

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tl"
	"net"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

const queryTimeout = 3000 * time.Millisecond

type ADNL interface {
	Query(ctx context.Context, req, result tl.Serializable) error
	SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey))
	Close()
}

var connect = func(ctx context.Context, addr string, peerKey ed25519.PublicKey, ourKey ed25519.PrivateKey) (ADNL, error) {
	return adnl.Connect(ctx, addr, peerKey, ourKey)
}

type Client struct {
	activeNodes    map[string]*dhtNode
	knownNodesInfo map[string]*Node
	queryTimeout   time.Duration
	mx             sync.RWMutex
	minNodeMx      sync.Mutex

	closer chan bool
	closed bool
}

type NodeInfo struct {
	Address string
	Key     ed25519.PublicKey
}

var Logger = func(v ...any) {}

func NewClientFromConfigUrl(ctx context.Context, cfgUrl string) (*Client, error) {
	cfg, err := liteclient.GetConfigFromUrl(ctx, cfgUrl)
	if err != nil {
		return nil, err
	}

	return NewClientFromConfig(ctx, cfg)
}

func NewClientFromConfig(ctx context.Context, cfg *liteclient.GlobalConfig) (*Client, error) {
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

	return NewClient(dl.Sub(time.Now()), nodes)
}

func NewClient(connectTimeout time.Duration, nodes []*Node) (*Client, error) {
	c := &Client{
		activeNodes:    map[string]*dhtNode{},
		knownNodesInfo: map[string]*Node{},
		closer:         make(chan bool, 1),
	}

	ch := make(chan bool, len(nodes))

	for _, node := range nodes {
		go func(node *Node) {
			ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
			defer cancel()

			_, err := c.addNode(ctx, node)
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
			delete(c.activeNodes, id)
		case _StateThrottle, _StateActive: // TODO: handle throttle in a diff list
			c.activeNodes[id] = node
		}
	}
}

func (c *Client) addNode(ctx context.Context, node *Node) (_ *dhtNode, err error) {
	pub, ok := node.ID.(adnl.PublicKeyED25519)
	if !ok {
		return nil, fmt.Errorf("unsupported id type %s", reflect.TypeOf(node.ID).String())
	}

	kid, err := adnl.ToKeyID(pub)
	if err != nil {
		return nil, err
	}

	signature := node.Signature
	node.Signature = nil
	toVerify, err := tl.Serialize(node, true)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize node: %w", err)
	}
	if !ed25519.Verify(pub.Key, toVerify, signature) {
		return nil, fmt.Errorf("bad signature for node: %s", hex.EncodeToString(pub.Key))
	}
	node.Signature = signature

	keyID := hex.EncodeToString(kid)

	c.mx.RLock()
	kNode := c.activeNodes[keyID]
	c.mx.RUnlock()

	if kNode != nil {
		return kNode, nil
	}

	// connect to first available address of node
	for _, udp := range node.AddrList.Addresses {
		addr := udp.IP.String() + ":" + fmt.Sprint(udp.Port)

		kNode, err = connectToNode(ctx, kid, addr, pub.Key, c.nodeStateHandler(keyID))
		if err != nil {
			// failed to connect, we will try next addr
			continue
		}

		c.mx.Lock()
		c.knownNodesInfo[keyID] = node
		c.mx.Unlock()

		// connected successfully
		break
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to node: %w", err)
	}

	return kNode, nil
}

func (c *Client) FindAddresses(ctx context.Context, key []byte) (*address.List, ed25519.PublicKey, error) {
	if len(key) != 32 {
		return nil, nil, fmt.Errorf("key should have 256 bits")
	}

	val, err := c.FindValue(ctx, &Key{
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
	return c.Store(ctx, []byte("address"), 0, data, ttl, ownerKey, copies)
}

func (c *Client) Store(ctx context.Context, name []byte, index int32, value []byte, ttl time.Duration, ownerKey ed25519.PrivateKey, copies int) (copiesMade int, idKey []byte, err error) {
	id := adnl.PublicKeyED25519{Key: ownerKey.Public().(ed25519.PublicKey)}
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
			UpdateRule: UpdateRuleSignature{},
		},
		Data: value,
		TTL:  int32(time.Now().Add(ttl).Unix()),
	}

	val.KeyDescription.Signature, err = signTL(val.KeyDescription, ownerKey)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to sign key description: %w", err)
	}
	val.Signature, err = signTL(val, ownerKey)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to sign value: %w", err)
	}

	kid, err := adnl.ToKeyID(val.KeyDescription.Key)
	if err != nil {
		return 0, nil, err
	}

	checked := map[string]bool{}

	plist := c.buildPriorityList(kid)

	copiesLeft := copies
	for copiesLeft > 0 {
		node, _ := plist.getNode()
		if node == nil {
			break
		}

		strId := hex.EncodeToString(node.id)

		if !checked[strId] {
			nodes, err := node.findNodes(ctx, kid, _K)
			if err != nil {
				continue
			}

			hasBetter := false
			currentPriority := leadingZeroBits(xor(kid, node.id))
			for _, n := range nodes {
				nid, err := adnl.ToKeyID(n.ID)
				if err != nil {
					continue
				}

				if checked[hex.EncodeToString(nid)] {
					continue
				}

				priority := leadingZeroBits(xor(kid, nid))
				if priority > currentPriority {
					addCtx, cancel := context.WithTimeout(ctx, queryTimeout)
					dNode, err := c.addNode(addCtx, n)
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
				checked[strId] = true
				plist.markNotUsed(node)
				continue
			}
		}

		storeCtx, cancel := context.WithTimeout(ctx, queryTimeout)
		err = node.storeValue(storeCtx, kid, &val)
		cancel()
		if err != nil {
			continue
		}

		copiesLeft--
	}

	if copiesLeft == copies {
		return 0, nil, fmt.Errorf("failed to store value: zero copies made")
	}

	return copies - copiesLeft, idKey, nil
}

func signTL(obj tl.Serializable, key ed25519.PrivateKey) ([]byte, error) {
	data, err := tl.Serialize(obj, true)
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(key, data), nil
}

func (c *Client) FindValue(ctx context.Context, key *Key) (*Value, error) {
	id, keyErr := adnl.ToKeyID(key)
	if keyErr != nil {
		return nil, keyErr
	}

	plist := c.buildPriorityList(id)

	threadCtx, stopThreads := context.WithCancel(ctx)
	defer stopThreads()

	const threads = 4
	result := make(chan *Value, threads)
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
					result <- v
				case []*Node:
					for _, n := range v {
						connectCtx, connectCancel := context.WithTimeout(threadCtx, queryTimeout)

						node, err = c.addNode(connectCtx, n)
						connectCancel()
						if err != nil {
							continue
						}

						plist.addNode(node)
					}
				}
			}
		}()
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case val := <-result:
		if val == nil {
			return nil, ErrDHTValueIsNotFound
		}
		return val, nil
	}
}

func (c *Client) buildPriorityList(id []byte) *priorityList {
	plist := newPriorityList(_K, id)

	c.mx.RLock()
	for _, node := range c.activeNodes {
		plist.addNode(node)
	}
	c.mx.RUnlock()

	return plist
}
