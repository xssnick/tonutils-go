package dht

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func newCorrectDhtNode(a byte, b byte, c byte, d byte, port string) (*dhtNode, error) {
	tPubKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, err
	}

	kId, err := tl.Hash(adnl.PublicKeyED25519{Key: tPubKey})
	if err != nil {
		return nil, err
	}
	resDhtNode := &dhtNode{
		adnlId:    kId,
		addr:      net.IPv4(a, b, c, d).To4().String() + ":" + port,
		serverKey: tPubKey,
	}
	return resDhtNode, nil
}

func TestNode_findNodes(t *testing.T) {
	tDhtNode, err := newCorrectDhtNode(1, 2, 3, 4, "12356")
	if err != nil {
		t.Fatal("failed to prepare test dht node, err: ", err)
	}

	pubKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal("failed to prepare test pub key, err: ", err)
	}
	pubKeyAdnl := adnl.PublicKeyED25519{Key: pubKey}

	idKey, err := tl.Hash(pubKeyAdnl)
	if err != nil {
		t.Fatal("failed to prepare test key id, err: ", err)
	}
	tKey := Key{
		idKey,
		[]byte("lol"),
		0,
	}
	kId, err := tl.Hash(tKey)
	if err != nil {
		t.Fatal("failed to prepare test key id, err: ", err)
	}

	tNode, err := newCorrectNode(4, 5, 6, 7, 1245)
	if err != nil {
		t.Fatal("failed to prepare test node, err: ", err)
	}
	t.Run("good response", func(t *testing.T) {
		gateway := &MockGateway{}
		client := &Client{
			gateway: gateway,
		}
		gateway.reg = func(addr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
			return MockADNL{
				query: func(ctx context.Context, req, result tl.Serializable) error {
					switch request := req.(type) {
					case tl.Raw:
						var _req FindNode
						_, err := tl.Parse(&_req, request, true)
						if err != nil {
							t.Fatal("failed to prepare test data, err", err)
						}

						if bytes.Equal(kId, _req.Key) {
							reflect.ValueOf(result).Elem().Set(reflect.ValueOf(NodesList{[]*Node{tNode}}))
						} else {
							t.Fatal("bad request received")
						}
					default:
						return fmt.Errorf("mock err: unsupported request type '%s'", reflect.TypeOf(request).String())
					}
					return nil
				},
			}, nil
		}

		tDhtNode.client = client
		nodesL, err := tDhtNode.findNodes(context.Background(), kId, 10)
		if err != nil {
			t.Fatal("failed to execute findNodes func, err: ", err)
		}
		if reflect.DeepEqual(nodesL[0], tNode) != true {
			t.Error("bad node received")
		}
	})

	t.Run("bad response", func(t *testing.T) {
		gateway := &MockGateway{}
		client := &Client{
			gateway: gateway,
		}
		gateway.reg = func(addr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
			return MockADNL{
				query: func(ctx context.Context, req, result tl.Serializable) error {
					switch request := req.(type) {
					case tl.Raw:
						var _req FindNode
						_, err := tl.Parse(&_req, request, true)
						if err != nil {
							t.Fatal("failed to prepare test data, err", err)
						}

						if bytes.Equal(kId, _req.Key) {
							reflect.ValueOf(result).Elem().Set(reflect.ValueOf(Pong{}))
						} else {
							t.Fatal("bad request received")
						}
					default:
						return fmt.Errorf("mock err: unsupported request type '%s'", reflect.TypeOf(request).String())
					}
					return nil
				},
			}, nil
		}
		tDhtNode.client = client

		_, err := tDhtNode.findNodes(context.Background(), kId, 10)
		if err == nil {
			t.Error("got error nil, want error not nil")
		}
		if strings.Contains(err.Error(), "unexpected response") != true {
			t.Errorf("got unexcpected error '%s', want unexpected response", err.Error())
		}
	})
}

func TestNode_storeValue(t *testing.T) {
	tDhtNode, err := newCorrectDhtNode(1, 2, 3, 4, "12356")
	if err != nil {
		t.Fatal("failed to prepare test dht node, err: ", err)
	}

	hexAddr := "516618cf6cbe9004f6883e742c9a2e3ca53ed02e3e36f4cef62a98ee1e449174"
	siteAddr, err := hex.DecodeString(hexAddr)
	if err != nil {
		t.Fatal("failed to prepare test site address, err: ", err.Error())
	}

	valFound, err := correctValue(siteAddr)
	if err != nil {
		t.Fatal("failed to prepare test value")
	}

	val := valFound.Value

	kId, err := tl.Hash(val.KeyDescription.Key)
	if err != nil {
		t.Fatal("failed to prepare test key id, err: ", err)
	}

	t.Run("good response", func(t *testing.T) {
		gateway := &MockGateway{}
		client := &Client{
			gateway: gateway,
		}
		gateway.reg = func(addr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
			return MockADNL{
				query: func(ctx context.Context, req, result tl.Serializable) error {
					switch request := req.(type) {
					case tl.Raw:
						var _req Store
						_, err := tl.Parse(&_req, request, true)
						if err != nil {
							t.Fatal("failed to prepare test data, err", err)
						}

						if reflect.DeepEqual(val, *_req.Value) {
							reflect.ValueOf(result).Elem().Set(reflect.ValueOf(Stored{}))
						} else {
							t.Fatal("bad request received")
						}
					default:
						return fmt.Errorf("mock err: unsupported request type '%s'", reflect.TypeOf(request).String())
					}
					return nil
				},
			}, nil
		}
		tDhtNode.client = client

		err := tDhtNode.storeValue(context.Background(), kId, &val)
		if err != nil {
			t.Fatal("failed to execute storeValue func, err: ", err)
		}
	})

	t.Run("bad response", func(t *testing.T) {
		gateway := &MockGateway{}
		client := &Client{
			gateway: gateway,
		}
		gateway.reg = func(addr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
			return MockADNL{
				query: func(ctx context.Context, req, result tl.Serializable) error {
					switch request := req.(type) {
					case tl.Raw:
						var _req Store
						_, err := tl.Parse(&_req, request, true)
						if err != nil {
							t.Fatal("failed to prepare test data, err", err)
						}

						if reflect.DeepEqual(val, *_req.Value) {
							reflect.ValueOf(result).Elem().Set(reflect.ValueOf(Pong{}))
						} else {
							t.Fatal("bad request received")
						}
					default:
						return fmt.Errorf("mock err: unsupported request type '%s'", reflect.TypeOf(request).String())
					}
					return nil
				},
			}, nil
		}
		tDhtNode.client = client

		err := tDhtNode.storeValue(context.Background(), kId, &val)
		if err == nil {
			t.Error("got error nil, want error not nil")
		}
		if strings.Contains(err.Error(), "unexpected response") != true {
			t.Errorf("got unexcpected error '%s', want unexpected response", err.Error())
		}
	})
}

func TestNode_findValue(t *testing.T) {
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

			cli, err := NewClientFromConfig(gateway, cnf)
			if err != nil {
				t.Fatal(err)
			}

			time.Sleep(2 * time.Second)

			var testNode *dhtNode
			for _, val := range cli.knownNodes {
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
			testId, keyErr := tl.Hash(k)
			if keyErr != nil {
				t.Fatal("failed to prepare test id, err: ", keyErr)
			}

			res, err := testNode.findValue(context.Background(), testId, 12)
			if err != nil {
				t.Fatal("failed execution findValueRaw, err: ", err)
			}

			if reflect.TypeOf(res) != reflect.TypeOf(test.wantType) {
				t.Errorf("got type '%s', want '%s'", reflect.TypeOf(res).String(), reflect.TypeOf(test.wantType).String())
			}

			switch test.name {
			case "existing address":
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

func TestNode_checkValue(t *testing.T) {
	hexAddr := "516618cf6cbe9004f6883e742c9a2e3ca53ed02e3e36f4cef62a98ee1e449174"
	siteAddr, err := hex.DecodeString(hexAddr)
	if err != nil {
		t.Fatal("failed to prepare test site address, err: ", err.Error())
	}

	valFound, err := correctValue(siteAddr)
	if err != nil {
		t.Fatal("failed to prepare test value")
	}

	val := valFound.Value

	kId, err := tl.Hash(val.KeyDescription.Key)
	if err != nil {
		t.Fatal("failed to prepare test key id, err: ", err)
	}

	t.Run("correct value", func(t *testing.T) {
		err = checkValue(kId, &val)
		if err != nil {
			t.Fatal("failed to execute checkValue func, err: ", err)
		}
	})

	t.Run("corrupted value: bad value sign", func(t *testing.T) {
		val.Signature = []byte("qewrgheau;igqn41463[8u9y1436h1[iu1gh[8935]988hg]q5")
		err = checkValue(kId, &val)
		if err == nil {
			t.Error("got error nil, want error not nil")
		}
		if strings.Contains(err.Error(), "value's signature not match key") != true {
			t.Errorf("got unexcpected error '%s', want signature not match key", err.Error())
		}
	})

	//t.Run("corrupted value: bad value description sign", func(t *testing.T) {
	//	val.KeyDescription.ID = []byte("qewrgheau;igqn41463[8u9y1436h1[iu1gh[8935]988hg]q5")
	//	err = checkValue(kId, &val)
	//if err == nil {
	//	t.Error("got error nil, want error not nil")
	//}
	//if strings.Contains(err.Error(), "key description's signature not match key") != true {
	//	t.Errorf("got unexcpected error '%s', want signature not match key", err.Error())
	//}
	//})
}

func TestNode_weight(t *testing.T) {
	tPubKey, err := hex.DecodeString("75b9507dc58a931ea6e860d444987e82d8501e09191264c35b95f6956d8debe4")
	if err != nil {
		t.Fatal("failed to prepare test public key, err: ", err)
	}

	kId, err := tl.Hash(adnl.PublicKeyED25519{Key: tPubKey})
	if err != nil {
		t.Fatal("failed to prepare test key id, err: ", err)
	}
	tNode1 := &dhtNode{
		adnlId:       kId,
		ping:         0,
		addr:         net.IPv4(1, 2, 3, 4).To4().String() + ":" + "35465",
		serverKey:    tPubKey,
		currentState: _StateActive,
		mx:           sync.Mutex{},
	}

	tPubKey, err = hex.DecodeString("4680cd40ea26311fe68a6ca0a3dd48aae19561b915ca870b2412d846ae8f53ae")
	if err != nil {
		t.Fatal("failed to prepare test public key, err: ", err)
	}

	kId, err = tl.Hash(adnl.PublicKeyED25519{Key: tPubKey})
	if err != nil {
		t.Fatal("failed to prepare test key id, err: ", err)
	}
	tNode2 := &dhtNode{
		adnlId:       kId,
		ping:         0,
		addr:         net.IPv4(1, 2, 3, 4).To4().String() + ":" + "35465",
		serverKey:    tPubKey,
		currentState: _StateFail,
		mx:           sync.Mutex{},
	}

	tPubKey, err = hex.DecodeString("63c92be0faffbda7dcc32a4380a19c98a75a6d58b9aceadb02cc0bc0bfd6b7d3")
	if err != nil {
		t.Fatal("failed to prepare test public key, err: ", err)
	}

	kId, err = tl.Hash(adnl.PublicKeyED25519{Key: tPubKey})
	if err != nil {
		t.Fatal("failed to prepare test key id, err: ", err)
	}
	tNode3 := &dhtNode{
		adnlId:       kId,
		ping:         0,
		addr:         net.IPv4(1, 2, 3, 4).To4().String() + ":" + "35465",
		serverKey:    tPubKey,
		currentState: _StateActive,
		mx:           sync.Mutex{},
	}

	tests := []struct {
		testNode *dhtNode
		testId   []byte
		want     int
	}{
		{tNode1, []byte{0b00100100, 0b10100100, 0b00100101}, 1<<30 + 2097152},
		{tNode2, []byte{0b00100100, 0b10100100, 0b00100101}, 1<<30 + 1048576 - 1<<20},
		{tNode3, []byte{0b00100100, 0b10100100, 0b00100101}, 1<<30 + 0},
	}
	for _, test := range tests {
		t.Run("distance test", func(t *testing.T) {
			res := test.testNode.distance(test.testId)
			if res != test.want {
				t.Errorf("got '%d', want '%d'", res, test.want)
			}
		})
	}
}

func TestNode_weight2(t *testing.T) {
	key, err := hex.DecodeString("75b9507dc58a931ea6e860d444987e82d8501e09191264c35b95f6952d8debe4")
	if err != nil {
		t.Fatal("failed to prepare test public key, err: ", err)
	}

	tPubKey, err := hex.DecodeString("75b9507dc58a931ea6e860d444987e82d8501e09191264c35b95f6956d8debe4")
	if err != nil {
		t.Fatal("failed to prepare test public key, err: ", err)
	}

	kId, err := tl.Hash(adnl.PublicKeyED25519{Key: tPubKey})
	if err != nil {
		t.Fatal("failed to prepare test key id, err: ", err)
	}
	tNode1 := &dhtNode{
		adnlId:       kId,
		ping:         100000,
		addr:         net.IPv4(1, 2, 3, 4).To4().String() + ":" + "35465",
		serverKey:    tPubKey,
		currentState: _StateActive,
	}
	tNode2 := &dhtNode{
		adnlId:       kId,
		ping:         5000000,
		addr:         net.IPv4(1, 2, 3, 4).To4().String() + ":" + "35465",
		serverKey:    tPubKey,
		currentState: _StateActive,
	}
	tNode3 := &dhtNode{
		adnlId:       kId,
		ping:         100000,
		addr:         net.IPv4(1, 2, 3, 4).To4().String() + ":" + "35465",
		serverKey:    tPubKey,
		currentState: _StateFail,
	}

	println(tNode1.distance(key), tNode2.distance(key), tNode3.distance(key))
}

func TestNode_xor(t *testing.T) {
	tests := []struct {
		give1 []byte
		give2 []byte
		want  []byte
	}{
		{
			[]byte{0b10001111},
			[]byte{0b10001111},
			[]byte{0b00000000},
		},
		{
			[]byte{0b00001111, 0b10001111},
			[]byte{0b10001111},
			[]byte{0b10000000},
		},
		{
			[]byte{0b00001111, 0b10001110},
			[]byte{0b10001111},
			[]byte{0b10000000},
		},
		{
			[]byte{0b00001111},
			[]byte{0b10001111, 0b10001111},
			[]byte{0b10000000},
		},
		{
			[]byte{0b01101110},
			[]byte{0b10001111, 0b10001111},
			[]byte{0b11100001},
		},
		{
			[]byte{0b00000000, 0b00000000, 0b00000000},
			[]byte{0b10001111, 0b10001111, 0b10001111},
			[]byte{0b10001111, 0b10001111, 0b10001111},
		},
		{
			[]byte{0b00000000, 0b00000100, 0b00000000},
			[]byte{0b00000000, 0b00000000, 0b00000000},
			[]byte{0b00000000, 0b00000100, 0b00000000},
		},
	}
	for _, test := range tests {
		t.Run("xor test", func(t *testing.T) {
			res := xor(test.give1, test.give2)
			if bytes.Equal(res, test.want) != true {
				t.Errorf("got '%b', want '%b'", res, test.want)
			}
		})
	}
}

func TestNode_leadingZeroBits(t *testing.T) {
	tests := []struct {
		give []byte
		want int
	}{
		{[]byte{0b10001111}, 0},
		{[]byte{0b00001111}, 4},
		{[]byte{0b01001111}, 1},
		{[]byte{0b00000000}, 8},
		{[]byte{0b00000001}, 7},
		{[]byte{0b00000000, 0b00000000, 0b00000000}, 24},
		{[]byte{0b00000000, 0b00000000, 0b00000001}, 23},
		{[]byte{0b00000000, 0b10000000, 0b10000001}, 8},
		{[]byte{0b00000111}, 5},
		{[]byte{0b00011111}, 3},
		{[]byte{0b00000011}, 6},
	}
	for _, test := range tests {
		t.Run("leadingZeroBits test", func(t *testing.T) {
			res := leadingZeroBits(test.give)
			if res != test.want {
				t.Errorf("got '%d', want '%d'", res, test.want)
			}
		})
	}
}
