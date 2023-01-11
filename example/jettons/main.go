package main

import (
	"context"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/ton/jetton"
	"github.com/xssnick/tonutils-go/ton/nft"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"log"
	"strings"

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

	w := getWallet(api)

	scaleton := address.MustParseAddr("EQAs2mC5qJZ4G_10Nv0pJE2k82wByrPsZ61GyKxThz7MxuAT")
	master := jetton.NewJettonMasterClient(api, scaleton)

	data, err := master.GetJettonData(ctx)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("total supply:", data.TotalSupply.Uint64())
	log.Println("mintable:", data.Mintable)
	log.Println("admin addr:", data.AdminAddr)
	log.Println("offchain content uri:", data.Content.(*nft.ContentOnchain))
	log.Println()

	tokenWallet, err := master.GetJettonWallet(ctx, address.MustParseAddr("EQAzbDIfTrhUYBffr8jPWj71NYR5XybV0iexqEzbXY9HjU_u"))
	if err != nil {
		log.Fatal(err)
	}

	tokenBalance, err := tokenWallet.GetBalance(ctx)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("token balance:", tokenBalance.String())
	_ = w
}

func getWallet(api *ton.APIClient) *wallet.Wallet {
	words := strings.Split("cement secret mad fatal tip credit thank year toddler arrange good version melt truth embark debris execute answer please narrow fiber school achieve client", " ")
	w, err := wallet.FromSeed(api, words, wallet.V3)
	if err != nil {
		panic(err)
	}
	return w
}
