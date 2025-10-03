package main

import (
	"context"
	"encoding/hex"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"github.com/xssnick/tonutils-go/toncenter"
	"log"
	"strings"
	"time"
)

func main() {
	tc := toncenter.New("https://testnet.toncenter.com",
		// toncenter.WithAPIKey("YOUR_KEY"),
		toncenter.WithTimeout(10*time.Second),
		// the free rate limit without an api key is 1 per sec,
		// but we set it a bit lower to be sure
		toncenter.WithRateLimit(0.85),
	)

	// seed words of an account, you can generate them with any wallet or using wallet.NewSeed() method
	words := strings.Split("giraffe soccer exotic sadness angry satoshi promote doctor odor joke rose deal nice inflict engine kiwi wheat eyebrow force envelope obvious tip weasel scan", " ")

	// convert seed to a private key, depending on a type of phrase
	// WithBIP39(true) or WithLedger() options could be used
	key, err := wallet.SeedToPrivateKeyWithOptions(words)
	if err != nil {
		log.Fatalln("SeedToPrivateKeyWithOptions err:", err.Error())
		return
	}

	// as you can see, we are not passing WithAPI option, because external api will be used
	w, err := wallet.FromPrivateKeyWithOptions(key, wallet.V4R2)
	if err != nil {
		log.Fatalln("FromSeed err:", err.Error())
		return
	}

	log.Println("wallet address:", w.WalletAddress().Testnet(true))

	addr := address.MustParseAddr("0QAcsLrH81e_Wh3nrH7Td3rqptMsWNZ5zueGz7I7qtA1qDE_")

	log.Println("sending transaction...")

	// default message ttl is 3 minutes, it is time during which you can send it to blockchain
	// if you need to set longer TTL, you could use this method
	// w.GetSpec().(*wallet.SpecV4R2).SetMessagesTTL(uint32((10 * time.Minute) / time.Second))

	// get current wallet seqno from ton center API
	w.GetSpec().(*wallet.SpecV4R2).SetSeqnoFetcher(func(ctx context.Context, sub uint32) (uint32, error) {
		res, err := tc.V2().GetWalletInformation(ctx, w.WalletAddress())
		if err != nil {
			return 0, err
		}
		return uint32(res.Seqno), nil
	})

	// if our wallet is already deployed, we can skip this step and just use withStateInit = false
	contractInfo, err := tc.V2().GetAddressInformation(context.Background(), w.WalletAddress())
	if err != nil {
		log.Fatalln("get account info err:", err.Error())
		return
	}

	// add state init to deploy, if wallet is not yet deployed
	withStateInit := contractInfo.State != "active"

	comment, _ := wallet.CreateCommentCell("Hello from tonutils-go with toncenter api!")

	// if destination wallet is not initialized you should set bounce = true
	ext, err := w.PrepareExternalMessageForMany(context.Background(), withStateInit, []*wallet.Message{
		wallet.SimpleMessageAutoBounce(addr, tlb.MustFromTON("0.003"), comment),
	})
	if err != nil {
		log.Fatalln("BuildTransfer err:", err.Error())
		return
	}

	// this hash could be used for transaction discovery in explorers
	log.Println("external message hash:", hex.EncodeToString(ext.NormalizedHash()))

	// if you wish to send a message from a diff source, or later, you could serialize it to BoC
	msgCell, _ := tlb.ToCell(ext)

	// send message to blockchain
	if err = tc.V2().SendBoc(context.Background(), msgCell.ToBOC()); err != nil {
		log.Fatalln("Failed to send external message:", err.Error())
		return
	}

	log.Println("transaction sent, we are not waiting for confirmation")
	log.Println("track: https://testnet.tonscan.org/tx/" + hex.EncodeToString(ext.NormalizedHash()))
}
