package dht

import (
	"context"
	"crypto/ed25519"
	"reflect"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

func benchmarkDHTValue(b *testing.B) (*Value, []byte) {
	b.Helper()

	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		b.Fatal(err)
	}

	addrList := address.List{
		Addresses: []address.Address{
			&address.UDP{Port: 9999},
		},
	}
	data, err := tl.Serialize(addrList, true)
	if err != nil {
		b.Fatal(err)
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
		b.Fatal(err)
	}
	return &val, keyID
}

func BenchmarkDHTFindValueMock(b *testing.B) {
	val, _ := benchmarkDHTValue(b)
	node, err := newCorrectNode(1, 2, 3, 4, 12345)
	if err != nil {
		b.Fatal(err)
	}

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
					reflect.ValueOf(result).Elem().Set(reflect.ValueOf(ValueFoundResult{Value: *val}))
				}
				return nil
			},
		}, nil
	}

	dhtCli, err := NewClient(gateway, []*Node{node})
	if err != nil {
		b.Fatal(err)
	}

	key := &Key{
		ID:    append([]byte(nil), val.KeyDescription.Key.ID...),
		Name:  append([]byte(nil), val.KeyDescription.Key.Name...),
		Index: val.KeyDescription.Key.Index,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err = dhtCli.FindValue(context.Background(), key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDHTServerRefreshNodes(b *testing.B) {
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		b.Fatal(err)
	}
	pub := key.Public().(ed25519.PublicKey)
	id, err := tl.Hash(keys.PublicKeyED25519{Key: pub})
	if err != nil {
		b.Fatal(err)
	}

	gw := &MockGateway{
		pub: pub,
		id:  id,
		addresses: address.List{
			Addresses: []address.Address{
				&address.UDP{Port: 17555},
			},
			Version:    1,
			ReinitDate: 1,
		},
	}
	gw.reg = func(addr string, key ed25519.PublicKey) (adnl.Peer, error) {
		return &MockADNL{
			query: func(ctx context.Context, req, result tl.Serializable) error {
				node, err := newCorrectNode(1, 2, 3, 4, 17050)
				if err != nil {
					return err
				}
				reflect.ValueOf(result).Elem().Set(reflect.ValueOf(*node))
				return nil
			},
		}, nil
	}

	server, err := NewServer(gw, key, nil, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer server.Close()

	nodes := make([]*dhtNode, 0, 64)
	for i := 0; i < 64; i++ {
		n, err := newCorrectNode(1, 2, byte(i), 4, int32(17000+i))
		if err != nil {
			b.Fatal(err)
		}
		node, err := server.addNodeWithStatus(n, true)
		if err != nil {
			b.Fatal(err)
		}
		nodes = append(nodes, node)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, node := range nodes {
			node.lastPingAt = 0
			node.readyAt = 1
		}
		server.refreshNodes()
	}
}
