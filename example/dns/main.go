package main

import (
	"context"
	"github.com/xssnick/tonutils-go/ton/dns"
	"log"

	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/ton"
)

func main() {
	client := liteclient.NewConnectionPool()

	// connect to testnet lite server
	err := client.AddConnectionsFromConfigUrl(context.Background(), "https://ton-blockchain.github.io/testnet-global.config.json")
	if err != nil {
		panic(err)
	}

	// initialize ton api lite connection wrapper
	api := ton.NewAPIClient(client)

	resolver := dns.NewDNSClient(api, dns.RootContractAddr(api))

	domain, err := resolver.Resolve(context.Background(), "alice.ton")
	if err != nil {
		panic(err)
	}

	log.Println("wallet record:", domain.GetWalletRecord())

	nftData, err := domain.GetNFTData(context.Background())
	if err != nil {
		panic(err)
	}

	log.Println("domain owner:", nftData.OwnerAddress)
	log.Println("parent dns contract:", nftData.CollectionAddress)
}
