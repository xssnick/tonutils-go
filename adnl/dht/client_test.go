package dht

import (
	"context"
	"encoding/hex"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/liteclient"
	"sync"
	"testing"
	"time"
)

type MockAPI struct {
	getConfigFromUrl func(ctx context.Context) *liteclient.GlobalConfig
	newClient        func(connectTimeout time.Duration, nodes []NodeInfo) (*Client, error)
}

func (m MockAPI) GetConfigFromUrl(ctx context.Context) *liteclient.GlobalConfig {
	return m.getConfigFromUrl(ctx)
}

func (m MockAPI) NewClient(connectTimeout time.Duration, nodes []NodeInfo) (*Client, error) {
	return m.newClient(connectTimeout, nodes)
}

func TestClient_FindValue(t *testing.T) {
	testAddr := "516618cf6cbe9004f6883e742c9a2e3ca53ed02e3e36f4cef62a98ee1e449174" // ADNL address of foundation.ton
	//
	//m.getConfigFromUrl = func(ctx context.Context) *liteclient.GlobalConfig {
	//	cfg := &liteclient.GlobalConfig{
	//		Type: "config.global",
	//		DHT: liteclient.DHTConfig{
	//			Type: "dht.config.global",
	//			K:    6,
	//			A:    3,
	//			StaticNodes: liteclient.DHTNodes{
	//				Type: "dht.node",
	//				Nodes: []liteclient.DHTNode{
	//					{
	//						Type: "dht.node",
	//						ID: liteclient.ServerID{
	//							"pub.ed25519",
	//							"C1uy64rfGxp10SPSqbsxWhbumy5SM0YbvljCudwpZeI="},
	//						AddrList: liteclient.DHTAddressList{
	//							"adnl.addressList",
	//							[]liteclient.DHTAddress{
	//								{
	//									"adnl.address.udp",
	//									-1307380867,
	//									15888,
	//								},
	//							},
	//							0,
	//							0,
	//							0,
	//							0},
	//						Version:   -1,
	//						Signature: "s+tnHMTzPYG8abau+1dUs8tBJ+CDt+jIPmGfaVd7nmfb1gt6lL10G2IwkNeWhkxjZcAHRc0azWFVxp+IjIOOBQ==",
	//					},
	//					{
	//						Type: "dht.node",
	//						ID: liteclient.ServerID{
	//							"pub.ed25519",
	//							"key"},
	//						AddrList: liteclient.DHTAddressList{
	//							"adnl.addressList",
	//							[]liteclient.DHTAddress{
	//								{
	//									"adnl.address.udp",
	//									-1111111111,
	//									11111,
	//								},
	//							},
	//							0,
	//							0,
	//							0,
	//							0},
	//						Version:   -1,
	//						Signature: "1111111zPYG8abau+1dUs8tBJ+CDt+jIPmGfaVd7nmfb1gt6lL10G2IwkNeWhkxjZcAHRc0azWFVxp+IjIOOBQ==",
	//					},
	//				},
	//			},
	//		},
	//		Liteservers: nil,
	//		Validator:   liteclient.ValidatorConfig{},
	//	}
	//	return cfg
	//}
	//cfg := m.GetConfigFromUrl(context.Background())

	id1, err := hex.DecodeString("1f33660985679d67234cbffe3a901b509e7308b04aaaddcd4df56d9378326c35")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := hex.DecodeString("9cf5d80d05522d7a4f3bb949f35f2c0bf57c0727f2c6c59f5ee8762860959d9f")
	if err != nil {
		t.Fatal(err)
	}
	dhtClient := Client{
		activeNodes: map[string]*dhtNode{
			"1f33660985679d67234cbffe3a901b509e7308b04aaaddcd4df56d9378326c35": {
				id:   id1,
				adnl: &adnl.ADNL{},
				ping: 0,
			},
			"9cf5d80d05522d7a4f3bb949f35f2c0bf57c0727f2c6c59f5ee8762860959d9f": {
				id:   id2,
				adnl: &adnl.ADNL{},
				ping: 0,
			},
		},
		knownNodesInfo: map[string]*Node{},
		queryTimeout:   0,
		mx:             sync.RWMutex{},
		minNodeMx:      sync.Mutex{},
	}

	siteAddr, err := hex.DecodeString(testAddr)
	if err != nil {
		t.Fatal(err)
	}

	_, err = dhtClient.FindValue(context.Background(), &Key{
		ID:    siteAddr,
		Name:  []byte("address"),
		Index: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
}
