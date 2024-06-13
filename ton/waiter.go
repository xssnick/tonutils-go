package ton

import (
	"context"
	"github.com/xssnick/tonutils-go/tl"
	"time"
)

type waiterClient struct {
	seqno    uint32
	original LiteClient
}

func (w *waiterClient) QueryLiteserver(ctx context.Context, payload tl.Serializable, result tl.Serializable) error {
	var timeout = 10 * time.Second

	deadline, ok := ctx.Deadline()
	if ok {
		t := deadline.Sub(time.Now())
		if t < timeout {
			timeout = t
		}
	}

	prefix, err := tl.Serialize(WaitMasterchainSeqno{
		Seqno:   int32(w.seqno),
		Timeout: int32(timeout / time.Millisecond),
	}, true)
	if err != nil {
		return err
	}

	suffix, err := tl.Serialize(payload, true)
	if err != nil {
		return err
	}

	return w.original.QueryLiteserver(ctx, tl.Raw(append(prefix, suffix...)), result)
}

func (w *waiterClient) StickyContext(ctx context.Context) context.Context {
	return w.original.StickyContext(ctx)
}

func (w *waiterClient) StickyNodeID(ctx context.Context) uint32 {
	return w.original.StickyNodeID(ctx)
}

func (w *waiterClient) StickyContextNextNode(ctx context.Context) (context.Context, error) {
	return w.original.StickyContextNextNode(ctx)
}

func (w *waiterClient) StickyContextNextNodeBalanced(ctx context.Context) (context.Context, error) {
	return w.original.StickyContextNextNodeBalanced(ctx)
}
