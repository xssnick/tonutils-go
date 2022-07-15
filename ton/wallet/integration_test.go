package wallet

import (
	"context"
	"log"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
)

var api = func() *ton.APIClient {
	client := liteclient.NewConnectionPool()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := client.AddConnection(ctx, "135.181.140.212:13206", "K0t3+IWLOXHYMvMcrGZDPs+pn58a17LFbnXoQkKc2xw=")
	if err != nil {
		panic(err)
	}

	return ton.NewAPIClient(client)
}()

func Test_WalletTransfer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	seed := strings.Split("burger letter already sleep chimney mix regular sunset tired empower candy candy area organ mix caution area caution candy uncover empower burger room dog", " ")
	w, err := FromSeed(api, seed, V3)
	if err != nil {
		t.Fatal("FromSeed err:", err.Error())
		return
	}

	log.Println("test wallet address:", w.Address())

	block, err := api.GetMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("GetMasterchainInfo err:", err.Error())
		return
	}

	balance, err := w.GetBalance(ctx, block)
	if err != nil {
		t.Fatal("GetBalance err:", err.Error())
		return
	}

	comment := randString(150)
	addr := address.MustParseAddr("EQAaQOzG_vqjGo71ZJNiBdU1SRenbqhEzG8vfpZwubzyB0T8")
	if balance.NanoTON().Uint64() >= 3000000 {
		err = w.Transfer(ctx, addr, tlb.MustFromTON("0.003"), comment)
		if err != nil {
			t.Fatal("Transfer err:", err.Error())
			return
		}
	} else {
		t.Fatal("net enough balance")
		return
	}

	try, maxTries := 0, 15
	for try < maxTries {
		try++

		log.Println(try, "tx sent, waiting for confirmation...")
		time.Sleep(3 * time.Second)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		block, err = api.GetMasterchainInfo(ctx)
		if err != nil {
			t.Fatal("GetMasterchainInfo err:", err.Error())
			return
		}

		acc, err := api.GetAccount(ctx, block, addr)
		if err != nil {
			t.Fatal("get account err:", err.Error())
			return
		}

		txl, err := api.ListTransactions(ctx, addr, 3, acc.LastTxLT, acc.LastTxHash)
		if err != nil {
			t.Fatal("ListTransactions err:", err.Error())
			return
		}

		cancel()

		for _, tx := range txl {
			if tx.IO.In.MsgType == tlb.MsgTypeInternal {
				intTx := tx.IO.In.AsInternal()

				if intTx.Comment() != comment {
					continue
				}

				if intTx.Amount.NanoTON().Uint64() != 3000000 {
					t.Fatal("amount not match", intTx.Amount.NanoTON().Uint64(), 3000000)
				}

				return
			}
		}

	}

	t.Fatal("transaction not confirmed!")
}

func randString(n int) string {
	var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ" +
		"абвгдежзиклмнопрстиквфыйцэюяАБВГДЕЖЗИЙКЛМНОПРСТИЮЯЗФЫУю!№%:,.!;(!)_+" +
		"😱😨🍫💋💎😄🎉☠️🙈😁🙂📱😨😮🤮👿👏🤞🖕🤜👂👃👀")

	rand.Seed(time.Now().UnixNano())
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}
