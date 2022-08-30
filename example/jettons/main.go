package main

import (
	"context"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
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
	err := client.AddConnectionsFromConfigUrl(context.Background(), "https://ton-blockchain.github.io/testnet-global.config.json")
	if err != nil {
		panic(err)
	}

	// initialize ton api lite connection wrapper
	api := ton.NewAPIClient(client)

	w := getWallet(api)

	scaleton := address.MustParseAddr("EQAbMQzuuGiCne0R7QEj9nrXsjM7gNjeVmrlBZouyC-SCLlO")
	master := jetton.NewJettonMasterClient(api, scaleton)

	data, err := master.GetJettonData(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	log.Println("total supply:", data.TotalSupply.Uint64())
	log.Println("mintable:", data.Mintable)
	log.Println("admin addr:", data.AdminAddr)
	log.Println("offchain content uri:", data.Content.(*nft.ContentOffchain).URI)
	log.Println()

	tokenWallet, err := master.GetJettonWallet(context.Background(), w.Address())
	if err != nil {
		log.Fatal(err)
	}

	tokenBalance, err := tokenWallet.GetBalance(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	log.Println("token balance:", tokenBalance.String())
	if tokenBalance.NanoTON().Uint64() > 10000000 {
		log.Println("transferring tokens...")

		to, err := w.GetSubwallet(2)
		if err != nil {
			log.Fatal(err)
		}

		amountTokens := tlb.MustFromTON("0.002")

		// transfer some to our sub-wallet
		transferPayload, err := tokenWallet.BuildTransferPayload(to.Address(), amountTokens, tlb.MustFromTON("0"), nil)
		if err != nil {
			panic(err)
		}

		msg := wallet.SimpleMessage(tokenWallet.Address(), tlb.MustFromTON("0.05"), transferPayload)

		err = w.Send(context.Background(), msg, true)
		if err != nil {
			panic(err)
		}

		log.Println("transfer completed!")
	} else {
		log.Println("transfer was skipped, not enough token balance")
	}
}

func getWallet(api *ton.APIClient) *wallet.Wallet {
	words := strings.Split("cement secret mad fatal tip credit thank year toddler arrange good version melt truth embark debris execute answer please narrow fiber school achieve client", " ")
	w, err := wallet.FromSeed(api, words, wallet.V3)
	if err != nil {
		panic(err)
	}
	return w
}
