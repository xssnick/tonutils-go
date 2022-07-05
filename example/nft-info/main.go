package main

import (
	"context"
	"fmt"
	"log"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func main() {
	client := liteclient.NewClient()

	// connect to mainnet lite server
	err := client.Connect(context.Background(), "135.181.140.212:13206", "K0t3+IWLOXHYMvMcrGZDPs+pn58a17LFbnXoQkKc2xw=")
	if err != nil {
		log.Fatalln("connection err: ", err.Error())
		return
	}

	// initialize ton api lite connection wrapper
	api := ton.NewAPIClient(client)

	// we need fresh block info to run get methods
	b, err := api.GetMasterchainInfo(context.Background())
	if err != nil {
		log.Fatalln("get block err:", err.Error())
		return
	}

	// call method to get data from nft address
	res, err := api.RunGetMethod(context.Background(), b, address.MustParseAddr("EQAMo8TmviyZpBLLJMDDwYDhFsdyj26XoAM4_mNyMtfxuT6O"), "get_nft_data")
	if err != nil {
		log.Fatalln("run get method err:", err.Error())
		return
	}

	collectionAddr, err := res[2].(*cell.Cell).BeginParse().LoadAddr()
	if err != nil {
		log.Fatalln("addr err:", err.Error())
		return
	}

	ownerAddr, err := res[3].(*cell.Cell).BeginParse().LoadAddr()
	if err != nil {
		log.Fatalln("addr err:", err.Error())
		return
	}

	fmt.Println("NFT collection: ", collectionAddr.String())
	fmt.Println("NFT owner: ", ownerAddr.String())
	fmt.Println("Content:", res[4].(*cell.Cell).Dump())
}
