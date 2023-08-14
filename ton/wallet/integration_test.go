package wallet

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"math/rand"
	"os"
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.AddConnectionsFromConfigUrl(ctx, "https://ton-blockchain.github.io/testnet-global.config.json")
	if err != nil {
		panic(err)
	}

	return ton.NewAPIClient(client)
}()

var apiMain = func() *ton.APIClient {
	client := liteclient.NewConnectionPool()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.AddConnectionsFromConfigUrl(ctx, "https://ton.org/global.config.json")
	if err != nil {
		panic(err)
	}

	return ton.NewAPIClient(client)
}()

var _seed = os.Getenv("WALLET_SEED")

func Test_WalletTransfer(t *testing.T) {
	seed := strings.Split(_seed, " ")

	for _, v := range []Version{V3R2, V4R2, HighloadV2R2, V3R1, V4R1, HighloadV2Verified} {
		ver := v
		for _, isSubwallet := range []bool{false, true} {
			isSubwallet := isSubwallet
			t.Run("send for wallet ver "+fmt.Sprint(ver)+" subwallet "+fmt.Sprint(isSubwallet), func(t *testing.T) {
				t.Parallel()

				ctx := api.Client().StickyContext(context.Background())
				ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
				defer cancel()

				w, err := FromSeed(api, seed, ver)
				if err != nil {
					t.Fatal("FromSeed err:", err.Error())
					return
				}

				if isSubwallet {
					w, err = w.GetSubwallet(1)
					if err != nil {
						t.Fatal("GetSubwallet err:", err.Error())
						return
					}
				}

				log.Println(ver, "-> test wallet address:", w.Address(), isSubwallet)

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
				addr := address.MustParseAddr("EQA8aJTl0jfFnUZBJjTeUxu9OcbsoPBp9UcHE9upyY_X35kE")
				if balance.Nano().Uint64() >= 3000000 {
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
}

func Test_WalletFindTransactionByInMsgHash(t *testing.T) {
	seed := strings.Split(_seed, " ")
	ctx := api.Client().StickyContext(context.Background())

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
		address.MustParseAddr("EQA8aJTl0jfFnUZBJjTeUxu9OcbsoPBp9UcHE9upyY_X35kE"),
		tlb.MustFromTON("0.0031337"),
		body,
	)

	// the waitConfirmation flag is optional
	inMsgHash, err := w.SendManyGetInMsgHash(ctx, []*Message{msg}, true)
	t.Logf("internal message hash: %s", hex.EncodeToString(inMsgHash))

	// find tx hash
	tx, err := w.FindTransactionByInMsgHash(ctx, inMsgHash, 30)
	if err != nil {
		t.Fatal("cannot find tx:", err.Error())
	}
	t.Logf("sent message hash: %s", hex.EncodeToString(tx.Hash))
}

func TestWallet_DeployContract(t *testing.T) {
	seed := strings.Split(_seed, " ")
	ctx := api.Client().StickyContext(context.Background())

	// init wallet
	w, err := FromSeed(api, seed, HighloadV2R2)
	if err != nil {
		t.Fatal("FromSeed err:", err.Error())
	}
	t.Logf("wallet address: %s", w.Address().String())

	codeBytes, _ := hex.DecodeString("b5ee9c72410104010020000114ff00f4a413f4bcf2c80b010203844003020009a1b63c43510007a0000061d2421bb1")
	code, _ := cell.FromBOC(codeBytes)

	addr, err := w.DeployContract(ctx, tlb.MustFromTON("0.005"), cell.BeginCell().EndCell(), code, cell.BeginCell().MustStoreUInt(rand.Uint64(), 64).EndCell(), true)
	if err != nil {
		t.Fatal("deploy err:", err)
	}
	t.Logf("contract address: %s", addr.String())

	block, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("CurrentMasterchainInfo err:", err.Error())
		return
	}

	res, err := api.RunGetMethod(ctx, block, addr, "dappka", 5, 10)
	if err != nil {
		t.Fatal("run err:", err)
	}

	if res.MustInt(0).Uint64() != 5 || res.MustInt(1).Uint64() != 50 {
		t.Fatal("result err:", res.MustInt(0).Uint64(), res.MustInt(1).Uint64())
	}
}

func TestWallet_TransferEncrypted(t *testing.T) {
	seed := strings.Split(_seed, " ")
	ctx := api.Client().StickyContext(context.Background())

	// init wallet
	w, err := FromSeed(api, seed, HighloadV2R2)
	if err != nil {
		t.Fatal("FromSeed err:", err.Error())
	}
	t.Logf("wallet address: %s", w.Address().String())

	err = w.TransferWithEncryptedComment(ctx, address.MustParseAddr("EQC9bWZd29foipyPOGWlVNVCQzpGAjvi1rGWF7EbNcSVClpA"), tlb.MustFromTON("0.005"), "привет:"+randString(30), true)
	if err != nil {
		t.Fatal("transfer err:", err)
	}
}

func TestGetWalletVersion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var testCases = []struct {
		Addr    *address.Address
		Version Version
	}{
		{
			Addr:    address.MustParseAddr("EQCetCJb1W-oAqQtiiWuAa1JibQ0LHnFytgJWtTvX5La_ZON"),
			Version: V3,
		}, {
			Addr:    address.MustParseAddr("EQBfAN7LfaUYgXZNw5Wc7GBgkEX2yhuJ5ka95J1JJwXXf4a8"),
			Version: V3,
		}, {
			Addr:    address.MustParseAddr("EQA5Fa4g4JfeQoA41N6mJx0MvH75i30dV1CXKoOijFa-XnmZ"),
			Version: V4R2,
		}, {
			Addr:    address.MustParseAddr("EQAaQOzG_vqjGo71ZJNiBdU1SRenbqhEzG8vfpZwubzyB0T8"),
			Version: V4R1,
		}, {
			Addr:    address.MustParseAddr("EQAkbIA32zna94YX1Oii371zF-CHOPHB8DLIJa1QBcdNNGmq"),
			Version: V4R2,
		}, {
			Addr:    address.MustParseAddr("EQBREtZ3r9bEuFSCWYtqx5KbJBDRPdSSCG3wzJvQDXcvXagl"),
			Version: Unknown,
		},
	}

	ctx = apiMain.Client().StickyContext(ctx)
	master, err := apiMain.CurrentMasterchainInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range testCases {
		account, err := apiMain.GetAccount(ctx, master, test.Addr)
		if err != nil {
			t.Fatal(err)
		}
		if v := GetWalletVersion(account); v != test.Version {
			t.Fatalf("%s: expected: %d, got: %d", test.Addr.String(), test.Version, v)
		}
	}
}

func TestWallet_GetPublicKey(t *testing.T) {
	pub, err := GetPublicKey(context.Background(), apiMain, address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N"))
	if err != nil {
		t.Fatal(err.Error())
		return
	}

	key, _ := hex.DecodeString("72c9ed6b62a6e2eba14a93b90462e7a367777beb8a38fb15b9f33844d22ce2ff")
	if !bytes.Equal(pub, key) {
		t.Fatal("wrong key: " + hex.EncodeToString(pub))
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
