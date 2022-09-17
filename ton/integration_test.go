package ton

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

var api = func() *APIClient {
	client := liteclient.NewConnectionPool()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := client.AddConnectionsFromConfigUrl(ctx, "https://ton-blockchain.github.io/global.config.json")
	if err != nil {
		panic(err)
	}

	return NewAPIClient(client)
}()

var testContractAddr = func() *address.Address {
	return address.MustParseAddr("EQBL2_3lMiyywU17g-or8N7v9hDmPCpttzBPE2isF2GTzpK4")
}()

func Test_CurrentChainInfo(t *testing.T) {
	b, err := api.CurrentMasterchainInfo(context.Background())
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()

	cached, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block 2 err:", err.Error())
		return
	}

	if cached.SeqNo != b.SeqNo {
		t.Fatal("not eq")
	}
}

func TestAPIClient_GetBlockData(t *testing.T) {
	b, err := api.CurrentMasterchainInfo(context.Background())
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	_, err = api.GetBlockData(context.Background(), b)
	if err != nil {
		t.Fatal("GetBlockData err:", err.Error())
		return
	}

	// TODO: data check
}

func Test_RunMethod(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	b, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	c1 := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().BeginParse()
	c2 := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()

	res, err := api.RunGetMethod(ctx, b, testContractAddr, "clltst2", c1, c2)
	if err != nil {
		t.Fatal("run get method err:", err.Error())
		return
	}

	fmt.Println(res.result)
	if !bytes.Equal(res.MustSlice(0).MustToCell().Hash(), c1.MustToCell().Hash()) {
		t.Fatal("1st arg not eq return 1st value")
	}

	cmp2 := cell.BeginCell().MustStoreUInt(0xAA, 8).MustStoreRef(c2).EndCell()
	if !bytes.Equal(res.MustCell(1).Hash(), cmp2.Hash()) {
		t.Fatal("1st arg not eq return 1st value")
	}
}

func Test_ExternalMessage(t *testing.T) { // need to deploy contract on test-net - > than change config to test-net.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ctx = api.client.StickyContext(ctx)

	b, err := api.GetMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	res, err := api.RunGetMethod(ctx, b, testContractAddr, "get_total")
	if err != nil {
		t.Fatal("run get method err:", err.Error())
		return
	}

	seqno := res.MustInt(0)
	total := res.MustInt(1)

	data := cell.BeginCell().
		MustStoreBigInt(seqno, 64).
		MustStoreUInt(1, 16). // add 1 to total
		EndCell()

	err = api.SendExternalMessage(ctx, &tlb.ExternalMessage{
		DstAddr: testContractAddr,
		Body:    data,
	})
	if err != nil {
		// FYI: it can fail if not enough balance on contract
		t.Fatal("SendExternalMessage err:", err.Error())
		return
	}

	// TODO: wait for update and check result

	log.Printf("Current seqno = %d and total = %d", seqno, total)
}

func Test_Account(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	b, err := api.GetMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	addr := address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N")
	res, err := api.GetAccount(ctx, b, addr)
	if err != nil {
		t.Fatal("get account err:", err.Error())
		return
	}

	if res.HasGetMethod("run_ticktock") {
		t.Fatal("has ticktock as get method")
	}

	fmt.Printf("Is active: %v\n", res.IsActive)
	if res.IsActive {
		fmt.Printf("Status: %s\n", res.State.Status)
		fmt.Printf("Balance: %s TON\n", res.State.Balance.TON())
		if res.Data != nil {
			fmt.Printf("Data: %s\n", res.Data.Dump())
		}
	} else {
		t.Fatal("TF account not active")
	}

	// take last tx info from account info
	lastHash := res.LastTxHash
	lastLt := res.LastTxLT

	fmt.Printf("\nTransactions:\n")
	for i := 0; i < 2; i++ {
		// last transaction has 0 prev lt
		if lastLt == 0 {
			break
		}

		// load transactions in batches with size 5
		list, err := api.ListTransactions(ctx, addr, 5, lastLt, lastHash)
		if err != nil {
			t.Fatal("send err:", err.Error())
			return
		}

		// oldest = first in list
		for _, t := range list {
			fmt.Println(t.String())
		}

		// set previous info from the oldest transaction in list
		lastHash = list[0].PrevTxHash
		lastLt = list[0].PrevTxLT
	}
}

func Test_AccountHasMethod(t *testing.T) {
	connectionPool := liteclient.NewConnectionPool()

	_ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx := connectionPool.StickyContext(_ctx)

	b, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	addr := address.MustParseAddr("EQCW0cn9TQuZ3tW_Tche1HIGGa7apwFsi7v3YtmYC6FoIzLr")
	res, err := api.GetAccount(ctx, b, addr)
	if err != nil {
		t.Fatal("get account err:", err.Error())
		return
	}

	if !res.HasGetMethod("get_nft_data") {
		t.Fatal("nft not has get_nft_data")
	}

	if res.HasGetMethod("seqno") {
		t.Fatal("nft has seqno")
	}

	if res.HasGetMethod("recv_internal") {
		t.Fatal("has recv_internal as get method")
	}

	if res.HasGetMethod("recv_external") {
		t.Fatal("has recv_external as get method")
	}
}

func Test_BlockScan(t *testing.T) {
	var shards []*tlb.BlockInfo
	for {
		// we need fresh block info to run get methods
		master, err := api.GetMasterchainInfo(context.Background())
		if err != nil {
			log.Fatalln("get block err:", err.Error())
			return
		}

		shards, err = api.GetBlockShardsInfo(context.Background(), master)
		if err != nil {
			log.Fatalln("get shards err:", err.Error())
			return
		}

		if len(shards) == 0 {
			log.Println("master block without shards, waiting for next...")
			time.Sleep(3 * time.Second)
			continue
		}
		break
	}

	var err error
	for {
		var txList []*tlb.Transaction

		for _, shard := range shards {
			log.Printf("scanning block %d of shard %d...", shard.SeqNo, shard.Shard)

			var fetchedIDs []*tlb.TransactionID
			var after *tlb.TransactionID
			var more = true

			// load all transactions in batches with 100 transactions in each while exists
			for more {
				fetchedIDs, more, err = api.GetBlockTransactions(context.Background(), shard, 100, after)
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
					tx, err := api.GetTransaction(context.Background(), shard, address.NewAddress(0, 0, id.AccountID), id.LT)
					if err != nil {
						log.Fatalln("get tx data err:", err.Error())
						return
					}
					txList = append(txList, tx)
				}
			}
		}

		if len(txList) > 0 {
			for i, transaction := range txList {
				log.Println(i, transaction.String())
				return
			}
		} else {
			log.Println("no transactions in this block")
		}

		for i, shard := range shards {
			// wait for next block and get its info
			for {
				time.Sleep(3 * time.Second)

				shards[i], err = api.LookupBlock(context.Background(), shard.Workchain, shard.Shard, shard.SeqNo+1)
				if err != nil {
					if err == ErrBlockNotFound {
						log.Printf("block %d of shard %d is not exists yet, waiting a bit longer...", shard.SeqNo+1, shard.Shard)
						continue
					}

					log.Fatalln("lookup block err:", err.Error())
					return
				}
				break
			}
		}
	}
}

func TestAPIClient_WaitNextBlock(t *testing.T) {
	c, err := api.CurrentMasterchainInfo(context.Background())
	if err != nil {
		t.Fatal("get curr block err:", err.Error())
	}

	n, err := api.WaitNextMasterBlock(context.Background(), c)
	if err != nil {
		t.Fatal("wait block err:", err.Error())
	}

	if n.SeqNo != c.SeqNo+1 {
		t.Fatal("seqno incorrect")
	}

	c.Workchain = 7
	n, err = api.WaitNextMasterBlock(context.Background(), c)
	if err == nil {
		t.Fatal("it works with not master")
	}
}
