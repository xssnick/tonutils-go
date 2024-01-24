package main

import (
	"context"
	"fmt"
	"log"
	"sort"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/ton"
)

func main() {
	client := liteclient.NewConnectionPool()

	// connect to mainnet lite servers
	err := client.AddConnectionsFromConfigUrl(context.Background(), "https://ton.org/global.config.json")
	if err != nil {
		log.Fatalln("connection err: ", err.Error())
		return
	}
	// initialize ton api lite connection wrapper
	api := ton.NewAPIClient(client, ton.ProofCheckPolicyFast).WithRetry()

	// if we want to route all requests to the same node, we can use it
	ctx := client.StickyContext(context.Background())

	// we need fresh block info to run get methods
	b, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		log.Fatalln("get block err:", err.Error())
		return
	}

	tx, err := api.GetTransaction(context.Background(), &ton.BlockIDExt{
		Workchain: 0,
		Shard:     -9223372036854775808,
		SeqNo:     41191932,
		RootHash:  []byte{29, 203, 55, 116, 247, 252, 187, 187, 48, 139, 153, 193, 252, 163, 116, 207, 230, 0, 127, 68, 185, 213, 9, 236, 116, 199, 12, 118, 171, 11, 234, 5},
		FileHash:  []byte{250, 85, 139, 105, 94, 217, 189, 239, 205, 116, 107, 127, 111, 54, 87, 212, 198, 176, 212, 241, 20, 25, 59, 202, 177, 249, 185, 72, 86, 145, 7, 241},
	}, address.MustParseAddr("EQDQUqZl5WPeGnOXmWJdZvMsIOTSKIQLVdGiMXve06T5woQ6"), 43848894000001)
	if err != nil {
		log.Fatalln("get ttxxx err:", err.Error())
		return
	}
	println(tx.Dump())
	panic("eee")

	addr := address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N")

	// we use WaitForBlock to make sure block is ready,
	// it is optional but escapes us from liteserver block not ready errors
	res, err := api.WaitForBlock(b.SeqNo).GetAccount(ctx, b, addr)
	if err != nil {
		log.Fatalln("get account err:", err.Error())
		return
	}

	fmt.Printf("Is active: %v\n", res.IsActive)
	if res.IsActive {
		fmt.Printf("Status: %s\n", res.State.Status)
		fmt.Printf("Balance: %s TON\n", res.State.Balance.String())
		if res.Data != nil {
			fmt.Printf("Data: %s\n", res.Data.Dump())
		}
	}

	// take last tx info from account info
	lastHash := res.LastTxHash
	lastLt := res.LastTxLT

	fmt.Printf("\nTransactions:\n")
	for {
		// last transaction has 0 prev lt
		if lastLt == 0 {
			break
		}

		// load transactions in batches with size 15
		list, err := api.ListTransactions(ctx, addr, 15, lastLt, lastHash)
		if err != nil {
			log.Printf("send err: %s", err.Error())
			return
		}
		// set previous info from the oldest transaction in list
		lastHash = list[0].PrevTxHash
		lastLt = list[0].PrevTxLT

		// reverse list to show the newest first
		sort.Slice(list, func(i, j int) bool {
			return list[i].LT > list[j].LT
		})

		for _, t := range list {
			fmt.Println(t.String())
		}
	}
}
