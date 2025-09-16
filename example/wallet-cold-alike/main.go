package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"log"
	"strings"
)

func main() {
	// for completely offline mode you could use liteclient.NewOfflineClient() instead
	client := liteclient.NewConnectionPool()

	// connect to mainnet lite server
	err := client.AddConnection(context.Background(), "135.181.140.212:13206", "K0t3+IWLOXHYMvMcrGZDPs+pn58a17LFbnXoQkKc2xw=")
	if err != nil {
		log.Fatalln("connection err: ", err.Error())
		return
	}

	api := ton.NewAPIClient(client)
	// bound all requests to single ton node
	ctx := client.StickyContext(context.Background())

	// seed words of account, you can generate them with any wallet or using wallet.NewSeed() method
	words := strings.Split("birth pattern then forest walnut then phrase walnut fan pumpkin pattern then cluster blossom verify then forest velvet pond fiction pattern collect then then", " ")

	w, err := wallet.FromSeedWithOptions(api, words, wallet.V3)
	if err != nil {
		log.Fatalln("FromSeed err:", err.Error())
		return
	}

	log.Println("wallet address:", w.WalletAddress())

	addr := address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N")

	log.Println("sending transaction...")

	// default message ttl is 3 minutes, it is time during which you can send it to blockchain
	// if you need to set longer TTL, you could use this method
	// w.GetSpec().(*wallet.SpecV3).SetMessagesTTL(uint32((10 * time.Minute) / time.Second))

	w.GetSpec().(*wallet.SpecV3).SetSeqnoFetcher(func(ctx context.Context, sub uint32) (uint32, error) {
		// Get seqno from your database here, this func will be called during BuildTransfer to get seqno for transaction
		return 1, nil
	})

	comment, _ := wallet.CreateCommentCell("Hello from tonutils-go!")
	withStateInit := true // if wallet is initialized, you may set false to not send additional data

	// if destination wallet is not initialized you should set bounce = true
	ext, err := w.PrepareExternalMessageForMany(context.Background(), withStateInit, []*wallet.Message{
		wallet.SimpleMessageAutoBounce(addr, tlb.MustFromTON("0.003"), comment),
	})
	if err != nil {
		log.Fatalln("BuildTransfer err:", err.Error())
		return
	}

	// this hash could be used for transaction discovery in explorers
	log.Println("External message hash:", hex.EncodeToString(ext.NormalizedHash()))

	// if you wish to send message from diff source, or later, you could serialize it to BoC
	msgCell, _ := tlb.ToCell(ext)
	log.Println(base64.StdEncoding.EncodeToString(msgCell.ToBOC()))

	// send message to blockchain
	if err = api.SendExternalMessage(ctx, ext); err != nil {
		log.Fatalln("Failed to send external message:", err.Error())
		return
	}

	log.Println("transaction sent, we are not waiting for confirmation")

	return
}
