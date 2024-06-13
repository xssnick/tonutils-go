package main

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/dht"
	rldphttp "github.com/xssnick/tonutils-go/adnl/rldp/http"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/dns"
	"io"
	"net/http"
)

func main() {
	_, clientKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic(err)
	}

	gateway := adnl.NewGateway(clientKey)
	err = gateway.StartClient()
	if err != nil {
		panic(err)
	}

	dhtClient, err := dht.NewClientFromConfigUrl(context.Background(), gateway, "https://ton.org/global.config.json")
	if err != nil {
		panic(err)
	}

	client := &http.Client{
		Transport: rldphttp.NewTransport(dhtClient, getDNSResolver()),
	}

	resp, err := client.Get("http://utils.ton/")
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	fmt.Println("Status code:", resp.StatusCode, resp.Status)
	fmt.Println("Response:\n", string(data))
}

func getDNSResolver() *dns.Client {
	client := liteclient.NewConnectionPool()

	// connect to testnet lite server
	err := client.AddConnectionsFromConfigUrl(context.Background(), "https://ton.org/global.config.json")
	if err != nil {
		panic(err)
	}

	// initialize ton api lite connection wrapper
	api := ton.NewAPIClient(client)

	// get root dns address from network config
	root, err := dns.GetRootContractAddr(context.Background(), api)
	if err != nil {
		panic(err)
	}

	return dns.NewDNSClient(api, root)
}
