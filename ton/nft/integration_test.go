package nft

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"

	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/wallet"
)

var api = func() ton.APIClientWrapped {
	client := liteclient.NewConnectionPool()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := client.AddConnectionsFromConfigUrl(ctx, "https://ton-blockchain.github.io/testnet-global.config.json")
	if err != nil {
		panic(err)
	}

	return ton.NewAPIClient(client).WithRetryTimeout(0, 5*time.Second)
}()

var _seed = os.Getenv("WALLET_SEED")

func Test_NftMintTransfer(t *testing.T) {
	seed := strings.Split(_seed, " ")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	ctx = api.Client().StickyContext(ctx)

	w, err := wallet.FromSeed(api, seed, wallet.HighloadV2R2)
	if err != nil {
		t.Fatal("FromSeed err:", err.Error())
	}

	log.Println("test wallet address:", w.Address())

	block, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("CurrentMasterchainInfo err:", err.Error())
	}

	balance, err := w.GetBalance(ctx, block)
	if err != nil {
		t.Fatal("GetBalance err:", err.Error())
	}

	if balance.Nano().Uint64() < 3000000 {
		t.Fatal("not enough balance", w.Address(), balance.String())
	}

	collectionAddr := address.MustParseAddr("EQBTObWUuWTb5ECnLI4x6a3szzstmMDOcc5Kdo-CpbUY9Y5K") // address = deployCollection(w) w.seed = (fiction ... rather)
	collection := NewCollectionClient(api, collectionAddr)
	collectionData, err := collection.GetCollectionData(ctx)
	if err != nil {
		panic(err)
	}

	nftAddr, err := collection.GetNFTAddressByIndex(ctx, collectionData.NextItemIndex)
	if err != nil {
		t.Fatal("GetNFTAddressByIndex err:", err.Error())
	}

	itemURI := fmt.Sprint(collectionData.NextItemIndex) + "/" + fmt.Sprint(collectionData.NextItemIndex) + ".json"
	mintData, err := collection.BuildMintPayload(collectionData.NextItemIndex, w.Address(), tlb.MustFromTON("0.01"), &ContentOffchain{
		URI: itemURI,
	})
	if err != nil {
		t.Fatal("BuildMintPayload err:", err.Error())
	}

	fmt.Println("Minting NFT...")
	mint := wallet.SimpleMessage(collectionAddr, tlb.MustFromTON("0.025"), mintData)

	_, block, err = w.SendWaitTransaction(context.Background(), mint)
	if err != nil {
		t.Fatal("Send err:", err.Error())
	}

	// wait next block to be sure everything updated
	block, err = api.WaitForBlock(block.SeqNo + 5).GetMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("Wait master err:", err.Error())
	}

	fmt.Println("Minted NFT:", nftAddr.String(), 0)

	newAddr := address.MustParseAddr("EQB9ElEc88x6kOZytvUp0_18U-W1V-lBdvPcZ-BXdBWVrmeA") // address wallet with other seed = ("together ... lounge")
	nft := NewItemClient(api, nftAddr)
	transferData, err := nft.BuildTransferPayload(newAddr, tlb.MustFromTON("0.01"), nil)
	if err != nil {
		t.Fatal("BuildMintPayload err:", err.Error())
	}

	fmt.Println("Transferring NFT...")
	transfer := wallet.SimpleMessage(nftAddr, tlb.MustFromTON("0.065"), transferData)

	_, block, err = w.SendWaitTransaction(context.Background(), transfer)
	if err != nil {
		t.Fatal("Send err:", err.Error())
	}

	// wait next block to be sure everything updated
	block, err = api.WaitForBlock(block.SeqNo + 5).GetMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("Wait master err:", err.Error())
	}

	newData, err := nft.GetNFTDataAtBlock(ctx, block)
	if err != nil {
		t.Fatal("GetNFTData err:", err.Error())
	}

	fullContent, err := collection.GetNFTContentAtBlock(ctx, collectionData.NextItemIndex, newData.Content, block)
	if err != nil {
		t.Fatal("GetNFTData err:", err.Error())
	}

	if fullContent.(*ContentOffchain).URI != "https://tonutils.com/items/"+itemURI {
		t.Fatal("full content incorrect", fullContent.(*ContentOffchain).URI)
	}

	roy, err := collection.RoyaltyParamsAtBlock(ctx, block)
	if err != nil {
		t.Fatal("RoyaltyParams err:", err.Error())
	}

	if roy.Address.String() != "EQCyMa4xOmcr6H0OWbcYtwOTsf6YUSW3KuGj010WgWBVQhoJ" { // address which get paid for nft. Wallet.seed = (fiction ... rather)
		t.Fatal("royalty addr invalid")
	}

	if roy.Base != 0 || roy.Factor != 0 {
		t.Fatal("royalty invalid")
	}

	fmt.Println("Owner:", newData.OwnerAddress.String())
	fmt.Println("Full content:", fullContent.(*ContentOffchain).URI)
	fmt.Println("Royalty:", roy.Address.String(), roy.Base, "/", roy.Factor)

	if newData.OwnerAddress.String() != newAddr.String() {
		t.Fatal("nft owner not updated")
	}
}
