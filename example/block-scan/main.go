package main

import (
	"context"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"log"
)

func main() {
	client := liteclient.NewConnectionPool()

	// connect to mainnet lite servers
	err := client.AddConnectionsFromConfigUrl(context.Background(), "https://ton-blockchain.github.io/global.config.json")
	if err != nil {
		log.Fatalln("connection err: ", err.Error())
		return
	}

	// initialize ton api lite connection wrapper
	api := ton.NewAPIClient(client)

	master, err := api.GetMasterchainInfo(context.Background())
	if err != nil {
		log.Fatalln("get block err:", err.Error())
		return
	}

	// bound all requests to single lite server for consistency,
	// if it will go down, another lite server will be used
	ctx := api.Client().StickyContext(context.Background())

	for {
		log.Printf("scanning %d master block...\n", master.SeqNo)

		// getting information about other work-chains and shards of master block
		shards, err := api.GetBlockShardsInfo(ctx, master)
		if err != nil {
			log.Fatalln("get shards err:", err.Error())
			return
		}

		var txList []*tlb.Transaction

		// for each shard block getting transactions
		for _, shard := range shards {
			log.Printf("scanning block %d of shard %d...", shard.SeqNo, shard.Shard)

			var fetchedIDs []*tlb.TransactionID
			var after *tlb.TransactionID
			var more = true

			// load all transactions in batches with 100 transactions in each while exists
			for more {
				fetchedIDs, more, err = api.GetBlockTransactions(ctx, shard, 100, after)
				if err != nil {
					log.Fatalln("get tx ids err:", err.Error())
					return
				}

				if more {
					// set load offset for next query (pagination)
					after = fetchedIDs[len(fetchedIDs)-1]
				}

				for _, id := range fetchedIDs {
					// get full transaction by id
					tx, err := api.GetTransaction(ctx, shard, address.NewAddress(0, 0, id.AccountID), id.LT)
					if err != nil {
						log.Fatalln("get tx data err:", err.Error())
						return
					}
					txList = append(txList, tx)
				}
			}
		}

		for i, transaction := range txList {
			log.Println(i, transaction.String())
		}

		if len(txList) == 0 {
			log.Printf("no transactions in %d block\n", master.SeqNo)
		}

		master, err = api.WaitNextMasterBlock(ctx, master)
		if err != nil {
			log.Fatalln("wait next master err:", err.Error())
		}
	}
}
