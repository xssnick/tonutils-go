package ton

import (
	"context"
	"errors"
	"time"

	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tl"
)

type timeoutClient struct {
	original LiteClient
	timeout  time.Duration
}

type attemptTimeoutError struct {
	cause error
}

func (e attemptTimeoutError) Error() string {
	return e.cause.Error()
}

func (e attemptTimeoutError) Unwrap() error {
	return e.cause
}

func (e attemptTimeoutError) Is(target error) bool {
	return target == liteclient.ErrADNLReqTimeout
}

func (c *timeoutClient) QueryLiteserver(ctx context.Context, payload tl.Serializable, result tl.Serializable) error {
	tCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	err := c.original.QueryLiteserver(tCtx, payload, result)
	if err == nil {
		return nil
	}

	if errors.Is(err, liteclient.ErrADNLReqTimeout) {
		return err
	}

	// If the parent context is still alive, the timeout came from this wrapper's
	// per-attempt deadline and should stay retryable for the balancer.
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		return attemptTimeoutError{cause: err}
	}

	return err
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
