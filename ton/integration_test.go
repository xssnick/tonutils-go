package ton

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
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

const (
	integrationSetupTimeout        = 15 * time.Second
	integrationRequestTimeout      = 30 * time.Second
	integrationRetryAttemptTimeout = 3 * time.Second
)

var apiTestNet = func() APIClientWrapped {
	client := liteclient.NewConnectionPool()

	ctx, cancel := context.WithTimeout(context.Background(), integrationSetupTimeout)
	defer cancel()

	err := client.AddConnectionsFromConfigUrl(ctx, "https://ton-blockchain.github.io/testnet-global.config.json")
	if err != nil {
		panic(err)
	}

	return NewAPIClient(client, ProofCheckPolicyFast)
}()

var api = func() APIClientWrapped {
	client := liteclient.NewConnectionPool()

	ctx, cancel := context.WithTimeout(context.Background(), integrationSetupTimeout)
	defer cancel()

	cfg, err := liteclient.GetConfigFromUrl(ctx, "https://ton-blockchain.github.io/global.config.json")
	if err != nil {
		panic(err)
	}

	err = client.AddConnectionsFromConfig(ctx, cfg)
	if err != nil {
		panic(err)
	}

	a := NewAPIClient(client, ProofCheckPolicyFast).WithRetryTimeout(0, integrationRetryAttemptTimeout)
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

	// TODO: uncomment after aug dict impl
	/*x, err := tlb.ToCell(mb)
	if err != nil {
		t.Fatal("to cell err:", err.Error())
	}

	if !bytes.Equal(x.Hash(), b.RootHash) {
		t.Fatal("master hash not eq after serialize")
	}*/

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
		_, err = GetParentBlocks(&data.BlockInfo)
		if err != nil {
			t.Fatal("Get block parents err:", err.Error())
			return
		}

		x, err := tlb.ToCell(data)
		if err != nil {
			t.Fatal("to cell err:", err.Error())
		}

		if !bytes.Equal(x.Hash(), shard.RootHash) {
			t.Fatal("hash not eq after serialize")
		}

		var bData tlb.Block
		if err = tlb.LoadFromCell(&bData, x.MustBeginParse()); err != nil {
			t.Fatal(err)
		}

		x2, err := tlb.ToCell(bData)
		if err != nil {
			t.Fatal("to cell2 err:", err.Error())
		}

		if !bytes.Equal(x.Hash(), x2.Hash()) {
			t.Fatal("hash not eq after serialize/deserialize")
		}
	}

	// TODO: data check
}

func TestAPIClient_GetBlockHeader(t *testing.T) {
	ctx := api.Client().StickyContext(context.Background())

	b, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	hdr, err := api.WaitForBlock(b.SeqNo).GetBlockHeader(ctx, b)
	if err != nil {
		t.Fatal("Get master block data err:", err.Error())
		return
	}

	if hdr.SeqNo != b.SeqNo {
		t.Fatal("not eq")
	}
}

func TestAPIClient_GetOutMsgQueueSizesLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(apiTestNet.Client().StickyContext(context.Background()), 90*time.Second)
	defer cancel()

	wc := address.MasterchainID
	shard := int64(-9223372036854775808)
	sizes, err := apiTestNet.GetOutMsgQueueSizes(ctx, &wc, &shard)
	if err != nil {
		if isIntegrationNodeTimeout(err) {
			t.Skipf("live liteservers did not answer getOutMsgQueueSizes: %v", err)
		}
		t.Fatal("get out msg queue sizes err:", err.Error())
	}
	if len(sizes.Shards) == 0 {
		t.Fatal("expected at least one out msg queue size")
	}
	if sizes.Shards[0].ID == nil {
		t.Fatal("out msg queue size has nil block id")
	}
}

func TestAPIClient_OutMsgQueueProofMethods(t *testing.T) {
	ctx, cancel := context.WithTimeout(apiTestNet.Client().StickyContext(context.Background()), 90*time.Second)
	defer cancel()

	block, err := apiTestNet.CurrentMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
	}

	blockSize, err := apiTestNet.GetBlockOutMsgQueueSize(ctx, block)
	if err != nil {
		t.Fatal("get block out msg queue size err:", err.Error())
	}
	if blockSize.Proof == nil {
		t.Fatal("expected block out msg queue size proof")
	}
	if blockSize.Size < 0 {
		t.Fatalf("unexpected negative queue size: %d", blockSize.Size)
	}

	dispatchInfo, err := apiTestNet.GetDispatchQueueInfo(ctx, block, nil, 1)
	if err != nil {
		t.Fatal("get dispatch queue info err:", err.Error())
	}
	if dispatchInfo.Proof == nil {
		t.Fatal("expected dispatch queue info proof")
	}
	if len(dispatchInfo.AccountDispatchQueues) > 1 {
		t.Fatalf("expected at most one dispatch queue account, got %d", len(dispatchInfo.AccountDispatchQueues))
	}

	zeroAddr := address.NewAddress(0, 0, make([]byte, 32))
	messages, err := apiTestNet.GetDispatchQueueMessages(ctx, block, zeroAddr, 0, 2, WithDispatchQueueOneAccount(), WithDispatchQueueMessagesBOC())
	if err != nil {
		t.Fatal("get dispatch queue messages err:", err.Error())
	}
	if messages.Proof == nil {
		t.Fatal("expected dispatch queue messages proof")
	}
	if len(messages.Messages) > 2 {
		t.Fatalf("expected at most two dispatch queue messages, got %d", len(messages.Messages))
	}
}

func isIntegrationNodeTimeout(err error) bool {
	return errors.Is(err, liteclient.ErrADNLReqTimeout) ||
		errors.Is(err, liteclient.ErrNoNodesLeft) ||
		errors.Is(err, context.DeadlineExceeded)
}

// commented because public archival LS works too bad to test
/*func TestAPIClient_GetOldBlockData(t *testing.T) {
	client := liteclient.NewConnectionPool()

	var ok bool
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		err := client.AddConnection(ctx, "135.181.177.59:53312", "aF91CuUHuuOv9rm2W5+O/4h38M3sRm40DtSdRxQhmtQ=")
		cancel()
		if err != nil {
			log.Println("ERR TRY", i)
			continue
		}
		ok = true
		break
	}

	if !ok {
		panic("connect to archive node failed")
	}

	log.Println("CONNECTED")

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

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
	log.Println("LOOKUP: ", b.SeqNo)

	shards, err := api.GetBlockShardsInfo(ctx, b)
	if err != nil {
		log.Fatalln("get shards err:", err.Error())
		return
	}
	log.Println("SHARDS: ", b.SeqNo)

	for _, shard := range shards {
		data, err := api.GetBlockData(ctx, shard)
		if err != nil {
			t.Fatal("Get shard block data err:", err.Error())
			return
		}
		_, err = GetParentBlocks(&data.BlockInfo)
		if err != nil {
			t.Fatal("Get block parents err:", err.Error())
			return
		}
		log.Println("GOT SHARD: ", shard.Shard, shard.SeqNo)
	}

	_, err = api.GetBlockData(ctx, b)
	if err != nil {
		t.Fatal("Get master block data err:", err.Error())
		return
	}

	// TODO: data check
}*/

func Test_RunMethod(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), integrationRequestTimeout)
	defer cancel()

	b, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	c1 := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().MustBeginParse()
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
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	ctx = apiTestNet.Client().StickyContext(ctx)

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

	tx, block, _, err := apiTestNet.SendExternalMessageWaitTransaction(ctx, &tlb.ExternalMessage{
		DstAddr: testContractAddrTestNet,
		Body:    data,
	})
	if err != nil {
		// FYI: it can fail if not enough balance on contract
		t.Fatal("SendExternalMessage err:", err.Error())
		return
	}

	log.Printf("Current seqno = %d and total = %d | block: %d tx: %d hash: %s", seqno, total, block.SeqNo, tx.LT, base64.URLEncoding.EncodeToString(tx.Hash))
}

func Test_Account(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), integrationRequestTimeout)
	defer cancel()
	ctx = api.Client().StickyContext(ctx)

	b, err := api.GetMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	addr := address.MustParseAddr("EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs")
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

func Test_AccountMaster(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), integrationRequestTimeout)
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

	_ctx, cancel := context.WithTimeout(context.Background(), integrationRequestTimeout)
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
	ctx, cancel := context.WithTimeout(context.Background(), integrationRequestTimeout)
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

	if conf.Get(8).MustBeginParse().MustLoadUInt(8) != 0xC4 {
		t.Fatal("bad config response for 8 param")
	}

	for id := range conf.All() {
		if conf.Get(id) == nil {
			t.Fatalf("Get(%d) returned nil", id)
		}
	}

	root, err := conf.ToCell()
	if err != nil {
		t.Fatal("build config root err:", err.Error())
		return
	}

	parsedRoot, err := cell.FromBOC(root.ToBOC())
	if err != nil {
		t.Fatal("parse config boc err:", err.Error())
		return
	}

	testTLBBlockchainConfigCoverage(t, tlb.BlockchainConfig{Root: parsedRoot})
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

	if conf.Get(8).MustBeginParse().MustLoadUInt(8) != 0xC4 {
		t.Fatal("bad config response for 8 param")
	}
}

func testTLBBlockchainConfigCoverage(t *testing.T, cfg tlb.BlockchainConfig) {
	t.Helper()

	for _, id := range []uint32{
		tlb.ConfigParamConfigAddress,
		tlb.ConfigParamElectorAddress,
		tlb.ConfigParamMinterAddress,
		tlb.ConfigParamFeeCollectorAddress,
		tlb.ConfigParamDNSRootAddress,
		tlb.ConfigParamBurningConfig,
		tlb.ConfigParamExtraCurrencyMintPrices,
		tlb.ConfigParamExtraCurrencyToMint,
		tlb.ConfigParamGlobalVersion,
		tlb.ConfigParamMandatoryParams,
		tlb.ConfigParamCriticalParams,
		tlb.ConfigParamConfigVotingSetup,
		tlb.ConfigParamWorkchains,
		tlb.ConfigParamComplaintPricing,
		tlb.ConfigParamBlockCreateFees,
		tlb.ConfigParamValidatorElectionTimings,
		tlb.ConfigParamValidatorCountLimits,
		tlb.ConfigParamValidatorStakeLimits,
		tlb.ConfigParamStoragePrices,
		tlb.ConfigParamGlobalID,
		tlb.ConfigParamGasPricesMasterchain,
		tlb.ConfigParamGasPricesBasechain,
		tlb.ConfigParamBlockLimitsMasterchain,
		tlb.ConfigParamBlockLimitsBasechain,
		tlb.ConfigParamMsgForwardPricesMasterchain,
		tlb.ConfigParamMsgForwardPricesBasechain,
		tlb.ConfigParamCatchainConfig,
		tlb.ConfigParamConsensusConfig,
		tlb.ConfigParamNewConsensusConfig,
		tlb.ConfigParamFundamentalSMCAddresses,
		tlb.ConfigParamPrevValidators,
		tlb.ConfigParamPrevTempValidators,
		tlb.ConfigParamCurrentValidators,
		tlb.ConfigParamCurrentTempValidators,
		tlb.ConfigParamNextValidators,
		tlb.ConfigParamNextTempValidators,
		tlb.ConfigParamValidatorTempKeys,
		tlb.ConfigParamMisbehaviourPunishment,
		tlb.ConfigParamSizeLimits,
		tlb.ConfigParamSuspendedAddressList,
		tlb.ConfigParamPrecompiledContracts,
	} {
		param, err := cfg.GetParam(id)
		if err != nil {
			if errors.Is(err, tlb.ErrBlockchainConfigParamAbsent) && testTLBBlockchainConfigOptionalParam(id) {
				continue
			}

			t.Fatalf("GetParam(%d) failed: %v", id, err)
		}

		if param == nil {
			t.Fatalf("GetParam(%d) returned nil cell", id)
		}
	}

	testTLBBlockchainConfigMustGet(t, "GetConfigAddress", func() error {
		_, err := cfg.GetConfigAddress()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetElectorAddress", func() error {
		_, err := cfg.GetElectorAddress()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetMinterAddress", func() error {
		_, err := cfg.GetMinterAddress()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetFeeCollectorAddress", func() error {
		_, err := cfg.GetFeeCollectorAddress()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetDNSRootAddress", func() error {
		_, err := cfg.GetDNSRootAddress()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetBurningConfig", func() error {
		_, err := cfg.GetBurningConfig()
		return err
	})

	testTLBBlockchainConfigMaybeAbsent(t, "GetExtraCurrencyMintPrices", func() error {
		_, err := cfg.GetExtraCurrencyMintPrices()
		return err
	})

	testTLBBlockchainConfigMaybeAbsent(t, "GetExtraCurrencyToMint", func() error {
		_, err := cfg.GetExtraCurrencyToMint()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetGlobalVersion", func() error {
		_, err := cfg.GetGlobalVersion()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetMandatoryParams", func() error {
		_, err := cfg.GetMandatoryParams()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetCriticalParams", func() error {
		_, err := cfg.GetCriticalParams()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetConfigVotingSetup", func() error {
		_, err := cfg.GetConfigVotingSetup()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetWorkchains", func() error {
		_, err := cfg.GetWorkchains()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetComplaintPricing", func() error {
		_, err := cfg.GetComplaintPricing()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetBlockCreateFees", func() error {
		_, err := cfg.GetBlockCreateFees()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetValidatorElectionTimings", func() error {
		_, err := cfg.GetValidatorElectionTimings()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetValidatorCountLimits", func() error {
		_, err := cfg.GetValidatorCountLimits()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetValidatorStakeLimits", func() error {
		_, err := cfg.GetValidatorStakeLimits()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetStoragePrices", func() error {
		_, err := cfg.GetStoragePrices(0)
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetGlobalID", func() error {
		_, err := cfg.GetGlobalID()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetGasPrices(masterchain)", func() error {
		_, err := cfg.GetGasPrices(true)
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetGasPrices(basechain)", func() error {
		_, err := cfg.GetGasPrices(false)
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetBlockLimits(masterchain)", func() error {
		_, err := cfg.GetBlockLimits(true)
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetBlockLimits(basechain)", func() error {
		_, err := cfg.GetBlockLimits(false)
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetMsgForwardPrices(masterchain)", func() error {
		_, err := cfg.GetMsgForwardPrices(true)
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetMsgForwardPrices(basechain)", func() error {
		_, err := cfg.GetMsgForwardPrices(false)
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetCatchainConfig", func() error {
		_, err := cfg.GetCatchainConfig()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetConsensusConfig", func() error {
		_, err := cfg.GetConsensusConfig()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetNewConsensusConfig", func() error {
		_, err := cfg.GetNewConsensusConfig()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetFundamentalSmartContractAddresses", func() error {
		_, err := cfg.GetFundamentalSmartContractAddresses()
		return err
	})

	testTLBBlockchainConfigMaybeAbsent(t, "GetPrevValidators", func() error {
		_, err := cfg.GetPrevValidators()
		return err
	})

	testTLBBlockchainConfigMaybeAbsent(t, "GetPrevTempValidators", func() error {
		_, err := cfg.GetPrevTempValidators()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetCurrentValidators", func() error {
		_, err := cfg.GetCurrentValidators()
		return err
	})

	testTLBBlockchainConfigMaybeAbsent(t, "GetCurrentTempValidators", func() error {
		_, err := cfg.GetCurrentTempValidators()
		return err
	})

	testTLBBlockchainConfigMaybeAbsent(t, "GetNextValidators", func() error {
		_, err := cfg.GetNextValidators()
		return err
	})

	testTLBBlockchainConfigMaybeAbsent(t, "GetNextTempValidators", func() error {
		_, err := cfg.GetNextTempValidators()
		return err
	})

	testTLBBlockchainConfigMaybeAbsent(t, "GetValidatorTempKeys", func() error {
		_, err := cfg.GetValidatorTempKeys()
		return err
	})

	testTLBBlockchainConfigMaybeAbsent(t, "GetMisbehaviourPunishmentConfig", func() error {
		_, err := cfg.GetMisbehaviourPunishmentConfig()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetSizeLimitsConfig", func() error {
		_, err := cfg.GetSizeLimitsConfig()
		return err
	})

	testTLBBlockchainConfigMaybeAbsent(t, "GetSuspendedAddressList", func() error {
		_, err := cfg.GetSuspendedAddressList()
		return err
	})

	testTLBBlockchainConfigMustGet(t, "GetPrecompiledContractsConfig", func() error {
		_, err := cfg.GetPrecompiledContractsConfig()
		return err
	})
}

func testTLBBlockchainConfigOptionalParam(id uint32) bool {
	switch id {
	case tlb.ConfigParamMinterAddress,
		tlb.ConfigParamFeeCollectorAddress,
		tlb.ConfigParamExtraCurrencyMintPrices,
		tlb.ConfigParamExtraCurrencyToMint,
		tlb.ConfigParamNewConsensusConfig,
		tlb.ConfigParamFundamentalSMCAddresses,
		tlb.ConfigParamPrevValidators,
		tlb.ConfigParamPrevTempValidators,
		tlb.ConfigParamCurrentTempValidators,
		tlb.ConfigParamNextValidators,
		tlb.ConfigParamNextTempValidators,
		tlb.ConfigParamValidatorTempKeys,
		tlb.ConfigParamMisbehaviourPunishment,
		tlb.ConfigParamSizeLimits,
		tlb.ConfigParamSuspendedAddressList,
		tlb.ConfigParamPrecompiledContracts:
		return true
	}

	return false
}

func testTLBBlockchainConfigMustGet(t *testing.T, name string, fn func() error) {
	t.Helper()

	if err := fn(); err != nil {
		t.Fatalf("%s failed: %v", name, err)
	}
}

func testTLBBlockchainConfigMaybeAbsent(t *testing.T, name string, fn func() error) {
	t.Helper()

	if err := fn(); err != nil && !errors.Is(err, tlb.ErrBlockchainConfigParamAbsent) {
		t.Fatalf("%s failed: %v", name, err)
	}
}

func Test_LSErrorCase(t *testing.T) {
	connectionPool := liteclient.NewConnectionPool()

	_ctx, cancel := context.WithTimeout(context.Background(), integrationRequestTimeout)
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

/*
func TestAccountStorage_LoadFromCell_ExtraCurrencies(t *testing.T) {
	client := liteclient.NewConnectionPool()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := client.AddConnection(ctx, "135.181.177.59:53312", "aF91CuUHuuOv9rm2W5+O/4h38M3sRm40DtSdRxQhmtQ=")
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
		if err != nil {
			t.Fatal("no proof")
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
}*/

func TestAPIClient_GetBlockProofForward(t *testing.T) {
	cfg, err := liteclient.GetConfigFromUrl(context.Background(), "https://ton-blockchain.github.io/global.config.json")
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

	addr := address.MustParseAddr("Ef8zMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzM0vF")

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
	initLT := acc.LastTxLT - 10
	log.Println(initLT)
	lastLT := initLT

	ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	ch := make(chan *tlb.Transaction)
	go api.SubscribeOnTransactions(ctx, addr, lastLT, ch)

	gotTx := false
	for tx := range ch {
		if lastLT > tx.LT {
			t.Fatal("incorrect tx order")
		}
		lastLT = tx.LT

		gotTx = true
		println(tx.Now, tx.String())
		cancel()
	}

	if !gotTx {
		t.Fatal("no transactions")
	}
}

func TestAPIClient_GetLibraries(t *testing.T) {
	_ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx := apiTestNet.Client().StickyContext(_ctx)

	addr := address.MustParseAddr("0QDSbmZlj51noKgXhUmrfcIcjJXXtLgDis2ydvx8uKKqXhHQ")

	b, err := apiTestNet.CurrentMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	acc, err := apiTestNet.WaitForBlock(b.SeqNo).GetAccount(ctx, b, addr)
	if err != nil {
		t.Fatal("get acc err:", err.Error())
		return
	}

	println(acc.Code.Dump())

	bSnake := acc.Code.MustBeginParse().MustLoadBinarySnake()

	resp, err := apiTestNet.GetLibraries(ctx, bSnake[1:], make([]byte, 32), bSnake[1:])
	if err != nil {
		t.Fatal("get libraries err:", err.Error())
		return
	}

	if len(resp) != 3 {
		t.Fatal("incorrect resp get libraries length:", len(resp))
		return
	}

	if resp[0] == nil {
		t.Fatal("first should be not empty")
	}
	if resp[1] != nil {
		t.Fatal("second should be empty", hex.EncodeToString(resp[1].Hash()))
	}
	if resp[2] == nil {
		t.Fatal("third should be not empty")
	}
}

func TestAPIClient_WithRetryTimeout(t *testing.T) {
	apiTimeout := api.WithRetryTimeout(1, 1*time.Millisecond)

	_, err := apiTimeout.GetMasterchainInfo(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("expected deadline exceeded error but", err)
	}
}

func TestAPIClient_FindLastTransactionByInMsgHash(t *testing.T) {
	addr := address.MustParseAddr("Ef8zMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzM0vF")

	block, err := api.CurrentMasterchainInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	acc, err := api.GetAccount(context.Background(), block, addr)
	if err != nil {
		t.Fatal(err)
	}

	list, err := api.ListTransactions(context.Background(), addr, 20, acc.LastTxLT, acc.LastTxHash)
	if err != nil {
		t.Fatal(err)
	}

	tx := list[len(list)-1]

	// find tx hash
	tx, err = api.FindLastTransactionByInMsgHash(context.Background(), addr, tx.IO.In.Msg.Payload().Hash(), 30)
	if err != nil {
		t.Fatal("cannot find tx:", err.Error())
	}
	t.Logf("tx hash: %s %s", hex.EncodeToString(tx.Hash), hex.EncodeToString(acc.LastTxHash))
}

func TestAPIClient_FindLastTransactionByOutMsgHash(t *testing.T) {
	addr := address.MustParseAddr("EQB3ncyBUTjZUA5EnFKR5_EnOMI9V1tTEAAPaiU71gc4TiUt")

	block, err := api.CurrentMasterchainInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	acc, err := api.GetAccount(context.Background(), block, addr)
	if err != nil {
		t.Fatal(err)
	}

	list, err := api.ListTransactions(context.Background(), addr, 20, acc.LastTxLT, acc.LastTxHash)
	if err != nil {
		t.Fatal(err)
	}

	var hash []byte
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].IO.Out == nil {
			continue
		}

		ls, err := list[i].IO.Out.ToSlice()
		if err != nil {
			continue
		}

		if len(ls) == 0 {
			continue
		}
		hash = ls[0].Msg.Payload().Hash()
	}

	if hash == nil {
		t.Fatal("no outs")
	}

	// find tx hash
	tx, err := api.FindLastTransactionByOutMsgHash(context.Background(), addr, hash, 30)
	if err != nil {
		t.Fatal("cannot find tx:", err.Error())
	}
	t.Logf("tx hash: %s %s", hex.EncodeToString(tx.Hash), hex.EncodeToString(acc.LastTxHash))
}
