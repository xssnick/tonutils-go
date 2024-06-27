package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/nft"
	"github.com/xssnick/tonutils-go/ton/wallet"
)

func main() {
	client := liteclient.NewConnectionPool()

	// connect to mainnet lite server
	err := client.AddConnection(context.Background(), "135.181.140.212:13206", "K0t3+IWLOXHYMvMcrGZDPs+pn58a17LFbnXoQkKc2xw=")
	if err != nil {
		panic(err)
	}

	// initialize ton api lite connection wrapper
	api := ton.NewAPIClient(client)
	w := getWallet(api)

	log.Println("Deploy wallet:", w.WalletAddress().String())

	collectionAddr := address.MustParseAddr("EQCSrRIKVEBaRd8aQfsOaNq3C4FVZGY5Oka55A5oFMVEs0lY")
	collection := nft.NewCollectionClient(api, collectionAddr)

	collectionData, err := collection.GetCollectionData(context.Background())
	if err != nil {
		panic(err)
	}

	nftAddr, err := collection.GetNFTAddressByIndex(context.Background(), collectionData.NextItemIndex)
	if err != nil {
		panic(err)
	}

	mintData, err := collection.BuildMintPayload(collectionData.NextItemIndex, w.WalletAddress(), tlb.MustFromTON("0.01"), &nft.ContentOffchain{
		URI: fmt.Sprint(collectionData.NextItemIndex) + ".json",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println("Minting NFT...")
	mint := wallet.SimpleMessage(collectionAddr, tlb.MustFromTON("0.025"), mintData)

	err = w.Send(context.Background(), mint, true)
	if err != nil {
		panic(err)
	}

	fmt.Println("Minted NFT:", nftAddr.String())

	newData, err := nft.NewItemClient(api, nftAddr).GetNFTData(context.Background())
	if err != nil {
		panic(err)
	}

	fmt.Println("Minted NFT addr: ", nftAddr.String())
	fmt.Println("NFT Owner:", newData.OwnerAddress.String())
}

func getWallet(api *ton.APIClient) *wallet.Wallet {
	words := strings.Split("birth pattern then forest walnut then phrase walnut fan pumpkin pattern then cluster blossom verify then forest velvet pond fiction pattern collect then then", " ")
	w, err := wallet.FromSeed(api, words, wallet.V4R2)
	if err != nil {
		panic(err)
	}
	return w
}
