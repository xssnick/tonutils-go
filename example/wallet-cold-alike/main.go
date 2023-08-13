package main

import (
	"context"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"log"
	"strings"
)

func main() {
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

	w, err := wallet.FromSeed(api, words, wallet.V3)
	if err != nil {
		log.Fatalln("FromSeed err:", err.Error())
		return
	}

	log.Println("wallet address:", w.Address())

	block, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		log.Fatalln("CurrentMasterchainInfo err:", err.Error())
		return
	}

	balance, err := w.GetBalance(ctx, block)
	if err != nil {
		log.Fatalln("GetBalance err:", err.Error())
		return
	}

	if balance.NanoTON().Uint64() >= 3000000 {
		addr := address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N")

		log.Println("sending transaction and waiting for confirmation...")

		// default message ttl is 3 minutes, it is time during which you can send it to blockchain
		// if you need to set longer TTL, you could use this method
		// w.GetSpec().(*wallet.SpecV3).SetMessagesTTL(uint32((10 * time.Minute) / time.Second))

		// if destination wallet is not initialized you should set bounce = true
		msg, err := w.BuildTransfer(addr, tlb.MustFromTON("0.003"), false, "Hello from tonutils-go!")
		if err != nil {
			log.Fatalln("BuildTransfer err:", err.Error())
			return
		}

		// pack message to send later or from other place
		ext, err := w.BuildExternalMessage(ctx, msg)
		if err != nil {
			log.Fatalln("BuildExternalMessage err:", err.Error())
			return
		}

		// if you wish to send it from diff source, or later, you could serialize it to BoC
		// msgCell, _ := ext.ToCell()
		// log.Println(base64.StdEncoding.EncodeToString(msgCell.ToBOC()))

		// send message to blockchain
		err = api.SendExternalMessage(ctx, ext)
		if err != nil {
			log.Fatalln("Failed to send external message:", err.Error())
			return
		}

		log.Println("transaction sent")

		return
	}

	log.Println("not enough balance:", balance.String())
}
