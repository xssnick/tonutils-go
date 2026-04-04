package dht

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"math/bits"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xssnick/tonutils-go/adnl/overlay"
	"github.com/xssnick/tonutils-go/tl"
)

const _MaxFailCount = 3
const (
	_pingIntervalDefault = 60 * time.Second
	_pingIntervalMax     = 4 * time.Hour
)

type dhtNode struct {
	adnlId []byte
	client *Client

	ping      int64
	addr      string
	serverKey ed25519.PublicKey
	version   int32
	node      *Node

	badScore int32

	inFlyQueries int32

	missedPings uint32
	lastPingAt  int64
	readyAt     int64
	failedFrom  int64
	pingEvery   int64

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

func (c *Client) initNode(id []byte, addr string, serverKey ed25519.PublicKey, version int32) *dhtNode {
	n := &dhtNode{
		adnlId:     id,
		addr:       addr,
		serverKey:  serverKey,
		client:     c,
		version:    version,
		failedFrom: time.Now().UnixNano(),
		pingEvery:  int64(_pingIntervalDefault),
	}
	return n
}

func (n *dhtNode) absorb(other *dhtNode) {
	if n == nil || other == nil {
		return
	}

	n.mx.Lock()
	defer n.mx.Unlock()

	n.addr = other.addr
	n.serverKey = append(ed25519.PublicKey(nil), other.serverKey...)
	n.version = other.version
	n.node = cloneNode(other.node)
}

func (n *dhtNode) asNode() *Node {
	return cloneNode(n.node)
}

func (n *dhtNode) findNodes(ctx context.Context, id []byte, K int32) (result []*Node, err error) {
	val, err := tl.Serialize(FindNode{
		Key: id,
		K:   K,
	}, true)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize dht query: %w", err)
	}
	val, err = n.client.applyQueryPrefix(val)
	if err != nil {
		return nil, fmt.Errorf("failed to wrap dht query: %w", err)
	}

	var res any
	err = n.query(ctx, tl.Raw(val), &res)
	if err != nil {
		return nil, fmt.Errorf("failed to query dht node: %w", err)
	}

	switch r := res.(type) {
	case NodesList:
		for _, node := range r.List {
			if err = node.validate(0, n.clientNetworkID()); err != nil {
				return nil, fmt.Errorf("untrusted nodes list response: %w", err)
			}
		}
		return r.List, nil
	}

	return nil, fmt.Errorf("failed to find nodes, unexpected response type %s", reflect.TypeOf(res).String())
}

func (n *dhtNode) getSignedAddressList(ctx context.Context) (*Node, error) {
	val, err := tl.Serialize(SignedAddressListQuery{}, true)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize dht signed address list query: %w", err)
	}
	val, err = n.client.applyQueryPrefix(val)
	if err != nil {
		return nil, fmt.Errorf("failed to wrap dht signed address list query: %w", err)
	}

	var res any
	err = n.query(ctx, tl.Raw(val), &res)
	if err != nil {
		return nil, fmt.Errorf("failed to query dht node: %w", err)
	}

	switch r := res.(type) {
	case Node:
		if err = r.validate(n.version, n.clientNetworkID()); err != nil {
			return nil, fmt.Errorf("untrusted signed address list response: %w", err)
		}
		return cloneNode(&r), nil
	}

	return nil, fmt.Errorf("failed to get signed address list, unexpected response type %s", reflect.TypeOf(res).String())
}

func (n *dhtNode) storeValue(ctx context.Context, id []byte, value *Value) error {
	if err := checkValueWithNetworkID(id, value, n.clientNetworkID()); err != nil {
		return fmt.Errorf("corrupted value: %w", err)
	}

	val, err := tl.Serialize(Store{
		Value: value,
	}, true)
	if err != nil {
		return fmt.Errorf("failed to serialize dht query: %w", err)
	}
	val, err = n.client.applyQueryPrefix(val)
	if err != nil {
		return fmt.Errorf("failed to wrap dht query: %w", err)
	}

	var res any
	err = n.query(ctx, tl.Raw(val), &res)
	if err != nil {
		return fmt.Errorf("failed to query dht node: %w", err)
	}

	switch res.(type) {
	case Stored:
		return nil
	}

	return fmt.Errorf("failed to find nodes, unexpected response type %s", reflect.TypeOf(res).String())
}

func (n *dhtNode) findValue(ctx context.Context, id []byte, K int32) (result any, err error) {
	val, err := tl.Serialize(FindValue{
		Key: id,
		K:   K,
	}, true)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize dht value: %w", err)
	}
	val, err = n.client.applyQueryPrefix(val)
	if err != nil {
		return nil, fmt.Errorf("failed to wrap dht value query: %w", err)
	}

	var res any
	err = n.query(ctx, tl.Raw(val), &res)
	if err != nil {
		return nil, fmt.Errorf("failed to do query to dht node: %w", err)
	}

	switch r := res.(type) {
	case ValueNotFoundResult:
		for _, node := range r.Nodes.List {
			err = node.validate(0, n.clientNetworkID())
			if err != nil {
				return nil, fmt.Errorf("untrusted nodes list response: %s", err.Error())
			}
		}
		return r.Nodes.List, nil
	case ValueFoundResult:
		if err = checkValueWithNetworkID(id, &r.Value, n.clientNetworkID()); err != nil {
			return nil, fmt.Errorf("corrupted value: %w", err)
		}
		if !isValueAcceptable(&r.Value) {
			return n.findNodes(ctx, id, K)
		}
		return &r.Value, nil
	}

	return nil, fmt.Errorf("failed to find value, unexpected response type %s", reflect.TypeOf(res).String())
}

func checkValue(id []byte, value *Value) error {
	return checkValueWithNetworkID(id, value, _UnknownNetworkID)
}

func checkValueWithNetworkID(id []byte, value *Value, ourNetworkID int32) error {
	if value == nil {
		return fmt.Errorf("nil value")
	}
	if len(value.Data) > _MaxValueSize {
		return fmt.Errorf("too big value")
	}

	k := value.KeyDescription.Key
	if len(k.Name) == 0 || len(k.Name) > 127 || k.Index < 0 || k.Index > 15 { // TODO: move to better place when dht store ready
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
		if len(value.Signature) > 0 {
			return fmt.Errorf("cannot have signature in DhtUpdateRuleAnybody")
		}
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
		if len(value.Signature) > 0 {
			return fmt.Errorf("cannot have signature in DhtUpdateRuleOverlayNodes")
		}
		if err = checkOverlayNodesValue(k.ID, value.Data, ourNetworkID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("update rule type %s is not supported yet", reflect.TypeOf(value.KeyDescription.UpdateRule))
	}
	return nil
}

func checkOverlayNodesValue(overlayID, data []byte, ourNetworkID int32) error {
	var nodes overlay.NodesList

	_, err := tl.Parse(&nodes, data, true)
	if err != nil {
		return fmt.Errorf("unsupported data for overlay nodes rule, err: %s", err.Error())
	}

	for i := range nodes.List {
		err = checkOverlayNode(&nodes.List[i], overlayID, ourNetworkID)
		if err != nil {
			return fmt.Errorf("untrusted overlay response: %s", err.Error())
		}
	}
	return nil
}

func checkOverlayNode(node *overlay.Node, overlayID []byte, ourNetworkID int32) error {
	if node == nil {
		return fmt.Errorf("nil overlay node")
	}
	if !bytes.Equal(node.Overlay, overlayID) {
		return fmt.Errorf("bad overlay id")
	}

	pub, ok := node.ID.(keys.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("unsupported id type %s", reflect.TypeOf(node.ID).String())
	}

	signature, err := splitNodeSignature(node.Signature, ourNetworkID)
	if err != nil {
		return err
	}

	id, err := tl.Hash(node.ID)
	if err != nil {
		return fmt.Errorf("failed to calc id: %w", err)
	}

	toVerify, err := tl.Serialize(overlay.NodeToSign{
		ID:      id,
		Overlay: node.Overlay,
		Version: node.Version,
	}, true)
	if err != nil {
		return fmt.Errorf("failed to serialize node: %w", err)
	}
	if !ed25519.Verify(pub.Key, toVerify, signature) {
		return fmt.Errorf("bad signature for node: %s", hex.EncodeToString(pub.Key))
	}
	return nil
}

func isValueAcceptable(value *Value) bool {
	switch value.KeyDescription.UpdateRule.(type) {
	case UpdateRuleOverlayNodes:
		var nodes overlay.NodesList

		if _, err := tl.Parse(&nodes, value.Data, true); err != nil {
			return false
		}

		now := time.Now().Unix()
		for _, node := range nodes.List {
			if int64(node.Version)+600 > now {
				return true
			}
		}
		return false
	default:
		return true
	}
}

func (n *dhtNode) clientNetworkID() int32 {
	if n == nil || n.client == nil {
		return _UnknownNetworkID
	}
	return n.client.networkID
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

func (n *dhtNode) isReady() bool {
	if n == nil {
		return false
	}
	return atomic.LoadInt64(&n.readyAt) > 0
}

func (n *dhtNode) failedAt() int64 {
	if n == nil {
		return 0
	}
	return atomic.LoadInt64(&n.failedFrom)
}

func (n *dhtNode) shouldPing(now time.Time) bool {
	if n == nil {
		return false
	}
	last := atomic.LoadInt64(&n.lastPingAt)
	every := time.Duration(atomic.LoadInt64(&n.pingEvery))
	if every <= 0 {
		every = _pingIntervalDefault
	}
	if last == 0 {
		return true
	}
	return now.Sub(time.Unix(0, last)) >= every
}

func (n *dhtNode) markPingAttempt(now time.Time) {
	if n == nil {
		return
	}
	atomic.StoreInt64(&n.lastPingAt, now.UnixNano())
}

func (n *dhtNode) markPingSuccess() {
	if n == nil {
		return
	}
	now := time.Now().UnixNano()
	atomic.StoreUint32(&n.missedPings, 0)
	atomic.StoreInt64(&n.lastPingAt, now)
	atomic.StoreInt64(&n.pingEvery, int64(_pingIntervalDefault))
	if atomic.LoadInt64(&n.readyAt) == 0 {
		atomic.StoreInt64(&n.readyAt, now)
	}
}

func (n *dhtNode) markPingFailure() {
	if n == nil {
		return
	}
	missed := atomic.AddUint32(&n.missedPings, 1)
	if missed <= _MaxFailCount {
		return
	}

	cur := time.Duration(atomic.LoadInt64(&n.pingEvery))
	if cur <= 0 {
		cur = _pingIntervalDefault
	}
	next := time.Duration(float64(cur) * 1.1)
	if next > _pingIntervalMax {
		next = _pingIntervalMax
	}
	atomic.StoreInt64(&n.pingEvery, int64(next))
	atomic.StoreInt64(&n.readyAt, 0)
	atomic.StoreInt64(&n.failedFrom, time.Now().UnixNano())
}

func (c *Client) applyQueryPrefix(payload []byte) ([]byte, error) {
	if c == nil || c.queryPrefix == nil {
		return payload, nil
	}

	prefix, err := c.queryPrefix()
	if err != nil {
		return nil, err
	}
	return append(prefix, payload...), nil
}

func (n *dhtNode) updateStatus(isGood bool) {
	if isGood {
		atomic.StoreInt32(&n.badScore, 0)
		return
	}

	badScore := atomic.LoadInt32(&n.badScore)
	if badScore <= _MaxFailCount {
		atomic.AddInt32(&n.badScore, 1)
	}
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
