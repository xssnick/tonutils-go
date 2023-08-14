package ton

import (
	"context"
	"fmt"
	"github.com/xssnick/tonutils-go/tl"
	"strings"
)

type retryClient struct {
	original LiteClient
}

func (w *retryClient) QueryLiteserver(ctx context.Context, payload tl.Serializable, result tl.Serializable) error {
	for {
		err := w.original.QueryLiteserver(ctx, payload, result)
		if err != nil {
			if lsErr, ok := err.(LSError); ok && (lsErr.Code == 651 || lsErr.Code == -400) ||
				strings.HasPrefix(err.Error(), "adnl request timeout, node") { // block not applied error
				// try next node
				origErr := err
				if ctx, err = w.original.StickyContextNextNode(ctx); err != nil {
					return fmt.Errorf("retryable error received, but failed to try with next node, "+
						"looks like all active nodes was already tried, original error: %w", origErr)
				}
				continue
			}
		}
		return err
	}
}

func (w *retryClient) StickyContext(ctx context.Context) context.Context {
	return w.original.StickyContext(ctx)
}

func (w *retryClient) StickyNodeID(ctx context.Context) uint32 {
	return w.original.StickyNodeID(ctx)
}

func (w *retryClient) StickyContextNextNode(ctx context.Context) (context.Context, error) {
	return w.original.StickyContextNextNode(ctx)
}
