package dht

import (
	"bytes"
	"crypto/ed25519"
	"testing"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

func testPublicKeyBytes(id any) []byte {
	switch pub := id.(type) {
	case keys.PublicKeyED25519:
		return pub.Key
	case keys.PublicKeyAES:
		return pub.Key
	case keys.PublicKeyUnEnc:
		return pub.Key
	case keys.PublicKeyOverlay:
		return pub.Key
	default:
		panic("unsupported test public key")
	}
}

func TestMemoryValueStoreCopiesPublicKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   func([]byte) any
	}{
		{"ed25519", func(key []byte) any { return keys.PublicKeyED25519{Key: key} }},
		{"aes", func(key []byte) any { return keys.PublicKeyAES{Key: key} }},
		{"unenc", func(key []byte) any { return keys.PublicKeyUnEnc{Key: key} }},
		{"overlay", func(key []byte) any { return keys.PublicKeyOverlay{Key: key} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := bytes.Repeat([]byte{42}, 32)
			want := append([]byte(nil), key...)
			store := NewMemoryValueStore(1)
			keyID := []byte("key")
			if err := store.Put(keyID, &Value{KeyDescription: KeyDescription{ID: tc.id(key)}}); err != nil {
				t.Fatal(err)
			}

			key[0] ^= 1
			stored, err := store.Get(keyID)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(testPublicKeyBytes(stored.KeyDescription.ID), want) {
				t.Fatal("Put retained the caller's public-key slice")
			}

			testPublicKeyBytes(stored.KeyDescription.ID)[0] ^= 1
			stored, err = store.Get(keyID)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(testPublicKeyBytes(stored.KeyDescription.ID), want) {
				t.Fatal("Get exposed the stored public-key slice")
			}

			if err := store.ForEach(func(_ []byte, value *Value) error {
				testPublicKeyBytes(value.KeyDescription.ID)[0] ^= 1
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			stored, err = store.Get(keyID)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(testPublicKeyBytes(stored.KeyDescription.ID), want) {
				t.Fatal("ForEach exposed the stored public-key slice")
			}
		})
	}
}

func TestClientCopiesRoutingPublicKeys(t *testing.T) {
	descriptor, err := newCorrectNode(1, 2, 3, 4, 17003)
	if err != nil {
		t.Fatal(err)
	}
	id, err := tl.Hash(descriptor.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := append(ed25519.PublicKey(nil), descriptor.ID.(keys.PublicKeyED25519).Key...)
	client, err := NewClient(&MockGateway{}, []*Node{descriptor})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	descriptor.ID.(keys.PublicKeyED25519).Key[0] ^= 1
	exported := client.RoutingNodes()
	if len(exported) != 1 {
		t.Fatalf("got %d routing nodes, want 1", len(exported))
	}
	if err := exported[0].CheckSignature(); err != nil {
		t.Fatalf("caller mutation corrupted routing descriptor: %v", err)
	}
	internal := client.buckets[affinity(id, client.selfID)].findNode(id)
	if !bytes.Equal(internal.snapshot().serverKey, want) {
		t.Fatal("caller mutation corrupted the connection public key")
	}

	exported[0].ID.(keys.PublicKeyED25519).Key[0] ^= 1
	if err := client.RoutingNodes()[0].CheckSignature(); err != nil {
		t.Fatalf("snapshot mutation corrupted routing descriptor: %v", err)
	}
	if !bytes.Equal(internal.snapshot().serverKey, want) {
		t.Fatal("snapshot mutation corrupted the connection public key")
	}
}

func TestBuildSignedNodeCopiesPublicKey(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := newCorrectNode(1, 2, 3, 4, 17003)
	if err != nil {
		t.Fatal(err)
	}
	node, err := BuildSignedNode(keys.PublicKeyED25519{Key: pub}, descriptor.AddrList, 1, _UnknownNetworkID, key)
	if err != nil {
		t.Fatal(err)
	}

	pub[0] ^= 1
	if err := node.CheckSignature(); err != nil {
		t.Fatalf("caller mutation invalidated the signed node: %v", err)
	}
}
