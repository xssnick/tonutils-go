package liteclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

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

type Client struct {
	activeReqs  map[string]*LiteRequest
	activeNodes []*node
	reqMx       sync.RWMutex
	nodesMx     sync.RWMutex

	onDisconnect     func(addr, key string)
	roundRobinOffset uint64
}

var ErrNoActiveConnections = errors.New("no active connections")

func NewClient() *Client {
	c := &Client{
		activeReqs: map[string]*LiteRequest{},
	}

	// default reconnect policy
	c.SetOnDisconnect(c.DefaultReconnect(3*time.Second, 3))

	return c
}

func (c *Client) Do(ctx context.Context, typeID int32, payload []byte) (*LiteResponse, error) {
	id := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, id); err != nil {
		return nil, err
	}

	// buffered chanel to not block listener
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

	err := c.queryWithBalancer(req)
	if err != nil {
		return nil, err
	}

	// wait response
	select {
	case resp := <-ch:
		if resp.err != nil {
			return nil, resp.err
		}

		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *Client) queryWithBalancer(req *LiteRequest) error {
	nodeOffset := atomic.AddUint64(&c.roundRobinOffset, 1)

	var firstNode *node
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

func (c *Client) SetOnDisconnect(cb OnDisconnectCallback) {
	c.reqMx.Lock()
	c.onDisconnect = cb
	c.reqMx.Unlock()
}
