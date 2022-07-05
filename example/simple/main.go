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

	// connect to testnet lite server
	err := client.Connect(context.Background(), "65.21.74.140:46427", "JhXt7H1dZTgxQTIyGiYV4f9VUARuDxFl/1kVBjLSMB8=")
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

	/*
		We will run such method of contract
		cell mult(int a, int b) method_id {
		  return begin_cell().store_uint(a * b, 64).end_cell();
		}
	*/

	res, err := api.RunGetMethod(context.Background(), b, address.MustParseAddr("kQB3P0cDOtkFDdxB77YX-F2DGkrIszmZkmyauMnsP1gg0inM"), "mult", 7, 7)
	if err != nil {
		log.Fatalln("run get method err:", err.Error())
		return
	}

	val, err := res[0].(*cell.Cell).BeginParse().LoadUInt(64)
	if err != nil {
		println("ERR", err.Error())
		return
	}

	fmt.Printf("parsed result: %d\n", val)
}
