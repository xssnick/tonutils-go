package dht

import (
	"bytes"
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
	"math/big"
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

type dhtNode struct {
	id   []byte
	adnl ADNL
	ping time.Duration // TODO: ping nodes
}

type Client struct {
	activeNodes    map[string]*dhtNode
	knownNodesInfo map[string]*Node
	queryTimeout   time.Duration
	mx             sync.RWMutex
	minNodeMx      sync.Mutex
}

type NodeInfo struct {
	Address string
	Key     ed25519.PublicKey
}

func NewClientFromConfigUrl(ctx context.Context, cfgUrl string) (*Client, error) {
	cfg, err := liteclient.GetConfigFromUrl(ctx, cfgUrl)
	if err != nil {
		return nil, err
	}

	dl, ok := ctx.Deadline()
	if !ok {
		dl = time.Now().Add(10 * time.Second)
	}

	return NewClientFromConfig(dl.Sub(time.Now()), cfg)
}

func NewClientFromConfig(connectTimeout time.Duration, cfg *liteclient.GlobalConfig) (*Client, error) {
	var nodes []NodeInfo
	for _, node := range cfg.DHT.StaticNodes.Nodes {
		ip := make(net.IP, 4)
		ii := int32(node.AddrList.Addrs[0].IP)
		binary.BigEndian.PutUint32(ip, uint32(ii))

		pp, _ := base64.StdEncoding.DecodeString(node.ID.Key)

		nodes = append(nodes, NodeInfo{
			Address: ip.String() + ":" + fmt.Sprint(node.AddrList.Addrs[0].Port),
			Key:     pp,
		})
	}

	return NewClient(connectTimeout, nodes)
}

func NewClient(connectTimeout time.Duration, nodes []NodeInfo) (*Client, error) {
	c := &Client{
		activeNodes:    map[string]*dhtNode{},
		knownNodesInfo: map[string]*Node{},
	}

	ch := make(chan bool, len(nodes))

	for _, node := range nodes {
		go func(node NodeInfo) {
			ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
			defer cancel()

			conn, err := c.connect(ctx, node.Address, node.Key, "")
			if err != nil {
				// failed to connect, we will try next addr
				return
			}

			var res Node
			err = conn.Query(ctx, SignedAddressListQuery{}, &res)
			if err != nil {
				return
			}
			conn.Close() // TODO: do it using 1 connection

			_, err = c.addNode(ctx, &res)
			if err != nil {
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

func (c *Client) addNode(ctx context.Context, node *Node) (_ *dhtNode, err error) {
	pub, ok := node.ID.(adnl.PublicKeyED25519)
	if !ok {
		return nil, fmt.Errorf("unsupported id type %s", reflect.TypeOf(node.ID).String())
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

	kid, err := adnl.ToKeyID(pub)
	if err != nil {
		return nil, err
	}

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
		conn, err := c.connect(ctx, addr, pub.Key, keyID)
		if err != nil {
			// failed to connect, we will try next addr
			continue
		}

		kNode = &dhtNode{
			id:   kid,
			adnl: conn,
		}

		c.mx.Lock()
		c.knownNodesInfo[keyID] = node
		c.activeNodes[keyID] = kNode
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

func (c *Client) FindValue(ctx context.Context, key *Key) (*Value, error) {
	id, keyErr := adnl.ToKeyID(key)
	if keyErr != nil {
		return nil, keyErr
	}

	c.minNodeMx.Lock()
	if len(c.activeNodes) < _K {
		connected := int64(_K)

		tasks := make(chan *Node)

		for i := 0; i < _K; i++ {
			go func() {
				for {
					node := <-tasks
					if node == nil {
						return
					}

					connCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
					_, err := c.addNode(connCtx, node)
					cancel()

					if err == nil {
						atomic.AddInt64(&connected, 1)
					}
				}
			}()
		}

		for nID, info := range c.knownNodesInfo {
			if _, ok := c.activeNodes[nID]; ok {
				// skip already active
				continue
			}

			if atomic.LoadInt64(&connected) > _K {
				break
			}

			tasks <- info
		}

		close(tasks)
	}
	c.minNodeMx.Unlock()

	plist := newPriorityList(_K)

	c.mx.RLock()
	for _, node := range c.activeNodes {
		priority := new(big.Int).SetBytes(xor(id, node.id))
		plist.addNode(node, priority)
	}
	c.mx.RUnlock()

	threadCtx, stopThreads := context.WithCancel(ctx)
	defer stopThreads()

	const threads = _K
	result := make(chan *Value, threads)
	var numInProgress int64
	for i := 0; i < threads; i++ {
		go func() {
			for {
				select {
				case <-threadCtx.Done():
					return
				default:
				}

				// we get most prioritized node, priority depends on depth
				node, _ := plist.getNode()
				if node == nil {
					if atomic.LoadInt64(&numInProgress) > 0 {
						// something is pending
						runtime.Gosched()
						continue
					}
					result <- nil
					return
				}

				findCtx, findCancel := context.WithTimeout(threadCtx, queryTimeout)

				atomic.AddInt64(&numInProgress, 1)
				val, err := c.FindValueRaw(findCtx, node, id, _K)
				findCancel()
				if err != nil {
					atomic.AddInt64(&numInProgress, -1)
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

						// priority depends on xor node id to key we are looking for. (smaller = higher)
						priority := new(big.Int).SetBytes(xor(id, node.id))

						plist.addNode(node, priority)
					}
				}
				atomic.AddInt64(&numInProgress, -1)
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

func (c *Client) FindValueRaw(ctx context.Context, node *dhtNode, id []byte, K int32) (result any, err error) {
	val, err := tl.Serialize(FindValue{
		Key: id,
		K:   K,
	}, true)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize dht value: %w", err)
	}

	var res any
	err = node.adnl.Query(ctx, tl.Raw(val), &res)
	if err != nil {
		c.mx.Lock()
		delete(c.activeNodes, hex.EncodeToString(node.id))
		c.mx.Unlock()

		return nil, fmt.Errorf("failed to do query to dht node: %w", err)
	}

	switch r := res.(type) {
	case ValueNotFoundResult:
		return r.Nodes.List, nil
	case ValueFoundResult:
		k := r.Value.KeyDescription.Key
		if len(k.Name) == 0 || len(k.Name) > 127 || k.Index < 0 || k.Index > 15 { // TODO: move to better place when dht store ready
			return nil, fmt.Errorf("invalid dht key")
		}

		idKey, err := adnl.ToKeyID(k)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(id, idKey) {
			return nil, fmt.Errorf("unwanted key received")
		}

		idPub, err := adnl.ToKeyID(r.Value.KeyDescription.ID)
		if err != nil {
			return nil, err
		}

		if !bytes.Equal(k.ID, idPub) {
			return nil, fmt.Errorf("inconsistent dht key description")
		}

		switch r.Value.KeyDescription.UpdateRule.(type) {
		case UpdateRuleAnybody:
			// no checks
		case UpdateRuleSignature:
			pub, ok := r.Value.KeyDescription.ID.(adnl.PublicKeyED25519)
			if !ok {
				return nil, fmt.Errorf("unsupported value's key type: %s", reflect.ValueOf(r.Value.KeyDescription.ID).String())
			}

			signature := r.Value.Signature
			r.Value.Signature = nil
			dataToCheck, err := tl.Serialize(r.Value, true)
			if err != nil {
				return nil, fmt.Errorf("failed to serialize value: %w", err)
			}
			if !ed25519.Verify(pub.Key, dataToCheck, signature) {
				return nil, fmt.Errorf("value's signature not match key")
			}

			// check key signature
			signature = r.Value.KeyDescription.Signature
			r.Value.KeyDescription.Signature = nil
			dataToCheck, err = tl.Serialize(r.Value.KeyDescription, true)
			if err != nil {
				return nil, fmt.Errorf("failed to serialize key description: %w", err)
			}
			if !ed25519.Verify(pub.Key, dataToCheck, signature) {
				return nil, fmt.Errorf("key description's signature not match key")
			}

		// case UpdateRuleOverlayNodes:
		// TODO: check sign
		/*pub, ok := r.Value.Data
		if !ok {
			return nil, fmt.Errorf("unsupported value's key type: %s", reflect.ValueOf(r.Value.KeyDescription.ID).String())
		}*/
		default:
			return nil, fmt.Errorf("update rule type %s is not supported yet", reflect.TypeOf(r.Value.KeyDescription.UpdateRule))
		}

		return &r.Value, nil
	}

	return nil, fmt.Errorf("failed to find value, unexpected response type %s", reflect.TypeOf(res).String())
}

func (c *Client) connect(ctx context.Context, addr string, key ed25519.PublicKey, id string) (ADNL, error) {
	a, err := connect(ctx, addr, key, nil)
	if err != nil {
		return nil, err
	}

	a.SetDisconnectHandler(c.disconnectHandler(id))

	return a, nil
}

func (c *Client) disconnectHandler(id string) func(addr string, key ed25519.PublicKey) {
	return func(addr string, key ed25519.PublicKey) {
		c.mx.Lock()
		delete(c.activeNodes, id)
		c.mx.Unlock()
	}
}

func xor(a, b []byte) []byte {
	if len(b) < len(a) {
		a = a[:len(b)]
	}

	tmp := make([]byte, len(a))
	n := copy(tmp, a)

	for i := 0; i < n; i++ {
		tmp[i] ^= b[i]
	}

	return tmp
}

/*
td::uint32 DhtMemberImpl::distance(DhtKeyId key_id, td::uint32 max_value) {
  if (!max_value) {
    max_value = 2 * k_;
  }
  td::uint32 res = 0;
  auto id_xor = key_id ^ key_;

  for (td::uint32 bit = 0; bit < 256; bit++) {
    if (id_xor.get_bit(bit)) {
      res += buckets_[bit].active_cnt();
      if (res >= max_value) {
        return max_value;
      }
    }
  }
  return res;
}
*/
