package dht

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

const storeDistanceThreshold = _K + 10

type Server struct {
	gateway *adnl.Gateway
	client  *Client
	key     ed25519.PrivateKey
	selfID  []byte

	values   map[string]*storedValue
	valuesMx sync.RWMutex
}

type storedValue struct {
	Value Value
}

func NewServer(gateway *adnl.Gateway, key ed25519.PrivateKey, staticNodes []*Node) (*Server, error) {
	if gateway == nil {
		return nil, fmt.Errorf("gateway is required")
	}
	if key == nil {
		return nil, fmt.Errorf("private key is required")
	}

	client, err := NewClient(gateway, staticNodes)
	if err != nil {
		return nil, err
	}

	selfID, err := tl.Hash(keys.PublicKeyED25519{Key: key.Public().(ed25519.PublicKey)})
	if err != nil {
		return nil, err
	}

	srv := &Server{
		gateway: gateway,
		client:  client,
		key:     key,
		selfID:  selfID,
		values:  map[string]*storedValue{},
	}

	gateway.SetConnectionHandler(func(peer adnl.Peer) error {
		peer.SetQueryHandler(func(msg *adnl.MessageQuery) error {
			return srv.handleQuery(peer, msg)
		})
		return nil
	})

	return srv, nil
}

func (s *Server) handleQuery(peer adnl.Peer, msg *adnl.MessageQuery) error {
	flattened := make([]tl.Serializable, 0, 2)
	flattenSerializable(msg.Data, &flattened)

	var payload tl.Serializable
	hasQuery := false

	for _, item := range flattened {
		switch v := item.(type) {
		case Query:
			hasQuery = true
			s.acceptNode(v.Node)
		case *Query:
			hasQuery = true
			s.acceptNode(v.Node)
		default:
			payload = item
		}
	}

	if payload == nil {
		if hasQuery {
			return peer.Answer(context.Background(), msg.ID, true)
		}
		return fmt.Errorf("dht: empty query payload")
	}

	return s.executeQuery(peer, msg.ID, payload)
}

func (s *Server) executeQuery(peer adnl.Peer, queryID []byte, payload tl.Serializable) error {
	switch req := payload.(type) {
	case tl.Raw:
		var obj any
		if _, err := tl.Parse(&obj, req, true); err != nil {
			return fmt.Errorf("dht: failed to parse raw query: %w", err)
		}
		serializable, ok := obj.(tl.Serializable)
		if !ok {
			return fmt.Errorf("dht: unsupported raw payload type %T", obj)
		}
		return s.executeQuery(peer, queryID, serializable)
	case Ping:
		return peer.Answer(context.Background(), queryID, Pong{ID: req.ID})
	case *Ping:
		return peer.Answer(context.Background(), queryID, Pong{ID: req.ID})
	case FindNode:
		return s.answerFindNode(peer, queryID, req.Key, int(req.K))
	case *FindNode:
		return s.answerFindNode(peer, queryID, req.Key, int(req.K))
	case FindValue:
		return s.answerFindValue(peer, queryID, req.Key, int(req.K))
	case *FindValue:
		return s.answerFindValue(peer, queryID, req.Key, int(req.K))
	case Store:
		return s.answerStore(peer, queryID, req.Value)
	case *Store:
		return s.answerStore(peer, queryID, req.Value)
	case SignedAddressListQuery:
		return s.answerAddressList(peer, queryID)
	case *SignedAddressListQuery:
		return s.answerAddressList(peer, queryID)
	default:
		return fmt.Errorf("dht: unsupported query type %T", payload)
	}
}

func (s *Server) acceptNode(node *Node) {
	if node == nil {
		return
	}
	if err := node.CheckSignature(); err != nil {
		return
	}
	_, _ = s.client.addNode(node)
}

func (s *Server) answerAddressList(peer adnl.Peer, queryID []byte) error {
	node := s.selfNode()
	if node == nil {
		return fmt.Errorf("dht: server has no address list configured")
	}
	return peer.Answer(context.Background(), queryID, *node)
}

func (s *Server) answerFindNode(peer adnl.Peer, queryID []byte, key []byte, limit int) error {
	nodes := s.collectNodes(key, limit)
	return peer.Answer(context.Background(), queryID, NodesList{List: nodes})
}

func (s *Server) answerFindValue(peer adnl.Peer, queryID []byte, key []byte, limit int) error {
	value := s.lookupValue(key)
	if value != nil {
		return peer.Answer(context.Background(), queryID, ValueFoundResult{Value: *value})
	}

	nodes := s.collectNodes(key, limit)
	return peer.Answer(context.Background(), queryID, ValueNotFoundResult{Nodes: NodesList{List: nodes}})
}

func (s *Server) answerStore(peer adnl.Peer, queryID []byte, value *Value) error {
	if value == nil {
		return fmt.Errorf("dht: store value is empty")
	}

	keyID, err := tl.Hash(value.KeyDescription.Key)
	if err != nil {
		return fmt.Errorf("dht: failed to compute key hash: %w", err)
	}

	if err := checkValue(keyID, value); err != nil {
		// invalid values are silently ignored to mimic reference behaviour
		return peer.Answer(context.Background(), queryID, Stored{})
	}

	if value.TTL <= int32(time.Now().Unix()) {
		return peer.Answer(context.Background(), queryID, Stored{})
	}

	if s.shouldStore(keyID) {
		s.storeValue(keyID, value)
	}

	return peer.Answer(context.Background(), queryID, Stored{})
}

func (s *Server) storeValue(keyID []byte, value *Value) {
	cloned := cloneValue(value)

	s.valuesMx.Lock()
	defer s.valuesMx.Unlock()

	entry := s.values[string(keyID)]
	if entry != nil {
		if !shouldReplaceValue(&entry.Value, cloned) {
			return
		}
	}

	s.values[string(keyID)] = &storedValue{Value: *cloned}
}

func (s *Server) lookupValue(key []byte) *Value {
	s.valuesMx.RLock()
	entry := s.values[string(key)]
	s.valuesMx.RUnlock()
	if entry == nil {
		return nil
	}

	if entry.Value.TTL <= int32(time.Now().Unix()) {
		s.valuesMx.Lock()
		delete(s.values, string(key))
		s.valuesMx.Unlock()
		return nil
	}

	return cloneValue(&entry.Value)
}

func (s *Server) shouldStore(key []byte) bool {
	nodes, affinities := s.client.nearestNodes(key, storeDistanceThreshold)
	if len(nodes) < storeDistanceThreshold {
		return true
	}
	if len(affinities) == 0 {
		return true
	}
	selfAffinity := int(affinity(key, s.selfID))
	return selfAffinity >= affinities[len(affinities)-1]
}

func (s *Server) collectNodes(key []byte, limit int) []*Node {
	if limit <= 0 {
		return nil
	}

	details := s.client.nearestNodeAffinities(key, limit)
	if self := s.selfNode(); self != nil {
		details = append(details, nodeAffinity{node: self, affinity: int(affinity(key, s.selfID))})
	}

	sort.Slice(details, func(i, j int) bool {
		return details[i].affinity > details[j].affinity
	})

	if len(details) > limit {
		details = details[:limit]
	}

	result := make([]*Node, 0, len(details))
	seen := map[string]struct{}{}

	for _, item := range details {
		if item.node == nil {
			continue
		}
		keyHash, err := tl.Hash(item.node.ID)
		if err == nil {
			if _, ok := seen[string(keyHash)]; ok {
				continue
			}
			seen[string(keyHash)] = struct{}{}
		}
		result = append(result, item.node)
	}

	return result
}

func (s *Server) selfNode() *Node {
	list := s.gateway.GetAddressList()
	if len(list.Addresses) == 0 {
		return nil
	}

	node := &Node{
		ID:       keys.PublicKeyED25519{Key: s.key.Public().(ed25519.PublicKey)},
		AddrList: cloneAddressList(&list),
		Version:  int32(time.Now().Unix()),
	}

	if err := signNode(node, s.key); err != nil {
		return nil
	}

	return node
}

func flattenSerializable(data tl.Serializable, out *[]tl.Serializable) {
	switch v := data.(type) {
	case []tl.Serializable:
		for _, item := range v {
			flattenSerializable(item, out)
		}
	default:
		*out = append(*out, v)
	}
}

func cloneAddressList(list *address.List) *address.List {
	if list == nil {
		return nil
	}
	clone := *list
	if len(list.Addresses) > 0 {
		clone.Addresses = make([]*address.UDP, len(list.Addresses))
		for i, addr := range list.Addresses {
			if addr == nil {
				continue
			}
			udp := *addr
			if addr.IP != nil {
				ip := make(net.IP, len(addr.IP))
				copy(ip, addr.IP)
				udp.IP = ip
			}
			clone.Addresses[i] = &udp
		}
	}
	return &clone
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
