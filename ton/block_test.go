package ton

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestLoadShardsFromHashes(t *testing.T) {
	data, err := hex.DecodeString("b5ee9c724102090100010b000103d040012201c002032201c00405284801012610bab489d8faa8c9dfaa65e8281895cfc66591881d1d5351574975ce386f2b00032201c00607284801013ca47d35fc14db1a5f2e33f74cf3e833974de0fea9f49759ea84d2522124d12b000201eb50134ea4181081ebe000013951e6cc660000013951e6cc6608c7a91c9653b122d1e49487ecc663e5bb59d8974d6ddd03a4c5cdd7c325498e92b103dc863224263143d3b59124e2a4bce36ddd4ce7f4c43ae0476430da34061280003e18b880000000000000001080fd6b2b80b3cecae02d12000000c90828480101c2256a5539b179d8831bcbfb692dc691ba4c604a72a60ff375306e2b29764a4900010013407735940203b9aca0202872d22f")
	if err != nil {
		t.Fatal(err)
	}

	cl, err := cell.FromBOC(data)
	if err != nil {
		t.Fatal(err)
	}

	di, err := cl.BeginParse().ToDict(32)
	if err != nil {
		t.Fatal(err)
	}

	gotShards, err := LoadShardsFromHashes(di, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotShards) != 1 {
		t.Fatal("not 1 shard")
	}

	gotShards, err = LoadShardsFromHashes(di, false)
	if err == nil {
		t.Fatal("should be err")
	}
}

func TestGetBlockTransactionsV3(t *testing.T) {
	t.Skip()
	client := liteclient.NewConnectionPool()

	configUrl := "https://ton.org/global.config.json"
	err := client.AddConnectionsFromConfigUrl(context.Background(), configUrl)
	if err != nil {
		panic(err)
	}
	api := NewAPIClient(client)
	apiClient := api.WithRetry()

	i := 0
	var block *BlockIDExt
	for {
		newBlock, err := api.CurrentMasterchainInfo(context.Background())
		if err != nil {
			panic(err)
		}

		if block != nil && block.Equals(newBlock) {
			time.Sleep(1 * time.Second)
			continue
		}

		block = newBlock

		b, err := api.GetBlockData(context.Background(), block)
		if err != nil {
			panic(err)
		}

		fmt.Printf("%+v\n\n", b)

		shards, err := api.GetBlockShardsInfo(context.Background(), block)
		if err != nil {
			panic(err)
		}
		for _, shard := range shards {
			fmt.Printf("%+v\n", shard)

			txs, err := apiClient.GetBlockTransactionsV3(context.Background(), shard, 10)
			if err != nil {
				panic(err)
			}
			for _, tx := range txs {
				println(tx.String())
			}

			fmt.Print("\n\n")
		}
		time.Sleep(1 * time.Second)
		i++
		if i > 10 {
			break
		}
	}
}
