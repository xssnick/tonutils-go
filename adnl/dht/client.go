package dht

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/tl"
	"log"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

const queryTimeout = 2000 * time.Millisecond

type Client struct {
	knownNodes   map[string]*adnl.ADNL
	queryTimeout time.Duration
	mx           sync.RWMutex
}

type NodeInfo struct {
	Address string
	Key     ed25519.PublicKey
}

var Logger = log.Printf

func NewClient(connectTimeout time.Duration, nodes []NodeInfo) (*Client, error) {
	c := &Client{
		knownNodes: map[string]*adnl.ADNL{},
	}

	ch := make(chan bool, len(nodes))

	for _, node := range nodes {
		go func(node NodeInfo) {
			ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
			defer cancel()

			conn, err := c.connect(ctx, node.Address, node.Key)
			if err != nil {
				// failed to connect, we will try next addr
				return
			}

			var res Node
			err = conn.Query(ctx, SignedAddressListQuery{}, &res)
			if err != nil {
				return
			}

			verified := false
			for _, udp := range res.AddrList.Addresses {
				if fmt.Sprintf("%s:%d", udp.IP.String(), udp.Port) == node.Address {
					verified = true
					break
				}
			}

			if !verified {
				conn.Close()
				return
			}

			c.mx.Lock()
			c.knownNodes[hex.EncodeToString(node.Key)] = conn
			c.mx.Unlock()

			ch <- true
		}(node)
	}

	select {
	case <-ch:
	case <-time.After(connectTimeout):
	}

	if len(c.knownNodes) == 0 {
		return nil, fmt.Errorf("no available nodes in the given list %v", nodes)
	}

	return c, nil
}

const _K = 14 // TODO: calculate and extend

func (c *Client) addNode(ctx context.Context, node *Node) (_ *adnl.ADNL, id string, err error) {
	pub, ok := node.ID.(adnl.PublicKeyED25519)
	if !ok {
		return nil, "", fmt.Errorf("unsupported id type %s", reflect.TypeOf(node.ID).String())
	}

	keyID := hex.EncodeToString(pub.Key)

	c.mx.RLock()
	conn := c.knownNodes[keyID]
	c.mx.RUnlock()

	if conn != nil {
		return conn, keyID, nil
	}

	// connect to first available address of node
	for _, udp := range node.AddrList.Addresses {
		addr := udp.IP.String() + ":" + fmt.Sprint(udp.Port)
		conn, err = c.connect(ctx, addr, pub.Key)
		if err != nil {
			// failed to connect, we will try next addr
			continue
		}

		c.mx.Lock()
		c.knownNodes[keyID] = conn
		c.mx.Unlock()

		// connected successfully
		break
	}

	if err != nil {
		return nil, "", fmt.Errorf("failed to connect to node: %w", err)
	}

	return conn, keyID, nil
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

	c.mx.RLock()
	plist := newPriorityList(c.knownNodes)
	c.mx.RUnlock()

	threadCtx, stopThreads := context.WithCancel(ctx)
	defer stopThreads()

	const threads = 8
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
				node, priority := plist.getNode()
				if node == nil {
					if atomic.LoadInt64(&numInProgress) > 0 {
						// something is pending
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

						var nodeID string
						node, nodeID, err = c.addNode(connectCtx, n)
						connectCancel()
						if err != nil {
							continue
						}

						// next nodes will have higher priority to query
						plist.addNode(nodeID, node, priority+1)
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

func (c *Client) FindValueRaw(ctx context.Context, node *adnl.ADNL, id []byte, K int32) (result any, err error) {
	var res any
	err = node.Query(ctx, FindValue{
		Key: id,
		K:   K,
	}, &res)
	if err != nil {
		return nil, fmt.Errorf("failed to do query to dht node: %w", err)
	}

	switch r := res.(type) {
	case ValueNotFoundResult:
		var verified []*Node
		for _, n := range r.Nodes.List {
			pub, ok := n.ID.(adnl.PublicKeyED25519)
			if !ok {
				Logger("unsupported id type %s", reflect.TypeOf(n.ID).String())
				continue
			}

			signature := n.Signature
			n.Signature = nil

			toVerify, err := tl.Serialize(n, true)
			if err != nil {
				Logger("failed to serialize node %s", reflect.TypeOf(n.ID).String())
				continue
			}
			if !ed25519.Verify(pub.Key, toVerify, signature) {
				Logger("bad signature for node %s", reflect.TypeOf(n.ID).String())
				continue
			}

			n.Signature = signature
			verified = append(verified, n)
		}

		return verified, nil
	case ValueFoundResult:
		pub, ok := r.Value.KeyDescription.ID.(adnl.PublicKeyED25519)
		if !ok {
			return nil, fmt.Errorf("unsupported value's key type: %s", reflect.ValueOf(r.Value.KeyDescription.ID).String())
		}

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

		idPub, err := adnl.ToKeyID(pub)
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
			signature := r.Value.Signature
			r.Value.Signature = nil
			dataToCheck, err := tl.Serialize(r.Value, true)
			if err != nil {
				return nil, fmt.Errorf("failed to serialize value: %w", err)
			}
			if !ed25519.Verify(pub.Key, dataToCheck, signature) {
				return nil, fmt.Errorf("value's signature not match key")
			}
		default:
			return nil, fmt.Errorf("update rule type %s is not supported yet", reflect.TypeOf(r.Value.KeyDescription.UpdateRule))
		}

		// check key signature
		signature := r.Value.KeyDescription.Signature
		r.Value.KeyDescription.Signature = nil
		dataToCheck, err := tl.Serialize(r.Value.KeyDescription, true)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize key description: %w", err)
		}
		if !ed25519.Verify(pub.Key, dataToCheck, signature) {
			return nil, fmt.Errorf("key description's signature not match key")
		}

		return &r.Value, nil
	}

	return nil, fmt.Errorf("failed to find value, unexpected response type %s", reflect.TypeOf(res).String())
}

func (c *Client) connect(ctx context.Context, addr string, key ed25519.PublicKey) (*adnl.ADNL, error) {
	a, err := adnl.NewADNL(key)
	if err != nil {
		return nil, err
	}

	err = a.Connect(ctx, addr)
	if err != nil {
		return nil, err
	}

	a.SetDisconnectHandler(c.disconnectHandler)

	return a, nil
}

func (c *Client) disconnectHandler(_ string, key ed25519.PublicKey) {
	c.mx.Lock()
	delete(c.knownNodes, hex.EncodeToString(key))
	c.mx.Unlock()
}

func (c *Client) pickBestNode(key []byte) (*adnl.ADNL, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key shuld be 256 bits")
	}

	// TODO: kademia like dht routing, calc best node
	for _, v := range c.knownNodes {
		return v, nil
	}

	return nil, fmt.Errorf("no dht nodes available")
}
