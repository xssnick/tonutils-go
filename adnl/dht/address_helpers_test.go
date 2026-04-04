package dht

import (
	"net"
	"testing"

	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/liteclient"
)

func TestFirstDialAddressIPv6(t *testing.T) {
	addr, dial, err := firstDialAddress([]address.Address{
		&address.UDP6{
			IP:   net.ParseIP("2001:db8::1"),
			Port: 40404,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if dial != "[2001:db8::1]:40404" {
		t.Fatalf("unexpected dial string %q", dial)
	}
	if addr == nil || !address.IPValue(addr).Equal(net.ParseIP("2001:db8::1")) {
		t.Fatalf("unexpected address %v", addr)
	}
}

func TestNodesFromConfigUDP6(t *testing.T) {
	nodes, err := nodesFromConfig(&liteclient.GlobalConfig{
		DHT: liteclient.DHTConfig{
			StaticNodes: liteclient.DHTNodes{
				Nodes: []liteclient.DHTNode{
					{
						ID: liteclient.ServerID{
							Type: "pub.ed25519",
							Key:  "6PGkPQSbyFp12esf1NqmDOaLoFA8i9+Mp5+cAx5wtTU=",
						},
						AddrList: liteclient.DHTAddressList{
							Addrs: []liteclient.DHTAddress{
								{
									Type: "adnl.address.udp6",
									IPv6: net.ParseIP("2001:db8::5"),
									Port: 50505,
								},
							},
						},
						Signature: "L4N1+dzXLlkmT5iPnvsmsixzXU0L6kPKApqMdcrGP5d9ssMhn69SzHFK+yIzvG6zQ9oRb4TnqPBaKShjjj2OBg==",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(nodes) != 1 {
		t.Fatalf("unexpected nodes len %d", len(nodes))
	}
	if len(nodes[0].AddrList.Addresses) != 1 {
		t.Fatalf("unexpected addr len %d", len(nodes[0].AddrList.Addresses))
	}
	if _, ok := nodes[0].AddrList.Addresses[0].(*address.UDP6); !ok {
		t.Fatalf("expected udp6 address, got %T", nodes[0].AddrList.Addresses[0])
	}
	if !address.IPValue(nodes[0].AddrList.Addresses[0]).Equal(net.ParseIP("2001:db8::5")) {
		t.Fatalf("unexpected ipv6 %v", address.IPValue(nodes[0].AddrList.Addresses[0]))
	}
}
