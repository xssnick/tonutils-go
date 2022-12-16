package dht

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/liteclient"
	"testing"
	"time"
)

func TestClient_FindValue1(t *testing.T) {
	testAddr := "516618cf6cbe9004f6883e742c9a2e3ca53ed02e3e36f4cef62a98ee1e449174" // ADNL address of foundation.ton
	//newADNL = func(key ed25519.PublicKey) (ADNL, error) {
	//	return MockADNL{
	//		connect: func(ctx context.Context, addr string) (err error) {
	//			return nil
	//		},
	//		query: func(ctx context.Context, req, result tl.Serializable) error {
	//			switch req.(type) {
	//			case SignedAddressListQuery:
	//				res := Node{
	//					"Fhldu4zlnb20/TUj9TXElZkiEmbndIiE/DXrbGKu+0c=",
	//					&address.List{
	//						Addresses: []*address.UDP{
	//							{net.IPv4(95, 217, 229, 89),
	//								14348,
	//							},
	//						},
	//						Version:    0,
	//						ReinitDate: 0,
	//						Priority:   0,
	//						ExpireAT:   0,
	//					},
	//					-1,
	//					[]byte{},
	//				}
	//				reflect.ValueOf(result).Elem().Set(reflect.ValueOf(res))
	//			}
	//
	//			//var list address.List
	//			//_, err := tl.Parse(&list, []byte(req), true)
	//			//if err != nil {
	//			//	return err
	//			//}
	//			return nil
	//		},
	//		setDisconnectHandler: func(handler func(addr string, key ed25519.PublicKey)) {
	//
	//		},
	//		close: func() error {
	//			return fmt.Errorf("lol3")
	//		},
	//	}, nil
	//}
	dhtCli, err := NewClientFromConfig(10*time.Second, &liteclient.GlobalConfig{
		Type: "config.global",
		DHT: liteclient.DHTConfig{
			Type: "dht.config.global",
			K:    6,
			A:    3,
			StaticNodes: liteclient.DHTNodes{
				Type: "dht.node",
				Nodes: []liteclient.DHTNode{
					//{
					//	Type: "dht.node",
					//	ID: liteclient.ServerID{
					//		"pub.ed25519",
					//		"C1uy64rfGxp10SPSqbsxWhbumy5SM0YbvljCudwpZeI="},
					//	AddrList: liteclient.DHTAddressList{
					//		"adnl.addressList",
					//		[]liteclient.DHTAddress{
					//			{
					//				"adnl.address.udp",
					//				-1307380867,
					//				15888,
					//			},
					//		},
					//		0,
					//		0,
					//		0,
					//		0},
					//	Version:   -1,
					//	Signature: "s+tnHMTzPYG8abau+1dUs8tBJ+CDt+jIPmGfaVd7nmfb1gt6lL10G2IwkNeWhkxjZcAHRc0azWFVxp+IjIOOBQ==",
					//},
					//{
					//	Type: "dht.node",
					//	ID: liteclient.ServerID{
					//		"pub.ed25519",
					//		"bn8klhFZgE2sfIDfvVI6m6+oVNi1nBRlnHoxKtR9WBU="},
					//	AddrList: liteclient.DHTAddressList{
					//		"adnl.addressList",
					//		[]liteclient.DHTAddress{
					//			{
					//				"adnl.address.udp",
					//				-1307380860,
					//				15888,
					//			},
					//		},
					//		0,
					//		0,
					//		0,
					//		0},
					//	Version:   -1,
					//	Signature: "fQ5zAa6ot4pfFWzvuJOR8ijM5ELWndSDsRhFKstW1tqVSNfwAdOC7tDC8mc4vgTJ6fSYSWmhnXGK/+T5f6sDCw==",
					//},
					{
						Type: "dht.node",
						ID: liteclient.ServerID{
							"pub.ed25519",
							"6PGkPQSbyFp12esf1NqmDOaLoFA8i9+Mp5+cAx5wtTU="},
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
				},
			},
		},
		Liteservers: nil,
		Validator:   liteclient.ValidatorConfig{},
	})
	if err != nil {
		t.Fatal(err)
	}
	//fmt.Println(dhtCli)
	//id, err := hex.DecodeString("e48f79ca38b9e6d75bb20c800b1c0e3b618bd1d2308b46d810bec167eb1f830b")
	//if err != nil {
	//	t.Fatal(err)
	//}

	//dhtClient := Client{
	//	knownNodesInfo: map[string]*Node{
	//		"e48f79ca38b9e6d75bb20c800b1c0e3b618bd1d2308b46d810bec167eb1f830b": {
	//			"Fhldu4zlnb20/TUj9TXElZkiEmbndIiE/DXrbGKu+0c=",
	//			&address.List{
	//				Addresses: []*address.UDP{
	//					{net.IPv4(95, 217, 229, 89),
	//						14348,
	//					},
	//				},
	//				Version:    0,
	//				ReinitDate: 0,
	//				Priority:   0,
	//				ExpireAT:   0,
	//			},
	//			-1,
	//			[]byte{},
	//		},
	//	},
	//	queryTimeout: 0,
	//	mx:           sync.RWMutex{},
	//	minNodeMx:    sync.Mutex{},
	//}
	siteAddr, err := hex.DecodeString(testAddr)
	if err != nil {
		t.Fatal(err)
	}
	res, err := base64.StdEncoding.DecodeString("YWRkcmVzcw==")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(123, string(res))
	time.Sleep(3 * time.Second)
	_, err = dhtCli.FindValue(context.Background(), &Key{
		ID:    siteAddr,
		Name:  []byte("address"),
		Index: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	//fmt.Println(123, json.Marshal(key))

}
