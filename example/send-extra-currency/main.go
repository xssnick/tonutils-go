package main

import (
	"context"
	"encoding/base64"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"log"
	"strings"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/wallet"
)

func main() {
	client := liteclient.NewConnectionPool()

	// connect to mainnet lite server
	err := client.AddConnectionsFromConfigUrl(context.Background(), "https://ton-blockchain.github.io/testnet-global.config.json")
	if err != nil {
		log.Fatalln("connection err: ", err.Error())
		return
	}

	api := ton.NewAPIClient(client).WithRetry()

	// seed words of account, you can generate them with any wallet or using wallet.NewSeed() method
	words := strings.Split("birth pattern then forest walnut then phrase walnut fan pumpkin pattern then cluster blossom verify then forest velvet pond fiction pattern collect then then", " ")

	w, err := wallet.FromSeed(api, words, wallet.ConfigV5R1Final{
		NetworkGlobalID: wallet.TestnetGlobalID,
		Workchain:       0,
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

	acc, err := api.GetAccount(context.Background(), block, w.WalletAddress())
	if err != nil {
		log.Fatalln("GetAccount err:", err.Error())
		return
	}

	if !acc.IsActive {
		log.Fatalln("Wallet is empty")
		return
	}
	balance := acc.State.Balance

	if !acc.State.ExtraCurrencies.IsEmpty() {
		currencies, err := acc.State.ExtraCurrencies.LoadAll()
		if err != nil {
			log.Fatalln("LoadAll err:", err.Error())
			return
		}

		ec := cell.NewDict(32)

		// printing all extra currencies we have, and preparing them to send
		for _, kv := range currencies {
			id := kv.Key.MustLoadBigUInt(32)
			amount := kv.Value.MustLoadVarUInt(32)

			log.Printf("ExtraCurrency ID: %d, Amount: %s\n", id.Uint64(), amount.String())

			// adding currency to send in tx
			_ = ec.SetIntKey(id, cell.BeginCell().MustStoreBigVarUInt(amount, 32).EndCell())
		}

		// sending extra currencies to another address
		tx, _, err := w.SendWaitTransaction(context.Background(), &wallet.Message{
			Mode: wallet.PayGasSeparately + wallet.IgnoreErrors,
			InternalMessage: &tlb.InternalMessage{
				Bounce:          true,
				DstAddr:         address.MustParseAddr("kQAbMQzuuGiCne0R7QEj9nrXsjM7gNjeVmrlBZouyC-SCALE"),
				Amount:          tlb.MustFromTON("0.05"),
				ExtraCurrencies: ec,
				Body:            cell.BeginCell().EndCell(),
			},
		})
		if err != nil {
			log.Fatalln("Send err:", err.Error())
			return
		}

		log.Println("transaction with extra currencies sent, hash:", base64.StdEncoding.EncodeToString(tx.Hash))

		return
	}

	// checking ton balance and minting extra currency through test contract
	if balance.Nano().Cmp(tlb.MustFromTON("3.15").Nano()) > 0 {
		log.Println("minting some extra currency...")

		tx, _, err := w.SendWaitTransaction(context.Background(), &wallet.Message{
			Mode: wallet.PayGasSeparately + wallet.IgnoreErrors, // pay fees separately (from balance, not from amount)
			InternalMessage: &tlb.InternalMessage{
				Bounce:  true, // return amount in case of processing error
				DstAddr: address.MustParseAddr("kQC_rkxBuZDwS81yvMSLzeXBNLCGFNofm0avwlMfNXCwoOgr"),
				Amount:  tlb.MustFromTON("3.1"),
				Body:    cell.BeginCell().EndCell(),
			},
		})
		if err != nil {
			log.Fatalln("Send err:", err.Error())
			return
		}

		log.Println("transaction sent, extra currency minted, hash:", base64.StdEncoding.EncodeToString(tx.Hash))
		log.Println("run again to send extra currencies")

		return
	}

	log.Println("not enough ton balance to mint extra currency:", balance.String())
}
