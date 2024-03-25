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
	tokenWallet, err := token.GetJettonWallet(ctx, w.WalletAddress())
	if err != nil {
		log.Fatal(err)
	}

	tokenBalance, err := tokenWallet.GetBalance(ctx)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("our jetton balance:", tokenBalance.String())

	amountTokens := tlb.MustFromDecimal("0.1", 9)

	comment, err := wallet.CreateCommentCell("Hello from tonutils-go!")
	if err != nil {
		log.Fatal(err)
	}

	// address of receiver's wallet (not token wallet, just usual)
	to := address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N")
	transferPayload, err := tokenWallet.BuildTransferPayloadV2(to, to, amountTokens, tlb.ZeroCoins, comment, nil)
	if err != nil {
		log.Fatal(err)
	}

	// your TON balance must be > 0.05 to send
	msg := wallet.SimpleMessage(tokenWallet.Address(), tlb.MustFromTON("0.05"), transferPayload)

	log.Println("sending transaction...")
	tx, _, err := w.SendWaitTransaction(ctx, msg)
	if err != nil {
		panic(err)
	}
	log.Println("transaction confirmed, hash:", base64.StdEncoding.EncodeToString(tx.Hash))
}
