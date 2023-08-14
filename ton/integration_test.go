package ton

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"reflect"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

var apiTestNet = func() *APIClient {
	client := liteclient.NewConnectionPool()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.AddConnectionsFromConfigUrl(ctx, "https://ton-blockchain.github.io/testnet-global.config.json")
	if err != nil {
		panic(err)
	}

	return NewAPIClient(client)
}()

var api = func() APIClientWrapped {
	client := liteclient.NewConnectionPool()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg, err := liteclient.GetConfigFromUrl(ctx, "https://ton.org/global.config.json")
	if err != nil {
		panic(err)
	}

	err = client.AddConnectionsFromConfig(ctx, cfg)
	if err != nil {
		panic(err)
	}

	a := NewAPIClient(client, ProofCheckPolicySecure).WithRetry()
	// a.SetTrustedBlockFromConfig(cfg)
	return a
}()

var testContractAddr = func() *address.Address {
	return address.MustParseAddr("EQBL2_3lMiyywU17g-or8N7v9hDmPCpttzBPE2isF2GTzpK4")
}()

var testContractAddrTestNet = func() *address.Address {
	return address.MustParseAddr("EQAOp1zuKuX4zY6L9rEdSLam7J3gogIHhfRu_gH70u2MQnmd")
}()

func Test_CurrentChainInfo(t *testing.T) {
	ctx := api.Client().StickyContext(context.Background())

	b, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Millisecond)
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
	ctx := api.Client().StickyContext(context.Background())

	b, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	_, err = api.WaitForBlock(b.SeqNo).GetBlockData(ctx, b)
	if err != nil {
		t.Fatal("Get master block data err:", err.Error())
		return
	}

	shards, err := api.WaitForBlock(b.SeqNo).GetBlockShardsInfo(ctx, b)
	if err != nil {
		log.Fatalln("get shards err:", err.Error())
		return
	}

	for _, shard := range shards {
		data, err := api.GetBlockData(ctx, shard)
		if err != nil {
			t.Fatal("Get shard block data err:", err.Error())
			return
		}
		_, err = data.BlockInfo.GetParentBlocks()
		if err != nil {
			t.Fatal("Get block parents err:", err.Error())
			return
		}
	}

	// TODO: data check
}

func TestAPIClient_GetOldBlockData(t *testing.T) {
	client := liteclient.NewConnectionPool()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.AddConnection(ctx, "135.181.177.59:53312", "aF91CuUHuuOv9rm2W5+O/4h38M3sRm40DtSdRxQhmtQ=")
	if err != nil {
		panic(err)
	}

	api := NewAPIClient(client)

	b, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	b, err = api.LookupBlock(ctx, b.Workchain, b.Shard, 3)
	if err != nil {
		t.Fatal("lookup err:", err.Error())
		return
	}

	shards, err := api.GetBlockShardsInfo(ctx, b)
	if err != nil {
		log.Fatalln("get shards err:", err.Error())
		return
	}

	for _, shard := range shards {
		data, err := api.GetBlockData(ctx, shard)
		if err != nil {
			t.Fatal("Get shard block data err:", err.Error())
			return
		}
		_, err = data.BlockInfo.GetParentBlocks()
		if err != nil {
			t.Fatal("Get block parents err:", err.Error())
			return
		}
	}

	_, err = api.GetBlockData(ctx, b)
	if err != nil {
		t.Fatal("Get master block data err:", err.Error())
		return
	}

	// TODO: data check
}

func Test_RunMethod(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	b, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	c1 := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().BeginParse()
	c2 := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()

	res, err := api.WaitForBlock(b.SeqNo).RunGetMethod(ctx, b, testContractAddr, "clltst2", c1, c2)
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ctx = apiTestNet.client.StickyContext(ctx)

	b, err := apiTestNet.GetMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	res, err := apiTestNet.WaitForBlock(b.SeqNo).RunGetMethod(ctx, b, testContractAddrTestNet, "get_total")
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

	err = apiTestNet.SendExternalMessage(ctx, &tlb.ExternalMessage{
		DstAddr: testContractAddrTestNet,
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = api.Client().StickyContext(ctx)

	b, err := api.GetMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	addr := address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N")
	res, err := api.WaitForBlock(b.SeqNo).GetAccount(ctx, b, addr)
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
		fmt.Printf("Balance: %s TON\n", res.State.Balance.String())
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

func Test_AccountMaster(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = api.Client().StickyContext(ctx)

	b, err := api.GetMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	addr := address.MustParseAddr("Ef9VVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVbxn")
	res, err := api.WaitForBlock(b.SeqNo).GetAccount(ctx, b, addr)
	if err != nil {
		t.Fatal("get account err:", err.Error())
		return
	}

	if !res.HasGetMethod("list_proposals") {
		t.Fatal("has no list_proposals as get method")
	}

	fmt.Printf("Is active: %v\n", res.IsActive)
	if res.IsActive {
		fmt.Printf("Status: %s\n", res.State.Status)
		fmt.Printf("Balance: %s TON\n", res.State.Balance.String())
		if res.Data == nil {
			t.Fatal("data null")
		}
	} else {
		t.Fatal("account not active")
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
	res, err := api.WaitForBlock(b.SeqNo).GetAccount(ctx, b, addr)
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
	ctx := api.Client().StickyContext(context.Background())
	var shards []*BlockIDExt
	for {
		// we need fresh block info to run get methods
		master, err := api.GetMasterchainInfo(ctx)
		if err != nil {
			log.Fatalln("get block err:", err.Error())
			return
		}

		shards, err = api.WaitForBlock(master.SeqNo).GetBlockShardsInfo(ctx, master)
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

			var fetchedIDs []TransactionShortInfo
			var after *TransactionID3
			var more = true

			// load all transactions in batches with 100 transactions in each while exists
			for more {
				fetchedIDs, more, err = api.GetBlockTransactionsV2(ctx, shard, 100, after)
				if err != nil {
					log.Fatalln("get tx ids err:", err.Error())
					return
				}

				if more {
					// set load offset for next query (pagination)
					after = fetchedIDs[len(fetchedIDs)-1].ID3()
				}

				for _, id := range fetchedIDs {
					// get full transaction by id
					tx, err := api.GetTransaction(ctx, shard, address.NewAddress(0, 0, id.Account), id.LT)
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

				shards[i], err = api.LookupBlock(ctx, shard.Workchain, shard.Shard, shard.SeqNo+1)
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

func Test_GetTime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	utime, err := api.GetTime(ctx)
	if err != nil {
		t.Fatal("get time err:", err.Error())
	}
	log.Println("current node utime: ", time.Unix(int64(utime), 0))
}

func Test_GetConfigParamsAll(t *testing.T) {
	ctx := api.Client().StickyContext(context.Background())

	b, err := api.GetMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block 2 err:", err.Error())
		return
	}

	conf, err := api.WaitForBlock(b.SeqNo).GetBlockchainConfig(ctx, b)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	if len(conf.All()) < 20 {
		t.Fatal("bad config response, too short")
	}

	if conf.Get(8).BeginParse().MustLoadUInt(8) != 0xC4 {
		t.Fatal("bad config response for 8 param")
	}
}

func Test_GetConfigParams8(t *testing.T) {
	ctx := api.Client().StickyContext(context.Background())

	b, err := api.GetMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block 2 err:", err.Error())
		return
	}

	conf, err := api.WaitForBlock(b.SeqNo).GetBlockchainConfig(ctx, b, 8)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	if len(conf.All()) != 1 {
		t.Fatal("bad config response, bad length")
	}

	if conf.Get(8).BeginParse().MustLoadUInt(8) != 0xC4 {
		t.Fatal("bad config response for 8 param")
	}
}

func Test_LSErrorCase(t *testing.T) {
	connectionPool := liteclient.NewConnectionPool()

	_ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx := connectionPool.StickyContext(_ctx)

	b, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}
	b.RootHash[12] = b.RootHash[12] << 1

	addr := address.MustParseAddr("EQCW0cn9TQuZ3tW_Tche1HIGGa7apwFsi7v3YtmYC6FoIzLr")
	_, err = api.GetAccount(ctx, b, addr)
	if err != nil {
		_, ok := err.(LSError)
		if !ok {
			t.Fatalf("not expected type of error, want LSError, got '%s'", reflect.TypeOf(err).String())
		}
	}
}

func TestAccountStorage_LoadFromCell_ExtraCurrencies(t *testing.T) {
	client := liteclient.NewConnectionPool()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.AddConnection(context.Background(), "135.181.177.59:53312", "aF91CuUHuuOv9rm2W5+O/4h38M3sRm40DtSdRxQhmtQ=")
	if err != nil {
		t.Fatal(err)
	}

	mainnetAPI := NewAPIClient(client)

	shard := uint64(0xa000000000000000)

	b, err := mainnetAPI.LookupBlock(ctx, 0, int64(shard), 3328952)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("with proof", func(t *testing.T) {
		_, err := mainnetAPI.GetAccount(ctx, b, address.MustParseAddr("EQCYv992KVNNCKZHSLLJgM2GGzsgL0UgWP24BCQBaAdqSE2I"))
		if err != ErrNoProof {
			t.Fatal(err)
		}
	})

	t.Run("without proof", func(t *testing.T) {
		mainnetAPI := NewAPIClient(client, ProofCheckPolicyUnsafe)

		a, err := mainnetAPI.GetAccount(ctx, b, address.MustParseAddr("EQCYv992KVNNCKZHSLLJgM2GGzsgL0UgWP24BCQBaAdqSE2I"))
		if err != nil {
			t.Fatal(err)
		}

		if a.State.ExtraCurrencies == nil {
			t.Fatal("expected extra currencies dict")
		}
	})
}

func TestAPIClient_GetBlockProofForward(t *testing.T) {
	cfg, err := liteclient.GetConfigFromUrl(context.Background(), "https://ton.org/global.config.json")
	if err != nil {
		t.Fatal("get cfg err:", err.Error())
		return
	}

	ctx := api.Client().StickyContext(context.Background())

	initBlock := BlockIDExt(cfg.Validator.InitBlock)
	known := &initBlock

	stm := time.Now()

	for _, dir := range []string{"backward", "forward"} {
		b, err := api.CurrentMasterchainInfo(ctx)
		if err != nil {
			t.Fatal("get block err:", err.Error())
			return
		}

		if dir == "backward" {
			known, b = b, known
		}

		t.Run("Block proof "+dir, func(t *testing.T) {
			if err = api.VerifyProofChain(ctx, known, b); err != nil {
				t.Fatal("failed to verify chain:", err.Error())
				return
			}
			log.Println("DONE!", time.Since(stm))
		})
	}
}

func TestAPIClient_SubscribeOnTransactions(t *testing.T) {
	_ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx := api.Client().StickyContext(_ctx)

	addr := address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N")

	b, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	acc, err := api.WaitForBlock(b.SeqNo).GetAccount(ctx, b, addr)
	if err != nil {
		t.Fatal("get acc err:", err.Error())
		return
	}
	initLT := acc.LastTxLT - 600000000000
	log.Println(initLT)
	lastLT := initLT

	ctx, cancel = context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()

	ch := make(chan *tlb.Transaction)
	go api.SubscribeOnTransactions(ctx, addr, lastLT, ch)

	for tx := range ch {
		if lastLT > tx.LT {
			t.Fatal("incorrect tx order")
		}
		lastLT = tx.LT

		println(tx.Now, tx.String())
	}

	if lastLT == initLT {
		t.Fatal("no transactions")
	}
}
