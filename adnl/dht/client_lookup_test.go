package dht

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tl"
)

type valueLookupFixture struct {
	client  *Client
	gateway *MockGateway
	value   *ValueFoundResult
	key     *Key
	target  []byte
}

func newValueLookupFixture(t *testing.T, k, a int) *valueLookupFixture {
	t.Helper()

	address, err := hex.DecodeString(testValueADNLID)
	if err != nil {
		t.Fatal(err)
	}
	value, err := correctValue(address)
	if err != nil {
		t.Fatal(err)
	}
	key := &value.Value.KeyDescription.Key
	target, err := tl.Hash(key)
	if err != nil {
		t.Fatal(err)
	}

	gateway := &MockGateway{id: target}
	client, err := NewClientFromConfig(gateway, &liteclient.GlobalConfig{
		DHT: liteclient.DHTConfig{K: k, A: a},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return &valueLookupFixture{client: client, gateway: gateway, value: value, key: key, target: target}
}

func (f *valueLookupFixture) addNode(distance byte) *dhtNode {
	id := append([]byte(nil), f.target...)
	id[0] ^= distance
	node := &dhtNode{adnlId: id, client: f.client, addr: fmt.Sprintf("%02x", distance)}
	f.client.buckets[affinity(id, f.client.selfID)].addNode(node, true)
	return node
}

func TestClient_FindValueFallsBackPastFailedShortlist(t *testing.T) {
	f := newValueLookupFixture(t, 2, 1)
	for distance := byte(1); distance <= 5; distance++ {
		f.addNode(distance)
	}
	var calls []string
	f.gateway.setReg(func(addr string, _ ed25519.PublicKey) (adnl.Peer, error) {
		return &MockADNL{query: func(_ context.Context, _, result tl.Serializable) error {
			calls = append(calls, addr)
			if addr != "05" {
				return fmt.Errorf("unreachable node")
			}
			reflect.ValueOf(result).Elem().Set(reflect.ValueOf(*f.value))
			return nil
		}}, nil
	})

	value, _, err := f.client.FindValue(context.Background(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(value, &f.value.Value) || !reflect.DeepEqual(calls, []string{"01", "02", "03", "04", "05"}) {
		t.Fatalf("did not reach the known deferred node: calls=%v value=%v", calls, value)
	}
}

func TestClient_FindValueUsesFullXORDistanceAndStopsAfterFound(t *testing.T) {
	f := newValueLookupFixture(t, 2, 1)
	for _, distance := range []byte{0xe0, 0xf0, 0xf8, 0x80} {
		f.addNode(distance)
	}
	var prepared atomic.Int32
	f.client.queryPrefix = func() ([]byte, error) {
		prepared.Add(1)
		return nil, nil
	}
	var calls []string
	f.gateway.setReg(func(addr string, _ ed25519.PublicKey) (adnl.Peer, error) {
		return &MockADNL{query: func(_ context.Context, _, result tl.Serializable) error {
			calls = append(calls, addr)
			reflect.ValueOf(result).Elem().Set(reflect.ValueOf(*f.value))
			return nil
		}}, nil
	})

	if _, _, err := f.client.FindValue(context.Background(), f.key); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"80"}) || prepared.Load() != 1 {
		t.Fatalf("expected only the XOR-closest query, calls=%v prepared=%d", calls, prepared.Load())
	}
}

func TestClient_FindValueWithKOneAndFailedNode(t *testing.T) {
	f := newValueLookupFixture(t, 1, 1)
	f.addNode(1).updateStatus(false)
	f.gateway.setReg(func(_ string, _ ed25519.PublicKey) (adnl.Peer, error) {
		return &MockADNL{query: func(_ context.Context, _, result tl.Serializable) error {
			reflect.ValueOf(result).Elem().Set(reflect.ValueOf(*f.value))
			return nil
		}}, nil
	})

	if _, _, err := f.client.FindValue(context.Background(), f.key); err != nil {
		t.Fatal(err)
	}
}

func TestClient_FindValueContinuationSkipsPreviousResults(t *testing.T) {
	f := newValueLookupFixture(t, 2, 1)
	for distance := byte(1); distance <= 5; distance++ {
		f.addNode(distance)
	}
	var calls []string
	f.gateway.setReg(func(addr string, _ ed25519.PublicKey) (adnl.Peer, error) {
		return &MockADNL{query: func(_ context.Context, _, result tl.Serializable) error {
			calls = append(calls, addr)
			reflect.ValueOf(result).Elem().Set(reflect.ValueOf(*f.value))
			return nil
		}}, nil
	})

	var continuation *Continuation
	for range 5 {
		var err error
		_, continuation, err = f.client.FindValue(context.Background(), f.key, continuation)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, _, err := f.client.FindValue(context.Background(), f.key, continuation)
	if !errors.Is(err, ErrDHTValueIsNotFound) {
		t.Fatalf("expected exhausted continuation, got %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"01", "02", "03", "04", "05"}) {
		t.Fatalf("continuation repeated or skipped a node: %v", calls)
	}
}

func TestClient_FindValueCanceledExhaustedSearch(t *testing.T) {
	for _, exhaustedContinuation := range []bool{false, true} {
		for _, expired := range []bool{false, true} {
			t.Run(fmt.Sprintf("continuation=%v/expired=%v", exhaustedContinuation, expired), func(t *testing.T) {
				f := newValueLookupFixture(t, 2, 1)
				continuation := &Continuation{}
				if exhaustedContinuation {
					continuation.checkedNodes = append(continuation.checkedNodes, f.addNode(1))
				}
				ctx, cancel := context.WithCancel(context.Background())
				want := context.Canceled
				if expired {
					cancel()
					ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
					want = context.DeadlineExceeded
				} else {
					cancel()
				}
				defer cancel()

				_, _, err := f.client.FindValue(ctx, f.key, continuation)
				if !errors.Is(err, want) {
					t.Fatalf("expected %v, got %v", want, err)
				}
			})
		}
	}
}

func TestClient_FindValueCancellationStopsQueries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newValueLookupFixture(t, 6, 3)
		for distance := byte(1); distance <= 5; distance++ {
			f.addNode(distance)
		}
		var active, queries atomic.Int32
		f.gateway.setReg(func(_ string, _ ed25519.PublicKey) (adnl.Peer, error) {
			return &MockADNL{query: func(ctx context.Context, _, _ tl.Serializable) error {
				queries.Add(1)
				active.Add(1)
				defer active.Add(-1)
				<-ctx.Done()
				return ctx.Err()
			}}, nil
		})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, _, err := f.client.FindValue(ctx, f.key)
			done <- err
		}()
		synctest.Wait()
		if active.Load() != 3 {
			t.Fatalf("expected three in-flight queries, got %d", active.Load())
		}

		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
		synctest.Wait()
		if active.Load() != 0 || queries.Load() != 3 {
			t.Fatalf("queries survived or started after cancellation: active=%d queries=%d", active.Load(), queries.Load())
		}
	})
}

func TestClient_FindValueCancellationDoesNotWaitForGateway(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newValueLookupFixture(t, 2, 1)
		f.addNode(1)
		releaseGateway := make(chan struct{})
		f.gateway.setReg(func(_ string, _ ed25519.PublicKey) (adnl.Peer, error) {
			<-releaseGateway
			return nil, fmt.Errorf("gateway stopped")
		})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, _, err := f.client.FindValue(ctx, f.key)
			done <- err
		}()
		synctest.Wait()
		cancel()
		synctest.Wait()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("expected cancellation, got %v", err)
			}
		default:
			t.Error("FindValue waited for a gateway without cancellation support")
		}
		close(releaseGateway)
		synctest.Wait()
	})
}

func TestClient_FindValueBoundsQueriesAndRetries(t *testing.T) {
	for _, tc := range []struct {
		name              string
		k, a, peers, peak int
	}{
		{name: "double parallelism", k: 6, a: 3, peers: 9, peak: 6},
		{name: "capped by k", k: 3, a: 2, peers: 5, peak: 3},
		{name: "k below parallelism", k: 2, a: 3, peers: 5, peak: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newValueLookupFixture(t, tc.k, tc.a)
				for distance := byte(1); distance <= byte(tc.peers); distance++ {
					f.addNode(distance)
				}
				var mu sync.Mutex
				active, peak := 0, 0
				calls := map[string]int{}
				f.gateway.setReg(func(addr string, _ ed25519.PublicKey) (adnl.Peer, error) {
					return &MockADNL{query: func(ctx context.Context, _, _ tl.Serializable) error {
						mu.Lock()
						calls[addr]++
						active++
						peak = max(peak, active)
						mu.Unlock()
						<-ctx.Done()
						mu.Lock()
						active--
						mu.Unlock()
						return ctx.Err()
					}}, nil
				})

				_, _, err := f.client.FindValue(context.Background(), f.key)
				if !errors.Is(err, ErrDHTValueIsNotFound) {
					t.Fatalf("expected not found, got %v", err)
				}
				synctest.Wait()
				if peak != tc.peak || active != 0 || len(calls) != tc.peers {
					t.Fatalf("unexpected query lifecycle: peak=%d active=%d calls=%v", peak, active, calls)
				}
				for addr, count := range calls {
					if count != retryableQueryAttempts {
						t.Fatalf("node %s queried %d times, want %d", addr, count, retryableQueryAttempts)
					}
				}
			})
		})
	}
}

func TestClient_FindValueHedgeReturnsValueAndCancelsSlowPeer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newValueLookupFixture(t, 3, 1)
		slow := f.addNode(1)
		f.addNode(2)
		f.addNode(3)
		var queries, active atomic.Int32
		f.gateway.setReg(func(addr string, _ ed25519.PublicKey) (adnl.Peer, error) {
			return &MockADNL{query: func(ctx context.Context, _, result tl.Serializable) error {
				queries.Add(1)
				active.Add(1)
				defer active.Add(-1)
				if addr == "01" {
					<-ctx.Done()
					return ctx.Err()
				}
				reflect.ValueOf(result).Elem().Set(reflect.ValueOf(*f.value))
				return nil
			}}, nil
		})

		started := time.Now()
		value, _, err := f.client.FindValue(context.Background(), f.key)
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if !reflect.DeepEqual(value, &f.value.Value) || time.Since(started) != lookupHedgeDelay {
			t.Fatalf("hedge did not return the expected value on time: elapsed=%v value=%v", time.Since(started), value)
		}
		if queries.Load() != 2 || active.Load() != 0 {
			t.Fatalf("unexpected query lifecycle: queries=%d active=%d", queries.Load(), active.Load())
		}
		if score := atomic.LoadInt32(&slow.badScore); score != 0 || !slow.isReady() {
			t.Fatalf("canceled slow peer was penalized: score=%d ready=%v", score, slow.isReady())
		}
	})
}
