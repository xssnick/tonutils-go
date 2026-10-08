package dht

import (
	"context"
	"crypto/ed25519"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
)

func TestNodeQueryTimeoutsMarkUnready(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		responsive := false
		gateway := &MockGateway{}
		gateway.setReg(func(string, ed25519.PublicKey) (adnl.Peer, error) {
			return &MockADNL{query: func(ctx context.Context, req, res tl.Serializable) error {
				if responsive {
					return nil
				}

				<-ctx.Done()
				return ctx.Err()
			}}, nil
		})

		node := newBucketTestNode(1)
		node.client = &Client{gateway: gateway}
		node.markPingSuccess()

		for i := 1; i <= _MaxFailCount+1; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
			var res tl.Serializable
			_, err := node.query(ctx, tl.Raw(nil), &res)
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("query %d: expected deadline exceeded, got %v", i, err)
			}
			if node.isReady() != (i <= _MaxFailCount) {
				t.Fatalf("query %d: unexpected readiness %v", i, node.isReady())
			}
		}

		failedAt := node.failedAt()
		if failedAt != time.Now().UnixNano() {
			t.Fatal("failure age did not start when the node became unready")
		}

		time.Sleep(backupReplaceAfter + time.Second)
		for range 6 {
			ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
			var res tl.Serializable
			node.query(ctx, tl.Raw(nil), &res)
			cancel()
		}
		if node.isReady() || node.failedAt() != failedAt {
			t.Fatal("repeated timeouts changed the node's unready state or failure age")
		}
		if score := atomic.LoadInt32(&node.badScore); score != _MaxFailCount+1 {
			t.Fatalf("unexpected failure score: %d", score)
		}

		responsive = true
		var res tl.Serializable
		if _, err := node.query(context.Background(), tl.Raw(nil), &res); err != nil {
			t.Fatal(err)
		}
		if node.isReady() || node.failedAt() != failedAt {
			t.Fatal("an unvalidated transport response restored node readiness")
		}

		node.markPingSuccess()
		if !node.isReady() || node.failedAt() != 0 || atomic.LoadInt32(&node.badScore) != 0 {
			t.Fatal("a validated response did not restore node health")
		}
	})
}

func TestNodeQueryShortDeadlinesKeepReady(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gateway := &MockGateway{}
		gateway.setReg(func(string, ed25519.PublicKey) (adnl.Peer, error) {
			return &MockADNL{query: func(ctx context.Context, req, res tl.Serializable) error {
				<-ctx.Done()
				return ctx.Err()
			}}, nil
		})

		node := newBucketTestNode(1)
		node.client = &Client{gateway: gateway}
		node.markPingSuccess()
		for range 10 {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			var res tl.Serializable
			_, err := node.query(ctx, tl.Raw(nil), &res)
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected deadline exceeded, got %v", err)
			}
		}
		if !node.isReady() || atomic.LoadInt32(&node.badScore) != 0 {
			t.Fatal("short caller deadlines penalized a ready node")
		}
	})
}

func TestNodePingFailuresPreserveFailureAge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		node := newBucketTestNode(1)
		node.markPingSuccess()
		for range _MaxFailCount + 1 {
			time.Sleep(time.Second)
			node.markPingFailure()
		}
		if node.isReady() || node.failedAt() != time.Now().UnixNano() {
			t.Fatal("ping failures did not mark the node unready")
		}

		failedAt := node.failedAt()
		time.Sleep(backupReplaceAfter + time.Second)
		node.markPingFailure()
		if node.failedAt() != failedAt {
			t.Fatal("another failed ping postponed replacement of the dead node")
		}

		node.updateStatus(false)
		node.markPingSuccess()
		if !node.isReady() || node.failedAt() != 0 || atomic.LoadInt32(&node.badScore) != 0 {
			t.Fatal("a successful ping did not restore node health")
		}
		if atomic.LoadUint32(&node.missedPings) != 0 || time.Duration(atomic.LoadInt64(&node.pingEvery)) != _pingIntervalDefault {
			t.Fatal("a successful ping did not reset the ping backoff")
		}
	})
}

func TestNodeConcurrentFailures(t *testing.T) {
	node := newBucketTestNode(1)
	node.markPingSuccess()

	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			node.updateStatus(false)
			node.markPingFailure()
		})
	}
	wg.Wait()

	if node.isReady() || node.failedAt() == 0 {
		t.Fatal("concurrent failures left the node ready")
	}
	if score := atomic.LoadInt32(&node.badScore); score != _MaxFailCount+1 {
		t.Fatalf("failure score exceeded its limit: %d", score)
	}

	node.markPingSuccess()
	for range _MaxFailCount {
		node.updateStatus(false)
		node.markPingFailure()
	}
	if !node.isReady() {
		t.Fatal("validated response did not reset the failure counters")
	}
}
