package liteclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/tl"
	"io"
	mRand "math/rand"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

const _StickyCtxKey = "_ton_node_sticky"

type OnDisconnectCallback func(addr, key string)

type ADNLResponse struct {
	Data tl.Serializable
	Err  error
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
}

var ErrNoActiveConnections = errors.New("no active connections")

// NewConnectionPool - ordinary pool to query liteserver
func NewConnectionPool() *ConnectionPool {
	c := &ConnectionPool{
		activeReqs: map[string]*ADNLRequest{},
	}

	// default reconnect policy
	c.SetOnDisconnect(c.DefaultReconnect(3*time.Second, -1))

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

func (c *ConnectionPool) StickyNodeID(ctx context.Context) uint32 {
	nodeID, _ := ctx.Value(_StickyCtxKey).(uint32)
	return nodeID
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

	var host string
	if nodeID, ok := ctx.Value(_StickyCtxKey).(uint32); ok && nodeID > 0 {
		host, err = c.querySticky(nodeID, req)
		if err != nil {
			return err
		}
	} else {
		host, err = c.queryWithBalancer(req)
		if err != nil {
			return err
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
		if resp.Err != nil {
			return resp.Err
		}

		reflect.ValueOf(result).Elem().Set(reflect.ValueOf(resp.Data))
		return nil
	case <-ctx.Done():
		if !hasDeadline {
			return fmt.Errorf("adnl request timeout, node %s", host)
		}
		return fmt.Errorf("deadline exceeded, node %s, err: %w", host, ctx.Err())
	}
}

func (c *ConnectionPool) querySticky(id uint32, req *ADNLRequest) (string, error) {
	c.nodesMx.RLock()
	for _, node := range c.activeNodes {
		if node.id == id {
			host, err := node.queryAdnl(req.QueryID, req.Data)
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

func (c *ConnectionPool) queryWithBalancer(req *ADNLRequest) (string, error) {
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

		host, err := reqNode.queryAdnl(req.QueryID, req.Data)
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
