package wallet

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
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

var _mainnetSeed = "burger letter already sleep chimney mix regular sunset tired empower candy candy area organ mix caution area caution candy uncover empower burger room dog"

func Test_WalletTransfer(t *testing.T) {
	seed := strings.Split(_mainnetSeed, " ")

	for _, ver := range []Version{V3, V4R2, HighloadV2R2} {
		t.Run("send for wallet ver "+fmt.Sprint(ver), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()

			w, err := FromSeed(api, seed, ver)
			if err != nil {
				t.Fatal("FromSeed err:", err.Error())
				return
			}

			log.Println(ver, "-> test wallet address:", w.Address())

			block, err := api.CurrentMasterchainInfo(ctx)
			if err != nil {
				t.Fatal("CurrentMasterchainInfo err:", err.Error())
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
				err = w.Transfer(ctx, addr, tlb.MustFromTON("0.003"), comment, true)
				if err != nil {
					t.Fatal("Transfer err:", err.Error())
					return
				}
			} else {
				t.Fatal("not enough balance")
				return
			}
		})
	}
}

func Test_WalletFindTransactionByMsgHash(t *testing.T) {
	seed := strings.Split(_mainnetSeed, " ")

	w, err := FromSeed(api, seed, HighloadV2R2)
	if err != nil {
		t.Fatal("FromSeed err:", err.Error())
	}

	t.Logf("wallet address: %s", w.Address().String())

	testCases := []struct {
		MsgHash string
		TxHash  string
	}{
		// https://ton.page/tx/2DID+ck8iTkaPKttwrYKl//v/gkoa2taclFK22rEvy0=
		{
			MsgHash: "dmEFTmFAZZxWUfPWnd2wan8r/nym7J0vJnWO2Bvp2M0=",
			TxHash:  "2DID+ck8iTkaPKttwrYKl//v/gkoa2taclFK22rEvy0=",
		},
		// https://ton.page/tx/BI4HyAa+z5LENhiKCI/6BNqUKSFxUlY7GSie/Oq9XsA=
		{
			MsgHash: "lqKW0iTyhcZ77pPDD4owkVfw2qNdxbh+QQt4YwoJz8c=",
			TxHash:  "BI4HyAa+z5LENhiKCI/6BNqUKSFxUlY7GSie/Oq9XsA=",
		},
	}

	for _, test := range testCases {
		msgHash, err := base64.StdEncoding.DecodeString(test.MsgHash)
		if err != nil {
			t.Fatal("msg hash base64 decode err:", err.Error())
		}

		tx, err := w.FindTransactionByInMsgHash(context.Background(), msgHash)
		if err != nil {
			t.Fatal("cannot find tx:", err.Error())
		}

		txHash, err := base64.StdEncoding.DecodeString(test.TxHash)
		if err != nil {
			t.Fatal("tx hash base64 decode err:", err.Error())
		}
		if !bytes.Equal(tx.Hash, txHash) {
			t.Fatal("FindTransactionByMsgHash returned wrong tx")
		}
	}
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
