package payments

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

var api = func() ton.APIClientWrapped {
	client := liteclient.NewConnectionPool()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.AddConnectionsFromConfigUrl(ctx, "https://ton-blockchain.github.io/testnet-global.config.json")
	if err != nil {
		panic(err)
	}

	return ton.NewAPIClient(client).WithRetry()
}()

var _seed = strings.Split(os.Getenv("WALLET_SEED"), " ")

func TestClient_DeployAsyncChannel(t *testing.T) {
	client := NewPaymentChannelClient(api)

	chID, err := RandomChannelID()
	if err != nil {
		t.Fatal(err)
	}

	_, ourKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	theirKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	w, err := wallet.FromSeed(api, _seed, wallet.HighloadV2R2)
	if err != nil {
		t.Fatal(fmt.Errorf("failed to init wallet: %w", err))
	}

	body, code, data, err := client.GetDeployAsyncChannelParams(chID, true, tlb.MustFromTON("0.005"), ourKey, theirKey, ClosingConfig{
		QuarantineDuration:       180,
		MisbehaviorFine:          tlb.MustFromTON("0.1"),
		ConditionalCloseDuration: 200,
	}, PaymentConfig{
		ExcessFee: tlb.MustFromTON("0.001"),
		DestA:     w.Address(),
		DestB:     address.MustParseAddr("EQBletedrsSdih8H_-bR0cDZhdbLRy73ol6psGCrRKDahFju"),
	})
	if err != nil {
		t.Fatal(fmt.Errorf("failed to build deploy channel params: %w", err))
	}

	channelAddr, _, block, err := w.DeployContractWaitTransaction(context.Background(), tlb.MustFromTON("0.02"), body, code, data)
	if err != nil {
		t.Fatal(fmt.Errorf("failed to deploy channel: %w", err))
	}

	block, err = client.api.WaitForBlock(block.SeqNo + 7).GetMasterchainInfo(context.Background())
	if err != nil {
		t.Fatal(fmt.Errorf("failed to wait block: %w", err))
	}

	ch, err := client.GetAsyncChannel(context.Background(), block, channelAddr, true)
	if err != nil {
		t.Fatal(fmt.Errorf("failed to get channel: %w", err))
	}

	if ch.Status != ChannelStatusOpen {
		t.Fatal("channel status incorrect")
	}

	if ch.Storage.BalanceA.Nano().Cmp(tlb.MustFromTON("0.005").Nano()) != 0 {
		t.Fatal("balance incorrect")
	}

	json.NewEncoder(os.Stdout).Encode(ch.Storage)
	log.Println("channel deployed:", channelAddr.String())
}
