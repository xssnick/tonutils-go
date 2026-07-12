package dht

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/liteclient"
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
func (m *mockPeer) SendNop(ctx context.Context) error                                     { return nil }
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
func (m *mockPeer) Stats() adnl.PeerStats               { return adnl.PeerStats{} }
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

func TestNewServerUsesLargeDefaultMemoryStore(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	store, ok := server.store.(*MemoryValueStore)
	if !ok {
		t.Fatalf("unexpected default store type %T", server.store)
	}
	if store.maxKeys != defaultMemoryStoreMaxKeys {
		t.Fatalf("expected default store max keys %d, got %d", defaultMemoryStoreMaxKeys, store.maxKeys)
	}
	if store.maxKeys != 100000 {
		t.Fatalf("expected default store max keys 100000, got %d", store.maxKeys)
	}
}

func TestNewServerFromConfigAppliesNetworkIDBeforeStaticNodes(t *testing.T) {
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	pub := key.Public().(ed25519.PublicKey)
	id, err := tl.Hash(keys.PublicKeyED25519{Key: pub})
	if err != nil {
		t.Fatal(err)
	}

	nodePub, nodeKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	nodeNetworkID := int32(17)
	cfgNetworkID := int32(18)
	nodeAddr := net.IPv4(1, 2, 3, 4).To4()
	node, err := BuildSignedNode(
		keys.PublicKeyED25519{Key: nodePub},
		&address.List{
			Addresses: []address.Address{
				&address.UDP{
					IP:   nodeAddr,
					Port: 17556,
				},
			},
		},
		1,
		nodeNetworkID,
		nodeKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &liteclient.GlobalConfig{
		DHT: liteclient.DHTConfig{
			K:         1,
			A:         1,
			NetworkID: &cfgNetworkID,
			StaticNodes: liteclient.DHTNodes{
				Nodes: []liteclient.DHTNode{
					{
						ID: liteclient.ServerID{
							Type: "pub.ed25519",
							Key:  base64.StdEncoding.EncodeToString(nodePub),
						},
						AddrList: liteclient.DHTAddressList{
							Addrs: []liteclient.DHTAddress{
								{
									Type: "adnl.address.udp",
									IP:   int(int32(binary.BigEndian.Uint32(nodeAddr))),
									Port: 17556,
								},
							},
						},
						Version:   int(node.Version),
						Signature: base64.StdEncoding.EncodeToString(node.Signature),
					},
				},
			},
		},
	}

	server, err := NewServerFromConfig(&MockGateway{pub: pub, id: id}, key, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	if server.networkID != cfgNetworkID {
		t.Fatalf("expected network id %d, got %d", cfgNetworkID, server.networkID)
	}
	for _, bucket := range server.buckets {
		if len(bucket.getNodes()) != 0 {
			t.Fatal("wrong-network static node entered bucket")
		}
	}
}

func TestNewServerFromConfigDefaultsNetworkID(t *testing.T) {
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	server, err := NewServerFromConfig(&MockGateway{}, key, &liteclient.GlobalConfig{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	if server.networkID != _UnknownNetworkID {
		t.Fatalf("expected network id %d, got %d", _UnknownNetworkID, server.networkID)
	}
}

func TestNewServerFromConfigRejectsTooLargeKA(t *testing.T) {
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	networkID := int32(17)

	tests := []struct {
		name string
		k    int
		a    int
		want string
	}{
		{
			name: "too large k",
			k:    _maxK + 1,
			want: "bad value k=11",
		},
		{
			name: "too large a",
			a:    _maxA + 1,
			want: "bad value a=11",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewServerFromConfig(&MockGateway{}, key, &liteclient.GlobalConfig{
				DHT: liteclient.DHTConfig{
					K:         tt.k,
					A:         tt.a,
					NetworkID: &networkID,
				},
			}, nil)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("got error %v, want %q", err, tt.want)
			}
		})
	}
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

func TestServer_HandleQuery_LogsAnswerContext(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	node, err := newCorrectNode(1, 2, 3, 4, 17001)
	if err != nil {
		t.Fatal(err)
	}
	peer := newMockPeerFromNode(t, node, "1.2.3.4:17001")

	prevLogger := Logger
	defer func() {
		Logger = prevLogger
	}()

	var got []any
	Logger = func(v ...any) {
		got = append([]any{}, v...)
	}

	err = server.handleQuery(peer, &adnl.MessageQuery{
		ID:   []byte{0x01, 0x02},
		Data: Ping{ID: 77},
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []any{
		"[DHT DEBUG] answering",
		reflect.TypeOf(Pong{}),
		"to", reflect.TypeOf(Ping{}),
		"client_ip", "1.2.3.4",
		"qid", "0102",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected log args\n got: %#v\nwant: %#v", got, want)
	}
}

func TestServer_ActiveNodesCount(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	if got := server.ActiveNodesCount(); got != 0 {
		t.Fatalf("empty active nodes count = %d, want 0", got)
	}

	addActiveTestNodeInBucket(t, server, 3)
	addActiveTestNodeInBucket(t, server, 17)

	if got := server.ActiveNodesCount(); got != 2 {
		t.Fatalf("active nodes count = %d, want 2", got)
	}
}

func TestServer_RoutingTableStats(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	addActiveTestNodeInBucket(t, server, 3)
	addActiveTestNodeInBucket(t, server, 17)
	addBackupTestNodeInBucket(t, server, 18)

	stats := server.RoutingTableStats()
	if stats.ActiveNodes != 2 {
		t.Fatalf("active nodes = %d, want 2", stats.ActiveNodes)
	}
	if stats.BackupNodes != 1 {
		t.Fatalf("backup nodes = %d, want 1", stats.BackupNodes)
	}
	if stats.FilledBuckets != 2 {
		t.Fatalf("filled buckets = %d, want 2", stats.FilledBuckets)
	}
	if stats.TotalBuckets != 256 {
		t.Fatalf("total buckets = %d, want 256", stats.TotalBuckets)
	}
}

func TestServer_RoutingNodesReturnsActiveSnapshot(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	active, err := newCorrectNode(127, 0, 0, 2, 17701)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := newCorrectNode(127, 0, 0, 3, 17702)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = server.addNodeWithStatus(active, true); err != nil {
		t.Fatal(err)
	}
	if _, err = server.addNodeWithStatus(backup, false); err != nil {
		t.Fatal(err)
	}

	nodes := server.RoutingNodes()
	if len(nodes) != 1 {
		t.Fatalf("routing nodes = %d, want 1 active node", len(nodes))
	}
	for _, node := range nodes {
		if node == nil {
			t.Fatal("routing nodes snapshot contains nil")
		}
	}
	gotKey := nodes[0].ID.(keys.PublicKeyED25519)
	wantKey := active.ID.(keys.PublicKeyED25519)
	if !bytes.Equal(gotKey.Key, wantKey.Key) {
		t.Fatal("routing nodes snapshot returned backup node")
	}
}

func TestServer_QueryHook(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	node, err := newCorrectNode(1, 2, 3, 4, 17001)
	if err != nil {
		t.Fatal(err)
	}
	peer := newMockPeerFromNode(t, node, "1.2.3.4:17001")

	type event struct {
		method string
		err    bool
	}
	var events []event
	server.SetQueryHook(func(method string, err error) {
		events = append(events, event{method: method, err: err != nil})
	})

	err = server.handleQuery(peer, &adnl.MessageQuery{
		ID:   []byte{0x01},
		Data: Ping{ID: 77},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = server.handleQuery(peer, &adnl.MessageQuery{
		ID:   []byte{0x02},
		Data: RegisterReverseConnection{},
	})
	if !errors.Is(err, errReverseConnectionsDisabled) {
		t.Fatalf("unexpected reverse connection error: %v", err)
	}

	want := []event{
		{method: "ping"},
		{method: "registerReverseConnection", err: true},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("unexpected query hook events\n got: %#v\nwant: %#v", events, want)
	}
}

func TestServer_QueryAdmissionHookRejectsBeforeStoreValidation(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	node, err := newCorrectNode(1, 2, 3, 4, 17001)
	if err != nil {
		t.Fatal(err)
	}
	peer := newMockPeerFromNode(t, node, "1.2.3.4:17001")

	rejectErr := errors.New("query rejected")
	var hookCalled bool
	server.SetQueryAdmissionHook(func(gotPeer adnl.Peer, method string) error {
		hookCalled = true
		if gotPeer.RemoteAddr() != peer.RemoteAddr() {
			t.Fatalf("expected peer addr %s, got %s", peer.RemoteAddr(), gotPeer.RemoteAddr())
		}
		if method != "store" {
			t.Fatalf("method = %q, want store", method)
		}
		return rejectErr
	})

	err = server.handleQuery(peer, &adnl.MessageQuery{
		ID:   make([]byte, 32),
		Data: Store{Value: &Value{}},
	})
	if !errors.Is(err, rejectErr) {
		t.Fatalf("expected admission error, got %v", err)
	}
	if !hookCalled {
		t.Fatal("query admission hook was not called")
	}
	if peer.answered != nil {
		t.Fatalf("rejected query was answered with %T", peer.answered)
	}
}

func TestPeerRemoteIP(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		want   string
	}{
		{
			name:   "ipv4 with port",
			remote: "1.2.3.4:17001",
			want:   "1.2.3.4",
		},
		{
			name:   "ipv6 with port",
			remote: "[2001:db8::1]:17001",
			want:   "2001:db8::1",
		},
		{
			name:   "raw",
			remote: "adnl-peer",
			want:   "adnl-peer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := peerRemoteIP(&mockPeer{remoteAddr: tt.remote})
			if got != tt.want {
				t.Fatalf("peerRemoteIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestServer_HandleQuery_ReverseQueriesDisabled(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	node, err := newCorrectNode(1, 2, 3, 4, 17002)
	if err != nil {
		t.Fatal(err)
	}
	peer := newMockPeerFromNode(t, node, "1.2.3.4:17002")

	err = server.handleQuery(peer, &adnl.MessageQuery{
		ID:   make([]byte, 32),
		Data: RegisterReverseConnection{},
	})
	if !errors.Is(err, errReverseConnectionsDisabled) {
		t.Fatalf("unexpected register reverse connection error: %v", err)
	}
	if peer.answered != nil {
		t.Fatalf("reverse connection query was answered with %T", peer.answered)
	}

	err = server.handleQuery(peer, &adnl.MessageQuery{
		ID:   make([]byte, 32),
		Data: RequestReversePing{},
	})
	if !errors.Is(err, errReverseConnectionsDisabled) {
		t.Fatalf("unexpected request reverse ping error: %v", err)
	}
	if peer.answered != nil {
		t.Fatalf("reverse ping query was answered with %T", peer.answered)
	}
}

func TestServer_HandleMessage_ReversePingContIgnored(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	server.gateway.(*MockGateway).reg = func(addr string, key ed25519.PublicKey) (adnl.Peer, error) {
		t.Fatalf("unexpected outgoing reverse ping to %s", addr)
		return nil, nil
	}

	err := server.handleMessage(&mockPeer{}, &adnl.MessageCustom{
		Data: RequestReversePingCont{
			Client: append([]byte{}, server.selfID...),
		},
	})
	if err != nil {
		t.Fatal(err)
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

func TestServer_StoreHookRejectsIncomingStore(t *testing.T) {
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

	rejectErr := errors.New("store rejected")
	var hookCalled bool
	server.SetStoreHook(func(gotPeer adnl.Peer, gotKeyID []byte, gotValue *Value) error {
		hookCalled = true
		if gotPeer.RemoteAddr() != peer.RemoteAddr() {
			t.Fatalf("expected peer addr %s, got %s", peer.RemoteAddr(), gotPeer.RemoteAddr())
		}
		if !bytes.Equal(gotKeyID, keyID) {
			t.Fatalf("expected key id %x, got %x", keyID, gotKeyID)
		}
		if !reflect.DeepEqual(gotValue, &val) {
			t.Fatal("unexpected value passed to hook")
		}
		return rejectErr
	})

	err = server.handleQuery(peer, &adnl.MessageQuery{
		ID:   make([]byte, 32),
		Data: Store{Value: &val},
	})
	if !errors.Is(err, rejectErr) {
		t.Fatalf("expected hook error, got %v", err)
	}
	if !hookCalled {
		t.Fatal("store hook was not called")
	}
	if peer.answered != nil {
		t.Fatalf("rejected store was answered with %T", peer.answered)
	}
	value, err := server.getStoredValue(keyID)
	if err != nil {
		t.Fatal(err)
	}
	if value != nil {
		t.Fatal("rejected value was stored")
	}
}

func TestServer_StoreAddressReturnsADNLID(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	node, err := newCorrectNode(1, 2, 3, 4, 17003)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.addNodeWithStatus(node, true); err != nil {
		t.Fatal(err)
	}

	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	adnlID, err := tl.Hash(keys.PublicKeyED25519{Key: pub})
	if err != nil {
		t.Fatal(err)
	}

	dhtKey, err := tl.Hash(Key{
		ID:    adnlID,
		Name:  []byte("address"),
		Index: 0,
	})
	if err != nil {
		t.Fatal(err)
	}

	var storeCalls int32
	server.gateway.(*MockGateway).reg = dhtStoreQueryMock(t, &storeCalls)

	addrList := address.List{
		Addresses: []address.Address{
			&address.UDP{
				IP:   net.IPv4(10, 20, 30, 40).To4(),
				Port: 30303,
			},
		},
	}

	stored, gotID, err := server.StoreAddress(context.Background(), addrList, time.Minute, key)
	if err != nil {
		t.Fatal(err)
	}
	if stored != 1 {
		t.Fatalf("expected 1 stored replica, got %d", stored)
	}
	if string(gotID) != string(adnlID) {
		t.Fatalf("expected adnl id %x, got %x", adnlID, gotID)
	}
	if string(gotID) == string(dhtKey) {
		t.Fatalf("StoreAddress returned internal dht key %x", gotID)
	}

	server.mx.Lock()
	_, storedUnderDHTKey := server.ourValues[string(dhtKey)]
	_, storedUnderADNLID := server.ourValues[string(adnlID)]
	server.mx.Unlock()

	if !storedUnderDHTKey {
		t.Fatal("address value was not cached under internal dht key")
	}
	if storedUnderADNLID {
		t.Fatal("address value was cached under adnl id")
	}
	if atomic.LoadInt32(&storeCalls) != 1 {
		t.Fatalf("expected 1 store call, got %d", storeCalls)
	}
}

func TestServer_StoreAddressSucceedsWithOnlyLocalStore(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	server.gateway.(*MockGateway).reg = func(addr string, key ed25519.PublicKey) (adnl.Peer, error) {
		t.Fatalf("unexpected outgoing DHT connection to %s", addr)
		return nil, nil
	}

	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	adnlID, err := tl.Hash(keys.PublicKeyED25519{Key: pub})
	if err != nil {
		t.Fatal(err)
	}

	dhtKey, err := tl.Hash(Key{
		ID:    adnlID,
		Name:  []byte("address"),
		Index: 0,
	})
	if err != nil {
		t.Fatal(err)
	}

	addrList := address.List{
		Addresses: []address.Address{
			&address.UDP{
				IP:   net.IPv4(10, 20, 30, 41).To4(),
				Port: 30304,
			},
		},
	}

	stored, gotID, err := server.StoreAddress(context.Background(), addrList, time.Minute, key)
	if err != nil {
		t.Fatal(err)
	}
	if stored != 1 {
		t.Fatalf("expected 1 local replica, got %d", stored)
	}
	if string(gotID) != string(adnlID) {
		t.Fatalf("expected adnl id %x, got %x", adnlID, gotID)
	}

	server.mx.RLock()
	_, cached := server.ourValues[string(dhtKey)]
	server.mx.RUnlock()
	if !cached {
		t.Fatal("address value was not cached under internal dht key")
	}

	value, err := server.store.Get(dhtKey)
	if err != nil {
		t.Fatal(err)
	}
	if value == nil {
		t.Fatal("address value was not stored locally")
	}

	var storedList address.List
	if _, err = tl.ParseNoCopy(&storedList, value.Data, true); err != nil {
		t.Fatal(err)
	}
	if len(storedList.Addresses) != 1 {
		t.Fatalf("expected 1 stored address, got %d", len(storedList.Addresses))
	}

	foundList, foundPub, err := server.FindAddresses(context.Background(), adnlID)
	if err != nil {
		t.Fatal(err)
	}
	if len(foundList.Addresses) != 1 {
		t.Fatalf("expected 1 found address, got %d", len(foundList.Addresses))
	}
	if string(foundPub) != string(pub) {
		t.Fatalf("expected found public key %x, got %x", pub, foundPub)
	}
}

func TestServer_StoreOverlayNodesSucceedsWithOnlyLocalStore(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	server.gateway.(*MockGateway).reg = func(addr string, key ed25519.PublicKey) (adnl.Peer, error) {
		t.Fatalf("unexpected outgoing DHT connection to %s", addr)
		return nil, nil
	}

	overlayKey := []byte("test-overlay-local")
	nodes, overlayID, _ := newTestOverlayNodes(t, overlayKey)

	stored, gotID, err := server.StoreOverlayNodes(context.Background(), overlayKey, nodes, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if stored != 1 {
		t.Fatalf("expected 1 local replica, got %d", stored)
	}
	if string(gotID) != string(overlayID) {
		t.Fatalf("expected overlay id %x, got %x", overlayID, gotID)
	}

	found, cont, err := server.FindOverlayNodes(context.Background(), overlayKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(found.List) != len(nodes.List) {
		t.Fatalf("expected %d found overlay nodes, got %d", len(nodes.List), len(found.List))
	}
	if cont == nil || !cont.checkedLocal {
		t.Fatal("expected local lookup to update continuation")
	}

	_, _, err = server.FindOverlayNodes(context.Background(), overlayKey, cont)
	if !errors.Is(err, ErrDHTValueIsNotFound) {
		t.Fatalf("expected continuation to skip local value, got %v", err)
	}
}

func TestServer_StoreOverlayNodesReturnsOverlayID(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	node, err := newCorrectNode(1, 2, 3, 4, 17003)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.addNodeWithStatus(node, true); err != nil {
		t.Fatal(err)
	}

	overlayKey := []byte("test-overlay")
	nodes, overlayID, dhtKey := newTestOverlayNodes(t, overlayKey)

	var storeCalls int32
	server.gateway.(*MockGateway).reg = dhtStoreQueryMock(t, &storeCalls)

	stored, gotID, err := server.StoreOverlayNodes(context.Background(), overlayKey, nodes, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if stored != 1 {
		t.Fatalf("expected 1 stored replica, got %d", stored)
	}
	if string(gotID) != string(overlayID) {
		t.Fatalf("expected overlay id %x, got %x", overlayID, gotID)
	}
	if string(gotID) == string(dhtKey) {
		t.Fatalf("StoreOverlayNodes returned internal dht key %x", gotID)
	}

	server.mx.Lock()
	_, storedUnderDHTKey := server.ourValues[string(dhtKey)]
	_, storedUnderOverlayID := server.ourValues[string(overlayID)]
	server.mx.Unlock()

	if !storedUnderDHTKey {
		t.Fatal("overlay value was not cached under internal dht key")
	}
	if storedUnderOverlayID {
		t.Fatal("overlay value was cached under overlay id")
	}
	if atomic.LoadInt32(&storeCalls) != 1 {
		t.Fatalf("expected 1 store call, got %d", storeCalls)
	}
}

func TestServer_HandleQuery_StoreRejectsTooLargeTTL(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	pubNode, err := newCorrectNode(1, 2, 3, 4, 17003)
	if err != nil {
		t.Fatal(err)
	}
	peer := newMockPeerFromNode(t, pubNode, "1.2.3.4:17003")

	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	val, _, err := buildStoreValue(
		keys.PublicKeyED25519{Key: pub},
		[]byte("address"),
		0,
		[]byte("value"),
		UpdateRuleSignature{},
		time.Minute,
		key,
	)
	if err != nil {
		t.Fatal(err)
	}
	val.TTL = int32(time.Now().Add(time.Duration(_MaxValueTTLSec+1) * time.Second).Unix())

	err = server.handleQuery(peer, &adnl.MessageQuery{
		ID:   make([]byte, 32),
		Data: Store{Value: &val},
	})
	if err == nil {
		t.Fatal("got error nil, want ttl error")
	}
	if !strings.Contains(err.Error(), "ttl is too big") {
		t.Fatalf("got unexpected error %q", err.Error())
	}
}

func TestServer_ShouldStoreLocallyMatchesCppRule(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()
	server.k = 2

	keyID := make([]byte, 32)
	id := func(v byte) []byte {
		out := make([]byte, 32)
		out[31] = v
		return out
	}
	nearest := []*dhtNode{
		{adnlId: id(1)},
		{adnlId: id(2)},
	}

	if !server.shouldStoreLocally(keyID, nearest[:1]) {
		t.Fatal("should store locally when fewer than k nearest nodes are known")
	}

	server.selfID = id(3)
	if server.shouldStoreLocally(keyID, nearest) {
		t.Fatal("should not store locally when self is farther than worst nearest node")
	}

	server.selfID = id(0)
	if !server.shouldStoreLocally(keyID, nearest) {
		t.Fatal("should store locally when self is closer than worst nearest node")
	}
}

func TestServer_RepublishOwnedValueEvenWhenNotClosest(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	val, keyID := newSignedTestValue(t)
	addActiveTestNodeInBucket(t, server, firstDistanceBit(t, keyID, server.selfID, true))
	if dist := server.distance(keyID, server.k+10); dist == 0 {
		t.Fatal("test setup error: expected self not to be closest")
	}

	var storeCalls int32
	server.gateway.(*MockGateway).reg = dhtStoreQueryMock(t, &storeCalls)

	server.mx.Lock()
	server.ourValues[string(keyID)] = cloneValue(&val)
	server.mx.Unlock()

	server.republishValues()

	if atomic.LoadInt32(&storeCalls) == 0 {
		t.Fatal("owned value was not republished")
	}
}

func TestServer_RepublishStoredSignatureValueWhenClosest(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	val, keyID := newSignedTestValue(t)
	addActiveTestNodeInBucket(t, server, firstDistanceBit(t, keyID, server.selfID, false))
	if dist := server.distance(keyID, server.k+10); dist != 0 {
		t.Fatalf("test setup error: expected self to be closest, dist=%d", dist)
	}

	var storeCalls int32
	server.gateway.(*MockGateway).reg = dhtStoreQueryMock(t, &storeCalls)

	if err := server.store.Put(keyID, &val); err != nil {
		t.Fatal(err)
	}

	server.republishValues()

	if atomic.LoadInt32(&storeCalls) == 0 {
		t.Fatal("stored signature value was not republished")
	}
}

func TestServer_RepublishStoredValuesIncrementally(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	firstValue, firstKeyID := newSignedTestValue(t)
	secondValue, secondKeyID := newSignedTestValue(t)
	bit := firstSharedDistanceBit(t, server.selfID, false, firstKeyID, secondKeyID)
	addActiveTestNodeInBucket(t, server, bit)
	if dist := server.distance(firstKeyID, server.k+10); dist != 0 {
		t.Fatalf("test setup error: expected self to be closest to first value, dist=%d", dist)
	}
	if dist := server.distance(secondKeyID, server.k+10); dist != 0 {
		t.Fatalf("test setup error: expected self to be closest to second value, dist=%d", dist)
	}

	var storeCalls int32
	server.gateway.(*MockGateway).reg = dhtStoreQueryMock(t, &storeCalls)

	if err := server.store.Put(firstKeyID, &firstValue); err != nil {
		t.Fatal(err)
	}
	if err := server.store.Put(secondKeyID, &secondValue); err != nil {
		t.Fatal(err)
	}

	server.republishValues()
	if got := atomic.LoadInt32(&storeCalls); got != 1 {
		t.Fatalf("expected 1 stored value republished after first tick, got %d", got)
	}

	server.republishValues()
	if got := atomic.LoadInt32(&storeCalls); got != 2 {
		t.Fatalf("expected 2 stored values republished after second tick, got %d", got)
	}
}

func TestServer_RepublishStoredSkipsFarValuesWithoutStarvingTick(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	nearValue, nearKeyID := newSignedTestValue(t)
	farKeyID := append([]byte{}, server.selfID...)
	for _, bit := range sharedDistanceBits(t, server.selfID, server.k+10, nearKeyID) {
		farKeyID[bit/8] ^= byte(1 << uint(7-(bit%8)))
		addActiveTestNodeInBucket(t, server, bit)
	}
	if dist := server.distance(farKeyID, server.k+10); dist < server.k+10 {
		t.Fatalf("test setup error: expected far value distance >= %d, got %d", server.k+10, dist)
	}
	if dist := server.distance(nearKeyID, server.k+10); dist != 0 {
		t.Fatalf("test setup error: expected self to be closest to near value, dist=%d", dist)
	}

	var storeCalls int32
	server.gateway.(*MockGateway).reg = dhtStoreQueryMock(t, &storeCalls)

	if err := server.store.Put(farKeyID, &nearValue); err != nil {
		t.Fatal(err)
	}
	if err := server.store.Put(nearKeyID, &nearValue); err != nil {
		t.Fatal(err)
	}

	server.republishStoreKeys = [][]byte{farKeyID, nearKeyID}
	server.republishStoreIndex = 0
	server.republishValues()

	if got := atomic.LoadInt32(&storeCalls); got == 0 {
		t.Fatal("near value was not republished in same tick")
	}
	value, err := server.store.Get(farKeyID)
	if err != nil {
		t.Fatal(err)
	}
	if value != nil {
		t.Fatal("far value was not deleted")
	}
}

func TestServer_FillBucketsRunsFindNode(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	node, err := newCorrectNode(1, 2, 3, 4, 18001)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.addNodeWithStatus(node, true); err != nil {
		t.Fatal(err)
	}

	var findNodeCalls int32
	server.gateway.(*MockGateway).reg = func(addr string, key ed25519.PublicKey) (adnl.Peer, error) {
		return MockADNL{
			query: func(ctx context.Context, req, result tl.Serializable) error {
				raw, ok := req.(tl.Raw)
				if !ok {
					return fmt.Errorf("unexpected request type %T", req)
				}

				payload := []byte(raw)
				var prefix Query
				if rest, err := tl.Parse(&prefix, raw, true); err == nil {
					payload = rest
				}

				var findNode FindNode
				if _, err := tl.Parse(&findNode, payload, true); err != nil {
					return err
				}
				atomic.AddInt32(&findNodeCalls, 1)
				reflect.ValueOf(result).Elem().Set(reflect.ValueOf(NodesList{}))
				return nil
			},
		}, nil
	}

	server.fillBuckets()
	if got := atomic.LoadInt32(&findNodeCalls); got == 0 {
		t.Fatal("fillBuckets did not send findNode query")
	}
}

func TestServer_BucketFillKeysPreferEmptyBuckets(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	keys := server.bucketFillKeys(3)
	if len(keys) != 3 {
		t.Fatalf("bucket fill keys = %d, want 3", len(keys))
	}
	for i, keyID := range keys {
		if got := affinity(keyID, server.selfID); got != uint(i) {
			t.Fatalf("bucket fill key %d targets bucket %d, want %d", i, got, i)
		}
	}

	server.fillBucketCursor = 0
	addActiveTestNodeInBucket(t, server, 0)
	keys = server.bucketFillKeys(1)
	if len(keys) != 1 {
		t.Fatalf("bucket fill keys = %d, want 1", len(keys))
	}
	if got := affinity(keys[0], server.selfID); got != 1 {
		t.Fatalf("bucket fill key targets bucket %d, want next empty bucket 1", got)
	}
}

func TestServer_BucketFillKeysUseUnderfilledBucketsWhenNoEmptyBuckets(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	for bit := range server.buckets {
		addActiveTestNodeInBucket(t, server, bit)
	}
	server.fillBucketCursor = 0

	keys := server.bucketFillKeys(1)
	if len(keys) != 1 {
		t.Fatalf("bucket fill keys = %d, want 1", len(keys))
	}
	if got := affinity(keys[0], server.selfID); got != 0 {
		t.Fatalf("bucket fill key targets bucket %d, want underfilled bucket 0", got)
	}
}

func TestServer_StartupFillBucketsRunsBroadFindNode(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	node, err := newCorrectNode(1, 2, 3, 4, 18001)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.addNodeWithStatus(node, true); err != nil {
		t.Fatal(err)
	}

	var findNodeCalls int32
	server.gateway.(*MockGateway).reg = func(addr string, key ed25519.PublicKey) (adnl.Peer, error) {
		return MockADNL{
			query: func(ctx context.Context, req, result tl.Serializable) error {
				raw, ok := req.(tl.Raw)
				if !ok {
					return fmt.Errorf("unexpected request type %T", req)
				}

				payload := []byte(raw)
				var prefix Query
				if rest, err := tl.Parse(&prefix, raw, true); err == nil {
					payload = rest
				}

				var findNode FindNode
				if _, err := tl.Parse(&findNode, payload, true); err != nil {
					return err
				}
				atomic.AddInt32(&findNodeCalls, 1)
				reflect.ValueOf(result).Elem().Set(reflect.ValueOf(NodesList{}))
				return nil
			},
		}, nil
	}

	server.startupFillBuckets()
	if got := atomic.LoadInt32(&findNodeCalls); got < 2 {
		t.Fatalf("startupFillBuckets sent %d findNode queries, want broad startup search", got)
	}
}

func TestBucketFillKeyTargetsBucketBit(t *testing.T) {
	selfID := bytes.Repeat([]byte{0x80}, 32)
	for _, bit := range []int{0, 1, 8, 64, 128, 255} {
		keyID := bucketFillKey(selfID, bit)
		if got := affinity(keyID, selfID); got != uint(bit) {
			t.Fatalf("bucketFillKey bit %d has affinity %d", bit, got)
		}
	}
}

func TestServer_JitteredIntervalBounds(t *testing.T) {
	base := 10 * time.Second
	jitter := time.Second
	for i := 0; i < 100; i++ {
		got := jitteredInterval(base, jitter)
		if got < base || got >= base+jitter {
			t.Fatalf("got jittered interval %s outside [%s, %s)", got, base, base+jitter)
		}
	}
	if got := jitteredInterval(base, 0); got != base {
		t.Fatalf("expected base interval without jitter, got %s", got)
	}
}

func TestServer_RandomBucketFillKeyCopiesFirstSelfBit(t *testing.T) {
	selfID := bytes.Repeat([]byte{0x80}, 32)
	for i := 0; i < 100; i++ {
		keyID := randomBucketFillKey(selfID)
		if len(keyID) != 32 {
			t.Fatalf("expected 32-byte key, got %d", len(keyID))
		}
		if !bitAt(keyID, 0) {
			t.Fatal("fill key did not copy first self bit")
		}
	}
}

func newSignedTestValue(t *testing.T) (Value, []byte) {
	t.Helper()

	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	val, keyID, err := buildStoreValue(
		keys.PublicKeyED25519{Key: pub},
		[]byte("address"),
		0,
		[]byte("value"),
		UpdateRuleSignature{},
		2*time.Minute,
		key,
	)
	if err != nil {
		t.Fatal(err)
	}
	return val, keyID
}

func firstDistanceBit(t *testing.T, keyID, selfID []byte, wantDifferent bool) int {
	t.Helper()

	for bit := 0; bit < 256; bit++ {
		if xorBit(keyID, selfID, bit) == wantDifferent {
			return bit
		}
	}
	t.Fatal("failed to find suitable distance bit")
	return 0
}

func firstSharedDistanceBit(t *testing.T, selfID []byte, wantDifferent bool, keyIDs ...[]byte) int {
	t.Helper()

	for bit := 0; bit < 256; bit++ {
		ok := true
		for _, keyID := range keyIDs {
			if xorBit(keyID, selfID, bit) != wantDifferent {
				ok = false
				break
			}
		}
		if ok {
			return bit
		}
	}
	t.Fatal("failed to find suitable shared distance bit")
	return 0
}

func sharedDistanceBits(t *testing.T, selfID []byte, count int, keyIDs ...[]byte) []int {
	t.Helper()

	bits := make([]int, 0, count)
	for bit := 0; bit < 256 && len(bits) < count; bit++ {
		ok := true
		for _, keyID := range keyIDs {
			if xorBit(keyID, selfID, bit) {
				ok = false
				break
			}
		}
		if ok {
			bits = append(bits, bit)
		}
	}
	if len(bits) != count {
		t.Fatalf("failed to find %d shared distance bits, got %d", count, len(bits))
	}
	return bits
}

func addActiveTestNodeInBucket(t *testing.T, server *Server, bit int) {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	id := append([]byte{}, server.selfID...)
	id[bit/8] ^= byte(1 << uint(7-(bit%8)))

	node := server.initNode(id, fmt.Sprintf("127.0.0.1:%d", 20000+bit), pub, 1)
	server.buckets[bit].addNode(node, true)
}

func addBackupTestNodeInBucket(t *testing.T, server *Server, bit int) {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	id := append([]byte{}, server.selfID...)
	id[bit/8] ^= byte(1 << uint(7-(bit%8)))

	node := server.initNode(id, fmt.Sprintf("127.0.0.1:%d", 21000+bit), pub, 1)
	server.buckets[bit].addNode(node, false)
}

func dhtStoreQueryMock(t *testing.T, storeCalls *int32) func(addr string, key ed25519.PublicKey) (adnl.Peer, error) {
	t.Helper()

	return func(addr string, key ed25519.PublicKey) (adnl.Peer, error) {
		return &MockADNL{
			query: func(ctx context.Context, req, result tl.Serializable) error {
				raw, ok := req.(tl.Raw)
				if !ok {
					return nil
				}

				payload := []byte(raw)
				var prefix Query
				if rest, err := tl.Parse(&prefix, raw, true); err == nil {
					payload = rest
				}

				var findNode FindNode
				if _, err := tl.Parse(&findNode, payload, true); err == nil {
					reflect.ValueOf(result).Elem().Set(reflect.ValueOf(NodesList{}))
					return nil
				}

				var store Store
				if _, err := tl.Parse(&store, payload, true); err == nil {
					atomic.AddInt32(storeCalls, 1)
					reflect.ValueOf(result).Elem().Set(reflect.ValueOf(Stored{}))
				}
				return nil
			},
		}, nil
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

func TestServer_AbsorbSenderNodeAddsMatchingAddressAsActive(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	node, err := newCorrectNode(5, 6, 7, 8, 17004)
	if err != nil {
		t.Fatal(err)
	}
	peer := newMockPeerFromNode(t, node, "5.6.7.8:17004")

	server.absorbSenderNode(peer, node)

	stats := server.RoutingTableStats()
	if stats.ActiveNodes != 1 {
		t.Fatalf("active nodes = %d, want 1", stats.ActiveNodes)
	}
	if stats.BackupNodes != 0 {
		t.Fatalf("backup nodes = %d, want 0", stats.BackupNodes)
	}
}

func TestServer_AbsorbSenderNodeIgnoresMismatchedAddress(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	node, err := newCorrectNode(5, 6, 7, 8, 17004)
	if err != nil {
		t.Fatal(err)
	}
	peer := newMockPeerFromNode(t, node, "9.9.9.9:17004")

	server.absorbSenderNode(peer, node)

	stats := server.RoutingTableStats()
	if stats.ActiveNodes != 0 {
		t.Fatalf("active nodes = %d, want 0", stats.ActiveNodes)
	}
	if stats.BackupNodes != 0 {
		t.Fatalf("backup nodes = %d, want 0", stats.BackupNodes)
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
