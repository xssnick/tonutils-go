package dht

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

func TestServerStoreAndFindValue(t *testing.T) {
	srvPub, srvPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	serverGw := adnl.NewGateway(srvPriv)
	port := getFreePort(t)
	serverAddr := fmt.Sprintf("127.0.0.1:%d", port)
	if err = serverGw.StartServer(serverAddr); err != nil {
		t.Fatal(err)
	}
	defer serverGw.Close()

	srv, err := NewServer(serverGw, srvPriv, nil)
	if err != nil {
		t.Fatal(err)
	}
	if srv == nil {
		t.Fatal("server is nil")
	}

	_, clientPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	clientGw := adnl.NewGateway(clientPriv)
	if err = clientGw.StartClient(); err != nil {
		t.Fatal(err)
	}
	defer clientGw.Close()

	serverNode := &Node{
		ID: keys.PublicKeyED25519{Key: srvPub},
		AddrList: &address.List{Addresses: []*address.UDP{
			{IP: net.IPv4(127, 0, 0, 1), Port: int32(port)},
		}},
		Version: int32(time.Now().Unix()),
	}
	if err = signNode(serverNode, srvPriv); err != nil {
		t.Fatalf("failed to sign server node: %v", err)
	}

	client, err := NewClient(clientGw, []*Node{serverNode})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	replicas, keyID, err := client.Store(ctx, keys.PublicKeyUnEnc{Key: []byte("owner")}, []byte("test"), 0, []byte("payload"), UpdateRuleAnybody{}, time.Minute, nil, 1)
	if err != nil {
		t.Fatalf("failed to store value: %v", err)
	}
	if replicas == 0 {
		t.Fatal("value was not stored")
	}

	value, _, err := client.FindValue(ctx, &Key{ID: keyID, Name: []byte("test"), Index: 0})
	if err != nil {
		t.Fatalf("failed to find stored value: %v", err)
	}
	if string(value.Data) != "payload" {
		t.Fatalf("unexpected value payload: %q", string(value.Data))
	}
}

func TestServerHandlesQueryPrefix(t *testing.T) {
	srvPub, srvPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	serverGw := adnl.NewGateway(srvPriv)
	port := getFreePort(t)
	serverAddr := fmt.Sprintf("127.0.0.1:%d", port)
	if err = serverGw.StartServer(serverAddr); err != nil {
		t.Fatal(err)
	}
	defer serverGw.Close()

	if _, err = NewServer(serverGw, srvPriv, nil); err != nil {
		t.Fatal(err)
	}

	clientPub, clientPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	clientGw := adnl.NewGateway(clientPriv)
	if err = clientGw.StartClient(); err != nil {
		t.Fatal(err)
	}
	defer clientGw.Close()

	peer, err := clientGw.RegisterClient(serverAddr, srvPub)
	if err != nil {
		t.Fatalf("failed to register client: %v", err)
	}
	defer peer.Close()

	clientNode := &Node{
		ID: keys.PublicKeyED25519{Key: clientPub},
		AddrList: &address.List{Addresses: []*address.UDP{
			{IP: net.IPv4(10, 0, 0, 1), Port: 12345},
		}},
		Version: int32(time.Now().Unix()),
	}
	if err = signNode(clientNode, clientPriv); err != nil {
		t.Fatalf("failed to sign client node: %v", err)
	}

	keyHash, err := tl.Hash(keys.PublicKeyED25519{Key: clientPub})
	if err != nil {
		t.Fatalf("failed to compute key hash: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var nodes NodesList
	err = peer.Query(ctx, []tl.Serializable{Query{Node: clientNode}, FindNode{Key: keyHash, K: 3}}, &nodes)
	if err != nil {
		t.Fatalf("findNode query failed: %v", err)
	}

	if len(nodes.List) == 0 {
		t.Fatal("expected at least one node in response")
	}
	if err = nodes.List[0].CheckSignature(); err != nil {
		t.Fatalf("invalid node signature in response: %v", err)
	}
}

func TestServerGetSignedAddressList(t *testing.T) {
	srvPub, srvPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	serverGw := adnl.NewGateway(srvPriv)
	port := getFreePort(t)
	serverAddr := fmt.Sprintf("127.0.0.1:%d", port)
	if err = serverGw.StartServer(serverAddr); err != nil {
		t.Fatal(err)
	}
	defer serverGw.Close()

	if _, err = NewServer(serverGw, srvPriv, nil); err != nil {
		t.Fatal(err)
	}

	clientPriv := srvPriv // reuse key for simplicity
	clientGw := adnl.NewGateway(clientPriv)
	if err = clientGw.StartClient(); err != nil {
		t.Fatal(err)
	}
	defer clientGw.Close()

	peer, err := clientGw.RegisterClient(serverAddr, srvPub)
	if err != nil {
		t.Fatalf("failed to register client: %v", err)
	}
	defer peer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var node Node
	if err = peer.Query(ctx, SignedAddressListQuery{}, &node); err != nil {
		t.Fatalf("failed to query signed address list: %v", err)
	}
	if err = node.CheckSignature(); err != nil {
		t.Fatalf("invalid signature on returned node: %v", err)
	}
}

func getFreePort(t *testing.T) int {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.LocalAddr().(*net.UDPAddr).Port
}
