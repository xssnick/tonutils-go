package dht

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/liteclient/adnl"
	"github.com/xssnick/tonutils-go/liteclient/adnl/address"
	"github.com/xssnick/tonutils-go/tl"
	"reflect"
	"sync"
	"time"
)

const queryTimeout = 500 * time.Millisecond

type Client struct {
	knownNodes   map[string]*adnl.ADNL
	queryTimeout time.Duration
	mx           sync.RWMutex
}

type NodeInfo struct {
	Address string
	Key     ed25519.PublicKey
}

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

const _K = 7 // TODO: calculate and extend

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

	signature := node.Signature
	node.Signature = nil
	toVerify, err := tl.Serialize(node, true)
	if err != nil {
		return nil, "", fmt.Errorf("ifailed to serialize given node")
	}
	if !ed25519.Verify(pub.Key, toVerify, signature) {
		return nil, "", fmt.Errorf("invalid signature for the given node")
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

	id, err := adnl.ToKeyID(Key{
		ID:    key,
		Name:  []byte("address"),
		Index: 0,
	})
	if err != nil {
		return nil, nil, err
	}

	node, err := c.pickBestNode(id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to pick best dht node: %w", err)
	}

	// TODO: try on diff nodes in case of fail
	val, err := c.findValue(ctx, node, id, map[string]bool{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to find value: %w", err)
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

func (c *Client) findValue(ctx context.Context, node *adnl.ADNL, id []byte, checked map[string]bool) (val *Value, err error) {
	cc, cancel := context.WithTimeout(ctx, queryTimeout)

	var res any
	err = node.Query(cc, FindValue{
		Key: id,
		K:   _K,
	}, &res)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("failed to do query to dht node: %w", err)
	}

	switch r := res.(type) {
	case ValueNotFoundResult:
		for _, n := range r.Nodes.List {
			cc, cancel = context.WithTimeout(ctx, queryTimeout)
			conn, keyID, err := c.addNode(cc, n)
			cancel()
			if err != nil {
				println("cant add", err.Error())
				continue
			}

			if checked[keyID] {
				continue
			}

			// mark as checked to not check again during this search
			checked[keyID] = true

			val, err = c.findValue(ctx, conn, id, checked)
			if err != nil {
				println("cant find", err.Error())

				// failed to find value, we will try next node
				continue
			}

			return val, nil
		}

		if err == nil {
			return nil, fmt.Errorf("value is not found with specified K")
		}
		return nil, fmt.Errorf("value is not found using specified node, err: %w", err)
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
