package ton

import (
	"context"
	"time"

	"github.com/xssnick/tonutils-go/tl"
)

type timeoutClient struct {
	original LiteClient
	timeout  time.Duration
}

func (c *timeoutClient) QueryLiteserver(ctx context.Context, payload tl.Serializable, result tl.Serializable) error {
	tCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	return c.original.QueryLiteserver(tCtx, payload, result)
}

func (c *timeoutClient) StickyContext(ctx context.Context) context.Context {
	return c.original.StickyContext(ctx)
}

func (c *timeoutClient) StickyNodeID(ctx context.Context) uint32 {
	return c.original.StickyNodeID(ctx)
}

func (c *timeoutClient) StickyContextNextNode(ctx context.Context) (context.Context, error) {
	return c.original.StickyContextNextNode(ctx)
}

func (c *timeoutClient) StickyContextNextNodeBalanced(ctx context.Context) (context.Context, error) {
	return c.original.StickyContextNextNodeBalanced(ctx)
}
