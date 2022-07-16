package liteclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	mRand "math/rand"
	"sync"
	"sync/atomic"
	"time"
)

const _StickyCtxKey = "_ton_node_sticky"

type OnDisconnectCallback func(addr, key string)

type LiteResponse struct {
	TypeID int32
	Data   []byte
	err    error
}

type LiteRequest struct {
	TypeID   int32
	QueryID  []byte
	Data     []byte
	RespChan chan *LiteResponse
}

type ConnectionPool struct {
	activeReqs  map[string]*LiteRequest
	activeNodes []*connection
	reqMx       sync.RWMutex
	nodesMx     sync.RWMutex

	onDisconnect     func(addr, key string)
	roundRobinOffset uint64
}

var ErrNoActiveConnections = errors.New("no active connections")

func NewConnectionPool() *ConnectionPool {
	c := &ConnectionPool{
		activeReqs: map[string]*LiteRequest{},
	}

	// default reconnect policy
	c.SetOnDisconnect(c.DefaultReconnect(3*time.Second, -1))

	return c
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
	var id uint32

	c.nodesMx.RLock()
	if len(c.activeNodes) > 0 {
		// pick random one
		id = c.activeNodes[mRand.Uint32()%uint32(len(c.activeNodes))].id
	}
	c.nodesMx.RUnlock()

	return context.WithValue(ctx, _StickyCtxKey, id)
}

// Do - builds and executes request to liteserver
func (c *ConnectionPool) Do(ctx context.Context, typeID int32, payload []byte) (*LiteResponse, error) {
	id := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, id); err != nil {
		return nil, err
	}

	// buffered channel to not block listener
	ch := make(chan *LiteResponse, 1)

	req := &LiteRequest{
		TypeID:   typeID,
		QueryID:  id,
		Data:     payload,
		RespChan: ch,
	}

	hexID := hex.EncodeToString(id)

	c.reqMx.Lock()
	c.activeReqs[hexID] = req
	c.reqMx.Unlock()

	defer func() {
		c.reqMx.Lock()
		delete(c.activeReqs, hexID)
		c.reqMx.Unlock()
	}()

	if nodeID, ok := ctx.Value(_StickyCtxKey).(uint32); ok && nodeID > 0 {
		err := c.querySticky(nodeID, req)
		if err != nil {
			return nil, err
		}
	} else {
		err := c.queryWithBalancer(req)
		if err != nil {
			return nil, err
		}
	}

	// additional timeout to not stuck forever with background context
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// wait for response
	select {
	case resp := <-ch:
		if resp.err != nil {
			return nil, resp.err
		}

		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timeoutCtx.Done():
		return nil, errors.New("liteserver request timeout")
	}
}

func (c *ConnectionPool) querySticky(id uint32, req *LiteRequest) error {
	c.nodesMx.RLock()
	for _, node := range c.activeNodes {
		if node.id == id {
			err := node.queryLiteServer(req.QueryID, req.TypeID, req.Data)
			if err == nil {
				return nil
			}
			break
		}
	}
	c.nodesMx.RUnlock()

	// fallback if bounded node is not available
	return c.queryWithBalancer(req)
}

func (c *ConnectionPool) queryWithBalancer(req *LiteRequest) error {
	nodeOffset := atomic.AddUint64(&c.roundRobinOffset, 1)

	var firstNode *connection
	for {
		c.nodesMx.RLock()
		if len(c.activeNodes) == 0 {
			c.nodesMx.RUnlock()
			return ErrNoActiveConnections
		}
		reqNode := c.activeNodes[nodeOffset%uint64(len(c.activeNodes))]
		c.nodesMx.RUnlock()

		if firstNode == nil {
			firstNode = reqNode
		} else if reqNode == firstNode {
			// all nodes were tried, nothing works
			return ErrNoActiveConnections
		}

		err := reqNode.queryLiteServer(req.QueryID, req.TypeID, req.Data)
		if err != nil {
			nodeOffset++
			continue
		}

		return nil
	}
}

func (c *ConnectionPool) SetOnDisconnect(cb OnDisconnectCallback) {
	c.reqMx.Lock()
	c.onDisconnect = cb
	c.reqMx.Unlock()
}
