package main

import (
	"context"
	"encoding/base64"
	"log"
	"strings"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/wallet"
)

func main() {
	client := liteclient.NewConnectionPool()

	// connect to mainnet lite server
	err := client.AddConnection(context.Background(), "135.181.140.212:13206", "K0t3+IWLOXHYMvMcrGZDPs+pn58a17LFbnXoQkKc2xw=")
	if err != nil {
		log.Fatalln("connection err: ", err.Error())
		return
	}

	api := ton.NewAPIClient(client, ton.ProofCheckPolicyFast).WithRetry()

	// seed words of account, you can generate them with any wallet or using wallet.NewSeed() method
	words := strings.Split("birth pattern then forest walnut then phrase walnut fan pumpkin pattern then cluster blossom verify then forest velvet pond fiction pattern collect then then", " ")

	// initialize high-load wallet
	w, err := wallet.FromSeed(api, words, wallet.HighloadV2R2)
	if err != nil {
		log.Fatalln("FromSeed err:", err.Error())
		return
	}

	log.Println("wallet address:", w.WalletAddress())

	block, err := api.CurrentMasterchainInfo(context.Background())
	if err != nil {
		log.Fatalln("CurrentMasterchainInfo err:", err.Error())
		return
	}

	balance, err := w.GetBalance(context.Background(), block)
	if err != nil {
		log.Fatalln("GetBalance err:", err.Error())
		return
	}

	// source to create messages from
	var receivers = map[string]string{
		"EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N": "0.001",
		"EQBx6tZZWa2Tbv6BvgcvegoOQxkRrVaBVwBOoW85nbP37_Go": "0.002",
		"EQBLS8WneoKVGrwq2MO786J6ruQNiv62NXr8Ko_l5Ttondoc": "0.003",
	}

	if balance.Nano().Uint64() >= 3000000 {
		// create comment cell to send in body of each message
		comment, err := wallet.CreateCommentCell("Hello from tonutils-go!")
		if err != nil {
			log.Fatalln("CreateComment err:", err.Error())
			return
		}

		var messages []*wallet.Message
		// generate message for each destination, in single transaction can be sent up to 254 messages
		for addrStr, amtStr := range receivers {
			messages = append(messages, &wallet.Message{
				Mode: 1, // pay fee separately
				InternalMessage: &tlb.InternalMessage{
					Bounce:  false, // force send, even to uninitialized wallets
					DstAddr: address.MustParseAddr(addrStr),
					Amount:  tlb.MustFromTON(amtStr),
					Body:    comment,
				},
			})
		}

		log.Println("sending transaction and waiting for confirmation...")

		// send transaction that contains all our messages, and wait for confirmation
		txHash, err := w.SendManyWaitTxHash(context.Background(), messages)
		if err != nil {
			log.Fatalln("Transfer err:", err.Error())
			return
		}

		log.Println("transaction sent, hash:", base64.StdEncoding.EncodeToString(txHash))
		log.Println("explorer link: https://tonscan.org/tx/" + base64.URLEncoding.EncodeToString(txHash))
		return
	}

	log.Println("not enough balance:", balance.String())
}
