package dht

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"reflect"
	"testing"
	"testing/synctest"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
)

func TestNearestNodeSearchWaitsForCloserInflight(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(map[bool]string{true: "success", false: "failure"}[success], func(t *testing.T) {
			search := newNearestNodeSearch(make([]byte, 32), 2, 4)
			closest := newBucketTestNode(1)
			middle := newBucketTestNode(2)
			farthest := newBucketTestNode(3)
			for _, node := range []*dhtNode{closest, middle, farthest} {
				search.Add(node)
			}
			for range 3 {
				search.Next()
			}
			search.Finish(middle, true)
			search.Finish(farthest, true)
			if search.CanReturn() {
				t.Fatal("search finished while the closest node was still in flight")
			}

			search.Finish(closest, success)
			if !search.CanReturn() {
				t.Fatal("search did not finish after the closest node completed")
			}
			want := []*dhtNode{middle, farthest}
			if success {
				want = []*dhtNode{closest, middle}
			}
			if got := search.Results(); !reflect.DeepEqual(got, want) {
				t.Fatal("search returned the wrong nearest nodes")
			}
		})
	}
}

func TestClient_collectNearestNodesWaitsForCloserInflight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		releaseClosest := make(chan struct{})
		gateway := &MockGateway{}
		gateway.setReg(func(addr string, peerKey ed25519.PublicKey) (adnl.Peer, error) {
			return &MockADNL{query: func(ctx context.Context, req, result tl.Serializable) error {
				if addr == "closest" {
					select {
					case <-releaseClosest:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				reflect.ValueOf(result).Elem().Set(reflect.ValueOf(NodesList{}))
				return nil
			}}, nil
		})

		client := &Client{gateway: gateway, selfID: make([]byte, 32), k: 2, a: 3}
		for i := range client.buckets {
			client.buckets[i] = newBucket(client.k)
		}
		for i, addr := range []string{"closest", "middle", "farthest"} {
			node := newBucketTestNode(byte(i + 1))
			node.client = client
			node.addr = addr
			client.buckets[affinity(node.adnlId, client.selfID)].addNode(node, true)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan []*dhtNode, 1)
		go func() {
			done <- client.collectNearestNodes(ctx, make([]byte, 32))
		}()

		synctest.Wait()
		select {
		case <-done:
			t.Fatal("lookup returned the farther nodes before the closest node responded")
		default:
		}

		close(releaseClosest)
		synctest.Wait()
		nodes := <-done
		if len(nodes) != 2 {
			t.Fatalf("expected 2 nodes, got %d", len(nodes))
		}
		for i, node := range nodes {
			if !bytes.Equal(node.adnlId, newBucketTestNode(byte(i+1)).adnlId) {
				t.Fatalf("unexpected node at position %d: %x", i, node.adnlId)
			}
		}
	})
}
