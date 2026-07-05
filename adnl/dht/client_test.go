package dht

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/adnl/overlay"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tl"
	"net"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type MockGateway struct {
	reg         func(addr string, key ed25519.PublicKey) (adnl.Peer, error)
	addresses   address.List
	connHandler func(client adnl.Peer) error
	pub         ed25519.PublicKey
	id          []byte
}

func (m *MockGateway) GetID() []byte {
	if len(m.id) > 0 {
		return append([]byte{}, m.id...)
	}
	return make([]byte, 32)
}

func (m *MockGateway) GetPublicKey() ed25519.PublicKey {
	return append(ed25519.PublicKey(nil), m.pub...)
}

func (m *MockGateway) GetAddressList() address.List {
	return m.addresses
}

func (m *MockGateway) Close() error {
	return nil
}

func (m *MockGateway) SetConnectionHandler(f func(client adnl.Peer) error) {
	m.connHandler = f
}

func (m *MockGateway) StartServer(listenAddr string) error {
	return nil
}

func (m *MockGateway) RegisterClient(addr string, key ed25519.PublicKey) (adnl.Peer, error) {
	if m.reg == nil {
		return nil, fmt.Errorf("mock register client is not configured")
	}
	return m.reg(addr, key)
}

func TestNewClientFromConfigRejectsTooLargeKA(t *testing.T) {
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
			_, err := NewClientFromConfig(&MockGateway{}, &liteclient.GlobalConfig{
				DHT: liteclient.DHTConfig{
					K: tt.k,
					A: tt.a,
				},
			})
			if err == nil || err.Error() != tt.want {
				t.Fatalf("got error %v, want %q", err, tt.want)
			}
		})
	}
}

type MockADNL struct {
	query func(ctx context.Context, req, result tl.Serializable) error
	close func()
}

func (m MockADNL) Ping(ctx context.Context) (time.Duration, error) {
	return 1 * time.Millisecond, nil
}

func (m MockADNL) GetPubKey() ed25519.PublicKey {
	return ed25519.PublicKey{}
}

func (m MockADNL) Reinit() {
}

func (m MockADNL) GetCloserCtx() context.Context {
	return context.Background()
}

func (m MockADNL) SetAddresses(addresses address.List) {}

func (m MockADNL) GetDisconnectHandler() func(addr string, key ed25519.PublicKey) {
	return nil
}

func (m MockADNL) GetQueryHandler() func(msg *adnl.MessageQuery) error {
	return nil
}

func (m MockADNL) SetCustomMessageHandler(handler func(msg *adnl.MessageCustom) error) {
	return
}

func (m MockADNL) SetQueryHandler(handler func(msg *adnl.MessageQuery) error) {
	return
}

func (m MockADNL) SendCustomMessage(ctx context.Context, req tl.Serializable) error {
	return nil
}

func (m MockADNL) SendNop(ctx context.Context) error {
	return nil
}

func (m MockADNL) Answer(ctx context.Context, queryID []byte, result tl.Serializable) error {
	return nil
}

func (m MockADNL) GetID() []byte {
	return nil
}

func (m MockADNL) RemoteAddr() string {
	return ""
}

func (m MockADNL) Query(ctx context.Context, req, result tl.Serializable) error {
	return m.query(ctx, req, result)
}

func (m MockADNL) SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey)) {}

func (m MockADNL) Close() {}

var cnf = &liteclient.GlobalConfig{
	Type: "config.global",
	DHT: liteclient.DHTConfig{
		Type: "dht.config.global",
		K:    6,
		A:    3,
		StaticNodes: liteclient.DHTNodes{
			Type: "dht.node",
			Nodes: []liteclient.DHTNode{
				{
					Type: "dht.node",
					ID: liteclient.ServerID{
						Type: "pub.ed25519",
						Key:  "6PGkPQSbyFp12esf1NqmDOaLoFA8i9+Mp5+cAx5wtTU="},
					AddrList: liteclient.DHTAddressList{
						Type: "adnl.addressList",
						Addrs: []liteclient.DHTAddress{
							{
								Type: "adnl.address.udp",
								IP:   -1185526007,
								Port: 22096,
							},
						}},
					Version:   -1,
					Signature: "L4N1+dzXLlkmT5iPnvsmsixzXU0L6kPKApqMdcrGP5d9ssMhn69SzHFK+yIzvG6zQ9oRb4TnqPBaKShjjj2OBg==",
				},
				{
					Type: "dht.node",
					ID: liteclient.ServerID{
						Type: "pub.ed25519",
						Key:  "bn8klhFZgE2sfIDfvVI6m6+oVNi1nBRlnHoxKtR9WBU="},
					AddrList: liteclient.DHTAddressList{
						Type: "adnl.addressList",
						Addrs: []liteclient.DHTAddress{
							{
								Type: "adnl.address.udp",
								IP:   -1307380860,
								Port: 15888,
							},
						}},
					Version:   -1,
					Signature: "fQ5zAa6ot4pfFWzvuJOR8ijM5ELWndSDsRhFKstW1tqVSNfwAdOC7tDC8mc4vgTJ6fSYSWmhnXGK/+T5f6sDCw==",
				},
			},
		},
	},
	Liteservers: nil,
	Validator:   liteclient.ValidatorConfig{},
}

func newCorrectNode(a byte, b byte, c byte, d byte, port int32) (*Node, error) {
	return newCorrectNodeWithVersion(a, b, c, d, port, 1671102718)
}

func newCorrectNodeWithVersion(a byte, b byte, c byte, d byte, port int32, version int32) (*Node, error) {
	tPubKey, tPrivKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, err
	}
	return newCorrectNodeWithKey(a, b, c, d, port, version, tPubKey, tPrivKey)
}

func newCorrectNodeWithKey(
	a byte,
	b byte,
	c byte,
	d byte,
	port int32,
	version int32,
	tPubKey ed25519.PublicKey,
	tPrivKey ed25519.PrivateKey,
) (*Node, error) {
	testNode := &Node{
		keys.PublicKeyED25519{Key: tPubKey},
		&address.List{
			Addresses: []address.Address{
				&address.UDP{
					IP:   net.IPv4(a, b, c, d).To4(),
					Port: port,
				},
			},
			Version:    0,
			ReinitDate: 0,
			Priority:   0,
			ExpireAt:   0,
		},
		version,
		nil,
	}

	toVerify, err := tl.Serialize(testNode, true)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize node: %w", err)
	}
	sign := ed25519.Sign(tPrivKey, toVerify)
	testNode.Signature = sign
	return testNode, nil
}

func correctValue(tAdnlAddr []byte) (*ValueFoundResult, error) {
	pubId, err := base64.StdEncoding.DecodeString("kn0+cePOZRw/FyE005Fj9w5MeSFp4589Ugv62TiK1Mo=")
	if err != nil {
		return nil, err
	}
	pubIdRes := keys.PublicKeyED25519{pubId}
	sign, err := base64.StdEncoding.DecodeString("Zwj4eW/tMbgzF7kQtI8AF11E0q76h5/3+hkylzHuJzKDD2sDd7sw/FXIiVptjrrOIPze8kbbDEkq4K5O78KeDQ==")
	if err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString("WOYnIgEAAADnpg1nkp5cpAUNAAD6Zphj+maYYwAAAAAAAAAA")
	if err != nil {
		return nil, err
	}
	sign2, err := base64.StdEncoding.DecodeString("+1cttR4nsAC0UsZwZTfDwvraxK9NxOjU0pXATkftiEyDgvbyLzPt24lOHl9B756NWBlv8NzqswhNiq7V+SV6Aw==")
	if err != nil {
		return nil, err
	}

	tValue := &ValueFoundResult{
		Value: Value{
			KeyDescription: KeyDescription{
				Key: Key{
					ID:    tAdnlAddr,
					Name:  []byte("address"),
					Index: 0,
				},
				ID:         pubIdRes,
				UpdateRule: UpdateRuleSignature{},
				Signature:  sign,
			},
			Data:      data,
			TTL:       1671121877,
			Signature: sign2,
		},
	}
	return tValue, nil
}

func TestClient_FindValue(t *testing.T) {
	existingValue := "516618cf6cbe9004f6883e742c9a2e3ca53ed02e3e36f4cef62a98ee1e449174"
	siteAddr, err := hex.DecodeString(existingValue)
	if err != nil {
		t.Fatal("failed to prepare test site address, err: ", err.Error())
	}

	tValue, err := correctValue(siteAddr)
	if err != nil {
		t.Fatal("failed to prepare test value, err:")
	}

	tests := []struct {
		name, addr string
		want       error
	}{
		{"existing address", "516618cf6cbe9004f6883e742c9a2e3ca53ed02e3e36f4cef62a98ee1e449174", nil},
		{"missing address", "1537ee02d6d0a65185630084427a26eafdc11ad24566d835291a43b780701f0e", ErrDHTValueIsNotFound},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gateway := &MockGateway{}
			gateway.reg = func(addr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
				return &MockADNL{
					query: func(ctx context.Context, req, result tl.Serializable) error {
						switch request := req.(type) {
						case Ping:
							reflect.ValueOf(result).Elem().Set(reflect.ValueOf(Pong{ID: request.ID}))
						case tl.Raw:
							var _req FindValue
							_, err := tl.Parse(&_req, request, true)
							if err != nil {
								t.Fatal(err)
							}

							k, err := tl.Hash(&Key{
								ID:    siteAddr,
								Name:  []byte("address"),
								Index: 0,
							})
							if err != nil {
								t.Fatal(err)
							}

							if bytes.Equal(k, _req.Key) {
								reflect.ValueOf(result).Elem().Set(reflect.ValueOf(*tValue))
							} else {
								reflect.ValueOf(result).Elem().Set(reflect.ValueOf(ValueNotFoundResult{Nodes: NodesList{nil}}))
							}
						}
						return nil
					},
				}, nil
			}

			dhtCli, err := NewClientFromConfig(gateway, cnf)
			if err != nil {
				t.Fatal(err)
			}

			siteAddr, err := hex.DecodeString(test.addr)
			if err != nil {
				t.Fatal(err)
			}

			time.Sleep(1 * time.Second)

			res, _, got := dhtCli.FindValue(context.Background(), &Key{
				ID:    siteAddr,
				Name:  []byte("address"),
				Index: 0,
			})
			if got != test.want {
				t.Errorf("got '%v', want '%v'", got, test.want)
			}

			if test.name == "existing address" {
				if !reflect.DeepEqual(res, &tValue.Value) {
					t.Errorf("got bad data")
				}
			}
		})
	}
}

func TestClient_FindValueRetriesTimedOutNode(t *testing.T) {
	existingValue := "516618cf6cbe9004f6883e742c9a2e3ca53ed02e3e36f4cef62a98ee1e449174"
	siteAddr, err := hex.DecodeString(existingValue)
	if err != nil {
		t.Fatal(err)
	}

	tValue, err := correctValue(siteAddr)
	if err != nil {
		t.Fatal(err)
	}

	node, err := newCorrectNode(1, 2, 3, 4, 12345)
	if err != nil {
		t.Fatal(err)
	}

	var findValueCalls int
	gateway := &MockGateway{}
	gateway.reg = func(addr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
		return &MockADNL{
			query: func(ctx context.Context, req, result tl.Serializable) error {
				request, ok := req.(tl.Raw)
				if !ok {
					return nil
				}

				var findValue FindValue
				if _, err = tl.Parse(&findValue, request, true); err == nil {
					findValueCalls++
					if findValueCalls == 1 {
						return context.DeadlineExceeded
					}
					reflect.ValueOf(result).Elem().Set(reflect.ValueOf(*tValue))
				}
				return nil
			},
		}, nil
	}

	dhtCli, err := NewClient(gateway, []*Node{node})
	if err != nil {
		t.Fatal(err)
	}

	res, _, err := dhtCli.FindValue(context.Background(), &Key{
		ID:    siteAddr,
		Name:  []byte("address"),
		Index: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if findValueCalls != 2 {
		t.Fatalf("expected 2 findValue attempts, got %d", findValueCalls)
	}
	if !reflect.DeepEqual(res, &tValue.Value) {
		t.Fatal("unexpected value returned")
	}
}

func TestClient_FindValueRetriesValueNotFound(t *testing.T) {
	existingValue := "516618cf6cbe9004f6883e742c9a2e3ca53ed02e3e36f4cef62a98ee1e449174"
	siteAddr, err := hex.DecodeString(existingValue)
	if err != nil {
		t.Fatal(err)
	}

	tValue, err := correctValue(siteAddr)
	if err != nil {
		t.Fatal(err)
	}

	node, err := newCorrectNode(1, 2, 3, 4, 12345)
	if err != nil {
		t.Fatal(err)
	}

	var findValueCalls int
	gateway := &MockGateway{}
	gateway.reg = func(addr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
		return &MockADNL{
			query: func(ctx context.Context, req, result tl.Serializable) error {
				request, ok := req.(tl.Raw)
				if !ok {
					return nil
				}

				var findValue FindValue
				if _, err = tl.Parse(&findValue, request, true); err == nil {
					findValueCalls++
					if findValueCalls == 1 {
						reflect.ValueOf(result).Elem().Set(reflect.ValueOf(ValueNotFoundResult{Nodes: NodesList{List: nil}}))
						return nil
					}
					reflect.ValueOf(result).Elem().Set(reflect.ValueOf(*tValue))
				}
				return nil
			},
		}, nil
	}

	dhtCli, err := NewClient(gateway, []*Node{node})
	if err != nil {
		t.Fatal(err)
	}

	res, _, err := dhtCli.FindValue(context.Background(), &Key{
		ID:    siteAddr,
		Name:  []byte("address"),
		Index: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if findValueCalls != 2 {
		t.Fatalf("expected 2 findValue attempts, got %d", findValueCalls)
	}
	if !reflect.DeepEqual(res, &tValue.Value) {
		t.Fatal("unexpected value returned")
	}
}

func TestClient_StoreRetriesTimedOutFindNode(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	node, err := newCorrectNode(1, 2, 3, 4, 12345)
	if err != nil {
		t.Fatal(err)
	}

	var findNodeCalls int
	var storeCalls int
	gateway := &MockGateway{}
	gateway.reg = func(addr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
		return &MockADNL{
			query: func(ctx context.Context, req, result tl.Serializable) error {
				request, ok := req.(tl.Raw)
				if !ok {
					return nil
				}

				var findNode FindNode
				if _, err = tl.Parse(&findNode, request, true); err == nil {
					findNodeCalls++
					if findNodeCalls == 1 {
						return context.DeadlineExceeded
					}
					reflect.ValueOf(result).Elem().Set(reflect.ValueOf(NodesList{List: []*Node{node}}))
					return nil
				}

				var store Store
				if _, err = tl.Parse(&store, request, true); err == nil {
					storeCalls++
					reflect.ValueOf(result).Elem().Set(reflect.ValueOf(Stored{}))
				}
				return nil
			},
		}, nil
	}

	dhtCli, err := NewClient(gateway, []*Node{node})
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("hello")
	stored, _, err := dhtCli.Store(context.Background(), keys.PublicKeyED25519{Key: pub}, []byte("address"), 0, data, UpdateRuleSignature{}, time.Hour, priv)
	if err != nil {
		t.Fatal(err)
	}
	if stored != 1 {
		t.Fatalf("expected 1 stored replica, got %d", stored)
	}
	if findNodeCalls != 2 {
		t.Fatalf("expected 2 findNode attempts, got %d", findNodeCalls)
	}
	if storeCalls != 1 {
		t.Fatalf("expected 1 store call, got %d", storeCalls)
	}
}

func TestClient_collectNearestNodesStopsAfterKClosestRespond(t *testing.T) {
	keyID := make([]byte, 32)
	nodeID := func(v byte) []byte {
		id := make([]byte, 32)
		id[31] = v
		return id
	}

	findNodeCalls := map[string]int{}
	gateway := &MockGateway{}
	gateway.reg = func(addr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
		return &MockADNL{
			query: func(ctx context.Context, req, result tl.Serializable) error {
				var findNode FindNode
				if _, err := tl.Parse(&findNode, req.(tl.Raw), true); err != nil {
					return err
				}
				findNodeCalls[addr]++
				reflect.ValueOf(result).Elem().Set(reflect.ValueOf(NodesList{}))
				return nil
			},
		}, nil
	}

	buckets := [256]*Bucket{}
	for i := range buckets {
		buckets[i] = newBucket(10)
	}

	dhtCli := &Client{
		buckets: buckets,
		gateway: gateway,
		selfID:  make([]byte, 32),
		k:       2,
		a:       1,
	}
	buckets[0].addNode(&dhtNode{adnlId: nodeID(1), client: dhtCli, addr: "1"}, true)
	buckets[0].addNode(&dhtNode{adnlId: nodeID(2), client: dhtCli, addr: "2"}, true)
	buckets[0].addNode(&dhtNode{adnlId: nodeID(3), client: dhtCli, addr: "3"}, true)

	nodes := dhtCli.collectNearestNodes(context.Background(), keyID)
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nearest nodes, got %d", len(nodes))
	}
	if findNodeCalls["1"] != 1 || findNodeCalls["2"] != 1 {
		t.Fatalf("expected two closest nodes to be queried once, got calls=%v", findNodeCalls)
	}
	if findNodeCalls["3"] != 0 {
		t.Fatalf("third closest node should not be queried, got calls=%v", findNodeCalls)
	}
}

func TestClient_collectNearestNodesPromotesSuccessfulBackup(t *testing.T) {
	keyID := make([]byte, 32)
	nodeID := make([]byte, 32)
	nodeID[31] = 1

	gateway := &MockGateway{}
	gateway.reg = func(addr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
		return &MockADNL{
			query: func(ctx context.Context, req, result tl.Serializable) error {
				var findNode FindNode
				if _, err := tl.Parse(&findNode, req.(tl.Raw), true); err != nil {
					return err
				}

				reflect.ValueOf(result).Elem().Set(reflect.ValueOf(NodesList{}))
				return nil
			},
		}, nil
	}

	buckets := [256]*Bucket{}
	for i := range buckets {
		buckets[i] = newBucket(10)
	}

	dhtCli := &Client{
		buckets: buckets,
		gateway: gateway,
		selfID:  make([]byte, 32),
		k:       1,
		a:       1,
	}

	bucketID := affinity(nodeID, dhtCli.selfID)
	bucket := buckets[bucketID]
	bucket.addNode(&dhtNode{adnlId: nodeID, client: dhtCli, addr: "1"}, false)

	active, backup := bucket.nodeCounts()
	if active != 0 || backup != 1 {
		t.Fatalf("initial bucket counts active=%d backup=%d, want 0/1", active, backup)
	}

	nodes := dhtCli.collectNearestNodes(context.Background(), keyID)
	if len(nodes) != 1 {
		t.Fatalf("expected one nearest node, got %d", len(nodes))
	}

	active, backup = bucket.nodeCounts()
	if active != 1 || backup != 0 {
		t.Fatalf("bucket counts after lookup active=%d backup=%d, want 1/0", active, backup)
	}
}

func TestClient_collectNearestNodesRetriesAfterOtherPending(t *testing.T) {
	keyID := make([]byte, 32)
	nodeID := func(v byte) []byte {
		id := make([]byte, 32)
		id[31] = v
		return id
	}

	calls := make(chan string, 8)
	var firstNodeCalls int32
	gateway := &MockGateway{}
	gateway.reg = func(addr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
		return &MockADNL{
			query: func(ctx context.Context, req, result tl.Serializable) error {
				var findNode FindNode
				if _, err := tl.Parse(&findNode, req.(tl.Raw), true); err != nil {
					return err
				}
				calls <- addr
				if addr == "1" && atomic.AddInt32(&firstNodeCalls, 1) == 1 {
					return context.DeadlineExceeded
				}

				reflect.ValueOf(result).Elem().Set(reflect.ValueOf(NodesList{}))
				return nil
			},
		}, nil
	}

	buckets := [256]*Bucket{}
	for i := range buckets {
		buckets[i] = newBucket(10)
	}

	dhtCli := &Client{
		buckets: buckets,
		gateway: gateway,
		selfID:  make([]byte, 32),
		k:       2,
		a:       1,
	}
	buckets[0].addNode(&dhtNode{adnlId: nodeID(1), client: dhtCli, addr: "1"}, true)
	buckets[0].addNode(&dhtNode{adnlId: nodeID(2), client: dhtCli, addr: "2"}, true)
	buckets[0].addNode(&dhtNode{adnlId: nodeID(3), client: dhtCli, addr: "3"}, true)

	nodes := dhtCli.collectNearestNodes(context.Background(), keyID)
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nearest nodes, got %d", len(nodes))
	}

	got := make([]string, 0, len(calls))
	for {
		select {
		case addr := <-calls:
			got = append(got, addr)
		default:
			if len(got) < 3 {
				t.Fatalf("expected at least 3 findNode calls, got %v", got)
			}
			if got[0] != "1" || got[1] != "2" {
				t.Fatalf("expected timeout retry to wait behind other pending nodes, got calls=%v", got)
			}
			return
		}
	}
}

func TestClient_collectNearestNodesHedgesSlowLookup(t *testing.T) {
	keyID := make([]byte, 32)
	nodeID := func(v byte) []byte {
		id := make([]byte, 32)
		id[31] = v
		return id
	}

	started := make(chan string, 8)
	gateway := &MockGateway{}
	gateway.reg = func(addr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
		return &MockADNL{
			query: func(ctx context.Context, req, result tl.Serializable) error {
				var findNode FindNode
				if _, err := tl.Parse(&findNode, req.(tl.Raw), true); err != nil {
					return err
				}
				started <- addr
				if addr == "1" {
					<-ctx.Done()
					return ctx.Err()
				}

				reflect.ValueOf(result).Elem().Set(reflect.ValueOf(NodesList{}))
				return nil
			},
		}, nil
	}

	buckets := [256]*Bucket{}
	for i := range buckets {
		buckets[i] = newBucket(10)
	}

	dhtCli := &Client{
		buckets: buckets,
		gateway: gateway,
		selfID:  make([]byte, 32),
		k:       3,
		a:       1,
	}
	buckets[0].addNode(&dhtNode{adnlId: nodeID(1), client: dhtCli, addr: "1"}, true)
	buckets[0].addNode(&dhtNode{adnlId: nodeID(2), client: dhtCli, addr: "2"}, true)
	buckets[0].addNode(&dhtNode{adnlId: nodeID(3), client: dhtCli, addr: "3"}, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		dhtCli.collectNearestNodes(ctx, keyID)
		close(done)
	}()

	if addr := <-started; addr != "1" {
		t.Fatalf("expected first lookup to start with closest node, got %s", addr)
	}

	select {
	case addr := <-started:
		if addr != "2" {
			t.Fatalf("expected hedge lookup to start second closest node, got %s", addr)
		}
	case <-time.After(lookupHedgeDelay + time.Second):
		t.Fatal("hedge lookup was not launched")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("collectNearestNodes did not stop after context cancel")
	}
}

func TestClient_collectNearestNodesReturnsBeforeInflightTail(t *testing.T) {
	keyID := make([]byte, 32)
	nodeID := func(v byte) []byte {
		id := make([]byte, 32)
		id[31] = v
		return id
	}

	thirdStarted := make(chan struct{})
	gateway := &MockGateway{}
	gateway.reg = func(addr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
		return &MockADNL{
			query: func(ctx context.Context, req, result tl.Serializable) error {
				var findNode FindNode
				if _, err := tl.Parse(&findNode, req.(tl.Raw), true); err != nil {
					return err
				}

				if addr == "3" {
					close(thirdStarted)
					<-ctx.Done()
					return ctx.Err()
				}

				<-thirdStarted
				reflect.ValueOf(result).Elem().Set(reflect.ValueOf(NodesList{}))
				return nil
			},
		}, nil
	}

	buckets := [256]*Bucket{}
	for i := range buckets {
		buckets[i] = newBucket(10)
	}

	dhtCli := &Client{
		buckets: buckets,
		gateway: gateway,
		selfID:  make([]byte, 32),
		k:       2,
		a:       3,
	}
	buckets[0].addNode(&dhtNode{adnlId: nodeID(1), client: dhtCli, addr: "1"}, true)
	buckets[0].addNode(&dhtNode{adnlId: nodeID(2), client: dhtCli, addr: "2"}, true)
	buckets[0].addNode(&dhtNode{adnlId: nodeID(3), client: dhtCli, addr: "3"}, true)

	done := make(chan []*dhtNode, 1)
	go func() {
		done <- dhtCli.collectNearestNodes(context.Background(), keyID)
	}()

	select {
	case nodes := <-done:
		if len(nodes) != 2 {
			t.Fatalf("expected 2 nearest nodes, got %d", len(nodes))
		}
	case <-time.After(time.Second):
		t.Fatal("collectNearestNodes waited for in-flight tail")
	}
}

func TestClient_storeValueToNodesPreparesPayloadOnce(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	val, keyID, err := buildStoreValue(keys.PublicKeyED25519{Key: pub}, []byte("address"), 0, []byte("value"), UpdateRuleSignature{}, time.Hour, priv)
	if err != nil {
		t.Fatal(err)
	}

	var prefixCalls int32
	var storeCalls int32
	gateway := &MockGateway{}
	gateway.reg = func(addr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
		return &MockADNL{
			query: func(ctx context.Context, req, result tl.Serializable) error {
				request, ok := req.(tl.Raw)
				if !ok {
					return fmt.Errorf("unsupported request type %T", req)
				}

				var store Store
				if _, err = tl.Parse(&store, request, true); err != nil {
					return err
				}
				if store.Value == nil {
					return fmt.Errorf("nil store value")
				}

				atomic.AddInt32(&storeCalls, 1)
				reflect.ValueOf(result).Elem().Set(reflect.ValueOf(Stored{}))
				return nil
			},
		}, nil
	}

	dhtCli := &Client{
		gateway: gateway,
	}
	dhtCli.queryPrefix = func() ([]byte, error) {
		atomic.AddInt32(&prefixCalls, 1)
		return nil, nil
	}

	nodes := []*dhtNode{
		{client: dhtCli},
		{client: dhtCli},
		{client: dhtCli},
	}
	stored, err := dhtCli.storeValueToNodes(context.Background(), keyID, &val, nodes)
	if err != nil {
		t.Fatal(err)
	}
	if stored != len(nodes) {
		t.Fatalf("expected %d stored replicas, got %d", len(nodes), stored)
	}
	if got := atomic.LoadInt32(&storeCalls); got != int32(len(nodes)) {
		t.Fatalf("expected %d store calls, got %d", len(nodes), got)
	}
	if got := atomic.LoadInt32(&prefixCalls); got != 1 {
		t.Fatalf("expected one prepared payload, got %d", got)
	}
}

func TestClient_addNodeRejectsTooOldVersion(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	current, err := newCorrectNodeWithKey(1, 2, 3, 4, 12345, 10, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	older, err := newCorrectNodeWithKey(1, 2, 3, 4, 12345, 9, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	newer, err := newCorrectNodeWithKey(1, 2, 3, 4, 12345, 11, pub, priv)
	if err != nil {
		t.Fatal(err)
	}

	cli, err := NewClient(&MockGateway{}, []*Node{current})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = cli.addNode(older); err == nil || err.Error() != "too old version" {
		t.Fatalf("got unexpected error %v", err)
	}

	if _, err = cli.addNode(newer); err != nil {
		t.Fatalf("failed to add newer node: %v", err)
	}
}

func TestClient_addNodeRejectsZeroVersion(t *testing.T) {
	node, err := newCorrectNodeWithVersion(1, 2, 3, 4, 12345, 0)
	if err != nil {
		t.Fatal(err)
	}

	cli, err := NewClient(&MockGateway{}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = cli.addNode(node); err == nil || err.Error() != "zero version" {
		t.Fatalf("got unexpected error %v", err)
	}
	for _, bucket := range cli.buckets {
		if len(bucket.getNodes()) != 0 {
			t.Fatal("zero-version node entered bucket")
		}
	}
}

func TestClient_addNodeRejectsTooLargeAddressList(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	addrList := &address.List{}
	for i := 0; i < 16; i++ {
		addrList.Addresses = append(addrList.Addresses, &address.UDP{
			IP:   net.IPv4(1, 2, 3, byte(10+i)).To4(),
			Port: int32(10000 + i),
		})
	}
	node, err := BuildSignedNode(keys.PublicKeyED25519{Key: pub}, addrList, 1, _UnknownNetworkID, key)
	if err != nil {
		t.Fatal(err)
	}

	cli, err := NewClient(&MockGateway{}, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = cli.addNode(node)
	if err == nil || !strings.Contains(err.Error(), "too big addr list") {
		t.Fatalf("got unexpected error %v", err)
	}
}

func TestClient_addNodeRejectsMalformedSerializableFields(t *testing.T) {
	selfPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	selfID, err := tl.Hash(keys.PublicKeyED25519{Key: selfPub})
	if err != nil {
		t.Fatal(err)
	}
	cli, err := NewClient(&MockGateway{id: selfID}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	addrList := &address.List{
		Addresses: []address.Address{
			&address.UDP{
				IP:   net.IPv4(127, 0, 0, 1).To4(),
				Port: 12345,
			},
		},
	}
	tests := []struct {
		name string
		node *Node
	}{
		{
			name: "nil id",
			node: &Node{
				AddrList:  addrList,
				Version:   int32(time.Now().Unix()),
				Signature: make([]byte, ed25519.SignatureSize),
			},
		},
		{
			name: "unsupported address",
			node: &Node{
				ID: keys.PublicKeyED25519{Key: pub},
				AddrList: &address.List{
					Addresses: []address.Address{struct{}{}},
				},
				Version:   int32(time.Now().Unix()),
				Signature: make([]byte, ed25519.SignatureSize),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err = cli.addNode(tt.node); err == nil {
				t.Fatal("got error nil, want error not nil")
			}
			for _, bucket := range cli.buckets {
				if len(bucket.getNodes()) != 0 {
					t.Fatal("malformed node entered bucket")
				}
			}
		})
	}
}

func TestClient_FindAddressesUnit(t *testing.T) {
	testAddr := "516618cf6cbe9004f6883e742c9a2e3ca53ed02e3e36f4cef62a98ee1e449174" // ADNL address of foundation.ton
	adnlAddr, err := hex.DecodeString(testAddr)
	if err != nil {
		t.Fatal("failed creating test value, err:", err)
	}

	value, err := correctValue(adnlAddr) //correct value if searching adnl "addr"
	if err != nil {
		t.Fatal("failed creating test value, err: ", err.Error())
	}

	valueToAddrList, err := correctValue(adnlAddr)
	if err != nil {
		t.Fatal("failed creating test value, err: ", err.Error())
	}
	var tAddrList address.List
	_, err = tl.Parse(&tAddrList, valueToAddrList.Value.Data, true)
	if err != nil {
		t.Fatal("failed to parse test address list: ", err)
	}

	pubId, err := base64.StdEncoding.DecodeString("kn0+cePOZRw/FyE005Fj9w5MeSFp4589Ugv62TiK1Mo=")
	if err != nil {
		t.Fatal("failed creating pId of test value, err:", err)
	}
	tPubIdRes := keys.PublicKeyED25519{Key: pubId}

	t.Run("find addresses positive case", func(t *testing.T) {
		gateway := &MockGateway{}
		gateway.reg = func(addr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
			return MockADNL{
				query: func(ctx context.Context, req, result tl.Serializable) error {
					switch request := req.(type) {
					case Ping:
						reflect.ValueOf(result).Elem().Set(reflect.ValueOf(Pong{ID: request.ID}))
					case tl.Raw:
						var _req FindValue
						_, err := tl.Parse(&_req, request, true)
						if err != nil {
							t.Fatal("failed to prepare test data, err", err)
						}

						k, err := tl.Hash(&Key{
							ID:    adnlAddr,
							Name:  []byte("address"),
							Index: 0,
						})
						if err != nil {
							t.Fatal(err)
						}

						if bytes.Equal(k, _req.Key) {
							reflect.ValueOf(result).Elem().Set(reflect.ValueOf(*value))
						} else {
							reflect.ValueOf(result).Elem().Set(reflect.ValueOf(ValueNotFoundResult{Nodes: NodesList{nil}}))
						}
					default:
						return fmt.Errorf("mock err: unsupported request type '%s'", reflect.TypeOf(req).String())
					}
					return nil
				},
			}, nil
		}

		cli, err := NewClientFromConfig(gateway, cnf)
		if err != nil {
			t.Fatal("failed to prepare test client, err:", err)
		}

		addrList, pubKey, err := cli.FindAddresses(context.Background(), adnlAddr)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(tPubIdRes.Key, pubKey) {
			t.Error("invalid pubKey received")
		}

		if !reflect.DeepEqual(&tAddrList, addrList) {
			t.Error("invalid address list received")
		}
	})
}

func TestClient_StoreAddressReturnsADNLID(t *testing.T) {
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

	node, err := newCorrectNode(1, 2, 3, 4, 12345)
	if err != nil {
		t.Fatal(err)
	}

	storedKeyID := make(chan []byte, 1)
	gateway := &MockGateway{}
	gateway.reg = func(addr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
		return &MockADNL{
			query: func(ctx context.Context, req, result tl.Serializable) error {
				raw, ok := req.(tl.Raw)
				if !ok {
					return fmt.Errorf("unsupported request type %T", req)
				}

				var findNode FindNode
				if _, err := tl.Parse(&findNode, raw, true); err == nil {
					reflect.ValueOf(result).Elem().Set(reflect.ValueOf(NodesList{}))
					return nil
				}

				var store Store
				if _, err := tl.Parse(&store, raw, true); err == nil {
					if store.Value == nil {
						return fmt.Errorf("nil store value")
					}
					storedKeyID <- append([]byte{}, store.Value.KeyDescription.Key.ID...)
					reflect.ValueOf(result).Elem().Set(reflect.ValueOf(Stored{}))
					return nil
				}

				return fmt.Errorf("unsupported dht query")
			},
		}, nil
	}

	cli, err := NewClient(gateway, []*Node{node})
	if err != nil {
		t.Fatal(err)
	}

	addrList := address.List{
		Addresses: []address.Address{
			&address.UDP{
				IP:   net.IPv4(10, 20, 30, 40).To4(),
				Port: 30303,
			},
		},
	}

	stored, gotID, err := cli.StoreAddress(context.Background(), addrList, time.Minute, key)
	if err != nil {
		t.Fatal(err)
	}
	if stored != 1 {
		t.Fatalf("expected 1 stored replica, got %d", stored)
	}
	if !bytes.Equal(gotID, adnlID) {
		t.Fatalf("expected adnl id %x, got %x", adnlID, gotID)
	}
	if bytes.Equal(gotID, dhtKey) {
		t.Fatalf("StoreAddress returned internal dht key %x", gotID)
	}

	select {
	case storedID := <-storedKeyID:
		if !bytes.Equal(storedID, adnlID) {
			t.Fatalf("stored value uses key id %x, want %x", storedID, adnlID)
		}
	default:
		t.Fatal("store query was not sent")
	}
}

func TestClient_StoreOverlayNodesReturnsOverlayID(t *testing.T) {
	overlayKey := []byte("test-overlay")
	nodes, overlayID, dhtKey := newTestOverlayNodes(t, overlayKey)

	node, err := newCorrectNode(1, 2, 3, 4, 12345)
	if err != nil {
		t.Fatal(err)
	}

	storedKeyID := make(chan []byte, 1)
	gateway := &MockGateway{}
	gateway.reg = func(addr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
		return &MockADNL{
			query: func(ctx context.Context, req, result tl.Serializable) error {
				raw, ok := req.(tl.Raw)
				if !ok {
					return fmt.Errorf("unsupported request type %T", req)
				}

				var findNode FindNode
				if _, err := tl.Parse(&findNode, raw, true); err == nil {
					reflect.ValueOf(result).Elem().Set(reflect.ValueOf(NodesList{}))
					return nil
				}

				var store Store
				if _, err := tl.Parse(&store, raw, true); err == nil {
					if store.Value == nil {
						return fmt.Errorf("nil store value")
					}
					storedKeyID <- append([]byte{}, store.Value.KeyDescription.Key.ID...)
					reflect.ValueOf(result).Elem().Set(reflect.ValueOf(Stored{}))
					return nil
				}

				return fmt.Errorf("unsupported dht query")
			},
		}, nil
	}

	cli, err := NewClient(gateway, []*Node{node})
	if err != nil {
		t.Fatal(err)
	}

	stored, gotID, err := cli.StoreOverlayNodes(context.Background(), overlayKey, nodes, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if stored != 1 {
		t.Fatalf("expected 1 stored replica, got %d", stored)
	}
	if !bytes.Equal(gotID, overlayID) {
		t.Fatalf("expected overlay id %x, got %x", overlayID, gotID)
	}
	if bytes.Equal(gotID, dhtKey) {
		t.Fatalf("StoreOverlayNodes returned internal dht key %x", gotID)
	}

	select {
	case storedID := <-storedKeyID:
		if !bytes.Equal(storedID, overlayID) {
			t.Fatalf("stored value uses key id %x, want %x", storedID, overlayID)
		}
	default:
		t.Fatal("store query was not sent")
	}
}

func newTestOverlayNodes(t *testing.T, overlayKey []byte) (*overlay.NodesList, []byte, []byte) {
	t.Helper()

	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	node, err := overlay.NewNode(overlayKey, key)
	if err != nil {
		t.Fatal(err)
	}

	overlayID, err := tl.Hash(keys.PublicKeyOverlay{Key: overlayKey})
	if err != nil {
		t.Fatal(err)
	}

	dhtKey, err := tl.Hash(Key{
		ID:    overlayID,
		Name:  []byte("nodes"),
		Index: 0,
	})
	if err != nil {
		t.Fatal(err)
	}

	return &overlay.NodesList{List: []overlay.Node{*node}}, overlayID, dhtKey
}

func TestClient_FindAddressesIntegration(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	gateway := adnl.NewGateway(priv)
	err = gateway.StartClient()
	if err != nil {
		t.Fatal(err)
	}

	// restore after unit tests
	testAddr := "89bea091caf4273d38b0dc24944d8798e057abcfa6ac08d5e0b26284c5c0609a" // ADNL address of utils.ton

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	dhtClient, err := NewClientFromConfigUrl(ctx, gateway, "https://ton-blockchain.github.io/global.config.json")
	if err != nil {
		t.Fatalf("failed to init DHT client: %s", err.Error())
	}

	time.Sleep(2 * time.Second)

	siteAddr, err := hex.DecodeString(testAddr)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = dhtClient.FindAddresses(ctx, siteAddr)
	if err != nil {
		t.Fatal(err)
	}
}

func TestClient_Close(t *testing.T) {
	gateway := &MockGateway{}
	gateway.reg = func(addr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
		return MockADNL{
			query: func(ctx context.Context, req, result tl.Serializable) error {
				switch request := req.(type) {
				case Ping:
					reflect.ValueOf(result).Elem().Set(reflect.ValueOf(Pong{ID: request.ID}))
				default:
					return fmt.Errorf("mock err: unsupported request type '%s'", reflect.TypeOf(request).String())
				}
				return nil
			},
			close: func() {
			},
		}, nil
	}

	cli, err := NewClientFromConfig(gateway, cnf)
	if err != nil {
		t.Fatal("failed to prepare test client, err: ", err)
	}
	t.Run("close client test", func(t *testing.T) {
		cli.Close()
		if cli.globalCtx.Err() == nil {
			t.Error("global context was not canceled")
		}
	})
}

func TestClient_StoreAddressIntegration(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	gateway := adnl.NewGateway(priv)
	err = gateway.StartClient()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	dhtClient, err := NewClientFromConfigUrl(ctx, gateway, "https://ton-blockchain.github.io/global.config.json")
	if err != nil {
		t.Fatalf("failed to init DHT client: %s", err.Error())
	}

	time.Sleep(2 * time.Second)

	pub, key, _ := ed25519.GenerateKey(nil)

	addrList := address.List{
		Addresses: []address.Address{
			&address.UDP{
				IP:   net.IPv4(1, 1, 1, 1).To4(),
				Port: 11111,
			},
			&address.UDP{
				IP:   net.IPv4(2, 2, 2, 2).To4(),
				Port: 22222,
			},
			&address.UDP{
				IP:   net.IPv4(3, 3, 3, 3).To4(),
				Port: 333333,
			},
		},
		Version:    0,
		ReinitDate: 0,
		Priority:   0,
		ExpireAt:   0,
	}

	_, _, err = dhtClient.StoreAddress(ctx, addrList, 12*time.Minute, key)
	if err != nil {
		t.Fatal(err)
	}

	kid, err := tl.Hash(keys.PublicKeyED25519{
		Key: pub,
	})
	if err != nil {
		t.Fatal(err)
	}

	res, _, err := dhtClient.FindAddresses(ctx, kid)
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Addresses) != 3 {
		t.Fatal("addr len not 3")
	}
}
