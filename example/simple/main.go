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
	client := liteclient.NewConnectionPool()

	// connect to mainnet lite server
	err := client.AddConnection(context.Background(), "135.181.140.212:13206", "K0t3+IWLOXHYMvMcrGZDPs+pn58a17LFbnXoQkKc2xw=")
	if err != nil {
		log.Fatalln("connection err: ", err.Error())
		return
	}

	// initialize ton api lite connection wrapper
	api := ton.NewAPIClient(client)

	// we need fresh block info to run get methods
	b, err := api.CurrentMasterchainInfo(context.Background())
	if err != nil {
		log.Fatalln("get block err:", err.Error())
		return
	}

	/*
		We will run such method of contract
		cell mult(int a, int b) method_id {
		  return begin_cell().store_uint(a * b, 64).end_cell();
		}
	*/

	res, err := api.WaitForBlock(b.SeqNo).RunGetMethod(context.Background(), b,
		address.MustParseAddr("kQBL2_3lMiyywU17g-or8N7v9hDmPCpttzBPE2isF2GTziky"), "mult", 7, 7)
	if err != nil {
		log.Fatalln("run get method err:", err.Error())
		return
	}

	val, err := res.MustCell(0).BeginParse().LoadUInt(64)
	if err != nil {
		println("ERR", err.Error())
		return
	}

	fmt.Printf("parsed result: %d\n", val)
}
