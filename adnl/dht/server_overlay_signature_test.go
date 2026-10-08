package dht

import (
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl/overlay"
)

func TestServerStoreOverlayNodesNetworkSignatures(t *testing.T) {
	for _, tc := range []struct {
		name      string
		prefixed  bool
		networkID int32
		wantError bool
	}{
		{name: "legacy"},
		{name: "matching network", prefixed: true, networkID: 42},
		{name: "unknown network", prefixed: true, networkID: _UnknownNetworkID},
		{name: "other network", prefixed: true, networkID: 43, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newStoreRegressionServer(t, NewMemoryValueStore(16))
			server.networkID = 42
			server.ourValues = map[string]*Value{}
			_, key, err := ed25519.GenerateKey(nil)
			if err != nil {
				t.Fatal(err)
			}
			overlayKey := []byte("network-signature-overlay")
			node, err := overlay.NewNode(overlayKey, key)
			if err != nil {
				t.Fatal(err)
			}
			if tc.prefixed {
				signature := make([]byte, 4+len(node.Signature))
				binary.LittleEndian.PutUint32(signature[:4], uint32(tc.networkID))
				copy(signature[4:], node.Signature)
				node.Signature = signature
			}

			stored, _, err := server.StoreOverlayNodes(context.Background(), overlayKey,
				&overlay.NodesList{List: []overlay.Node{*node}}, time.Minute)
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), "wrong network id") {
					t.Fatalf("expected wrong-network rejection, got %v", err)
				}
				if len(server.ourValues) != 0 {
					t.Fatal("rejected overlay value was cached for publication")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if stored != 1 {
				t.Fatalf("stored %d replicas, want 1", stored)
			}
			found, _, err := server.FindOverlayNodes(context.Background(), overlayKey)
			if err != nil {
				t.Fatal(err)
			}
			if len(found.List) != 1 || string(found.List[0].Signature) != string(node.Signature) {
				t.Fatal("stored overlay node lost its original signature")
			}
		})
	}
}
