package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func main() {
	client := liteclient.NewConnectionPool()

	servers := map[string]string{
		"135.181.140.212:13206": "K0t3+IWLOXHYMvMcrGZDPs+pn58a17LFbnXoQkKc2xw=",
		// ...
	}

	// we can connect to any number of servers, load balancing is done inside library
	for addr, key := range servers {
		// connect to lite server
		err := client.AddConnection(context.Background(), addr, key)
		if err != nil {
			log.Fatalln("connection err: ", err.Error())
			return
		}
	}

	// initialize ton api lite connection wrapper
	api := ton.NewAPIClient(client).WithRetry()

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
		address.MustParseAddr("kQBL2_3lMiyywU17g-or8N7v9hDmPCpttzBPE2isF2GTziky"), "mult", 7, 8)
	if err != nil {
		log.Fatalln("run get method err:", err.Error())
		return
	}

	// response can contain multiple results
	// for such case, for example: (int, int) get2ints()

	for _, c := range res.AsTuple() {
		switch res := c.(type) {
		case *cell.Cell:
			sz, payload, err := res.BeginParse().RestBits()
			if err != nil {
				println("ERR", err.Error())
				return
			}

			fmt.Printf("RESP CELL %d bits, hex(%s)\n", sz, hex.EncodeToString(payload))
		case *cell.Slice:
			sz, payload, err := res.RestBits()
			if err != nil {
				println("ERR", err.Error())
				return
			}

			fmt.Printf("RESP SLICE %d bits, hex(%s)\n", sz, hex.EncodeToString(payload))
		case *big.Int:
			fmt.Println("RESP INT", res.Uint64())
		}
	}
}
