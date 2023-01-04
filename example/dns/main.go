package main

import (
	"context"
	"encoding/hex"
	"github.com/xssnick/tonutils-go/ton/dns"
	"log"

	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/ton"
)

func main() {
	client := liteclient.NewConnectionPool()

	// connect to testnet lite server
	err := client.AddConnectionsFromConfigUrl(context.Background(), "https://ton-blockchain.github.io/global.config.json")
	if err != nil {
		panic(err)
	}

	ctx := client.StickyContext(context.Background())
	// initialize ton api lite connection wrapper
	api := ton.NewAPIClient(client)

	// get root dns address from network config
	root, err := dns.RootContractAddr(api)
	if err != nil {
		panic(err)
	}

	resolver := dns.NewDNSClient(api, root)
	domain, err := resolver.Resolve(ctx, "utils.ton")
	if err != nil {
		panic(err)
	}

	log.Println("wallet record:", domain.GetWalletRecord())
	log.Println("site record:", hex.EncodeToString(domain.GetSiteRecord()))

	nftData, err := domain.GetNFTData(ctx)
	if err != nil {
		panic(err)
	}

	log.Println("domain owner:", nftData.OwnerAddress)
	log.Println("parent dns contract:", nftData.CollectionAddress)
}
