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
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tl"
	"net"
	"reflect"
	"testing"
	"time"
)

type MockADNL struct {
	connect              func(ctx context.Context, addr string) (err error)
	query                func(ctx context.Context, req, result tl.Serializable) error
	setDisconnectHandler func(handler func(addr string, key ed25519.PublicKey))
	close                func() error
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

func newCorrectNode() (*Node, error) {
	testNode := Node{
		adnl.PublicKeyED25519{},
		&address.List{
			Addresses: []*address.UDP{
				{net.IPv4(8, 8, 8, 8).To4(),
					14348,
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

func newCorrectValue(tAdnlAddr []byte) (*ValueFoundResult, error) {
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
	testAddr := "516618cf6cbe9004f6883e742c9a2e3ca53ed02e3e36f4cef62a98ee1e449174"

	newADNL = func(key ed25519.PublicKey) (ADNL, error) {
		return MockADNL{
			connect: func(ctx context.Context, addr string) (err error) {
				return nil
			},
			query: func(ctx context.Context, req, result tl.Serializable) error {
				switch request := req.(type) {
				case SignedAddressListQuery:
					testNode, err := newCorrectNode()
					if err != nil {
						t.Fatal("failed creating test node, err: ", err.Error())
					}

					reflect.ValueOf(result).Elem().Set(reflect.ValueOf(*testNode))
				case tl.Raw:
					var _req FindValue
					_, err := tl.Parse(&_req, request, true)
					if err != nil {
						return err
					}

					addr, err := hex.DecodeString(testAddr)
					if err != nil {
						return err
					}

					k, err := adnl.ToKeyID(&Key{
						ID:    addr,
						Name:  []byte("address"),
						Index: 0,
					})
					if err != nil {
						return err
					}

					if bytes.Equal(k, _req.Key) {
						res, err := newCorrectValue(addr) //correct value if searching adnl "addr
						if err != nil {
							t.Fatal("failed creating test value, err: ", err.Error())
						}
						reflect.ValueOf(result).Elem().Set(reflect.ValueOf(*res))
					} else {
						reflect.ValueOf(result).Elem().Set(reflect.ValueOf(ValueNotFoundResult{Nodes: NodesList{nil}}))
					}
				}

				return nil
			},
			setDisconnectHandler: func(handler func(addr string, key ed25519.PublicKey)) {

			},
			close: func() error {
				return fmt.Errorf("lol3")
			},
		}, nil
	}

	dhtCli, err := NewClientFromConfig(10*time.Second, &liteclient.GlobalConfig{
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
				},
			},
		},
		Liteservers: nil,
		Validator:   liteclient.ValidatorConfig{},
	})
	if err != nil {
		t.Fatal(err)
	}

	siteAddr, err := hex.DecodeString(testAddr)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(1 * time.Second)

	_, err = dhtCli.FindValue(context.Background(), &Key{
		ID:    siteAddr,
		Name:  []byte("address"),
		Index: 0,
	})
	if err != nil {
		t.Fatal(err)
	}

}
