package dht

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
)

func TestClientAdvertisesOnlyValidatedResponders(t *testing.T) {
	for _, tt := range []struct {
		name       string
		reply      tl.Serializable
		wantActive bool
	}{
		{name: "unexpected response", reply: Pong{}},
		{name: "valid response", reply: NodesList{}, wantActive: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gateway := &MockGateway{}
			gateway.setReg(func(string, ed25519.PublicKey) (adnl.Peer, error) {
				return &MockADNL{query: func(ctx context.Context, req, result tl.Serializable) error {
					reflect.ValueOf(result).Elem().Set(reflect.ValueOf(tt.reply))
					return nil
				}}, nil
			})
			client, err := NewClient(gateway, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()

			descriptor, err := newCorrectNode(192, 0, 2, 1, 17555)
			if err != nil {
				t.Fatal(err)
			}
			node, err := client.addNode(descriptor)
			if err != nil {
				t.Fatal(err)
			}
			client.collectNearestNodes(context.Background(), node.adnlId)
			client.buckets[affinity(node.adnlId, client.selfID)].promoteReady()

			server := &Server{Client: client}
			advertised := server.getNearestNodes(node.adnlId, 10)
			if got := len(advertised) > 0; got != tt.wantActive {
				t.Fatalf("advertised peer = %v, want %v", got, tt.wantActive)
			}
			if node.isReady() != tt.wantActive {
				t.Fatalf("ready = %v, want %v", node.isReady(), tt.wantActive)
			}
		})
	}
}

func TestNodeInvalidSignedAddressListKeepsFailureCount(t *testing.T) {
	descriptor, err := newCorrectNode(192, 0, 2, 1, 17555)
	if err != nil {
		t.Fatal(err)
	}
	invalid := *cloneNode(descriptor)
	invalid.Version++ // The signature no longer matches the descriptor.

	gateway := &MockGateway{}
	gateway.setReg(func(string, ed25519.PublicKey) (adnl.Peer, error) {
		return &MockADNL{query: func(ctx context.Context, req, result tl.Serializable) error {
			reflect.ValueOf(result).Elem().Set(reflect.ValueOf(invalid))
			return nil
		}}, nil
	})
	client, err := NewClient(gateway, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	node, err := client.addNodeWithStatus(descriptor, true)
	if err != nil {
		t.Fatal(err)
	}
	for range _MaxFailCount + 1 {
		if _, err := node.getSignedAddressList(context.Background()); err == nil {
			t.Fatal("accepted an invalid signed address list")
		}
		node.markPingFailure()
	}
	if node.isReady() {
		t.Fatal("invalid responses reset ping failures and kept the node ready")
	}
}

func TestServerRefreshRejectsForeignSignedNode(t *testing.T) {
	source, err := newCorrectNode(192, 0, 2, 1, 17555)
	if err != nil {
		t.Fatal(err)
	}
	injected, err := newCorrectNodeWithVersion(192, 0, 2, 2, 17555, source.Version+1)
	if err != nil {
		t.Fatal(err)
	}
	injectedID, err := tl.Hash(injected.ID)
	if err != nil {
		t.Fatal(err)
	}

	var injectedQueries atomic.Int32
	gateway := &MockGateway{}
	gateway.setReg(func(addr string, key ed25519.PublicKey) (adnl.Peer, error) {
		if addr != "192.0.2.1:17555" {
			injectedQueries.Add(1)
			return nil, fmt.Errorf("injected endpoint is unreachable")
		}
		return &MockADNL{query: func(ctx context.Context, req, res tl.Serializable) error {
			reflect.ValueOf(res).Elem().Set(reflect.ValueOf(*injected))
			return nil
		}}, nil
	})
	client, err := NewClient(gateway, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	node, err := client.addNode(source)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{Client: client}
	server.refreshNodes()

	for _, advertised := range server.getNearestNodes(injectedID, 10) {
		id, err := tl.Hash(advertised.ID)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(id, injectedID) {
			t.Fatalf("advertised a foreign node without checking its endpoint; queries: %d", injectedQueries.Load())
		}
	}
	if client.buckets[affinity(injectedID, client.selfID)].findNode(injectedID) != nil {
		t.Fatal("foreign signed node entered the routing table")
	}
	if node.isReady() {
		t.Fatal("a reply signed by another key made the queried node ready")
	}
}
