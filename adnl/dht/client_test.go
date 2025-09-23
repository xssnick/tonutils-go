package dht

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tl"
	"net"
	"reflect"
	"strconv"
	"testing"
	"time"
)

type MockGateway struct {
	reg func(addr string, key ed25519.PublicKey) (adnl.Peer, error)
}

func (m *MockGateway) GetID() []byte {
	return make([]byte, 32)
}

func (m *MockGateway) GetAddressList() address.List {
	//TODO implement me
	panic("implement me")
}

func (m *MockGateway) Close() error {
	return nil
}

func (m *MockGateway) SetConnectionHandler(f func(client adnl.Peer) error) {}

func (m *MockGateway) StartServer(listenAddr string) error {
	return nil
}

func (m *MockGateway) RegisterClient(addr string, key ed25519.PublicKey) (adnl.Peer, error) {
	return m.reg(addr, key)
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
								"adnl.address.udp",
								-1185526007,
								22096,
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
								"adnl.address.udp",
								-1307380860,
								15888,
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

func makeStrAddress(ip int32, port int) string {
	_ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(_ip, uint32(ip))
	return _ip.String() + ":" + strconv.Itoa(port)
}

func newCorrectNode(a byte, b byte, c byte, d byte, port int32) (*Node, error) {
	tPubKey, tPrivKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, err
	}
	testNode := &Node{
		keys.PublicKeyED25519{Key: tPubKey},
		&address.List{
			Addresses: []*address.UDP{
				{net.IPv4(a, b, c, d).To4(),
					port,
				},
			},
			Version:    0,
			ReinitDate: 0,
			Priority:   0,
			ExpireAt:   0,
		},
		1671102718,
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
		Addresses: []*address.UDP{
			{
				net.IPv4(1, 1, 1, 1).To4(),
				11111,
			},
			{
				net.IPv4(2, 2, 2, 2).To4(),
				22222,
			},
			{
				net.IPv4(3, 3, 3, 3).To4(),
				333333,
			},
		},
		Version:    0,
		ReinitDate: 0,
		Priority:   0,
		ExpireAt:   0,
	}

	_, _, err = dhtClient.StoreAddress(ctx, addrList, 12*time.Minute, key, 3)
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

func TestClientBootstrapPopulatesRoutingTable(t *testing.T) {
	bootstrap, err := newCorrectNode(10, 0, 0, 1, 10001)
	if err != nil {
		t.Fatalf("failed to create bootstrap node: %v", err)
	}

	nodeA, err := newCorrectNode(10, 0, 0, 2, 10002)
	if err != nil {
		t.Fatalf("failed to create nodeA: %v", err)
	}

	nodeB, err := newCorrectNode(10, 0, 0, 3, 10003)
	if err != nil {
		t.Fatalf("failed to create nodeB: %v", err)
	}

	nodeC, err := newCorrectNode(10, 0, 0, 4, 10004)
	if err != nil {
		t.Fatalf("failed to create nodeC: %v", err)
	}

	addrOf := func(n *Node) string {
		udp := n.AddrList.Addresses[0]
		return net.JoinHostPort(udp.IP.String(), fmt.Sprint(udp.Port))
	}

	responses := map[string][]*Node{
		addrOf(bootstrap): {nodeA, nodeB},
		addrOf(nodeA):     {nodeC},
		addrOf(nodeB):     nil,
		addrOf(nodeC):     nil,
	}

	gateway := &MockGateway{}
	gateway.reg = func(addr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
		nodes := responses[addr]

		return MockADNL{
			query: func(ctx context.Context, req, result tl.Serializable) error {
				switch request := req.(type) {
				case tl.Raw:
					var parsed FindNode
					if _, err := tl.Parse(&parsed, request, true); err != nil {
						return err
					}

					clone := make([]*Node, len(nodes))
					for i, n := range nodes {
						clone[i] = n
					}
					reflect.ValueOf(result).Elem().Set(reflect.ValueOf(NodesList{List: clone}))
					return nil
				case Ping:
					reflect.ValueOf(result).Elem().Set(reflect.ValueOf(Pong{ID: request.ID}))
					return nil
				default:
					return fmt.Errorf("unexpected request type %T", req)
				}
			},
		}, nil
	}

	client, err := NewClient(gateway, []*Node{bootstrap})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err = client.Bootstrap(ctx, 4); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}

	ensurePresent := func(n *Node) {
		kid, err := tl.Hash(n.ID)
		if err != nil {
			t.Fatalf("failed to hash node id: %v", err)
		}
		bucket := client.buckets[affinity(kid, client.gateway.GetID())]
		if bucket.findNode(kid) == nil {
			t.Fatalf("node %s not added to buckets", addrOf(n))
		}
	}

	ensurePresent(bootstrap)
	ensurePresent(nodeA)
	ensurePresent(nodeB)
	ensurePresent(nodeC)
}

func TestClientBootstrapStopsWithoutProgress(t *testing.T) {
	bootstrap, err := newCorrectNode(10, 0, 0, 1, 10001)
	if err != nil {
		t.Fatalf("failed to create bootstrap node: %v", err)
	}

	addr := net.JoinHostPort(bootstrap.AddrList.Addresses[0].IP.String(), fmt.Sprint(bootstrap.AddrList.Addresses[0].Port))

	gateway := &MockGateway{}
	gateway.reg = func(reqAddr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
		if reqAddr != addr {
			return nil, fmt.Errorf("unexpected addr %s", reqAddr)
		}
		return MockADNL{
			query: func(ctx context.Context, req, result tl.Serializable) error {
				switch request := req.(type) {
				case tl.Raw:
					var parsed FindNode
					if _, err := tl.Parse(&parsed, request, true); err != nil {
						return err
					}
					reflect.ValueOf(result).Elem().Set(reflect.ValueOf(NodesList{}))
					return nil
				case Ping:
					reflect.ValueOf(result).Elem().Set(reflect.ValueOf(Pong{ID: request.ID}))
					return nil
				default:
					return fmt.Errorf("unexpected request type %T", req)
				}
			},
		}, nil
	}

	client, err := NewClient(gateway, []*Node{bootstrap})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err = client.Bootstrap(ctx, 3); err == nil {
		t.Fatal("expected bootstrap to fail when no new nodes are discovered")
	}
}

func TestRandomBootstrapKeyCopiesPrefix(t *testing.T) {
	selfID := make([]byte, 32)
	for i := range selfID {
		selfID[i] = byte(i)
	}

	randomData := append(make([]byte, 32), 0, 0)
	key, err := randomBootstrapKey(bytes.NewReader(randomData), selfID)
	if err != nil {
		t.Fatalf("randomBootstrapKey failed: %v", err)
	}

	if !bytes.Equal(key[:8], selfID[:8]) {
		t.Fatalf("expected prefix to match self id, got %x want %x", key[:8], selfID[:8])
	}
}

func TestRandomBootstrapKeyAllowsZeroPrefix(t *testing.T) {
	selfID := make([]byte, 32)
	for i := range selfID {
		selfID[i] = byte(i)
	}

	base := bytes.Repeat([]byte{0xAA}, 32)
	randomData := append(append([]byte{}, base...), 6, 64)
	key, err := randomBootstrapKey(bytes.NewReader(randomData), selfID)
	if err != nil {
		t.Fatalf("randomBootstrapKey failed: %v", err)
	}

	if !bytes.Equal(key, base) {
		t.Fatalf("expected key to remain random when prefix is zero, got %x", key)
	}
}
