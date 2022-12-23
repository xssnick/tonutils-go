package http

import (
	"context"
	"encoding/hex"
	"github.com/xssnick/tonutils-go/adnl/dht"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/dns"
	"net/http"
	"testing"
)

func Test_parseADNLAddress(t *testing.T) {
	res, err := parseADNLAddress("ui52b4urpcoigi26kfwp7vt2cgs2b5ljudwigvra35nhvymdqvqlfsa")
	if err != nil {
		t.Fatal(err)
	}

	if hex.EncodeToString(res) != "2d11dd07948bc4e4191af28b67feb3d08d2d07ab4d07641ab106fad3d70c1c2b" {
		t.Fatal("incorrect result")
	}
}

func TestTransport_RoundTrip(t *testing.T) {
	dhtClient, err := dht.NewClientFromConfigUrl(context.Background(), "https://ton-blockchain.github.io/global.config.json")
	if err != nil {
		t.Fatal(err)
	}

	transport := NewTransport(dhtClient, getDNSResolver())

	req, err := http.NewRequest(http.MethodGet, "http://foundation.ton/", nil)
	if err != nil {
		t.Fatal(err)
	}

	response, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		t.Errorf("got response code '%d', want '200'", response.StatusCode)
	}
}

func getDNSResolver() *dns.Client {
	client := liteclient.NewConnectionPool()

	// connect to testnet lite server
	err := client.AddConnectionsFromConfigUrl(context.Background(), "https://ton-blockchain.github.io/global.config.json")
	if err != nil {
		panic(err)
	}

	// initialize ton api lite connection wrapper
	api := ton.NewAPIClient(client)

	// get root dns address from network config
	root, err := dns.RootContractAddr(api)
	if err != nil {
		panic(err)
	}

	return dns.NewDNSClient(api, root)
}
