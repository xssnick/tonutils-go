package dht

import (
	"bytes"
	"crypto/ed25519"
	"reflect"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/adnl/overlay"
	"github.com/xssnick/tonutils-go/tl"
)

func newStoreRegressionServer(t *testing.T, store ValueStore) *Server {
	t.Helper()

	client, err := newClient(&MockGateway{}, nil, _UnknownNetworkID, 10, 3)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return &Server{Client: client, store: store}
}

func signedStoreRegressionValue(t *testing.T, key ed25519.PrivateKey, ttl time.Duration) (Value, []byte) {
	t.Helper()

	value, keyID, err := buildStoreValue(keys.PublicKeyED25519{Key: key.Public().(ed25519.PublicKey)},
		[]byte("address"), 0, []byte(ttl.String()), UpdateRuleSignature{}, ttl, key)
	if err != nil {
		t.Fatal(err)
	}
	return value, keyID
}

// Hiding the optional interfaces also exercises a custom ValueStore's fallback cleanup.
type blockingStoreRegressionStore struct {
	ValueStore
	getBlocked atomic.Bool
	getHook    func()
	scanHook   func()
}

func (s *blockingStoreRegressionStore) Get(keyID []byte) (*Value, error) {
	value, err := s.ValueStore.Get(keyID)
	if s.getHook != nil && s.getBlocked.CompareAndSwap(false, true) {
		s.getHook()
	}
	return value, err
}

func (s *blockingStoreRegressionStore) ForEach(fn func([]byte, *Value) error) error {
	err := s.ValueStore.ForEach(fn)
	if s.scanHook != nil {
		s.scanHook()
	}
	return err
}

// Keep the first storage operation paused while the second is offered. An
// unprotected operation can finish; a serialized one waits until release.
func runOverlappingStoreOperations(t *testing.T, entered, release chan struct{}, first, second func() error) {
	t.Helper()

	firstDone := make(chan error, 1)
	go func() { firstDone <- first() }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first storage operation did not start")
	}

	secondDone := make(chan error, 1)
	go func() { secondDone <- second() }()
	var secondErr error
	secondFinished := false
	select {
	case secondErr = <-secondDone:
		secondFinished = true
	case <-time.After(20 * time.Millisecond):
	}
	close(release)

	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first storage operation did not finish")
	}
	if !secondFinished {
		select {
		case secondErr = <-secondDone:
		case <-time.After(5 * time.Second):
			t.Fatal("second storage operation did not finish")
		}
	}
	if secondErr != nil {
		t.Fatal(secondErr)
	}
}

func TestServerConcurrentStorePreservesNewestTTL(t *testing.T) {
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	base, keyID := signedStoreRegressionValue(t, key, time.Minute)
	older, _ := signedStoreRegressionValue(t, key, 2*time.Minute)
	newer, _ := signedStoreRegressionValue(t, key, 3*time.Minute)

	memory := NewMemoryValueStore(16)
	if err := memory.Put(keyID, &base); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	store := &blockingStoreRegressionStore{ValueStore: memory, getHook: func() {
		close(entered)
		<-release
	}}
	server := newStoreRegressionServer(t, store)

	runOverlappingStoreOperations(t, entered, release,
		func() error { return server.storeIn(keyID, &older) },
		func() error { return server.storeIn(keyID, &newer) })
	got, err := memory.Get(keyID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.TTL != newer.TTL || !bytes.Equal(got.Data, newer.Data) {
		t.Fatalf("concurrent Store lost newest value: got=%+v, newest TTL=%d", got, newer.TTL)
	}
}

func TestServerConcurrentStorePreservesBothOverlayPeers(t *testing.T) {
	overlayKey := []byte("concurrent-overlay")
	first := signedStoreRegressionOverlayNode(t, overlayKey, int32(time.Now().Unix()))
	second := signedStoreRegressionOverlayNode(t, overlayKey, int32(time.Now().Unix()))
	firstValue, keyID := storeRegressionOverlayValue(t, overlayKey, time.Minute, first)
	secondValue, _ := storeRegressionOverlayValue(t, overlayKey, time.Minute, second)

	memory := NewMemoryValueStore(16)
	entered, release := make(chan struct{}), make(chan struct{})
	store := &blockingStoreRegressionStore{ValueStore: memory, getHook: func() {
		close(entered)
		<-release
	}}
	server := newStoreRegressionServer(t, store)
	runOverlappingStoreOperations(t, entered, release,
		func() error { return server.storeIn(keyID, &firstValue) },
		func() error { return server.storeIn(keyID, &secondValue) })

	value, err := memory.Get(keyID)
	if err != nil {
		t.Fatal(err)
	}
	checkStoreRegressionOverlayNodes(t, value, first, second)
}

func TestServerExpiredDeletionPreservesConcurrentRefresh(t *testing.T) {
	for _, path := range []string{"read", "fallback cleanup"} {
		t.Run(path, func(t *testing.T) {
			_, key, err := ed25519.GenerateKey(nil)
			if err != nil {
				t.Fatal(err)
			}
			expired, keyID := signedStoreRegressionValue(t, key, -time.Minute)
			fresh, _ := signedStoreRegressionValue(t, key, time.Minute)
			memory := NewMemoryValueStore(16)
			if err := memory.Put(keyID, &expired); err != nil {
				t.Fatal(err)
			}

			entered, release := make(chan struct{}), make(chan struct{})
			hook := func() { close(entered); <-release }
			store := &blockingStoreRegressionStore{ValueStore: memory}
			if path == "read" {
				store.getHook = hook
			} else {
				store.scanHook = hook
			}
			server := newStoreRegressionServer(t, store)
			removeExpired := func() error {
				if path == "read" {
					_, err := server.getStoredValue(keyID)
					return err
				}
				server.cleanupStore(time.Now().Unix())
				return nil
			}
			runOverlappingStoreOperations(t, entered, release, removeExpired,
				func() error { return server.storeIn(keyID, &fresh) })

			got, err := memory.Get(keyID)
			if err != nil {
				t.Fatal(err)
			}
			if got == nil || got.TTL != fresh.TTL {
				t.Fatalf("expired deletion removed refreshed value: got=%+v", got)
			}
		})
	}
}

func signedStoreRegressionOverlayNode(t *testing.T, overlayKey []byte, version int32) overlay.Node {
	t.Helper()

	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	node, err := overlay.NewNode(overlayKey, key)
	if err != nil {
		t.Fatal(err)
	}
	node.Version = version
	if err := node.Sign(key); err != nil {
		t.Fatal(err)
	}
	return *node
}

func storeRegressionOverlayValue(t *testing.T, overlayKey []byte, ttl time.Duration, nodes ...overlay.Node) (Value, []byte) {
	t.Helper()

	id := keys.PublicKeyOverlay{Key: overlayKey}
	idHash, err := tl.Hash(id)
	if err != nil {
		t.Fatal(err)
	}
	data, err := tl.Serialize(overlay.NodesList{List: nodes}, true)
	if err != nil {
		t.Fatal(err)
	}
	value := Value{
		KeyDescription: KeyDescription{
			Key:        Key{ID: idHash, Name: []byte("nodes"), Index: 0},
			ID:         id,
			UpdateRule: UpdateRuleOverlayNodes{},
		},
		Data: data,
		TTL:  int32(time.Now().Add(ttl).Unix()),
	}
	keyID, err := tl.Hash(value.KeyDescription.Key)
	if err != nil {
		t.Fatal(err)
	}
	return value, keyID
}

func checkStoreRegressionOverlayNodes(t *testing.T, value *Value, want ...overlay.Node) {
	t.Helper()

	if value == nil {
		t.Fatal("missing overlay value")
	}
	var nodes overlay.NodesList
	if _, err := tl.Parse(&nodes, value.Data, true); err != nil {
		t.Fatal(err)
	}
	if len(nodes.List) != len(want) {
		t.Fatalf("stored %d nodes, want %d", len(nodes.List), len(want))
	}
	for _, expected := range want {
		found := false
		for _, node := range nodes.List {
			if reflect.DeepEqual(node, expected) {
				found = true
				if err := node.CheckSignature(); err != nil {
					t.Fatal(err)
				}
				break
			}
		}
		if !found {
			t.Fatalf("node metadata or signature changed: want=%+v, got=%+v", expected, nodes.List)
		}
	}
}

func TestServerOverlayStoreRejectsFutureVersion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		key := []byte("future-overlay")
		node := signedStoreRegressionOverlayNode(t, key, int32(time.Now().Unix()+_MaxOverlayNodeFutureSec+1))
		value, keyID := storeRegressionOverlayValue(t, key, time.Minute, node)
		server := newStoreRegressionServer(t, NewMemoryValueStore(16))
		peer := &mockPeer{}
		err := server.handleQuery(peer, &adnl.MessageQuery{ID: []byte{1}, Data: Store{Value: &value}})
		if err == nil || peer.answered != nil {
			t.Fatalf("future node accepted: err=%v, answer=%T", err, peer.answered)
		}
		got, err := server.store.Get(keyID)
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Fatal("future node entered storage")
		}
	})
}

func TestServerOverlayStoreFiltersAgesAndKeepsBoundarySignatures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		key := []byte("age-overlay")
		now := time.Now().Unix()
		stale := signedStoreRegressionOverlayNode(t, key, int32(now-_MaxOverlayNodeAgeSec))
		oldest := signedStoreRegressionOverlayNode(t, key, int32(now-_MaxOverlayNodeAgeSec+1))
		newest := signedStoreRegressionOverlayNode(t, key, int32(now+_MaxOverlayNodeFutureSec))
		value, keyID := storeRegressionOverlayValue(t, key, time.Minute, stale, oldest, newest)
		server := newStoreRegressionServer(t, NewMemoryValueStore(16))
		if err := server.storeIn(keyID, &value); err != nil {
			t.Fatal(err)
		}
		got, err := server.store.Get(keyID)
		if err != nil {
			t.Fatal(err)
		}
		checkStoreRegressionOverlayNodes(t, got, oldest, newest)
	})
}

func TestServerStoredOverlayPoisonIsRemoved(t *testing.T) {
	for _, operation := range []string{"read", "merge"} {
		t.Run(operation, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				key := []byte("poisoned-overlay")
				now := time.Now().Unix()
				honest := signedStoreRegressionOverlayNode(t, key, int32(now))
				future := signedStoreRegressionOverlayNode(t, key, 2147483647)
				stale := signedStoreRegressionOverlayNode(t, key, int32(now-_MaxOverlayNodeAgeSec))
				poison, keyID := storeRegressionOverlayValue(t, key, time.Minute, honest, future, stale)
				memory := NewMemoryValueStore(16)
				if err := memory.Put(keyID, &poison); err != nil {
					t.Fatal(err)
				}
				server := newStoreRegressionServer(t, memory)
				if operation == "read" {
					value, err := server.getStoredValue(keyID)
					if err != nil {
						t.Fatal(err)
					}
					checkStoreRegressionOverlayNodes(t, value, honest)
				} else {
					incoming, _ := storeRegressionOverlayValue(t, key, 2*time.Minute, honest)
					if err := server.storeIn(keyID, &incoming); err != nil {
						t.Fatal(err)
					}
				}
				stored, err := memory.Get(keyID)
				if err != nil {
					t.Fatal(err)
				}
				checkStoreRegressionOverlayNodes(t, stored, honest)
			})
		})
	}
}

func TestServerOverlayEmptyValuesAreRemoved(t *testing.T) {
	for _, operation := range []string{"initial stale", "read empty", "read poison", "merge stale"} {
		t.Run(operation, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				key := []byte("empty-overlay")
				stale := signedStoreRegressionOverlayNode(t, key, int32(time.Now().Unix()-_MaxOverlayNodeAgeSec))
				future := signedStoreRegressionOverlayNode(t, key, 2147483647)
				value, keyID := storeRegressionOverlayValue(t, key, time.Minute, stale)
				memory := NewMemoryValueStore(16)
				server := newStoreRegressionServer(t, memory)
				switch operation {
				case "initial stale":
					if err := server.storeIn(keyID, &value); err != nil {
						t.Fatal(err)
					}
				case "read empty", "read poison":
					stored, _ := storeRegressionOverlayValue(t, key, time.Minute)
					if operation == "read poison" {
						stored, _ = storeRegressionOverlayValue(t, key, time.Minute, future)
					}
					if err := memory.Put(keyID, &stored); err != nil {
						t.Fatal(err)
					}
					got, err := server.getStoredValue(keyID)
					if err != nil || got != nil {
						t.Fatalf("empty overlay returned: value=%+v, err=%v", got, err)
					}
				case "merge stale":
					if err := memory.Put(keyID, &value); err != nil {
						t.Fatal(err)
					}
					incoming, _ := storeRegressionOverlayValue(t, key, 2*time.Minute, stale)
					if err := server.storeIn(keyID, &incoming); err != nil {
						t.Fatal(err)
					}
				}
				got, err := memory.Get(keyID)
				if err != nil || got != nil {
					t.Fatalf("empty overlay remains stored: value=%+v, err=%v", got, err)
				}
			})
		})
	}
}

func TestServerOverlayExpiredValueIsNotMerged(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		key := []byte("expired-overlay")
		now := int32(time.Now().Unix())
		previous := signedStoreRegressionOverlayNode(t, key, now)
		incoming := signedStoreRegressionOverlayNode(t, key, now)
		expired, keyID := storeRegressionOverlayValue(t, key, -time.Second, previous)
		fresh, _ := storeRegressionOverlayValue(t, key, time.Minute, incoming)
		memory := NewMemoryValueStore(16)
		if err := memory.Put(keyID, &expired); err != nil {
			t.Fatal(err)
		}
		server := newStoreRegressionServer(t, memory)
		if err := server.storeIn(keyID, &fresh); err != nil {
			t.Fatal(err)
		}
		value, err := memory.Get(keyID)
		if err != nil {
			t.Fatal(err)
		}
		checkStoreRegressionOverlayNodes(t, value, incoming)
	})
}

func TestMergeOverlayNodesBoundedAndPreservesSignatures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		key := []byte("bounded-overlay")
		now := time.Now().Unix()
		var candidates []overlay.Node
		for i := 0; i < 10; i++ {
			candidates = append(candidates, signedStoreRegressionOverlayNode(t, key, int32(now+int64(i))))
		}
		left, _ := storeRegressionOverlayValue(t, key, time.Minute, candidates[:5]...)
		right, _ := storeRegressionOverlayValue(t, key, time.Minute, candidates[5:]...)
		data, err := mergeOverlayNodesData(left.Data, right.Data)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) > _MaxValueSize {
			t.Fatalf("merged data exceeds limit: %d", len(data))
		}
		var result overlay.NodesList
		if _, err := tl.Parse(&result, data, true); err != nil {
			t.Fatal(err)
		}
		if len(result.List) != 5 {
			t.Fatalf("retained %d nodes, want 5", len(result.List))
		}
		seen := map[string]bool{}
		for _, node := range result.List {
			id, err := tl.Hash(node.ID)
			if err != nil {
				t.Fatal(err)
			}
			if seen[string(id)] {
				t.Fatal("duplicate retained peer")
			}
			seen[string(id)] = true
			found := false
			for _, original := range candidates {
				if reflect.DeepEqual(node, original) {
					found = true
					break
				}
			}
			if !found {
				t.Fatal("bounded merge changed a signed record")
			}
			if err := node.CheckSignature(); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestMergeOverlayNodesKeepsNewestRecordPerIdentity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		key := []byte("duplicate-overlay")
		_, privateKey, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		newest, err := overlay.NewNode(key, privateKey)
		if err != nil {
			t.Fatal(err)
		}
		older := *newest
		older.Version--
		if err := older.Sign(privateKey); err != nil {
			t.Fatal(err)
		}
		left, _ := storeRegressionOverlayValue(t, key, time.Minute, older, *newest)
		right, _ := storeRegressionOverlayValue(t, key, time.Minute, older)
		for _, pair := range [][2][]byte{{left.Data, right.Data}, {right.Data, left.Data}} {
			data, err := mergeOverlayNodesData(pair[0], pair[1])
			if err != nil {
				t.Fatal(err)
			}
			left.Data = data
			checkStoreRegressionOverlayNodes(t, &left, *newest)
		}
	})
}
