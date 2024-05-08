package liteclient

import (
	"context"
	"fmt"
	"github.com/xssnick/tonutils-go/tl"
)

var ErrOfflineMode = fmt.Errorf("offline mode is used")

type OfflineClient struct{}

func NewOfflineClient() OfflineClient {
	return OfflineClient{}
}

func (o OfflineClient) QueryLiteserver(ctx context.Context, payload tl.Serializable, result tl.Serializable) error {
	return ErrOfflineMode
}

func (o OfflineClient) StickyContext(ctx context.Context) context.Context {
	return nil
}

func (o OfflineClient) StickyContextNextNode(ctx context.Context) (context.Context, error) {
	return ctx, nil
}

func (o OfflineClient) StickyNodeID(ctx context.Context) uint32 {
	return 0
}
