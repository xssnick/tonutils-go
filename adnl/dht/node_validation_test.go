package dht

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/adnl/overlay"
	"github.com/xssnick/tonutils-go/tl"
)

func TestNodeFindValueExpiration(t *testing.T) {
	for _, tt := range []struct {
		name      string
		ttl       time.Duration
		corrupted bool
		wantValue bool
	}{
		{name: "expired", ttl: -24 * time.Hour},
		{name: "expires now"},
		{name: "live", ttl: time.Second, wantValue: true},
		{name: "invalid expired signature", ttl: -time.Hour, corrupted: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				pub, priv, err := ed25519.GenerateKey(nil)
				if err != nil {
					t.Fatal(err)
				}
				value, keyID, err := buildStoreValue(keys.PublicKeyED25519{Key: pub}, []byte("address"), 0,
					[]byte("signed address"), UpdateRuleSignature{}, tt.ttl, priv)
				if err != nil {
					t.Fatal(err)
				}
				if tt.corrupted {
					value.Signature[0] ^= 1
				}
				candidate, err := newCorrectNode(192, 0, 2, 2, 17555)
				if err != nil {
					t.Fatal(err)
				}
				var requests []any
				gateway := &MockGateway{}
				gateway.setReg(func(string, ed25519.PublicKey) (adnl.Peer, error) {
					return MockADNL{query: func(ctx context.Context, req, result tl.Serializable) error {
						var query any
						if _, err := tl.Parse(&query, req.(tl.Raw), true); err != nil {
							return err
						}
						requests = append(requests, query)
						switch query.(type) {
						case FindValue:
							*result.(*any) = ValueFoundResult{Value: value}
						case FindNode:
							*result.(*any) = NodesList{List: []*Node{candidate}}
						default:
							return fmt.Errorf("unexpected query %T", query)
						}
						return nil
					}}, nil
				})
				client, err := NewClient(gateway, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer client.Close()
				node := client.initNode(make([]byte, 32), "192.0.2.1:17555", pub, 1)
				got, err := node.findValue(context.Background(), keyID, 10)
				if tt.corrupted {
					if err == nil || len(requests) != 1 || atomic.LoadInt32(&node.badScore) != 1 {
						t.Fatalf("invalid signature: result=%T err=%v queries=%d score=%d", got, err,
							len(requests), atomic.LoadInt32(&node.badScore))
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if tt.wantValue {
					if !reflect.DeepEqual(got, &value) || len(requests) != 1 {
						t.Fatalf("live value: result=%T queries=%d", got, len(requests))
					}
					return
				}
				if !reflect.DeepEqual(got, []*Node{candidate}) || len(requests) != 2 {
					t.Fatalf("expired value did not return fallback nodes: result=%T queries=%d", got, len(requests))
				}
				query, ok := requests[1].(FindNode)
				if !ok || !bytes.Equal(query.Key, keyID) || query.K != 10 {
					t.Fatalf("wrong fallback query: %#v", requests[1])
				}
			})
		})
	}
}

func TestNodeMetadataCommitKeepsNewestVersion(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(&MockGateway{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	makeNode := func(version int32) *dhtNode {
		t.Helper()
		descriptor, err := newCorrectNodeWithKey(192, 0, 2, byte(version), 17555, version, pub, priv)
		if err != nil {
			t.Fatal(err)
		}
		id, err := tl.Hash(descriptor.ID)
		if err != nil {
			t.Fatal(err)
		}
		node := client.initNode(id, describeNodeAddress(descriptor.AddrList), pub, version)
		node.node = descriptor
		return node
	}

	bucket := newBucket(2)
	current := makeNode(1)
	bucket.addNode(current, true)
	older, newer := makeNode(2), makeNode(3)
	// Both operations have passed the version check before either commits.
	for _, candidate := range []*dhtNode{older, newer} {
		if err := candidate.node.validate(current.snapshot().version, _UnknownNetworkID); err != nil {
			t.Fatal(err)
		}
	}
	bucket.addNode(newer, false)
	bucket.addNode(older, false)
	if got := current.snapshot(); got.version != 3 || got.addr != newer.snapshot().addr || !reflect.DeepEqual(got.node, newer.node) {
		t.Fatalf("metadata regressed after an older concurrent update committed: version=%d addr=%q", got.version, got.addr)
	}

	// A conflicting same-version descriptor also must not replace the current one.
	equal := makeNode(3)
	equal.addr = "192.0.2.99:17555"
	bucket.addNode(equal, true)
	if got := current.snapshot(); got.addr != newer.snapshot().addr {
		t.Fatalf("same-version update replaced the address: %q", got.addr)
	}
}

func TestNodeMetadataConcurrentUpdatesKeepNewestVersion(t *testing.T) {
	node := &dhtNode{version: 1}
	var wg sync.WaitGroup
	for version := int32(2); version <= 64; version++ {
		wg.Go(func() {
			node.absorb(&dhtNode{version: version, addr: fmt.Sprintf("192.0.2.%d:17555", version)})
		})
	}
	wg.Wait()
	if got := node.snapshot(); got.version != 64 || got.addr != "192.0.2.64:17555" {
		t.Fatalf("newest metadata lost: version=%d addr=%q", got.version, got.addr)
	}
}

func TestNodeSignedAddressListRefreshesUnchangedMetadata(t *testing.T) {
	for _, responseVersion := range []int32{1, 2} {
		t.Run(fmt.Sprintf("version %d", responseVersion), func(t *testing.T) {
			pub, priv, err := ed25519.GenerateKey(nil)
			if err != nil {
				t.Fatal(err)
			}
			current, err := newCorrectNodeWithKey(192, 0, 2, 2, 17555, 2, pub, priv)
			if err != nil {
				t.Fatal(err)
			}
			response, err := newCorrectNodeWithKey(192, 0, 2, 1, 17555, responseVersion, pub, priv)
			if err != nil {
				t.Fatal(err)
			}
			gateway := &MockGateway{}
			gateway.setReg(func(string, ed25519.PublicKey) (adnl.Peer, error) {
				return MockADNL{query: func(ctx context.Context, req, result tl.Serializable) error {
					*result.(*any) = *response
					return nil
				}}, nil
			})
			client, err := NewClient(gateway, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			node, err := client.addNodeWithStatus(current, true)
			if err != nil {
				t.Fatal(err)
			}
			server := &Server{Client: client}
			for range _MaxFailCount + 1 {
				node.updateStatus(false)
			}
			if node.isReady() {
				t.Fatal("test node did not become unready")
			}
			for range _MaxFailCount + 1 {
				atomic.StoreInt64(&node.lastPingAt, 0)
				server.refreshNodes()
			}
			if !node.isReady() || atomic.LoadUint32(&node.missedPings) != 0 || atomic.LoadInt32(&node.badScore) != 0 {
				t.Fatal("authentic unchanged metadata did not restore liveness")
			}
			if got := node.snapshot(); got.version != current.Version || got.addr != describeNodeAddress(current.AddrList) {
				t.Fatalf("old response replaced current metadata: version=%d addr=%q", got.version, got.addr)
			}
		})
	}
}

func TestNodeProtocolErrorsMarkActiveNodeUnready(t *testing.T) {
	for _, method := range []string{"findNodes", "findValue", "getSignedAddressList", "storePayload"} {
		t.Run(method, func(t *testing.T) {
			gateway := &MockGateway{}
			var reply any = Pong{}
			gateway.setReg(func(string, ed25519.PublicKey) (adnl.Peer, error) {
				return MockADNL{query: func(ctx context.Context, req, result tl.Serializable) error {
					*result.(*any) = reply
					return nil
				}}, nil
			})
			node := newBucketTestNode(1)
			node.client = &Client{gateway: gateway, networkID: _UnknownNetworkID}
			node.markPingSuccess()
			query := func() error {
				switch method {
				case "findNodes":
					_, err := node.findNodes(context.Background(), make([]byte, 32), 10)
					return err
				case "findValue":
					_, err := node.findValue(context.Background(), make([]byte, 32), 10)
					return err
				case "getSignedAddressList":
					_, err := node.getSignedAddressList(context.Background())
					return err
				default:
					return node.storePayload(context.Background(), nil)
				}
			}
			for i := 1; i <= _MaxFailCount+1; i++ {
				if err := query(); err == nil {
					t.Fatal("accepted a wrong DHT response type")
				}
				if score := atomic.LoadInt32(&node.badScore); score != int32(i) {
					t.Fatalf("error %d: badScore=%d", i, score)
				}
				if node.isReady() != (i <= _MaxFailCount) {
					t.Fatalf("error %d: ready=%v", i, node.isReady())
				}
			}
			reply = NodesList{}
			if _, err := node.findNodes(context.Background(), make([]byte, 32), 10); err != nil {
				t.Fatal(err)
			}
			if atomic.LoadInt32(&node.badScore) != 0 {
				t.Fatal("validated response did not reset failure score")
			}
		})
	}
}

func TestNodeFindValueFallbackCountsTransportFailureOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pub, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		value, keyID, err := buildStoreValue(keys.PublicKeyED25519{Key: pub}, []byte("address"), 0,
			[]byte("expired"), UpdateRuleSignature{}, -time.Hour, priv)
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		gateway := &MockGateway{}
		gateway.setReg(func(string, ed25519.PublicKey) (adnl.Peer, error) {
			return MockADNL{query: func(ctx context.Context, req, result tl.Serializable) error {
				calls++
				if calls == 1 {
					*result.(*any) = ValueFoundResult{Value: value}
					return nil
				}
				<-ctx.Done()
				return ctx.Err()
			}}, nil
		})
		node := newBucketTestNode(1)
		node.client = &Client{gateway: gateway, networkID: _UnknownNetworkID}
		node.markPingSuccess()
		ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
		defer cancel()
		if _, err := node.findValue(ctx, keyID, 10); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected timeout from fallback query, got %v", err)
		}
		if calls != 2 || atomic.LoadInt32(&node.badScore) != 1 || !node.isReady() {
			t.Fatalf("fallback transport failure counted incorrectly: calls=%d score=%d ready=%v",
				calls, atomic.LoadInt32(&node.badScore), node.isReady())
		}
	})
}

func TestNodeExpiredValueCannotResetProtocolFailures(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	value, keyID, err := buildStoreValue(keys.PublicKeyED25519{Key: pub}, []byte("address"), 0,
		[]byte("expired"), UpdateRuleSignature{}, -time.Hour, priv)
	if err != nil {
		t.Fatal(err)
	}
	gateway := &MockGateway{}
	gateway.setReg(func(string, ed25519.PublicKey) (adnl.Peer, error) {
		return MockADNL{query: func(ctx context.Context, req, result tl.Serializable) error {
			var query any
			if _, err := tl.Parse(&query, req.(tl.Raw), true); err != nil {
				return err
			}
			switch query.(type) {
			case FindValue:
				*result.(*any) = ValueFoundResult{Value: value}
			case FindNode:
				*result.(*any) = Pong{}
			default:
				return fmt.Errorf("unexpected query %T", query)
			}
			return nil
		}}, nil
	})
	node := newBucketTestNode(1)
	node.client = &Client{gateway: gateway, networkID: _UnknownNetworkID}
	node.markPingSuccess()
	for i := 1; i <= _MaxFailCount+1; i++ {
		if _, err := node.findValue(context.Background(), keyID, 10); err == nil {
			t.Fatal("accepted malformed fallback response")
		}
		if score := atomic.LoadInt32(&node.badScore); score != int32(i) {
			t.Fatalf("query %d: score=%d", i, score)
		}
	}
	if node.isReady() {
		t.Fatal("replaying expired values preserved readiness despite invalid fallback replies")
	}
}

func TestOverlayNodeVersionBounds(t *testing.T) {
	for _, tt := range []struct {
		name       string
		offset     int64
		acceptable bool
		valid      bool
	}{
		{name: "stale", offset: -_MaxOverlayNodeAgeSec, valid: true},
		{name: "oldest live", offset: -_MaxOverlayNodeAgeSec + 1, acceptable: true, valid: true},
		{name: "now", acceptable: true, valid: true},
		{name: "maximum skew", offset: _MaxOverlayNodeFutureSec, acceptable: true, valid: true},
		{name: "future", offset: _MaxOverlayNodeFutureSec + 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				pub, priv, err := ed25519.GenerateKey(nil)
				if err != nil {
					t.Fatal(err)
				}
				now := time.Now().Unix()
				node := overlay.Node{ID: keys.PublicKeyED25519{Key: pub}, Overlay: make([]byte, 32), Version: int32(now + tt.offset)}
				if err := node.Sign(priv); err != nil {
					t.Fatal(err)
				}
				if err := checkOverlayNode(&node, node.Overlay, _UnknownNetworkID); (err == nil) != tt.valid {
					t.Fatalf("valid=%v: got %v", tt.valid, err)
				}
				if got := overlayNodeVersionAcceptableAt(node.Version, now); got != tt.acceptable {
					t.Fatalf("acceptable=%v, want %v", got, tt.acceptable)
				}
			})
		})
	}
}

func TestNodeProtocolFailuresStopRoutingAnnouncements(t *testing.T) {
	descriptor, err := newCorrectNode(192, 0, 2, 1, 17555)
	if err != nil {
		t.Fatal(err)
	}
	gateway := &MockGateway{}
	gateway.setReg(func(string, ed25519.PublicKey) (adnl.Peer, error) {
		return MockADNL{query: func(ctx context.Context, req, result tl.Serializable) error {
			*result.(*any) = Pong{}
			return nil
		}}, nil
	})
	client, err := NewClient(gateway, []*Node{descriptor})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	id, err := tl.Hash(descriptor.ID)
	if err != nil {
		t.Fatal(err)
	}
	node := client.buckets[affinity(id, client.selfID)].findNode(id)
	server := &Server{Client: client}
	if len(client.RoutingNodes()) != 1 || len(server.getNearestNodes(id, 10)) != 1 {
		t.Fatal("ready node was not advertised before failures")
	}
	for range _MaxFailCount + 1 {
		if _, err := node.findNodes(context.Background(), id, 10); err == nil {
			t.Fatal("accepted malformed DHT reply")
		}
	}
	if got := client.RoutingNodes(); len(got) != 0 {
		t.Fatalf("client kept advertising %d failed nodes without maintenance", len(got))
	}
	if got := server.getNearestNodes(id, 10); len(got) != 0 {
		t.Fatalf("server kept advertising %d failed nodes without maintenance", len(got))
	}
}
