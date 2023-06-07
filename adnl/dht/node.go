package dht

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"math/rand"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/overlay"
	"github.com/xssnick/tonutils-go/tl"
)

const (
	_StateFail = iota
	_StateThrottle
	_StateActive
)

type dhtNode struct {
	id     []byte
	adnl   ADNL
	client *Client

	ping      int64
	addr      string
	serverKey ed25519.PublicKey

	onStateChange func(node *dhtNode, state int)
	currentState  int

	lastQueryAt int64

	mx sync.Mutex
}

func (c *Client) connectToNode(ctx context.Context, id []byte, addr string, serverKey ed25519.PublicKey, onStateChange func(node *dhtNode, state int)) (*dhtNode, error) {
	n := &dhtNode{
		id:            id,
		addr:          addr,
		serverKey:     serverKey,
		onStateChange: onStateChange,
		client:        c,
	}

	n.changeState(_StateThrottle)

	/*err := n.checkPing(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to ping node, err: %w", err)
	}*/

	return n, nil
}

func (n *dhtNode) Close() {
	n.mx.Lock()
	defer n.mx.Unlock()

	if n.adnl != nil {
		n.adnl.Close()
	}
}

func (n *dhtNode) changeState(state int) {
	n.mx.Lock()
	defer n.mx.Unlock()

	if state == _StateFail {
		// in case of fail - close connection
		if n.adnl != nil {
			n.adnl.Close()
			n.adnl = nil
		}
	}

	if n.currentState == state {
		return
	}
	n.currentState = state

	n.onStateChange(n, state)
}

func (n *dhtNode) getState() int {
	n.mx.Lock()
	defer n.mx.Unlock()
	return n.currentState
}

func (n *dhtNode) prepare() (ADNL, error) {
	n.mx.Lock()
	defer n.mx.Unlock()

	if n.adnl != nil {
		return n.adnl, nil
	}

	a, err := n.client.gateway.RegisterClient(n.addr, n.serverKey)
	if err != nil {
		return nil, err
	}
	a.SetDisconnectHandler(func(addr string, key ed25519.PublicKey) {
		n.changeState(_StateFail)
	})
	n.adnl = a

	return n.adnl, nil
}

func (n *dhtNode) findNodes(ctx context.Context, id []byte, K int32) (result []*Node, err error) {
	val, err := tl.Serialize(FindNode{
		Key: id,
		K:   K,
	}, true)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize dht query: %w", err)
	}

	var res any
	err = n.query(ctx, tl.Raw(val), &res)
	if err != nil {
		return nil, fmt.Errorf("failed to query dht node: %w", err)
	}

	switch r := res.(type) {
	case NodesList:
		return r.List, nil
	}

	return nil, fmt.Errorf("failed to find nodes, unexpected response type %s", reflect.TypeOf(res).String())
}

func (n *dhtNode) storeValue(ctx context.Context, id []byte, value *Value) error {
	if err := checkValue(id, value); err != nil {
		return fmt.Errorf("corrupted value: %w", err)
	}

	val, err := tl.Serialize(Store{
		Value: value,
	}, true)
	if err != nil {
		return fmt.Errorf("failed to serialize dht query: %w", err)
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

	var res any
	err = n.query(ctx, tl.Raw(val), &res)
	if err != nil {
		return nil, fmt.Errorf("failed to do query to dht node: %w", err)
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
	if len(k.Name) == 0 || len(k.Name) > 127 || k.Index < 0 || k.Index > 15 { // TODO: move to better place when dht store ready
		return fmt.Errorf("invalid dht key")
	}

	idKey, err := adnl.ToKeyID(k)
	if err != nil {
		return err
	}
	if !bytes.Equal(id, idKey) {
		return fmt.Errorf("unwanted key received")
	}

	idPub, err := adnl.ToKeyID(value.KeyDescription.ID)
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
		pub, ok := value.KeyDescription.ID.(adnl.PublicKeyED25519)
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

func (n *dhtNode) checkPing(ctx context.Context) error {
	ping := Ping{
		ID: int64(rand.Uint64()),
	}

	var res Pong
	err := n.query(ctx, ping, &res)
	if err != nil {
		n.changeState(_StateFail)
		return err
	}

	if res.ID != ping.ID {
		return fmt.Errorf("wrong pong id")
	}

	return nil
}

func (n *dhtNode) query(ctx context.Context, req, res tl.Serializable) error {
	a, err := n.prepare()
	if err != nil {
		return fmt.Errorf("failed to prepare dht node: %w", err)
	}

	t := time.Now()
	atomic.StoreInt64(&n.lastQueryAt, t.Unix())
	err = a.Query(ctx, req, res)
	if err != nil {
		return err
	}
	ping := time.Since(t)
	atomic.StoreInt64(&n.ping, int64(ping))

	switch {
	case ping > queryTimeout/3:
		n.changeState(_StateThrottle)
	default:
		n.changeState(_StateActive)
	}

	return nil
}

func (n *dhtNode) weight(id []byte) int {
	n.mx.Lock()
	defer n.mx.Unlock()

	w := leadingZeroBits(xor(id, n.id))
	if n.currentState == _StateFail {
		w -= 3 // less priority for failed
		if w < 0 {
			w = 0
		}
	}
	ping := time.Duration(atomic.LoadInt64(&n.ping))
	return (1 << 30) + ((w << 20) - int(ping/(time.Millisecond*5)))
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

func leadingZeroBits(a []byte) int {
	for i, b := range a {
		switch {
		case b&0b10000000 != 0:
			return i*8 + 0
		case b&0b1000000 != 0:
			return i*8 + 1
		case b&0b100000 != 0:
			return i*8 + 2
		case b&0b10000 != 0:
			return i*8 + 3
		case b&0b1000 != 0:
			return i*8 + 4
		case b&0b100 != 0:
			return i*8 + 5
		case b&0b10 != 0:
			return i*8 + 6
		case b&0b1 != 0:
			return i*8 + 7
		}
	}
	return len(a) * 8
}
