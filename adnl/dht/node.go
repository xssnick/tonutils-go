package dht

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"math/bits"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xssnick/tonutils-go/adnl/overlay"
	"github.com/xssnick/tonutils-go/tl"
)

const _MaxFailCount = 3

type dhtNode struct {
	adnlId []byte
	client *DHT

	ping      int64
	addr      string
	serverKey ed25519.PublicKey

	node *Node

	currentState int
	badScore     int32

	inFlyQueries int32

	mx sync.Mutex
}

type dhtNodeList []*dhtNode

func (l dhtNodeList) Len() int {
	return len(l)
}

func (l dhtNodeList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

func (l dhtNodeList) Less(i, j int) bool {
	if l[i] == nil || l[j] == nil {
		return false
	}
	iScore := atomic.LoadInt32(&l[i].badScore)
	jScore := atomic.LoadInt32(&l[j].badScore)

	if iScore != jScore {
		return iScore < jScore
	}
	return atomic.LoadInt64(&l[i].ping) < atomic.LoadInt64(&l[j].ping)
}

func (c *DHT) initNode(id []byte, addr string, serverKey ed25519.PublicKey, node *Node) *dhtNode {
	n := &dhtNode{
		adnlId:    id,
		addr:      addr,
		serverKey: serverKey,
		client:    c,
	}
	if node != nil {
		n.setNode(node)
	}
	return n
}

func (n *dhtNode) setNode(node *Node) {
	if node == nil {
		return
	}

	clone := cloneNode(node)
	n.mx.Lock()
	n.node = clone
	n.mx.Unlock()
}

func (n *dhtNode) updateEndpoint(addr string, key ed25519.PublicKey) {
	n.mx.Lock()
	if addr != "" {
		n.addr = addr
	}
	if key != nil {
		n.serverKey = key
	}
	n.mx.Unlock()
}

func (n *dhtNode) getNode() *Node {
	n.mx.Lock()
	defer n.mx.Unlock()
	if n.node == nil {
		return nil
	}
	return cloneNode(n.node)
}

func (n *dhtNode) findNodes(ctx context.Context, id []byte, K int32) (result []*Node, err error) {
	var query tl.Serializable = FindNode{
		Key: id,
		K:   K,
	}

	if n.client.serverMode {
		self, err := n.client.selfNode()
		if err != nil {
			return nil, fmt.Errorf("failed to get self node: %w", err)
		}
		query = []tl.Serializable{Query{Node: self}, query}
	}

	var res any
	err = n.query(ctx, query, &res)
	if err != nil {
		return nil, fmt.Errorf("failed to query dht node: %w", err)
	}

	switch r := res.(type) {
	case NodesList:
		return r.List, nil
	}

	return nil, fmt.Errorf("failed to find nodes, unexpected response type %s", reflect.TypeOf(res).String())
}

func (n *dhtNode) doPing(ctx context.Context) (err error) {
	var query tl.Serializable = Ping{
		ID: time.Now().Unix(),
	}

	if n.client.serverMode {
		self, err := n.client.selfNode()
		if err != nil {
			return fmt.Errorf("failed to get self node: %w", err)
		}
		query = []tl.Serializable{Query{Node: self}, query}
	}

	var res any
	err = n.query(ctx, query, &res)
	if err != nil {
		return fmt.Errorf("failed to ping dht node: %w", err)
	}

	switch res.(type) {
	case Pong:
		return nil
	}
	return fmt.Errorf("failed to ping node, unexpected response type %s", reflect.TypeOf(res).String())
}

func (n *dhtNode) storeValue(ctx context.Context, id []byte, value *Value) error {
	if err := checkValue(id, value); err != nil {
		return fmt.Errorf("corrupted value: %w", err)
	}

	var query tl.Serializable = Store{
		Value: value,
	}

	if n.client.serverMode {
		self, err := n.client.selfNode()
		if err != nil {
			return fmt.Errorf("failed to get self node: %w", err)
		}
		query = []tl.Serializable{Query{Node: self}, query}
	}

	var res any
	if err := n.query(ctx, query, &res); err != nil {
		return fmt.Errorf("failed to query dht node: %w", err)
	}

	switch res.(type) {
	case Stored:
		return nil
	}

	return fmt.Errorf("failed to find nodes, unexpected response type %s", reflect.TypeOf(res).String())
}

func (n *dhtNode) findValue(ctx context.Context, id []byte, K int32) (result any, err error) {
	var query tl.Serializable = FindValue{
		Key: id,
		K:   K,
	}

	if n.client.serverMode {
		self, err := n.client.selfNode()
		if err != nil {
			return nil, fmt.Errorf("failed to get self node: %w", err)
		}
		query = []tl.Serializable{Query{Node: self}, query}
	}

	var res any
	if err := n.query(ctx, query, &res); err != nil {
		return nil, fmt.Errorf("failed to query dht node: %w", err)
	}

	switch r := res.(type) {
	case ValueNotFoundResult:
		for _, node := range r.Nodes.List {
			err = node.CheckSignature()
			if err != nil {
				return nil, fmt.Errorf("untrusted nodes list response: %s", err.Error())
			}
		}
		return r.Nodes.List, nil
	case ValueFoundResult:
		if err = checkValue(id, &r.Value); err != nil {
			return nil, fmt.Errorf("corrupted value: %w", err)
		}
		return &r.Value, nil
	}

	return nil, fmt.Errorf("failed to find value, unexpected response type %s", reflect.TypeOf(res).String())
}

func checkValue(id []byte, value *Value) error {
	k := value.KeyDescription.Key
	if len(k.Name) == 0 || len(k.Name) > 127 ||
		k.Index < 0 || k.Index > 15 ||
		len(value.Data) > MaxValueSize {
		return fmt.Errorf("invalid dht key")
	}

	idKey, err := tl.Hash(k)
	if err != nil {
		return err
	}
	if !bytes.Equal(id, idKey) {
		return fmt.Errorf("unwanted key received")
	}

	idPub, err := tl.Hash(value.KeyDescription.ID)
	if err != nil {
		return err
	}

	if !bytes.Equal(k.ID, idPub) {
		return fmt.Errorf("inconsistent dht key description")
	}

	switch value.KeyDescription.UpdateRule.(type) {
	case UpdateRuleAnybody:
		// no checks
	case UpdateRuleSignature:
		pub, ok := value.KeyDescription.ID.(keys.PublicKeyED25519)
		if !ok {
			return fmt.Errorf("unsupported value's key type: %s", reflect.ValueOf(value.KeyDescription.ID).String())
		}

		// we need it to safely check data without touching original fields
		valueCopy := *value
		valueCopy.Signature = nil

		signature := value.Signature
		dataToCheck, err := tl.Serialize(valueCopy, true)
		if err != nil {
			return fmt.Errorf("failed to serialize value: %w", err)
		}
		if !ed25519.Verify(pub.Key, dataToCheck, signature) {
			return fmt.Errorf("value's signature not match key")
		}

		// check key signature
		signature = value.KeyDescription.Signature
		valueCopy.KeyDescription.Signature = nil
		dataToCheck, err = tl.Serialize(valueCopy.KeyDescription, true)
		if err != nil {
			return fmt.Errorf("failed to serialize key description: %w", err)
		}
		if !ed25519.Verify(pub.Key, dataToCheck, signature) {
			return fmt.Errorf("key description's signature not match key")
		}

	case UpdateRuleOverlayNodes:
		var nodes overlay.NodesList

		_, err = tl.Parse(&nodes, value.Data, true)
		if err != nil {
			return fmt.Errorf("unsupported data for overlay nodes rule, err: %s", err.Error())
		}

		for _, node := range nodes.List {
			err = node.CheckSignature()
			if err != nil {
				return fmt.Errorf("untrusted overlay response: %s", err.Error())
			}
		}
		return nil
	default:
		return fmt.Errorf("update rule type %s is not supported yet", reflect.TypeOf(value.KeyDescription.UpdateRule))
	}
	return nil
}

func (n *dhtNode) query(ctx context.Context, req, res tl.Serializable) error {
	if err := ctx.Err(); err != nil {
		// if context is canceled we are not trying to query
		return err
	}

	atomic.AddInt32(&n.inFlyQueries, 1)

	peer, err := n.client.gateway.RegisterClient(n.addr, n.serverKey)
	if err != nil {
		atomic.AddInt32(&n.inFlyQueries, -1)
		return err
	}

	defer func() {
		if atomic.AddInt32(&n.inFlyQueries, -1) == 0 && n.badScore > 1 {
			peer.Reinit()
		}
	}()

	t := time.Now()
	reportLimit := t.Add(queryTimeout - 500*time.Millisecond)
	err = peer.Query(ctx, req, res)
	if err != nil {
		if time.Now().After(reportLimit) {
			// to not report good nodes, because of our short deadline
			n.updateStatus(false)
		}
		return err
	}
	ping := time.Since(t)
	atomic.StoreInt64(&n.ping, int64(ping))

	n.updateStatus(true)

	return nil
}

func (n *dhtNode) updateStatus(isGood bool) {
	if isGood {
		atomic.StoreInt32(&n.badScore, 0)
		Logger("Make DHT peer", n.id(), "feel good")
		return
	}

	badScore := atomic.LoadInt32(&n.badScore)
	if badScore <= _MaxFailCount {
		badScore = atomic.AddInt32(&n.badScore, 1)
	}
	Logger("Make DHT peer", n.id(), "feel bad", badScore)
}

func (n *dhtNode) id() string {
	return hex.EncodeToString(n.adnlId)
}

func affinity(x, y []byte) uint {
	var result = uint(0)
	for i := 0; i < 32; i++ {
		k := x[i] ^ y[i]
		result += uint(bits.LeadingZeros8(k))
		if k != 0 {
			break
		}
	}
	return result
}

func cloneNode(node *Node) *Node {
	if node == nil {
		return nil
	}

	clone := *node

	switch id := node.ID.(type) {
	case keys.PublicKeyED25519:
		cp := make([]byte, len(id.Key))
		copy(cp, id.Key)
		clone.ID = keys.PublicKeyED25519{Key: cp}
	case keys.PublicKeyAES:
		cp := make([]byte, len(id.Key))
		copy(cp, id.Key)
		clone.ID = keys.PublicKeyAES{Key: cp}
	}

	if node.AddrList != nil {
		addrClone := *node.AddrList
		if len(node.AddrList.Addresses) > 0 {
			addrClone.Addresses = make([]*address.UDP, len(node.AddrList.Addresses))
			for i, addr := range node.AddrList.Addresses {
				if addr == nil {
					continue
				}
				udpClone := *addr
				if addr.IP != nil {
					ip := make(net.IP, len(addr.IP))
					copy(ip, addr.IP)
					udpClone.IP = ip
				}
				addrClone.Addresses[i] = &udpClone
			}
		}
		clone.AddrList = &addrClone
	}

	if len(node.Signature) > 0 {
		clone.Signature = append([]byte(nil), node.Signature...)
	}

	return &clone
}
