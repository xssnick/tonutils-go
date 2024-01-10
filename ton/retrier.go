package ton

import (
	"context"
	"fmt"
	"github.com/xssnick/tonutils-go/tl"
	"strings"
)

type retryClient struct {
	maxRetries int
	original   LiteClient
}

func (w *retryClient) QueryLiteserver(ctx context.Context, payload tl.Serializable, result tl.Serializable) error {
	tries := w.maxRetries
	for {
		err := w.original.QueryLiteserver(ctx, payload, result)
		if w.maxRetries > 0 && tries == w.maxRetries {
			return err
		}
		tries++

		if err != nil {
			if strings.HasPrefix(err.Error(), "adnl request timeout, node") {
				// try next node
				if ctx, err = w.original.StickyContextNextNode(ctx); err != nil {
					return fmt.Errorf("timeout error received, but failed to try with next node, "+
						"looks like all active nodes was already tried, original error: %w", err)
				}
				continue
			}
			return err
		}

		if tmp, ok := result.(*tl.Serializable); ok && tmp != nil {
			if lsErr, ok := (*tmp).(LSError); ok && (lsErr.Code == 651 ||
				lsErr.Code == 652 ||
				lsErr.Code == -400 ||
				lsErr.Code == -503 ||
				(lsErr.Code == 0 && strings.Contains(lsErr.Text, "Failed to get account state"))) {
				if ctx, err = w.original.StickyContextNextNode(ctx); err != nil { // try next node
					// no more nodes left, return as it is
					return nil
				}
				continue
			}
		}
		return nil
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
