package liteclient

import (
	"context"
	"errors"
	"testing"
)

func TestOfflineClientStickyContext(t *testing.T) {
	client := NewOfflineClient()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if got := client.StickyContext(ctx); got != ctx {
		t.Fatal("sticky context must preserve the original context")
	}
}

func TestOfflineClientNextNode(t *testing.T) {
	client := NewOfflineClient()
	ctx := context.Background()

	for _, test := range []struct {
		name string
		next func(context.Context) (context.Context, error)
	}{
		{name: "next", next: client.StickyContextNextNode},
		{name: "balanced", next: client.StickyContextNextNodeBalanced},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.next(ctx)
			if !errors.Is(err, ErrOfflineMode) {
				t.Fatalf("expected offline mode error, got %v", err)
			}

			if got != ctx {
				t.Fatal("node selection must preserve the original context")
			}
		})
	}
}
