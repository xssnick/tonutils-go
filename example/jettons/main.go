package main

import (
	"context"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/jetton"
	"github.com/xssnick/tonutils-go/ton/nft"
	"log"
	"strconv"
)

func main() {
	client := liteclient.NewConnectionPool()

	// connect to testnet lite server
	err := client.AddConnectionsFromConfigUrl(context.Background(), "https://ton.org/global.config.json")
	if err != nil {
		panic(err)
	}

	ctx := client.StickyContext(context.Background())

	// initialize ton api lite connection wrapper
	api := ton.NewAPIClient(client)

	tokenContract := address.MustParseAddr("EQBCFwW8uFUh-amdRmNY9NyeDEaeDYXd9ggJGsicpqVcHq7B")
	master := jetton.NewJettonMasterClient(api, tokenContract)

	data, err := master.GetJettonData(ctx)
	if err != nil {
		log.Fatal(err)
	}

	decimals := 9
	content := data.Content.(*nft.ContentOnchain)
	log.Println("total supply:", data.TotalSupply.Uint64())
	log.Println("mintable:", data.Mintable)
	log.Println("admin addr:", data.AdminAddr)
	log.Println("onchain content:")
	log.Println("	name:", content.Name)
	log.Println("	symbol:", content.GetAttribute("symbol"))
	if content.GetAttribute("decimals") != "" {
		decimals, err = strconv.Atoi(content.GetAttribute("decimals"))
		if err != nil {
			log.Fatal("invalid decimals")
		}
	}
	log.Println("	decimals:", decimals)
	log.Println("	description:", content.Description)
	log.Println()

	tokenWallet, err := master.GetJettonWallet(ctx, address.MustParseAddr("EQCWdteEWa4D3xoqLNV0zk4GROoptpM1-p66hmyBpxjvbbnn"))
	if err != nil {
		log.Fatal(err)
	}

	tokenBalance, err := tokenWallet.GetBalance(ctx)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("jetton balance:", tlb.MustFromNano(tokenBalance, decimals))
}
