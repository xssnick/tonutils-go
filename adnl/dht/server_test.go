package dht

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

type mockPeer struct {
	id         []byte
	pub        ed25519.PublicKey
	remoteAddr string

	answeredQueryID []byte
	answered        tl.Serializable
}

func (m *mockPeer) SetCustomMessageHandler(handler func(msg *adnl.MessageCustom) error) {}
func (m *mockPeer) SetQueryHandler(handler func(msg *adnl.MessageQuery) error)          {}
func (m *mockPeer) GetDisconnectHandler() func(addr string, key ed25519.PublicKey) {
	return nil
}
func (m *mockPeer) SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey)) {}
func (m *mockPeer) SendCustomMessage(ctx context.Context, req tl.Serializable) error      { return nil }
func (m *mockPeer) Query(ctx context.Context, req, result tl.Serializable) error          { return nil }
func (m *mockPeer) Answer(ctx context.Context, queryID []byte, result tl.Serializable) error {
	m.answeredQueryID = append([]byte{}, queryID...)
	m.answered = result
	return nil
}
func (m *mockPeer) Ping(ctx context.Context) (time.Duration, error) { return 0, nil }
func (m *mockPeer) GetQueryHandler() func(msg *adnl.MessageQuery) error {
	return nil
}
func (m *mockPeer) GetCloserCtx() context.Context       { return context.Background() }
func (m *mockPeer) SetAddresses(addresses address.List) {}
func (m *mockPeer) RemoteAddr() string                  { return m.remoteAddr }
func (m *mockPeer) GetID() []byte                       { return append([]byte{}, m.id...) }
func (m *mockPeer) GetPubKey() ed25519.PublicKey        { return append(ed25519.PublicKey(nil), m.pub...) }
func (m *mockPeer) Reinit()                             {}
func (m *mockPeer) Close()                              {}

func newMockPeerFromNode(t *testing.T, node *Node, remoteAddr string) *mockPeer {
	t.Helper()

	pub, ok := node.ID.(keys.PublicKeyED25519)
	if !ok {
		t.Fatalf("unsupported node key type %T", node.ID)
	}

	id, err := tl.Hash(node.ID)
	if err != nil {
		t.Fatal(err)
	}

	return &mockPeer{
		id:         id,
		pub:        append(ed25519.PublicKey(nil), pub.Key...),
		remoteAddr: remoteAddr,
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()

	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	pub := key.Public().(ed25519.PublicKey)
	id, err := tl.Hash(keys.PublicKeyED25519{Key: pub})
	if err != nil {
		t.Fatal(err)
	}

	gw := &MockGateway{
		pub: pub,
		id:  id,
		addresses: address.List{
			Addresses: []address.Address{
				&address.UDP{
					IP:   net.IPv4(127, 0, 0, 1).To4(),
					Port: 17555,
				},
			},
			Version:    1,
			ReinitDate: 1,
		},
	}

	server, err := NewServer(gw, key, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestServer_HandleQuery_Ping(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	node, err := newCorrectNode(1, 2, 3, 4, 17001)
	if err != nil {
		t.Fatal(err)
	}
	peer := newMockPeerFromNode(t, node, "1.2.3.4:17001")

	err = server.handleQuery(peer, &adnl.MessageQuery{
		ID:   make([]byte, 32),
		Data: Ping{ID: 77},
	})
	if err != nil {
		t.Fatal(err)
	}

	res, ok := peer.answered.(Pong)
	if !ok {
		t.Fatalf("unexpected answer type %T", peer.answered)
	}
	if res.ID != 77 {
		t.Fatalf("unexpected pong id %d", res.ID)
	}
}

func TestServer_HandleQuery_StoreAndFindValue(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	pubNode, err := newCorrectNode(1, 2, 3, 4, 17002)
	if err != nil {
		t.Fatal(err)
	}
	peer := newMockPeerFromNode(t, pubNode, "1.2.3.4:17002")

	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	addrList := address.List{
		Addresses: []address.Address{
			&address.UDP{
				IP:   net.IPv4(9, 9, 9, 9).To4(),
				Port: 9999,
			},
		},
	}
	data, err := tl.Serialize(addrList, true)
	if err != nil {
		t.Fatal(err)
	}

	val, keyID, err := buildStoreValue(
		keys.PublicKeyED25519{Key: pub},
		[]byte("address"),
		0,
		data,
		UpdateRuleSignature{},
		time.Minute,
		key,
	)
	if err != nil {
		t.Fatal(err)
	}

	err = server.handleQuery(peer, &adnl.MessageQuery{
		ID:   make([]byte, 32),
		Data: Store{Value: &val},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = server.handleQuery(peer, &adnl.MessageQuery{
		ID:   make([]byte, 32),
		Data: FindValue{Key: keyID, K: 3},
	})
	if err != nil {
		t.Fatal(err)
	}

	res, ok := peer.answered.(ValueFoundResult)
	if !ok {
		t.Fatalf("unexpected answer type %T", peer.answered)
	}
	if !reflect.DeepEqual(res.Value, val) {
		t.Fatalf("unexpected stored value")
	}
}

type failingValueStore struct{}

func (f *failingValueStore) Get(keyID []byte) (*Value, error) { return nil, nil }
func (f *failingValueStore) Put(keyID []byte, value *Value) error {
	return fmt.Errorf("put failed")
}
func (f *failingValueStore) Delete(keyID []byte) error                               { return nil }
func (f *failingValueStore) ForEach(fn func(keyID []byte, value *Value) error) error { return nil }
func (f *failingValueStore) Close() error                                            { return nil }

func TestServer_StoreReturnsLocalStoreError(t *testing.T) {
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	pub := key.Public().(ed25519.PublicKey)
	id, err := tl.Hash(keys.PublicKeyED25519{Key: pub})
	if err != nil {
		t.Fatal(err)
	}

	gw := &MockGateway{
		pub: pub,
		id:  id,
		addresses: address.List{
			Addresses: []address.Address{
				&address.UDP{
					IP:   net.IPv4(127, 0, 0, 1).To4(),
					Port: 17555,
				},
			},
			Version:    1,
			ReinitDate: 1,
		},
	}

	server, err := NewServer(gw, key, nil, &failingValueStore{})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	ownerPub, ownerKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = server.Store(context.Background(), keys.PublicKeyED25519{Key: ownerPub}, []byte("address"), 0, []byte("value"), UpdateRuleSignature{}, time.Minute, ownerKey)
	if err == nil || err.Error() != "put failed" {
		t.Fatalf("unexpected store error: %v", err)
	}
	if len(server.ourValues) != 0 {
		t.Fatalf("unexpected cached values after failed local store: %d", len(server.ourValues))
	}
}

func TestServer_CleanupExpiredValuesDoesNotBlockFindValue(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	keyID := []byte("expired-key")
	if err := server.store.Put(keyID, &Value{TTL: int32(time.Now().Add(-time.Second).Unix())}); err != nil {
		t.Fatal(err)
	}

	server.cleanup()

	node, err := newCorrectNode(1, 2, 3, 4, 17012)
	if err != nil {
		t.Fatal(err)
	}
	peer := newMockPeerFromNode(t, node, "1.2.3.4:17012")

	done := make(chan error, 1)
	go func() {
		done <- server.handleQuery(peer, &adnl.MessageQuery{
			ID:   make([]byte, 32),
			Data: FindValue{Key: keyID, K: 1},
		})
	}()

	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("findValue blocked after cleanup")
	}

	if _, ok := peer.answered.(ValueNotFoundResult); !ok {
		t.Fatalf("unexpected answer type %T", peer.answered)
	}
}

func TestServer_RefreshNodesUsesParallelWorkers(t *testing.T) {
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	pub := key.Public().(ed25519.PublicKey)
	id, err := tl.Hash(keys.PublicKeyED25519{Key: pub})
	if err != nil {
		t.Fatal(err)
	}

	var current int32
	var maxConcurrent int32

	gw := &MockGateway{
		pub: pub,
		id:  id,
		addresses: address.List{
			Addresses: []address.Address{
				&address.UDP{
					IP:   net.IPv4(127, 0, 0, 1).To4(),
					Port: 17555,
				},
			},
			Version:    1,
			ReinitDate: 1,
		},
	}
	gw.reg = func(addr string, key ed25519.PublicKey) (adnl.Peer, error) {
		return &MockADNL{
			query: func(ctx context.Context, req, result tl.Serializable) error {
				cur := atomic.AddInt32(&current, 1)
				for {
					max := atomic.LoadInt32(&maxConcurrent)
					if cur <= max || atomic.CompareAndSwapInt32(&maxConcurrent, max, cur) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				atomic.AddInt32(&current, -1)

				raw, ok := req.(tl.Raw)
				if !ok {
					return nil
				}
				var q Query
				payload, err := tl.Parse(&q, raw, true)
				if err == nil {
					var signed SignedAddressListQuery
					if _, err = tl.Parse(&signed, payload, true); err == nil {
						node, err := newCorrectNode(1, 2, 3, 4, 17050)
						if err != nil {
							return err
						}
						reflect.ValueOf(result).Elem().Set(reflect.ValueOf(*node))
						return nil
					}
				}
				return nil
			},
		}, nil
	}

	server, err := NewServer(gw, key, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	for i := 0; i < 8; i++ {
		node, err := newCorrectNode(1, 2, 3, byte(10+i), int32(17100+i))
		if err != nil {
			t.Fatal(err)
		}
		kn, err := server.addNodeWithStatus(node, true)
		if err != nil {
			t.Fatal(err)
		}
		atomic.StoreInt64(&kn.lastPingAt, 0)
	}

	server.refreshNodes()

	if atomic.LoadInt32(&maxConcurrent) <= 1 {
		t.Fatalf("expected concurrent refreshes, max concurrency = %d", maxConcurrent)
	}
}

func TestServer_HandleQuery_GetSignedAddressList(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	node, err := newCorrectNode(1, 2, 3, 4, 17003)
	if err != nil {
		t.Fatal(err)
	}
	peer := newMockPeerFromNode(t, node, "1.2.3.4:17003")

	err = server.handleQuery(peer, &adnl.MessageQuery{
		ID:   make([]byte, 32),
		Data: SignedAddressListQuery{},
	})
	if err != nil {
		t.Fatal(err)
	}

	res, ok := peer.answered.(Node)
	if !ok {
		t.Fatalf("unexpected answer type %T", peer.answered)
	}
	if err = res.CheckSignatureWithNetworkID(server.networkID); err != nil {
		t.Fatalf("bad signed node: %v", err)
	}
	if res.AddrList == nil || len(res.AddrList.Addresses) != 1 {
		t.Fatalf("unexpected address list")
	}
}

func TestServer_HandleWrappedQueryAddsNode(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	sender, err := newCorrectNode(5, 6, 7, 8, 17004)
	if err != nil {
		t.Fatal(err)
	}
	peer := newMockPeerFromNode(t, sender, "5.6.7.8:17004")

	targetPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	targetID, err := tl.Hash(keys.PublicKeyED25519{Key: targetPub})
	if err != nil {
		t.Fatal(err)
	}

	err = server.handleQuery(peer, &adnl.MessageQuery{
		ID: make([]byte, 32),
		Data: []tl.Serializable{
			Query{Node: sender},
			FindNode{Key: targetID, K: 3},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	res, ok := peer.answered.(NodesList)
	if !ok {
		t.Fatalf("unexpected answer type %T", peer.answered)
	}
	if len(res.List) == 0 {
		t.Fatal("expected wrapped sender to be added into routing table")
	}
}

func TestServer_FindNodeReturnsOnlyActiveNodes(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	activeNode, err := newCorrectNode(10, 0, 0, 1, 17005)
	if err != nil {
		t.Fatal(err)
	}
	backupNode, err := newCorrectNode(10, 0, 0, 2, 17006)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = server.addNodeWithStatus(activeNode, true); err != nil {
		t.Fatal(err)
	}
	if _, err = server.addNodeWithStatus(backupNode, false); err != nil {
		t.Fatal(err)
	}

	targetPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	targetID, err := tl.Hash(keys.PublicKeyED25519{Key: targetPub})
	if err != nil {
		t.Fatal(err)
	}

	res := server.getNearestNodes(targetID, 10)
	if len(res) != 1 {
		t.Fatalf("expected only active nodes in response, got %d", len(res))
	}

	gotID, err := tl.Hash(res[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	wantID, err := tl.Hash(activeNode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotID, wantID) {
		t.Fatal("returned non-active node")
	}
}
