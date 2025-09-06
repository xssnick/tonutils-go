package main

import (
	"context"
	"fmt"
	"log"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
)

// func to get storage map key
func getShardID(shard *ton.BlockIDExt) string {
	return fmt.Sprintf("%d|%d", shard.Workchain, shard.Shard)
}

func getNotSeenShards(ctx context.Context, api ton.APIClientWrapped, shard *ton.BlockIDExt, shardLastSeqno map[string]uint32) (ret []*ton.BlockIDExt, err error) {
	if no, ok := shardLastSeqno[getShardID(shard)]; ok && no == shard.SeqNo {
		return nil, nil
	}

	b, err := api.GetBlockData(ctx, shard)
	if err != nil {
		return nil, fmt.Errorf("get block data: %w", err)
	}

	parents, err := b.BlockInfo.GetParentBlocks()
	if err != nil {
		return nil, fmt.Errorf("get parent blocks (%d:%x:%d): %w", shard.Workchain, uint64(shard.Shard), shard.Shard, err)
	}

	for _, parent := range parents {
		ext, err := getNotSeenShards(ctx, api, parent, shardLastSeqno)
		if err != nil {
			return nil, err
		}
		ret = append(ret, ext...)
	}

	ret = append(ret, shard)
	return ret, nil
}

// FYI: You can find more advanced, optimized and parallelized block scanner in payment network implementation:
// https://github.com/xssnick/ton-payment-network/blob/master/tonpayments/chain/block-scan.go

func main() {
	// ===== 1) 初始化连接与配置 =====
	client := liteclient.NewConnectionPool()

	// 从官方地址拉取 TON 全局配置（包含 lite servers、公钥、创世信任块等）
	cfg, err := liteclient.GetConfigFromUrl(context.Background(), "https://ton-blockchain.github.io/global.config.json")
	if err != nil {
		log.Fatalln("get config err: ", err.Error())
		return
	}

	// 连接主网的 lite 服务器
	err = client.AddConnectionsFromConfig(context.Background(), cfg)
	if err != nil {
		log.Fatalln("connection err: ", err.Error())
		return
	}

	// 初始化 TON 轻客户端 API 封装，并开启证明校验（这里使用 ProofCheckPolicyFast），附带重试机制
	api := ton.NewAPIClient(client, ton.ProofCheckPolicyFast).WithRetry()
	// 使用配置中的可信块作为信任锚点（trusted block）
	api.SetTrustedBlockFromConfig(cfg)

	// 说明：从配置起始块到当前主链头需要做证明校验，可能需要近一分钟
	log.Println("checking proofs since config init block, it may take near a minute...")

	// ===== 2) 获取并验证当前主链头 =====
	// 查询当前主链（masterchain）最新区块头，并从已信任块起校验证明链
	master, err := api.GetMasterchainInfo(context.Background())
	if err != nil {
		log.Fatalln("get masterchain info err: ", err.Error())
		return
	}

	// 提示：可以把当前拿到的 master（可信主链头）持久化保存，下次启动时用 api.SetTrustedBlock 加速初始化
	// TIP: you could save and store last trusted master block (master variable data)
	// for faster initialization later using api.SetTrustedBlock

	log.Println("master proofs chain successfully verified, all data is now safe and trusted!")

	// ===== 3) 固定服务节点与初始化分片游标 =====
	// 将所有请求固定在同一台 lite 服务器上以保证读取一致性；若该节点宕机，会自动切换到其它节点
	ctx := api.Client().StickyContext(context.Background())

	// 用于记录“每个分片最近一次看到的 seqno”（用于补洞）
	shardLastSeqno := map[string]uint32{}

	// 从当前 master 区块读取所有工作链与分片头信息
	// 用以初始化每个分片的“最近已见 seqno”存储
	firstShards, err := api.GetBlockShardsInfo(ctx, master)
	if err != nil {
		log.Fatalln("get shards err:", err.Error())
		return
	}
	for _, shard := range firstShards {
		shardLastSeqno[getShardID(shard)] = shard.SeqNo
	}

	// ===== 4) 主循环：按主链高度推进，逐个扫描分片块的交易 =====
	// 流程要点：
	//   (1) 在 master 高度 N 下读取分片列表
	//   (2) 发现分片 seqno 跳号则回溯补齐（补洞），形成待扫描块列表
	//   (3) 对每个块：分页拉取交易“短信息”（每批 100 条）
	//   (4) 用 (account, LT) 定位并拉取完整交易，按需过滤/入库
	//   (5) 打印结果并将 master 推进到 N+1，重复
	for {
		log.Printf("scanning %d master block...\n", master.SeqNo)

		// 读取当前 master 区块下的所有工作链与分片头
		currentShards, err := api.GetBlockShardsInfo(ctx, master)
		if err != nil {
			log.Fatalln("get shards err:", err.Error())
			return
		}

		// 说明：master 中记录的分片区块可能存在“缺号”
		//      例如出现 2756461、2756463，但没有 2756462
		//      因此需要根据上次已见的 seqno 向后回填，确保不漏块
		var newShards []*ton.BlockIDExt
		for _, shard := range currentShards {
			// 计算该分片从上次 seen 到当前的“未扫描块”列表（用于补洞）
			notSeen, err := getNotSeenShards(ctx, api, shard, shardLastSeqno)
			if err != nil {
				log.Fatalln("get not seen shards err:", err.Error())
				return
			}
			// 更新该分片的最近已见 seqno
			shardLastSeqno[getShardID(shard)] = shard.SeqNo
			// 将需要补扫的块加入待扫描列表
			newShards = append(newShards, notSeen...)
		}
		// 也把当前 master 块本身加入扫描（master 也可能包含交易）
		newShards = append(newShards, master)

		var txList []*tlb.Transaction

		// 对“待扫描的每个分片/主链块”获取交易
		for _, shard := range newShards {
			log.Printf("scanning block %d of shard %x in workchain %d...", shard.SeqNo, uint64(shard.Shard), shard.Workchain)

			var fetchedIDs []ton.TransactionShortInfo
			var after *ton.TransactionID3
			var more = true

			// 分页加载该块中的全部交易短信息，每批 100 条，直到没有更多
			for more {
				// 使用 WaitForBlock(master.SeqNo) 确保读取视图至少一致到当前 master 高度
				fetchedIDs, more, err = api.WaitForBlock(master.SeqNo).GetBlockTransactionsV2(ctx, shard, 100, after)
				if err != nil {
					log.Fatalln("get tx ids err:", err.Error())
					return
				}

				if more {
					// 设置下一次查询的偏移量（分页游标）
					after = fetchedIDs[len(fetchedIDs)-1].ID3()
				}

				// 根据短信息逐条拉取完整交易
				for _, id := range fetchedIDs {
					// 通过 (account, LT) 精确定位交易
					tx, err := api.GetTransaction(ctx, shard, address.NewAddress(0, byte(shard.Workchain), id.Account), id.LT)
					if err != nil {
						log.Fatalln("get tx data err:", err.Error())
						return
					}
					txList = append(txList, tx)
				}
			}
		}

		// 打印本轮扫描到的交易
		for i, transaction := range txList {
			log.Println(i, transaction.String())
		}

		if len(txList) == 0 {
			log.Printf("no transactions in %d block\n", master.SeqNo)
		}

		// ===== 5) 推进主链：等待并切换到下一个 master 区块（N → N+1）=====
		master, err = api.WaitForBlock(master.SeqNo+1).LookupBlock(ctx, master.Workchain, master.Shard, master.SeqNo+1)
		if err != nil {
			log.Fatalln("get masterchain info err: ", err.Error())
			return
		}
	}
}
