package dht

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
	"math/rand"
	"reflect"
	"sync"
	"time"
)

const (
	_StateFail = iota
	_StateThrottle
	_StateActive
)

type dhtNode struct {
	id   []byte
	adnl ADNL

	ping      time.Duration
	addr      string
	serverKey ed25519.PublicKey

	onStateChange func(node *dhtNode, state int)
	currentState  int
	initialized   bool

	closed    bool
	closer    chan bool
	closerCtx context.Context

	mx sync.Mutex
}

func connectToNode(ctx context.Context, id []byte, addr string, serverKey ed25519.PublicKey, onStateChange func(node *dhtNode, state int)) (*dhtNode, error) {
	n := &dhtNode{
		id:            id,
		addr:          addr,
		serverKey:     serverKey,
		onStateChange: onStateChange,
		closer:        make(chan bool, 1),
	}

	var cancel func()
	n.closerCtx, cancel = context.WithCancel(context.Background())
	go func() {
		<-n.closer
		cancel()
	}()

	if err := n.connect(ctx); err != nil {
		n.Close()
		return nil, err
	}

	go n.poll()

	return n, nil
}

func (n *dhtNode) Close() {
	n.mx.Lock()
	defer n.mx.Unlock()

	if n.closed {
		return
	}

	close(n.closer)
	n.closed = true

	if n.adnl != nil {
		n.adnl.Close()
	}
}

func (n *dhtNode) changeState(state int) {
	n.mx.Lock()
	defer n.mx.Unlock()

	if n.currentState == state {
		return
	}
	n.currentState = state

	if state == _StateFail {
		// in case of fail - close connection
		if n.adnl != nil {
			n.adnl.Close()
		}
		n.adnl = nil

		// try to reconnect only if it was active before
		if n.initialized {
			go func() {
				wait := 150 * time.Millisecond
				for {
					select {
					case <-n.closer:
						return
					case <-time.After(wait):
					}

					ctx, cancel := context.WithTimeout(n.closerCtx, queryTimeout)
					err := n.connect(ctx)
					cancel()
					if err == nil {
						return
					}

					if wait *= 2; wait > 10*time.Second {
						wait = 10 * time.Second
					}
				}
			}()
		}
	} else {
		n.initialized = true
	}

	n.onStateChange(n, state)
}

func (n *dhtNode) connect(ctx context.Context) error {
	a, err := connect(n.closerCtx, n.addr, n.serverKey, nil)
	if err != nil {
		return err
	}

	a.SetDisconnectHandler(func(addr string, key ed25519.PublicKey) {
		n.changeState(_StateFail)
	})

	n.mx.Lock()
	n.adnl = a
	n.mx.Unlock()

	err = n.checkPing(ctx)
	if err != nil {
		n.mx.Lock()
		n.adnl = nil
		n.mx.Unlock()

		a.Close()

		return fmt.Errorf("failed to ping node, err: %w", err)
	}

	return nil
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

		signature := value.Signature
		value.Signature = nil
		dataToCheck, err := tl.Serialize(value, true)
		value.Signature = signature
		if err != nil {
			return fmt.Errorf("failed to serialize value: %w", err)
		}
		if !ed25519.Verify(pub.Key, dataToCheck, signature) {
			return fmt.Errorf("value's signature not match key")
		}

		// check key signature
		signature = value.KeyDescription.Signature
		value.KeyDescription.Signature = nil
		dataToCheck, err = tl.Serialize(value.KeyDescription, true)
		value.KeyDescription.Signature = signature
		if err != nil {
			return fmt.Errorf("failed to serialize key description: %w", err)
		}
		if !ed25519.Verify(pub.Key, dataToCheck, signature) {
			return fmt.Errorf("key description's signature not match key")
		}

	// case UpdateRuleOverlayNodes:
	// TODO: check sign
	/*pub, ok := r.Value.Data
	if !ok {
		return nil, fmt.Errorf("unsupported value's key type: %s", reflect.ValueOf(r.Value.KeyDescription.ID).String())
	}*/
	default:
		return fmt.Errorf("update rule type %s is not supported yet", reflect.TypeOf(value.KeyDescription.UpdateRule))
	}
	return nil
}

func (n *dhtNode) checkPing(ctx context.Context) error {
	ping := Ping{
		ID: int64(rand.Uint64()),
	}

	t := time.Now()

	var res Pong
	err := n.query(ctx, ping, &res)
	if err != nil {
		n.changeState(_StateFail)
		return err
	}

	if res.ID != ping.ID {
		n.changeState(_StateFail)
		return fmt.Errorf("wrong pong id")
	}
	n.ping = time.Since(t)

	switch {
	case n.ping > queryTimeout/2:
		n.changeState(_StateThrottle)
	default:
		n.changeState(_StateActive)
	}

	return nil
}

func (n *dhtNode) query(ctx context.Context, req, res tl.Serializable) error {
	n.mx.Lock()
	a := n.adnl
	n.mx.Unlock()

	if a == nil {
		return fmt.Errorf("adnl connection is not active")
	}

	err := a.Query(ctx, req, res)
	if err != nil {
		return err
	}

	return nil
}

func (n *dhtNode) poll() {
	for {
		select {
		case <-n.closer:
			return
		case <-time.After(5 * time.Second):
		}

		ctx, cancel := context.WithTimeout(n.closerCtx, queryTimeout)
		err := n.checkPing(ctx)
		cancel()
		if err != nil {
			continue
		}
	}
}

func (n *dhtNode) weight(id []byte) int {
	w := leadingZeroBits(xor(id, n.id))
	return (w << 20) + int(n.ping/(time.Millisecond*50))
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
