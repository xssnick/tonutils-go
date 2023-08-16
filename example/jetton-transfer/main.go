package main

import (
	"context"
	"encoding/base64"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/jetton"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"log"
	"strings"
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

	// seed words of account, you can generate them with any wallet or using wallet.NewSeed() method
	words := strings.Split("birth pattern then forest walnut then phrase walnut fan pumpkin pattern then cluster blossom verify then forest velvet pond fiction pattern collect then then", " ")

	w, err := wallet.FromSeed(api, words, wallet.V3R2)
	if err != nil {
		log.Fatalln("FromSeed err:", err.Error())
		return
	}

	token := jetton.NewJettonMasterClient(api, address.MustParseAddr("EQD0vdSA_NedR9uvbgN9EikRX-suesDxGeFg69XQMavfLqIw"))

	// find our jetton wallet
	tokenWallet, err := token.GetJettonWallet(ctx, w.Address())
	if err != nil {
		log.Fatal(err)
	}

	tokenBalance, err := tokenWallet.GetBalance(ctx)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("our jetton balance:", tokenBalance.String())

	amountTokens := tlb.MustFromDecimal("0.9", 9)

	// address of receiver's wallet (not token wallet, just usual)
	to := address.MustParseAddr("EQAvyxX5g_GvynfNl_XVQReZ3rstK5bM2OYu9nvren1SRnuN")
	transferPayload, err := tokenWallet.BuildTransferPayload(to, amountTokens, tlb.ZeroCoins, nil)
	if err != nil {
		log.Fatal(err)
	}

	msg := wallet.SimpleMessage(tokenWallet.Address(), tlb.MustFromTON("0.06"), transferPayload)

	log.Println("sending transaction...")
	tx, _, err := w.SendWaitTransaction(ctx, msg)
	if err != nil {
		panic(err)
	}
	log.Println("transaction confirmed:", base64.StdEncoding.EncodeToString(tx.Hash))
}
