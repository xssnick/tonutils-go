package main

import (
	"context"
	"fmt"
	"log"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/ton"
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
	b, err := api.GetBlockInfo(context.Background())
	if err != nil {
		log.Fatalln("get block err:", err.Error())
		return
	}

	res, err := api.GetAccount(context.Background(), b, address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N"))
	if err != nil {
		log.Fatalln("run get method err:", err.Error())
		return
	}

	fmt.Printf("Balance: %s TON\n", res.State.Balance.TON())
	fmt.Printf("Data: %s", res.Data.Dump())
}
