package liteclient

import (
	"context"
	"testing"
	"time"
)

func TestStickyContextPrefersFastMeasuredNodeOverWarmupNode(t *testing.T) {
	pool := &ConnectionPool{}

	pool.activeNodes = []*connection{
		{id: 1, weight: 900, lastRespTime: int64(60 * time.Millisecond)},
		{id: 2, weight: 900, lastRespTime: 0},
		{id: 3, weight: 900, lastRespTime: int64(300 * time.Millisecond)},
	}

	ctx := pool.StickyContext(context.Background())
	if got := pool.StickyNodeID(ctx); got != 1 {
		t.Fatalf("expected fastest measured node to be selected, got %d", got)
	}
}

func TestStickyContextNextNodeUsesBalancedSelection(t *testing.T) {
	pool := &ConnectionPool{}

	pool.activeNodes = []*connection{
		{id: 1, weight: 980, lastRespTime: int64(25 * time.Millisecond)},
		{id: 2, weight: 930, lastRespTime: int64(40 * time.Millisecond)},
		{id: 3, weight: 850, lastRespTime: int64(10 * time.Millisecond)},
	}

	ctx := pool.StickyContextWithNodeID(context.Background(), 1)
	ctx, err := pool.StickyContextNextNode(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if got := pool.StickyNodeID(ctx); got != 2 {
		t.Fatalf("expected next balanced node to be selected, got %d", got)
	}
}

func TestPingHealthUpdatesLatencyAndWeight(t *testing.T) {
	node := &connection{
		weight: 700,
	}

	node.notePingSent(1, time.Now().Add(-12*time.Second))
	node.expirePingsBefore(time.Now().Add(-10 * time.Second))

	if got := node.effectiveWeight(); got >= 700 {
		t.Fatalf("expected missed ping to reduce weight, got %d", got)
	}

	node.notePingSent(2, time.Now().Add(-20*time.Millisecond))
	node.notePong(2)

	if got := node.effectiveLatency(); got >= int64(_nodeWarmupLatency) {
		t.Fatalf("expected pong RTT to replace warmup latency, got %s", time.Duration(got))
	}

	if got := node.missedPings; got != 0 {
		t.Fatalf("expected pong to reset missed pings, got %d", got)
	}
}
