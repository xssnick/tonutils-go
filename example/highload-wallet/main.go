package main

import (
	"context"
	"encoding/base64"
	"log"
	"strings"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/wallet"
)

func main() {
	client := liteclient.NewConnectionPool()

	// connect to testnet lite server
	configUrl := "https://ton-blockchain.github.io/testnet-global.config.json"
	err := client.AddConnectionsFromConfigUrl(context.Background(), configUrl)
	if err != nil {
		panic(err)
	}

	api := ton.NewAPIClient(client, ton.ProofCheckPolicyFast).WithRetry()

	// seed words of account, you can generate them with any wallet or using wallet.NewSeed() method
	words := strings.Split("birth pattern then forest walnut then phrase walnut fan pumpkin pattern then cluster blossom verify then forest velvet pond fiction pattern collect then then", " ")

	// initialize high-load wallet
	w, err := wallet.FromSeed(api, words, wallet.ConfigHighloadV3{
		MessageTTL: 60 * 5,
		MessageBuilder: func(ctx context.Context, subWalletId uint32) (id uint32, createdAt int64, err error) {
			// Due to specific of externals emulation on liteserver,
			// we need to take something less than or equals to block time, as message creation time,
			// otherwise external message will be rejected, because time will be > than emulation time
			// hope it will be fixed in the next LS versions
			createdAt = time.Now().Unix() - 30

			// example query id which will allow you to send 1 tx per second
			// but you better to implement your own iterator in database, then you can send unlimited
			// but make sure id is less than 1 << 23, when it is higher start from 0 again
			return uint32(createdAt % (1 << 23)), createdAt, nil
		},
	})
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
		// generate message for each destination, in single batch can be sent up to 65k messages (but consider messages size, external size limit is 64kb)
		for addrStr, amtStr := range receivers {
			addr := address.MustParseAddr(addrStr)
			messages = append(messages, &wallet.Message{
				Mode: wallet.PayGasSeparately + wallet.IgnoreErrors, // pay fee separately, ignore action errors
				InternalMessage: &tlb.InternalMessage{
					IHRDisabled: true, // disable hyper routing (currently not works in ton)
					Bounce:      addr.IsBounceable(),
					DstAddr:     addr,
					Amount:      tlb.MustFromTON(amtStr),
					Body:        comment,
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
		log.Println("explorer link: https://testnet.tonscan.org/tx/" + base64.URLEncoding.EncodeToString(txHash))
		return
	}

	log.Println("not enough balance:", balance.String())
}
