package wallet

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
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

	// prepare external message
	msg := SimpleMessage(
		address.MustParseAddr("EQAaQOzG_vqjGo71ZJNiBdU1SRenbqhEzG8vfpZwubzyB0T8"),
		tlb.FromNanoTON(big.NewInt(31337)),
		body,
	)
	ext, err := w.BuildMessageForMany(context.Background(), []*Message{msg})
	if err != nil {
		t.Fatal("BuildMessageForMany err:", err.Error())
	}

	// SendManyWaitTxHash: send external message, wait for confirm and return tx hash
	block, err := api.CurrentMasterchainInfo(context.Background())
	if err != nil {
		t.Fatal(fmt.Errorf("failed to get block: %w", err))
	}
	acc, err := api.GetAccount(context.Background(), block, w.addr)
	if err != nil {
		t.Fatal(fmt.Errorf("failed to get account state: %w", err))
	}

	err = api.SendExternalMessage(context.Background(), ext)
	if err != nil {
		t.Fatal(fmt.Errorf("failed to send message: %w", err))
	}

	txHash, err := w.waitConfirmation(context.Background(), block, acc, ext.StateInit, ext.Body)
	if err != nil {
		t.Fatal(fmt.Errorf("failed to send message: %w", err))
	}
	t.Logf("sent tx hash: %s", hex.EncodeToString(txHash))

	// find tx hash and compare returned hashes
	tx, err := w.FindTransactionByInMsgHash(context.Background(), ext.Body.Hash())
	if err != nil {
		t.Fatal("cannot find tx:", err.Error())
	}
	if !bytes.Equal(tx.Hash, txHash) {
		t.Fatal("FindTransactionByMsgHash returned wrong tx")
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
