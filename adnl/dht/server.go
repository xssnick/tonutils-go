package dht

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"sort"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

const storeDistanceThreshold = _K + 10

var StoreKeysLimit = 500000
var MaxValueSize = 768
var MaxKeyTTLSec int64 = 3600 * 12 // 12 hours

func (c *DHT) processQuery(peer adnl.Peer, queryID []byte, payload tl.Serializable) error {
	switch req := payload.(type) {
	case []tl.Serializable:
		if len(req) != 2 {
			return fmt.Errorf("bad query")
		}

		q, ok := req[0].(Query)
		if !ok {
			return fmt.Errorf("bad query prefix")
		}

		if err := q.Node.CheckSignature(); err != nil {
			return fmt.Errorf("dht: invalid node signature: %w", err)
		}
		_, _ = c.addNode(q.Node)

		return c.processQuery(peer, queryID, req[1])
	case Query:
		if err := req.Node.CheckSignature(); err != nil {
			return fmt.Errorf("dht: invalid node signature: %w", err)
		}
		_, _ = c.addNode(req.Node)

		return peer.Answer(context.Background(), queryID, true)
	case Ping:
		return peer.Answer(context.Background(), queryID, Pong{ID: req.ID})
	case FindNode:
		return peer.Answer(context.Background(), queryID, NodesList{List: c.collectNodes(req.Key, int(req.K))})
	case FindValue:
		value := c.lookupValue(req.Key)
		if value != nil {
			return peer.Answer(context.Background(), queryID, ValueFoundResult{Value: *value})
		}

		nodes := c.collectNodes(req.Key, int(req.K))
		return peer.Answer(context.Background(), queryID, ValueNotFoundResult{Nodes: NodesList{List: nodes}})
	case Store:
		keyID, err := tl.Hash(req.Value.KeyDescription.Key)
		if err != nil {
			return fmt.Errorf("dht: failed to compute key hash: %w", err)
		}

		if err = checkValue(keyID, req.Value); err != nil {
			// invalid values are silently ignored to mimic reference behavior
			return peer.Answer(context.Background(), queryID, Stored{})
		}

		now := time.Now().Unix()
		if int64(req.Value.TTL) > now+MaxKeyTTLSec {
			// too big key ttl
			return peer.Answer(context.Background(), queryID, Stored{})
		}
		if int64(req.Value.TTL) <= now {
			// expired key ttl
			return peer.Answer(context.Background(), queryID, Stored{})
		}

		if c.shouldStore(keyID) {
			c.storeValue(keyID, req.Value)
		}

		return peer.Answer(context.Background(), queryID, Stored{})
	case SignedAddressListQuery:
		node, err := c.selfNode()
		if err != nil {
			return err
		}
		return peer.Answer(context.Background(), queryID, *node)
	default:
		return fmt.Errorf("dht: unsupported query type %T", payload)
	}
}

func (c *DHT) storeValue(keyID []byte, value *Value) {
	cloned := cloneValue(value)

	c.valuesMx.Lock()
	defer c.valuesMx.Unlock()

	if len(c.values) >= StoreKeysLimit {
		return
	}

	entry := c.values[string(keyID)]
	if entry != nil && !shouldReplaceValue(entry, cloned) {
		return
	}

	c.values[string(keyID)] = cloned
}

func (c *DHT) lookupValue(key []byte) *Value {
	c.valuesMx.RLock()
	entry := c.values[string(key)]
	c.valuesMx.RUnlock()
	if entry == nil {
		return nil
	}

	if entry.TTL <= int32(time.Now().Unix()) {
		c.valuesMx.Lock()
		delete(c.values, string(key))
		c.valuesMx.Unlock()
		return nil
	}

	return entry
}

func (c *DHT) cleaner() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		select {
		case <-c.globalCtx.Done():
			return
		case <-tick.C:
		}

		c.valuesMx.Lock()
		now := time.Now().Unix()
		for s2, value := range c.values {
			if int64(value.TTL) <= now {
				delete(c.values, s2)
			}
		}
		c.valuesMx.Unlock()
	}
}

func (c *DHT) shouldStore(key []byte) bool {
	nodes, affinities := c.nearestNodes(key, storeDistanceThreshold)
	if len(nodes) < storeDistanceThreshold || len(affinities) == 0 {
		return true
	}

	selfAffinity := int(affinity(key, c.gateway.GetID()))
	return selfAffinity >= affinities[len(affinities)-1]
}

func (c *DHT) collectNodes(key []byte, limit int) []*Node {
	if limit <= 0 {
		return nil
	}

	details := c.nearestNodeAffinities(key, limit)
	if c.serverMode {
		self, err := c.selfNode()
		if err != nil {
			return nil
		}
		details = append(details, nodeAffinity{node: self, affinity: int(affinity(key, c.gateway.GetID()))})
	}

	sort.Slice(details, func(i, j int) bool {
		return details[i].affinity > details[j].affinity
	})

	if len(details) > limit {
		details = details[:limit]
	}

	result := make([]*Node, 0, len(details))
	seen := map[string]bool{}

	for _, item := range details {
		if item.node == nil {
			continue
		}

		keyHash, err := tl.Hash(item.node.ID)
		if err == nil {
			if seen[string(keyHash)] {
				continue
			}
			seen[string(keyHash)] = true
		}
		result = append(result, item.node)
	}

	return result
}

func (c *DHT) selfNode() (*Node, error) {
	ver := time.Now().Unix()
	cache := c.selfCached.Load()
	if cache != nil && int64(cache.Version)+3 >= ver {
		// 3 sec cache
		return cache, nil
	}

	list := c.gateway.GetAddressList()
	node := &Node{
		ID:       keys.PublicKeyED25519{Key: c.gateway.GetKey().Public().(ed25519.PublicKey)},
		AddrList: &list,
		Version:  int32(ver),
	}

	if err := signNode(node, c.gateway.GetKey()); err != nil {
		return nil, err
	}
	c.selfCached.Store(node)
	return node, nil
}

func signNode(node *Node, key ed25519.PrivateKey) error {
	signature := node.Signature
	node.Signature = nil
	data, err := tl.Serialize(node, true)
	if err != nil {
		node.Signature = signature
		return err
	}
	node.Signature = ed25519.Sign(key, data)
	return nil
}

func cloneValue(value *Value) *Value {
	if value == nil {
		return nil
	}

	cloned := *value
	cloned.Data = append([]byte(nil), value.Data...)
	cloned.Signature = append([]byte(nil), value.Signature...)
	cloned.KeyDescription = cloneKeyDescription(value.KeyDescription)
	return &cloned
}

func cloneKeyDescription(desc KeyDescription) KeyDescription {
	cloned := desc
	cloned.Key = cloneKey(desc.Key)
	cloned.Signature = append([]byte(nil), desc.Signature...)
	cloned.ID = clonePublicKey(desc.ID)
	return cloned
}

func cloneKey(key Key) Key {
	cloned := key
	cloned.ID = append([]byte(nil), key.ID...)
	cloned.Name = append([]byte(nil), key.Name...)
	return cloned
}

func clonePublicKey(key any) any {
	switch v := key.(type) {
	case keys.PublicKeyED25519:
		cp := make([]byte, len(v.Key))
		copy(cp, v.Key)
		return keys.PublicKeyED25519{Key: cp}
	case keys.PublicKeyAES:
		cp := make([]byte, len(v.Key))
		copy(cp, v.Key)
		return keys.PublicKeyAES{Key: cp}
	case keys.PublicKeyOverlay:
		cp := append([]byte(nil), v.Key...)
		return keys.PublicKeyOverlay{Key: cp}
	case keys.PublicKeyUnEnc:
		cp := append([]byte(nil), v.Key...)
		return keys.PublicKeyUnEnc{Key: cp}
	default:
		return key
	}
}

func shouldReplaceValue(current, next *Value) bool {
	if current == nil || next == nil {
		return false
	}

	switch current.KeyDescription.UpdateRule.(type) {
	case UpdateRuleAnybody:
		return true
	case UpdateRuleSignature:
		return next.TTL >= current.TTL
	case UpdateRuleOverlayNodes:
		return next.TTL >= current.TTL
	default:
		return next.TTL >= current.TTL
	}
}
