package ton

import (
	"context"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/tl"
	"time"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func init() {
	tl.Register(MasterchainInfo{}, "liteServer.masterchainInfo last:tonNode.blockIdExt state_root_hash:int256 init:tonNode.zeroStateIdExt = liteServer.MasterchainInfo")
	tl.Register(BlockIDExt{}, "tonNode.blockIdExt workchain:int shard:long seqno:int root_hash:int256 file_hash:int256 = tonNode.BlockIdExt")
	tl.Register(ZeroStateIDExt{}, "tonNode.zeroStateIdExt workchain:int root_hash:int256 file_hash:int256 = tonNode.ZeroStateIdExt")
	tl.Register(GetBlockData{}, "liteServer.getBlock id:tonNode.blockIdExt = liteServer.BlockData")
	tl.Register(ListBlockTransactions{}, "liteServer.listBlockTransactions id:tonNode.blockIdExt mode:# count:# after:mode.7?liteServer.transactionId3 reverse_order:mode.6?true want_proof:mode.5?true = liteServer.BlockTransactions")
	tl.Register(GetAllShardsInfo{}, "liteServer.getAllShardsInfo id:tonNode.blockIdExt = liteServer.AllShardsInfo")
	tl.Register(GetMasterchainInf{}, "liteServer.getMasterchainInfo = liteServer.MasterchainInfo")
	tl.Register(WaitMasterchainSeqno{}, "liteServer.waitMasterchainSeqno seqno:int timeout_ms:int = Object")
	tl.Register(LookupBlock{}, "liteServer.lookupBlock mode:# id:tonNode.blockId lt:mode.1?long utime:mode.2?int = liteServer.BlockHeader")
	tl.Register(BlockInfoShort{}, "tonNode.blockId workchain:int shard:long seqno:int = tonNode.BlockId")
	tl.Register(BlockData{}, "liteServer.blockData id:tonNode.blockIdExt data:bytes = liteServer.BlockData")
	tl.Register(BlockHeader{}, "liteServer.blockHeader id:tonNode.blockIdExt mode:# header_proof:bytes = liteServer.BlockHeader")
	tl.Register(BlockTransactions{}, "liteServer.blockTransactions id:tonNode.blockIdExt req_count:# incomplete:Bool ids:(vector liteServer.transactionId) proof:bytes = liteServer.BlockTransactions")
	tl.Register(AllShardsInfo{}, "liteServer.allShardsInfo id:tonNode.blockIdExt proof:bytes data:bytes = liteServer.AllShardsInfo")
	tl.Register(Object{}, "object ? = Object")
	tl.Register(True{}, "true = True")
	tl.Register(TransactionID3{}, "liteServer.transactionId3 account:int256 lt:long = liteServer.TransactionId3")
	tl.Register(TransactionID{}, "liteServer.transactionId mode:# account:mode.0?int256 lt:mode.1?long hash:mode.2?int256 = liteServer.TransactionId")
}

type Object struct{}
type True struct{}

// TODO: will be moved here in the next version
type BlockIDExt = tlb.BlockInfo

type MasterchainInfo struct {
	Last          *BlockIDExt     `tl:"struct"`
	StateRootHash []byte          `tl:"int256"`
	Init          *ZeroStateIDExt `tl:"struct"`
}

type BlockHeader struct {
	ID          *BlockIDExt `tl:"struct"`
	Mode        uint32      `tl:"flags"`
	HeaderProof []byte      `tl:"bytes"`
}

type ZeroStateIDExt struct {
	Workchain int32  `tl:"int"`
	RootHash  []byte `tl:"int256"`
	FileHash  []byte `tl:"int256"`
}

type AllShardsInfo struct {
	ID    *BlockIDExt `tl:"struct"`
	Proof []byte      `tl:"bytes"`
	Data  []byte      `tl:"bytes"`
}

type BlockTransactions struct {
	ID             *BlockIDExt     `tl:"struct"`
	ReqCount       int32           `tl:"int"`
	Incomplete     bool            `tl:"bool"`
	TransactionIds []TransactionID `tl:"vector struct"`
	Proof          []byte          `tl:"bytes"`
}

type BlockData struct {
	ID      *BlockIDExt `tl:"struct"`
	Payload []byte      `tl:"bytes"`
}

type LookupBlock struct {
	Mode  uint32          `tl:"flags"`
	ID    *BlockInfoShort `tl:"struct"`
	LT    uint64          `tl:"?1 long"`
	UTime uint32          `tl:"?2 int"`
}

type BlockInfoShort struct {
	Workchain int32 `tl:"int"`
	Shard     int64 `tl:"long"`
	Seqno     int32 `tl:"int"`
}

type WaitMasterchainSeqno struct {
	Seqno   int32 `tl:"int"`
	Timeout int32 `tl:"int"`
}

type GetAllShardsInfo struct {
	ID *BlockIDExt `tl:"struct"`
}

type GetMasterchainInf struct{}

type ListBlockTransactions struct {
	ID           *BlockIDExt     `tl:"struct"`
	Mode         uint32          `tl:"flags"`
	Count        uint32          `tl:"int"`
	After        *TransactionID3 `tl:"?7 struct"`
	ReverseOrder *True           `tl:"?6 struct boxed"`
	WantProof    *True           `tl:"?5 struct boxed"`
}

type TransactionShortInfo struct {
	Account []byte
	LT      uint64
	Hash    []byte
}

func (t *TransactionShortInfo) ID3() *TransactionID3 {
	return &TransactionID3{
		Account: t.Account,
		LT:      t.LT,
	}
}

type TransactionID struct {
	Flags   uint32 `tl:"flags"`
	Account []byte `tl:"?0 int256"`
	LT      uint64 `tl:"?1 long"`
	Hash    []byte `tl:"?2 int256"`
}

type TransactionID3 struct {
	Account []byte `tl:"int256"`
	LT      uint64 `tl:"long"`
}

type GetBlockData struct {
	ID *BlockIDExt `tl:"struct"`
}

var ErrBlockNotFound = errors.New("block not found")
var ErrNoNewBlocks = errors.New("no new blocks in a given timeout or in 10 seconds")

func (c *APIClient) Client() LiteClient {
	return c.client
}

// CurrentMasterchainInfo - cached version of GetMasterchainInfo to not do it in parallel many times
func (c *APIClient) CurrentMasterchainInfo(ctx context.Context) (_ *BlockIDExt, err error) {
	// if not sticky - id will be 0
	nodeID := c.client.StickyNodeID(ctx)

	c.curMastersLock.RLock()
	master := c.curMasters[nodeID]
	if master == nil {
		master = &masterInfo{}
		c.curMasters[nodeID] = master
	}
	c.curMastersLock.RUnlock()

	master.mx.Lock()
	defer master.mx.Unlock()

	if time.Now().After(master.updatedAt.Add(5 * time.Second)) {
		ctx = c.client.StickyContext(ctx)

		var block *BlockIDExt
		block, err = c.GetMasterchainInfo(ctx)
		if err != nil {
			return nil, err
		}

		block, err = c.waitMasterBlock(ctx, block.SeqNo)
		if err != nil {
			return nil, err
		}

		master.updatedAt = time.Now()
		master.block = block
	}

	return master.block, nil
}

// GetMasterchainInfo - gets the latest state of master chain
func (c *APIClient) GetMasterchainInfo(ctx context.Context) (*BlockIDExt, error) {
	var resp tl.Serializable
	err := c.client.QueryLiteserver(ctx, GetMasterchainInf{}, &resp)
	if err != nil {
		return nil, err
	}

	switch t := resp.(type) {
	case MasterchainInfo:
		return t.Last, nil
	case LSError:
		return nil, t
	}
	return nil, errUnexpectedResponse(resp)
}

// LookupBlock - find block information by seqno, shard and chain
func (c *APIClient) LookupBlock(ctx context.Context, workchain int32, shard int64, seqno uint32) (*BlockIDExt, error) {
	var resp tl.Serializable
	err := c.client.QueryLiteserver(ctx, LookupBlock{
		Mode: 1,
		ID: &BlockInfoShort{
			Workchain: workchain,
			Shard:     shard,
			Seqno:     int32(seqno),
		},
	}, &resp)
	if err != nil {
		return nil, err
	}

	switch t := resp.(type) {
	case BlockHeader:
		return t.ID, nil
	case LSError:
		// 651 = block not found code
		if t.Code == 651 {
			return nil, ErrBlockNotFound
		}
		return nil, t
	}
	return nil, errUnexpectedResponse(resp)
}

// GetBlockData - get block detailed information
func (c *APIClient) GetBlockData(ctx context.Context, block *BlockIDExt) (*tlb.Block, error) {
	var resp tl.Serializable
	err := c.client.QueryLiteserver(ctx, GetBlockData{ID: block}, &resp)
	if err != nil {
		return nil, err
	}

	switch t := resp.(type) {
	case BlockData:
		cl, err := cell.FromBOC(t.Payload)
		if err != nil {
			return nil, fmt.Errorf("failed to parse block boc: %w", err)
		}

		var bData tlb.Block
		if err = tlb.LoadFromCell(&bData, cl.BeginParse()); err != nil {
			return nil, fmt.Errorf("failed to parse block data: %w", err)
		}
		return &bData, nil
	case LSError:
		return nil, t
	}
	return nil, errUnexpectedResponse(resp)
}

// GetBlockTransactions - list of block transactions
// Deprecated: Will be removed in the next release, use GetBlockTransactionsV2
func (c *APIClient) GetBlockTransactions(ctx context.Context, block *BlockIDExt, count uint32, after ...*tlb.TransactionID) ([]*tlb.TransactionID, bool, error) {
	var id3 *TransactionID3
	if len(after) > 0 && after[0] != nil {
		id3 = &TransactionID3{
			Account: after[0].AccountID,
			LT:      after[0].LT,
		}
	}

	list, more, err := c.GetBlockTransactionsV2(ctx, block, count, id3)
	if err != nil {
		return nil, false, err
	}
	oldList := make([]*tlb.TransactionID, 0, len(list))
	for _, item := range list {
		oldList = append(oldList, &tlb.TransactionID{
			LT:        item.LT,
			Hash:      item.Hash,
			AccountID: item.Account,
		})
	}
	return oldList, more, nil
}

// GetBlockTransactionsV2 - list of block transactions
func (c *APIClient) GetBlockTransactionsV2(ctx context.Context, block *BlockIDExt, count uint32, after ...*TransactionID3) ([]TransactionShortInfo, bool, error) {
	withAfter := uint32(0)
	var afterTx *TransactionID3
	if len(after) > 0 && after[0] != nil {
		afterTx = after[0]
		withAfter = 1
	}

	var resp tl.Serializable
	err := c.client.QueryLiteserver(ctx, ListBlockTransactions{
		Mode:  0b111 | (withAfter << 7),
		ID:    block,
		Count: count,
		After: afterTx,
	}, &resp)
	if err != nil {
		return nil, false, err
	}

	switch t := resp.(type) {
	case BlockTransactions:
		txIds := make([]TransactionShortInfo, 0, len(t.TransactionIds))
		for _, id := range t.TransactionIds {
			if id.LT == 0 || id.Hash == nil || id.Account == nil {
				return nil, false, fmt.Errorf("invalid ls response, fields are nil")
			}
			txIds = append(txIds, TransactionShortInfo{
				Account: id.Account,
				LT:      id.LT,
				Hash:    id.Hash,
			})
		}
		return txIds, t.Incomplete, nil
	case LSError:
		return nil, false, t
	}
	return nil, false, errUnexpectedResponse(resp)
}

// GetBlockShardsInfo - gets the information about workchains and its shards at given masterchain state
func (c *APIClient) GetBlockShardsInfo(ctx context.Context, master *BlockIDExt) ([]*BlockIDExt, error) {
	var resp tl.Serializable
	err := c.client.QueryLiteserver(ctx, GetAllShardsInfo{ID: master}, &resp)
	if err != nil {
		return nil, err
	}

	switch t := resp.(type) {
	case AllShardsInfo:
		c, err := cell.FromBOC(t.Data)
		if err != nil {
			return nil, err
		}

		var inf tlb.AllShardsInfo
		err = tlb.LoadFromCell(&inf, c.BeginParse())
		if err != nil {
			return nil, err
		}

		var shards []*BlockIDExt

		for _, kv := range inf.ShardHashes.All() {
			workchain, err := kv.Key.BeginParse().LoadInt(32)
			if err != nil {
				return nil, fmt.Errorf("load workchain err: %w", err)
			}

			var binTree tlb.BinTree
			err = binTree.LoadFromCell(kv.Value.BeginParse().MustLoadRef())
			if err != nil {
				return nil, fmt.Errorf("load BinTree err: %w", err)
			}

			for _, bk := range binTree.All() {
				var shardDesc tlb.ShardDesc
				if err = tlb.LoadFromCell(&shardDesc, bk.Value.BeginParse()); err != nil {
					return nil, fmt.Errorf("load ShardDesc err: %w", err)
				}

				shards = append(shards, &BlockIDExt{
					Workchain: int32(workchain),
					Shard:     shardDesc.NextValidatorShard,
					SeqNo:     shardDesc.SeqNo,
					RootHash:  shardDesc.RootHash,
					FileHash:  shardDesc.FileHash,
				})
			}
		}

		return shards, nil
	case LSError:
		return nil, t
	}
	return nil, errUnexpectedResponse(resp)
}

// WaitNextMasterBlock - wait for the next block of master chain
func (c *APIClient) waitMasterBlock(ctx context.Context, seqno uint32) (*BlockIDExt, error) {
	var timeout = 10 * time.Second

	deadline, ok := ctx.Deadline()
	if ok {
		t := deadline.Sub(time.Now())
		if t < timeout {
			timeout = t
		}
	}

	prefix, err := tl.Serialize(WaitMasterchainSeqno{
		Seqno:   int32(seqno),
		Timeout: int32(timeout / time.Millisecond),
	}, true)
	if err != nil {
		return nil, err
	}

	suffix, err := tl.Serialize(GetMasterchainInf{}, true)
	if err != nil {
		return nil, err
	}

	var resp tl.Serializable
	err = c.client.QueryLiteserver(ctx, tl.Raw(append(prefix, suffix...)), &resp)
	if err != nil {
		return nil, err
	}

	switch t := resp.(type) {
	case MasterchainInfo:
		return t.Last, nil
	case LSError:
		if t.Code == 652 {
			return nil, ErrNoNewBlocks
		}
		return nil, t
	}
	return nil, errUnexpectedResponse(resp)
}

func (c *APIClient) WaitNextMasterBlock(ctx context.Context, master *BlockIDExt) (*BlockIDExt, error) {
	if master.Workchain != -1 {
		return nil, errors.New("not a master block passed")
	}

	ctx = c.client.StickyContext(ctx)

	m, err := c.waitMasterBlock(ctx, master.SeqNo+1)
	if err != nil {
		return nil, err
	}

	return m, nil
}
