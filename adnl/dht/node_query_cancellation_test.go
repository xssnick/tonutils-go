package dht

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
)

func TestNodeQueryLateCancellationKeepsFailureScore(t *testing.T) {
	for _, tt := range []struct {
		name      string
		cancelOwn bool
		wantError error
		wantScore int32
	}{
		{name: "caller cancellation", cancelOwn: true, wantError: context.Canceled},
		{name: "query timeout", wantError: context.DeadlineExceeded, wantScore: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				gateway := &MockGateway{}
				gateway.setReg(func(string, ed25519.PublicKey) (adnl.Peer, error) {
					return MockADNL{query: func(ctx context.Context, _, _ tl.Serializable) error {
						<-ctx.Done()
						return fmt.Errorf("transport: %w", ctx.Err())
					}}, nil
				})
				node := newBucketTestNode(1)
				node.client = &Client{gateway: gateway, networkID: _UnknownNetworkID}
				node.markPingSuccess()

				ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
				defer cancel()
				if tt.cancelOwn {
					go func() {
						time.Sleep(queryTimeout - 100*time.Millisecond)
						cancel()
					}()
				}
				_, err := node.findNodes(ctx, make([]byte, 32), 10)
				if !errors.Is(err, tt.wantError) {
					t.Fatalf("got %v, want %v", err, tt.wantError)
				}
				if score := atomic.LoadInt32(&node.badScore); score != tt.wantScore {
					t.Fatalf("badScore=%d, want %d", score, tt.wantScore)
				}
			})
		})
	}
}
