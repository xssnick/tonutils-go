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
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tl"
	"net"
	"reflect"
	"testing"
	"time"
)

type MockADNL struct {
	connect func(ctx context.Context, addr string) (err error)
	query   func(ctx context.Context, req, result tl.Serializable) error
}

func (m MockADNL) Connect(ctx context.Context, addr string) (err error) {
	return m.connect(ctx, addr)
}

func (m MockADNL) Query(ctx context.Context, req, result tl.Serializable) error {
	return m.query(ctx, req, result)
}

func (m MockADNL) SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey)) {
}

func (m MockADNL) Close() error {
	return nil
}

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
						"pub.ed25519",
						"C1uy64rfGxp10SPSqbsxWhbumy5SM0YbvljCudwpZeI="},
					AddrList: liteclient.DHTAddressList{
						"adnl.addressList",
						[]liteclient.DHTAddress{
							{
								"adnl.address.udp",
								-1185526007,
								22096,
							},
						},
						0,
						0,
						0,
						0},
					Version:   -1,
					Signature: "L4N1+dzXLlkmT5iPnvsmsixzXU0L6kPKApqMdcrGP5d9ssMhn69SzHFK+yIzvG6zQ9oRb4TnqPBaKShjjj2OBg==",
				},
				{
					Type: "dht.node",
					ID: liteclient.ServerID{
						"pub.ed25519",
						"bn8klhFZgE2sfIDfvVI6m6+oVNi1nBRlnHoxKtR9WBU="},
					AddrList: liteclient.DHTAddressList{
						"adnl.addressList",
						[]liteclient.DHTAddress{
							{
								"adnl.address.udp",
								-1307380860,
								15888,
							},
						},
						0,
						0,
						0,
						0},
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
	testNode := Node{
		adnl.PublicKeyED25519{},
		&address.List{
			Addresses: []*address.UDP{
				{net.IPv4(a, b, c, d).To4(),
					port,
				},
			},
			Version:    0,
			ReinitDate: 0,
			Priority:   0,
			ExpireAT:   0,
		},
		1671102718,
		nil,
	}

	tPubKey, tPrivKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, err
	}
	testNode.ID = adnl.PublicKeyED25519{tPubKey}

	toVerify, err := tl.Serialize(testNode, true)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize node: %w", err)
	}
	sign := ed25519.Sign(tPrivKey, toVerify)
	testNode.Signature = sign

	return &testNode, nil
}

func newIncorrectNode(a byte, b byte, c byte, d byte, port int32) (*Node, error) {
	testNode := Node{
		adnl.PublicKeyED25519{},
		&address.List{
			Addresses: []*address.UDP{
				{net.IPv4(a, b, c, d).To4(),
					port,
				},
			},
			Version:    0,
			ReinitDate: 0,
			Priority:   0,
			ExpireAT:   0,
		},
		1671102718,
		nil,
	}
	testNodeCorrupted := Node{
		adnl.PublicKeyED25519{},
		&address.List{
			Addresses: []*address.UDP{
				{net.IPv4(a^1, b^1, c^1, d^1).To4(),
					port ^ 1,
				},
			},
			Version:    0,
			ReinitDate: 0,
			Priority:   0,
			ExpireAT:   0,
		},
		1671102718,
		nil,
	}

	tPubKey, tPrivKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, err
	}
	testNode.ID = adnl.PublicKeyED25519{tPubKey}

	toVerify, err := tl.Serialize(testNode, true)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize node: %w", err)
	}

	sign := ed25519.Sign(tPrivKey, toVerify)
	testNodeCorrupted.Signature = sign

	return &testNode, nil
}

func correctValue(tAdnlAddr []byte) (*ValueFoundResult, error) {
	pubId, err := base64.StdEncoding.DecodeString("kn0+cePOZRw/FyE005Fj9w5MeSFp4589Ugv62TiK1Mo=")
	if err != nil {
		return nil, err
	}
	pubIdRes := adnl.PublicKeyED25519{pubId}
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
			newADNL = func(key ed25519.PublicKey) (ADNL, error) {
				return MockADNL{
					connect: func(ctx context.Context, addr string) (err error) {
						return nil
					},
					query: func(ctx context.Context, req, result tl.Serializable) error {
						switch request := req.(type) {
						case SignedAddressListQuery:
							testNode, err := newCorrectNode(1, 2, 3, 4, 12345)
							if err != nil {
								t.Fatal("failed creating test node, err: ", err.Error())
							}
							reflect.ValueOf(result).Elem().Set(reflect.ValueOf(*testNode))
						case tl.Raw:
							var _req FindValue
							_, err := tl.Parse(&_req, request, true)
							if err != nil {
								t.Fatal(err)
							}

							k, err := adnl.ToKeyID(&Key{
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

			dhtCli, err := NewClientFromConfig(10*time.Second, cnf)
			if err != nil {
				t.Fatal(err)
			}

			siteAddr, err := hex.DecodeString(test.addr)
			if err != nil {
				t.Fatal(err)
			}

			time.Sleep(2 * time.Second)

			res, got := dhtCli.FindValue(context.Background(), &Key{
				ID:    siteAddr,
				Name:  []byte("address"),
				Index: 0,
			})
			if got != test.want {
				t.Errorf("got '%v', want '%v'", got, test.want)
			}

			if test.name == "existing address" {
				tValue.Value.Signature = nil
				tValue.Value.KeyDescription.Signature = nil
				if !reflect.DeepEqual(res, &tValue.Value) {
					t.Errorf("got bad data")
				}
			}
		})
	}
}

func TestClient_NewClientFromConfig(t *testing.T) {
	corNode1, err := newCorrectNode(178, 18, 243, 132, 15888)
	if err != nil {
		t.Fatal("failed creating test node, err: ", err.Error())
	}

	pub1, ok := corNode1.ID.(adnl.PublicKeyED25519)
	if !ok {
		t.Fatalf("unsupported id type %s", reflect.TypeOf(corNode1.ID).String())
	}
	kId1, err := adnl.ToKeyID(pub1)
	if err != nil {
		t.Fatal(err)
	}
	keyID1 := hex.EncodeToString(kId1)

	corNode2, err := newCorrectNode(1, 2, 3, 4, 12345)
	if err != nil {
		t.Fatal("failed creating test node, err: ", err.Error())
	}
	pub2, ok := corNode2.ID.(adnl.PublicKeyED25519)
	if !ok {
		t.Fatalf("unsupported id type %s", reflect.TypeOf(corNode2.ID).String())
	}
	kId2, err := adnl.ToKeyID(pub2)
	if err != nil {
		t.Fatal(err)
	}
	keyID2 := hex.EncodeToString(kId2)

	incorNode, err := newIncorrectNode(178, 18, 243, 132, 15888)
	if err != nil {
		t.Fatal("failed creating test node, err: ", err.Error())
	}
	pub3, ok := incorNode.ID.(adnl.PublicKeyED25519)
	if !ok {
		t.Fatalf("unsupported id type %s", reflect.TypeOf(corNode2.ID).String())
	}
	kId3, err := adnl.ToKeyID(pub3)
	if err != nil {
		t.Fatal(err)
	}
	keyID3 := hex.EncodeToString(kId3)

	tests := []struct {
		name          string
		responseNode1 *Node
		responseNode2 *Node
		wantLenNodes  int
		idToCheckAdd1 string
		idToCheckAdd2 string
		checkAdd1     bool
		checkAdd2     bool
	}{
		{
			"positive case (all nodes valid)", corNode1, corNode2, 2, keyID1, keyID2, true, true,
		},
		{
			"negative case (one of two nodes with bad sign)", corNode1, incorNode, 1, keyID1, keyID3, true, false,
		},
	}

	for _, test := range tests {
		reqCount := 0
		t.Run(test.name, func(t *testing.T) {
			newADNL = func(key ed25519.PublicKey) (ADNL, error) {
				return MockADNL{
					connect: func(ctx context.Context, addr string) error {
						return nil
					},
					query: func(ctx context.Context, req, result tl.Serializable) error {
						switch req.(type) {
						case SignedAddressListQuery:
							if reqCount == 0 {
								reflect.ValueOf(result).Elem().Set(reflect.ValueOf(*test.responseNode1))
								reqCount++
							} else {
								reflect.ValueOf(result).Elem().Set(reflect.ValueOf(*test.responseNode2))
							}
						default:
							return fmt.Errorf("mock err: unsupported request type '%s'", reflect.TypeOf(req).String())
						}
						return nil
					},
				}, nil
			}
			cli, err := NewClientFromConfig(10*time.Second, cnf)
			if err != nil {
				t.Fatal(err)
			}

			time.Sleep(2 * time.Second)

			if len(cli.activeNodes) != test.wantLenNodes || len(cli.knownNodesInfo) != test.wantLenNodes {
				t.Errorf("added nodes count (active'%d', known'%d') but expected(%d)", len(cli.activeNodes), len(cli.knownNodesInfo), test.wantLenNodes)
			}

			resDhtNode1, ok := cli.activeNodes[test.idToCheckAdd1]
			if ok != test.checkAdd1 {
				t.Errorf("invalid active nodes addition")
			}
			if hex.EncodeToString(resDhtNode1.id) != test.idToCheckAdd1 {
				t.Errorf("bad data resived")
			}

			resNode1, ok := cli.knownNodesInfo[test.idToCheckAdd1]
			if ok != test.checkAdd1 {
				t.Errorf("invalid known nodes nodes addition")
			}
			if !reflect.DeepEqual(resNode1, test.responseNode1) {
				t.Errorf("bad data resived")
			}

			if test.name == "negative case (one of two nodes with bad sign)" {
				_, ok := cli.activeNodes[test.idToCheckAdd2]
				if ok != test.checkAdd2 {
					t.Errorf("invalid active nodes addition")
				}
				_, ok = cli.knownNodesInfo[test.idToCheckAdd2]
				if ok != test.checkAdd2 {
					t.Errorf("invalid known nodes nodes addition")
				}
			} else {
				resDhtNode2, ok := cli.activeNodes[test.idToCheckAdd2]
				if ok != test.checkAdd2 {
					t.Errorf("invalid active nodes addition")
				}
				if hex.EncodeToString(resDhtNode2.id) != test.idToCheckAdd2 {
					t.Errorf("bad data resived")
				}

				resNode2, ok := cli.knownNodesInfo[test.idToCheckAdd2]
				if ok != test.checkAdd2 {
					t.Errorf("invalid known nodes nodes addition")
				}
				if !reflect.DeepEqual(resNode2, test.responseNode2) {
					t.Errorf("bad data resived")
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
	tPubIdRes := adnl.PublicKeyED25519{pubId}

	t.Run("find addresses positive case", func(t *testing.T) {
		newADNL = func(key ed25519.PublicKey) (ADNL, error) {
			return MockADNL{
				connect: func(ctx context.Context, addr string) (err error) {
					return nil
				},
				query: func(ctx context.Context, req, result tl.Serializable) error {
					switch request := req.(type) {
					case SignedAddressListQuery:
						testNode, err := newCorrectNode(1, 2, 3, 4, 12345)
						if err != nil {
							t.Fatal("failed creating test node, err: ", err.Error())
						}
						reflect.ValueOf(result).Elem().Set(reflect.ValueOf(*testNode))
					case tl.Raw:
						var _req FindValue
						_, err := tl.Parse(&_req, request, true)
						if err != nil {
							t.Fatal("failed to prepare test data, err", err)
						}

						k, err := adnl.ToKeyID(&Key{
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
		cli, err := NewClientFromConfig(10*time.Second, cnf)
		if err != nil {
			t.Fatal("failed to prepare test client, err:", err)
		}
		time.Sleep(2 * time.Second)

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
	testAddr := "516618cf6cbe9004f6883e742c9a2e3ca53ed02e3e36f4cef62a98ee1e449174" // ADNL address of foundation.ton

	cfg, err := liteclient.GetConfigFromUrl(context.Background(), "https://ton-blockchain.github.io/global.config.json")
	if err != nil {
		t.Fatalf("cannot fetch network config, error: %s", err)
	}

	var nodes []NodeInfo
	for _, node := range cfg.DHT.StaticNodes.Nodes {
		ip := make(net.IP, 4)
		ii := int32(node.AddrList.Addrs[0].IP)
		binary.BigEndian.PutUint32(ip, uint32(ii))

		pp, err := base64.StdEncoding.DecodeString(node.ID.Key)
		if err != nil {
			continue
		}

		nodes = append(nodes, NodeInfo{
			Address: ip.String() + ":" + fmt.Sprint(node.AddrList.Addrs[0].Port),
			Key:     pp,
		})
	}

	dhtClient, err := NewClient(10*time.Second, nodes)
	if err != nil {
		t.Fatalf("failed to init DHT client: %s", err.Error())
	}

	time.Sleep(2 * time.Second)

	siteAddr, err := hex.DecodeString(testAddr)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = dhtClient.FindAddresses(context.Background(), siteAddr)
	if err != nil {
		t.Fatal(err)
	}
}

func TestClient_FindValueRaw(t *testing.T) {
	existingValue := "516618cf6cbe9004f6883e742c9a2e3ca53ed02e3e36f4cef62a98ee1e449174"
	siteAddr, err := hex.DecodeString(existingValue)
	if err != nil {
		t.Fatal("failed to prepare test site address, err: ", err.Error())
	}

	var typeValue any
	typeValue = &Value{}
	var typeNode any
	typeNode = []*Node{}

	tValue, err := correctValue(siteAddr)
	if err != nil {
		t.Fatal("failed to prepare test value, err:")
	}

	var tNodesList []*Node
	tNode, err := newCorrectNode(8, 8, 8, 8, 12345)
	if err != nil {
		t.Fatal("failed creating test node, err: ", err.Error())
	}
	tNodesList = append(tNodesList, tNode)

	tests := []struct {
		name, addr string
		wantType   any
	}{
		{"existing address", "516618cf6cbe9004f6883e742c9a2e3ca53ed02e3e36f4cef62a98ee1e449174", typeValue},
		{"missing address", "1537ee02d6d0a65185630084427a26eafdc11ad24566d835291a43b780701f0e", typeNode},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			newADNL = func(key ed25519.PublicKey) (ADNL, error) {
				return MockADNL{
					connect: func(ctx context.Context, addr string) error {
						return nil
					},
					query: func(ctx context.Context, req, result tl.Serializable) error {
						switch request := req.(type) {
						case SignedAddressListQuery:
							testNode, err := newCorrectNode(1, 2, 3, 4, 12345)
							if err != nil {
								t.Fatal("failed creating test node, err: ", err.Error())
							}
							reflect.ValueOf(result).Elem().Set(reflect.ValueOf(*testNode))
						case tl.Raw:
							var _req FindValue
							_, err := tl.Parse(&_req, request, true)
							if err != nil {
								t.Fatal("failed to prepare test data, err", err)
							}

							k, err := adnl.ToKeyID(&Key{
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
								reflect.ValueOf(result).Elem().Set(reflect.ValueOf(ValueNotFoundResult{Nodes: NodesList{tNodesList}}))
							}
						default:
							return fmt.Errorf("mock err: unsupported request type '%s'", reflect.TypeOf(request).String())
						}
						return nil
					},
				}, nil
			}

			cli, err := NewClientFromConfig(10*time.Second, cnf)
			if err != nil {
				t.Fatal(err)
			}

			time.Sleep(2 * time.Second)

			var testNode *dhtNode
			for _, val := range cli.activeNodes {
				testNode = val
			}

			siteAddr, err := hex.DecodeString(test.addr)
			if err != nil {
				t.Fatal(err)
			}
			k := &Key{
				ID:    siteAddr,
				Name:  []byte("address"),
				Index: 0,
			}
			testId, keyErr := adnl.ToKeyID(k)
			if keyErr != nil {
				t.Fatal("failed to prepare test id, err: ", keyErr)
			}

			res, err := cli.FindValueRaw(context.Background(), testNode, testId, 12)
			if err != nil {
				t.Fatal("failed execution findValueRaw, err: ", err)
			}

			if reflect.TypeOf(res) != reflect.TypeOf(test.wantType) {
				t.Errorf("got type '%s', want '%s'", reflect.TypeOf(res).String(), reflect.TypeOf(test.wantType).String())
			}

			switch test.name {
			case "existing address":
				tValue.Value.Signature = nil
				tValue.Value.KeyDescription.Signature = nil
				if !reflect.DeepEqual(*res.(*Value), tValue.Value) {
					t.Errorf("got bad data")
				}
			case "missing address":
				if !reflect.DeepEqual(res.([]*Node)[0], tNodesList[0]) {
					t.Errorf("got bad data")
				}
			default:
				t.Fatal("test error: unsupported test name")
			}
		})
	}
}
