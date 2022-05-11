package liteclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"sync"
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
	activeReqs map[string]*LiteRequest
	mx         sync.RWMutex

	requester chan *LiteRequest

	activeConnections int32
	onDisconnect      func(addr, key string)
}

var ErrNoActiveConnections = errors.New("no active connections")

func NewClient() *Client {
	c := &Client{
		activeReqs: map[string]*LiteRequest{},
		requester:  make(chan *LiteRequest),
	}

	// default reconnect policy
	c.SetOnDisconnect(c.DefaultReconnect(3*time.Second, 3))

	return c
}

func (c *Client) Do(ctx context.Context, typeID int32, payload []byte) (*LiteResponse, error) {
	if c.activeConnections == 0 {
		return nil, ErrNoActiveConnections
	}

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

	c.mx.Lock()
	c.activeReqs[hexID] = req
	c.mx.Unlock()

	defer func() {
		c.mx.Lock()
		delete(c.activeReqs, hexID)
		c.mx.Unlock()
	}()

	// add request to queue
	select {
	case c.requester <- req:
	case <-ctx.Done():
		return nil, ctx.Err()
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

func (c *Client) SetOnDisconnect(cb OnDisconnectCallback) {
	c.mx.Lock()
	c.onDisconnect = cb
	c.mx.Unlock()
}
