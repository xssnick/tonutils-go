package wallet

import (
	"context"
	"encoding/hex"
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
	"github.com/xssnick/tonutils-go/tvm/cell"
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

func Test_WalletFindTransactionByInMsgHash(t *testing.T) {
	seed := strings.Split(_mainnetSeed, " ")

	// init wallet
	w, err := FromSeed(api, seed, HighloadV2R2)
	if err != nil {
		t.Fatal("FromSeed err:", err.Error())
	}
	t.Logf("wallet address: %s", w.Address().String())

	// set comment
	root := cell.BeginCell().MustStoreUInt(0, 32)
	if err := root.StoreStringSnake(".. .. . .... .. .. . .. ."); err != nil {
		t.Fatal(fmt.Errorf("failed to build comment: %w", err))
	}
	body := root.EndCell()

	// prepare simple transfer
	msg := SimpleMessage(
		address.MustParseAddr("EQAaQOzG_vqjGo71ZJNiBdU1SRenbqhEzG8vfpZwubzyB0T8"),
		tlb.MustFromTON("0.0031337"),
		body,
	)

	// the waitConfirmation flag is optional
	inMsgHash, err := w.SendManyGetInMsgHash(context.Background(), []*Message{msg}, true)
	t.Logf("internal message hash: %s", hex.EncodeToString(inMsgHash))

	// find tx hash
	tx, err := w.FindTransactionByInMsgHash(context.Background(), inMsgHash)
	if err != nil {
		t.Fatal("cannot find tx:", err.Error())
	}
	t.Logf("sent message hash: %s", hex.EncodeToString(tx.Hash))
}

func TestWallet_DeployContract(t *testing.T) {
	seed := strings.Split(_mainnetSeed, " ")

	// init wallet
	w, err := FromSeed(api, seed, HighloadV2R2)
	if err != nil {
		t.Fatal("FromSeed err:", err.Error())
	}
	t.Logf("wallet address: %s", w.Address().String())

	codeBytes, _ := hex.DecodeString("b5ee9c72410104010020000114ff00f4a413f4bcf2c80b010203844003020009a1b63c43510007a0000061d2421bb1")
	code, _ := cell.FromBOC(codeBytes)

	addr, err := w.DeployContract(context.Background(), tlb.MustFromTON("0.005"), cell.BeginCell().EndCell(), code, cell.BeginCell().MustStoreUInt(rand.Uint64(), 64).EndCell(), true)
	if err != nil {
		t.Fatal("deploy err:", err)
	}
	t.Logf("contract address: %s", addr.String())

	block, err := api.CurrentMasterchainInfo(context.Background())
	if err != nil {
		t.Fatal("CurrentMasterchainInfo err:", err.Error())
		return
	}

	res, err := api.RunGetMethod(context.Background(), block, addr, "dappka", 5, 10)
	if err != nil {
		t.Fatal("run err:", err)
	}

	if res.MustInt(0).Uint64() != 5 || res.MustInt(1).Uint64() != 50 {
		t.Fatal("result err:", res.MustInt(0).Uint64(), res.MustInt(1).Uint64())
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
