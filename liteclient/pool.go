package liteclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
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

func (c *ConnectionPool) StickyNodeID(ctx context.Context) uint32 {
	nodeID, _ := ctx.Value(_StickyCtxKey).(uint32)
	return nodeID
}

// Do - builds and executes request to liteserver
func (c *ConnectionPool) Do(ctx context.Context, typeID int32, payload []byte) (*LiteResponse, error) {
	id := make([]byte, 32)

	_, err := io.ReadFull(rand.Reader, id)
	if err != nil {
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

	var host string
	if nodeID, ok := ctx.Value(_StickyCtxKey).(uint32); ok && nodeID > 0 {
		host, err = c.querySticky(nodeID, req)
		if err != nil {
			return nil, err
		}
	} else {
		host, err = c.queryWithBalancer(req)
		if err != nil {
			return nil, err
		}
	}

	_, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		// fallback timeout to not stuck forever with background context
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	// wait for response
	select {
	case resp := <-ch:
		if resp.err != nil {
			return nil, resp.err
		}

		return resp, nil
	case <-ctx.Done():
		if !hasDeadline {
			return nil, fmt.Errorf("liteserver request timeout, node %s", host)
		}
		return nil, fmt.Errorf("deadline exceeded, node %s, err: %w", host, ctx.Err())
	}
}

func (c *ConnectionPool) querySticky(id uint32, req *LiteRequest) (string, error) {
	c.nodesMx.RLock()
	for _, node := range c.activeNodes {
		if node.id == id {
			host, err := node.queryLiteServer(req.QueryID, req.TypeID, req.Data)
			if err == nil {
				c.nodesMx.RUnlock()
				return host, nil
			}
			break
		}
	}
	c.nodesMx.RUnlock()

	// fallback if bounded node is not available
	return c.queryWithBalancer(req)
}

func (c *ConnectionPool) queryWithBalancer(req *LiteRequest) (string, error) {
	nodeOffset := atomic.AddUint64(&c.roundRobinOffset, 1)

	var firstNode *connection
	for {
		c.nodesMx.RLock()
		if len(c.activeNodes) == 0 {
			c.nodesMx.RUnlock()
			return "", ErrNoActiveConnections
		}
		reqNode := c.activeNodes[nodeOffset%uint64(len(c.activeNodes))]
		c.nodesMx.RUnlock()

		if firstNode == nil {
			firstNode = reqNode
		} else if reqNode == firstNode {
			// all nodes were tried, nothing works
			return "", ErrNoActiveConnections
		}

		host, err := reqNode.queryLiteServer(req.QueryID, req.TypeID, req.Data)
		if err != nil {
			nodeOffset++
			continue
		}

		return host, nil
	}
}

func (c *ConnectionPool) SetOnDisconnect(cb OnDisconnectCallback) {
	c.reqMx.Lock()
	c.onDisconnect = cb
	c.reqMx.Unlock()
}
