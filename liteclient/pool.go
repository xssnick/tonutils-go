package liteclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	mRand "math/rand"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xssnick/tonutils-go/tl"
)

const _StickyCtxKey = "_ton_node_sticky"
const _StickyCtxUsedNodesKey = "_ton_used_nodes_sticky"

var (
	ErrNoActiveConnections = errors.New("no active connections")
	ErrADNLReqTimeout      = errors.New("adnl request timeout")
	ErrNoNodesLeft         = errors.New("no more active nodes left")
)

type OnDisconnectCallback func(addr, key string)

type ADNLResponse struct {
	Data tl.Serializable
}

type ADNLRequest struct {
	QueryID  []byte
	Data     any
	RespChan chan *ADNLResponse
}

type ConnectionPool struct {
	activeReqs  map[string]*ADNLRequest
	activeNodes []*connection
	reqMx       sync.RWMutex
	nodesMx     sync.RWMutex

	onDisconnect     func(addr, key string)
	roundRobinOffset uint64

	authKey ed25519.PrivateKey

	globalCtx context.Context
	stop      func()
}

// NewConnectionPool - ordinary pool to query liteserver
func NewConnectionPool() *ConnectionPool {
	c := &ConnectionPool{
		activeReqs: map[string]*ADNLRequest{},
	}

	// default reconnect policy
	c.SetOnDisconnect(c.DefaultReconnect(3*time.Second, -1))
	c.globalCtx, c.stop = context.WithCancel(context.Background())

	return c
}

// NewConnectionPoolWithAuth - will do TCP authorization after connection, can be used to communicate with storage-daemon
func NewConnectionPoolWithAuth(key ed25519.PrivateKey) *ConnectionPool {
	p := NewConnectionPool()
	p.authKey = key
	return p
}

// StickyContext - bounds all requests with this context to the same lite-server node, if possible.
// This is useful when we are doing some requests which depends on each other.
//
// For example: we requested MasterchainInfo from node A,
// and we are trying to request account with this block info from node B
// but node B don't have yet information about this block, so it can return error.
// If we use StickyContext, all requests where its passed will be routed to same node.
//
// In case if sticky node goes down, default balancer will be used as fallback
func (c *ConnectionPool) StickyContext(ctx context.Context) context.Context {
	if ctx.Value(_StickyCtxKey) != nil {
		return ctx
	}

	var id uint32

	c.nodesMx.RLock()
	if len(c.activeNodes) > 0 {
		// pick random one
		id = c.activeNodes[mRand.Uint32()%uint32(len(c.activeNodes))].id
	}
	c.nodesMx.RUnlock()

	return context.WithValue(ctx, _StickyCtxKey, id)
}

// StickyContextNextNode - select next node in the available list (pseudo random)
func (c *ConnectionPool) StickyContextNextNode(ctx context.Context) (context.Context, error) {
	nodeID, _ := ctx.Value(_StickyCtxKey).(uint32)
	usedNodes, _ := ctx.Value(_StickyCtxUsedNodesKey).([]uint32)
	if nodeID > 0 {
		usedNodes = append(usedNodes, nodeID)
	}

	c.nodesMx.RLock()
	defer c.nodesMx.RUnlock()

iter:
	for _, node := range c.activeNodes {
		for _, usedNode := range usedNodes {
			if usedNode == node.id {
				continue iter
			}
		}

		return context.WithValue(context.WithValue(ctx, _StickyCtxKey, node.id), _StickyCtxUsedNodesKey, usedNodes), nil
	}

	return ctx, ErrNoNodesLeft
}

// StickyContextNextNodeBalanced - select next node based on its weight and availability
func (c *ConnectionPool) StickyContextNextNodeBalanced(ctx context.Context) (context.Context, error) {
	nodeID, _ := ctx.Value(_StickyCtxKey).(uint32)
	usedNodes, _ := ctx.Value(_StickyCtxUsedNodesKey).([]uint32)
	if nodeID > 0 {
		usedNodes = append(usedNodes, nodeID)
	}

	c.nodesMx.RLock()
	defer c.nodesMx.RUnlock()

	var reqNode *connection

iter:
	for _, node := range c.activeNodes {
		for _, usedNode := range usedNodes {
			if usedNode == node.id {
				continue iter
			}
		}

		if reqNode == nil {
			reqNode = node
			continue
		}

		// select best node on this moment
		nw, old := atomic.LoadInt64(&node.weight), atomic.LoadInt64(&reqNode.weight)
		if nw > old || (nw == old && atomic.LoadInt64(&node.lastRespTime) < atomic.LoadInt64(&reqNode.lastRespTime)) {
			reqNode = node
		}
	}

	if reqNode != nil {
		return context.WithValue(context.WithValue(ctx, _StickyCtxKey, reqNode.id), _StickyCtxUsedNodesKey, usedNodes), nil
	}

	return ctx, ErrNoNodesLeft
}

func (c *ConnectionPool) StickyContextWithNodeID(ctx context.Context, nodeId uint32) context.Context {
	return context.WithValue(ctx, _StickyCtxKey, nodeId)
}

func (c *ConnectionPool) StickyNodeID(ctx context.Context) uint32 {
	nodeID, _ := ctx.Value(_StickyCtxKey).(uint32)
	return nodeID
}

func (c *ConnectionPool) Stop() {
	c.stop()

	c.nodesMx.RLock()
	defer c.nodesMx.RUnlock()

	for _, node := range c.activeNodes {
		_ = node.tcp.Close()
	}
}

// QueryLiteserver - sends request to liteserver
func (c *ConnectionPool) QueryLiteserver(ctx context.Context, request tl.Serializable, result tl.Serializable) error {
	return c.QueryADNL(ctx, LiteServerQuery{Data: request}, result)
}

// QueryADNL - sends ADNL request to peer
func (c *ConnectionPool) QueryADNL(ctx context.Context, request tl.Serializable, result tl.Serializable) error {
	id := make([]byte, 32)
	_, err := io.ReadFull(rand.Reader, id)
	if err != nil {
		return err
	}

	_, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		// fallback timeout to not stuck forever with background context
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
	}

	// buffered channel to not block listener
	ch := make(chan *ADNLResponse, 1)
	req := &ADNLRequest{
		QueryID:  id,
		Data:     request,
		RespChan: ch,
	}

	strId := string(id)

	c.reqMx.Lock()
	c.activeReqs[strId] = req
	c.reqMx.Unlock()

	defer func() {
		c.reqMx.Lock()
		delete(c.activeReqs, strId)
		c.reqMx.Unlock()
	}()

	tm := time.Now()

	var node *connection
	if nodeID, ok := ctx.Value(_StickyCtxKey).(uint32); ok && nodeID > 0 {
		node, err = c.querySticky(nodeID, req)
		if err != nil {
			return err
		}
	} else {
		node, err = c.queryWithSmartBalancer(req)
		if err != nil {
			return err
		}
	}

	// wait for response
	select {
	case resp := <-ch:
		atomic.AddInt64(&node.weight, 1)
		atomic.StoreInt64(&node.lastRespTime, int64(time.Since(tm)))

		reflect.ValueOf(result).Elem().Set(reflect.ValueOf(resp.Data))
		return nil
	case <-ctx.Done():
		if time.Since(tm) < 200*time.Millisecond {
			// consider it as too short timeout to punish node
			atomic.AddInt64(&node.weight, 1)
		}

		if !hasDeadline {
			return fmt.Errorf("%w, node %s", ErrADNLReqTimeout, node.addr)
		}

		return fmt.Errorf("deadline exceeded, node %s, err: %w", node.addr, ctx.Err())
	}
}

func (c *ConnectionPool) querySticky(id uint32, req *ADNLRequest) (*connection, error) {
	c.nodesMx.RLock()
	for _, node := range c.activeNodes {
		if node.id == id {
			atomic.AddInt64(&node.weight, -1)
			_, err := node.queryAdnl(req.QueryID, req.Data)
			if err == nil {
				c.nodesMx.RUnlock()
				return node, nil
			}
			break
		}
	}
	c.nodesMx.RUnlock()

	// fallback if bounded node is not available
	return c.queryWithSmartBalancer(req)
}

func (c *ConnectionPool) queryWithSmartBalancer(req *ADNLRequest) (*connection, error) {
	var reqNode *connection

	c.nodesMx.RLock()
	for _, node := range c.activeNodes {
		if reqNode == nil {
			reqNode = node
			continue
		}

		nw, old := atomic.LoadInt64(&node.weight), atomic.LoadInt64(&reqNode.weight)
		if nw > old || (nw == old && atomic.LoadInt64(&node.lastRespTime) < atomic.LoadInt64(&reqNode.lastRespTime)) {
			reqNode = node
		}
	}
	c.nodesMx.RUnlock()

	if reqNode == nil {
		return nil, ErrNoActiveConnections
	}

	atomic.AddInt64(&reqNode.weight, -1)

	_, err := reqNode.queryAdnl(req.QueryID, req.Data)
	if err != nil {
		return nil, err
	}
	return reqNode, nil
}

func (c *ConnectionPool) SetOnDisconnect(cb OnDisconnectCallback) {
	c.reqMx.Lock()
	c.onDisconnect = cb
	c.reqMx.Unlock()
}
