package dht

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
)

func newNearestSeedFixture(count int) *Client {
	client := &Client{selfID: make([]byte, 32), k: 10}
	for i := range client.buckets {
		client.buckets[i] = newBucket(client.k)
	}
	for i := 0; i < count; i++ {
		bit := i / 16
		hash := sha256.Sum256([]byte(fmt.Sprint(i)))
		id := hash[:]
		for prefix := 0; prefix < bit; prefix++ {
			setBit(id, prefix, false)
		}
		setBit(id, bit, true)
		node := &dhtNode{adnlId: id}
		client.buckets[bit].addNode(node, i%16 < 8)
		if i%7 == 0 {
			node.updateStatus(false)
		}
	}
	return client
}

// Keep the former insertion strategy as a test oracle and benchmark baseline.
func seedNearestIncrementally(client *Client, search *nearestNodeSearch) {
	var badNodes []*dhtNode
	for i := len(client.buckets) - 1; i >= 0; i-- {
		for _, node := range client.buckets[i].getNodes() {
			if node == nil {
				continue
			}
			if atomic.LoadInt32(&node.badScore) == 0 {
				search.Add(node)
			} else {
				badNodes = append(badNodes, node)
			}
		}
	}
	for _, node := range badNodes {
		search.Add(node)
	}
}

func TestClientSeedNearestNodeSearchPreservesCandidates(t *testing.T) {
	client := newNearestSeedFixture(64)
	for _, target := range []byte{0, 0x55, 0xff} {
		for _, populated := range []bool{false, true} {
			t.Run(fmt.Sprintf("target=%02x/populated=%v", target, populated), func(t *testing.T) {
				keyID := bytes.Repeat([]byte{target}, 32)
				search := newNearestNodeSearch(keyID, client.k, client.k*2)
				reference := newNearestNodeSearch(keyID, client.k, client.k*2)
				if populated {
					node := client.buckets[0].getNodes()[0]
					for _, state := range []*nearestNodeSearch{search, reference} {
						state.Add(node)
						state.Next()
						state.Finish(node, false)
						state.Retry(node)
					}
				}

				client.seedNearestNodeSearch(search)
				seedNearestIncrementally(client, reference)
				if !reflect.DeepEqual(search, reference) {
					t.Fatal("bulk seeding changed candidate order, deduplication, or pending states")
				}
				queried := 0
				for {
					node := search.Next()
					want := reference.Next()
					if node != want {
						t.Fatalf("candidate %d changed", queried)
					}
					if node == nil {
						break
					}
					search.Finish(node, false)
					reference.Finish(want, false)
					queried++
				}
				if queried != 64 {
					t.Fatalf("queried %d candidates, want 64", queried)
				}
			})
		}
	}
}

func BenchmarkDHTNearestNodeSearchSeed(b *testing.B) {
	for _, count := range []int{128, 256, 512} {
		client := newNearestSeedFixture(count)
		keyID := bytes.Repeat([]byte{0x55}, 32)
		for _, incremental := range []bool{false, true} {
			name := "bulk"
			if incremental {
				name = "incremental"
			}
			b.Run(fmt.Sprintf("%s/%d", name, count), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					search := newNearestNodeSearch(keyID, client.k, client.k*2)
					if incremental {
						seedNearestIncrementally(client, search)
					} else {
						client.seedNearestNodeSearch(search)
					}
				}
			})
		}
	}
}
