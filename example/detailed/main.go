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
	client := liteclient.NewClient()

	servers := map[string]string{
		"65.21.74.140:46427": "JhXt7H1dZTgxQTIyGiYV4f9VUARuDxFl/1kVBjLSMB8=",
		// ...
	}

	// we can connect to any number of servers, load balancing is done inside library
	for addr, key := range servers {
		// connect to testnet lite server
		err := client.Connect(context.Background(), addr, key)
		if err != nil {
			log.Fatalln("connection err: ", err.Error())
			return
		}
	}

	// initialize ton api lite connection wrapper
	api := ton.NewAPIClient(client)

	// we need fresh block info to run get methods
	b, err := api.GetBlockInfo(context.Background())
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

	res, err := api.RunGetMethod(context.Background(), b, address.MustParseAddr("kQB3P0cDOtkFDdxB77YX-F2DGkrIszmZkmyauMnsP1gg0inM"), "mult", 7, 8)
	if err != nil {
		log.Fatalln("run get method err:", err.Error())
		return
	}

	// response can contain multiple results
	// for such case, for example: (int, int) get2ints()

	for _, c := range res {
		switch res := c.(type) {
		case *cell.Cell: // it is used for slice also
			sz, payload, err := res.BeginParse().RestBits()
			if err != nil {
				println("ERR", err.Error())
				return
			}

			fmt.Printf("RESP CELL %d bits, hex(%s)\n", sz, hex.EncodeToString(payload))
		case *big.Int:
			fmt.Println("RESP BIG INT", res.Uint64())
		case uint64:
			fmt.Println("RESP UINT", res)
		}
	}
}
