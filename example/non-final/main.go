package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
)

func main() {
	client := liteclient.NewConnectionPool()

	servers := map[string]string{
		"88.99.58.187:7445": "qw7r8+rIWloQl9OUAffQlQN4Ltsqtj37iH/tGbH7on8=",
	}

	for addr, key := range servers {
		err := client.AddConnection(context.Background(), addr, key)
		if err != nil {
			log.Fatalln("connection err:", err.Error())
		}
	}

	// Non-final blocks are not finalized, so this example intentionally does not check proofs.
	api := ton.NewAPIClient(client, ton.ProofCheckPolicyUnsafe).WithRetryTimeout(1, 10*time.Second)
	ctx := client.StickyContext(context.Background())

	walletAddr := address.MustParseAddr("UQDnYZIpTwo9RN_84KZX3qIkLVIUJSo8d1yz1vMlKAp2uUaP")

	pending, err := api.GetAllNonfinalPendingShardBlocks(ctx)
	if err != nil {
		log.Fatalln("get non-final pending shard blocks err:", err.Error())
	}

	// mmm, _ := api.GetMasterchainInfo(ctx)
	var (
		block      *ton.BlockIDExt
		seqno      string
		lastRunErr error
	)
	for _, candidate := range pending.Candidates {
		if candidate.Workchain != walletAddr.Workchain() {
			continue
		}
		if !tlb.ShardID(uint64(candidate.Shard)).ContainsAddress(walletAddr) {
			continue
		}

		res, err := api.RunGetMethod(ctx, candidate, walletAddr, "seqno")
		if err != nil {
			// Non-final candidates can disappear or become unavailable between requests.
			lastRunErr = err
			continue
		}

		val, err := res.Int(0)
		if err != nil {
			log.Fatalln("parse seqno err:", err.Error())
		}

		block = candidate
		seqno = val.String()
		break
	}
	if block == nil {
		if lastRunErr != nil {
			log.Fatalln("run seqno err:", lastRunErr.Error())
		}
		log.Fatalln("no non-final candidate block for wallet shard")
	}

	fmt.Printf("wallet: %s\n", walletAddr.String())
	fmt.Printf("non-final block: wc=%d shard=%d seqno=%d\n", block.Workchain, block.Shard, block.SeqNo)
	fmt.Printf("seqno: %s\n", seqno)
}
