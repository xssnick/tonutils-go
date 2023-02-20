package ton

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/tl"
	"time"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func init() {
	tl.Register(GetBlockData{}, "liteServer.getBlock id:tonNode.blockIdExt = liteServer.BlockData")
	tl.Register(BlockTransactionsList{}, "liteServer.listBlockTransactions id:tonNode.blockIdExt mode:# count:# after:mode.7?liteServer.transactionId3 reverse_order:mode.6?true want_proof:mode.5?true = liteServer.BlockTransactions")
	tl.Register(GetAllShardsInfo{}, "liteServer.getAllShardsInfo id:tonNode.blockIdExt = liteServer.AllShardsInfo")
	tl.Register(GetMasterchainInf{}, "liteServer.getMasterchainInfo = liteServer.MasterchainInfo")
	tl.Register(WaitMasterChainSeqno{}, "liteServer.waitMasterchainSeqno seqno:int timeout_ms:int = Object")
	tl.Register(LookupBlock{}, "liteServer.lookupBlock mode:# id:tonNode.blockId lt:mode.1?long utime:mode.2?int = liteServer.BlockHeader")
	tl.Register(BlockInfoShort{}, "tonNode.blockId workchain:int shard:long seqno:int = tonNode.BlockId")
	tl.Register(BlockData{}, "liteServer.blockData id:tonNode.blockIdExt data:bytes = liteServer.BlockData")
	tl.Register(BlockTransactions{}, "liteServer.blockTransactions id:tonNode.blockIdExt req_count:# incomplete:Bool ids:(vector liteServer.transactionId) proof:bytes = liteServer.BlockTransactions")
	tl.Register(AllShardsInfo{}, "liteServer.allShardsInfo id:tonNode.blockIdExt proof:bytes data:bytes = liteServer.AllShardsInfo")
}

type AllShardsInfo struct {
	ID    *tlb.BlockInfo `tl:"struct"`
	Proof []byte         `tl:"bytes"`
	Data  []byte         `tl:"bytes"`
}

type BlockTransactions struct {
	ID             *tlb.BlockInfo      `tl:"struct"`
	ReqCount       int32               `tl:"int"`
	Incomplete     bool                `tl:"Bool"`
	TransactionIds []tlb.TransactionID `tl:"vector struct"`
	Proof          []byte              `tl:"bytes"`
}

type BlockData struct {
	ID      *tlb.BlockInfo `tl:"struct"`
	Payload []byte         `tl:"bytes"`
}

type LookupBlock struct {
	Mod int32           `tl:"int"`
	ID  *BlockInfoShort `tl:"struct"`
}

type BlockInfoShort struct {
	Workchain int32 `tl:"int"`
	Shard     int64 `tl:"long"`
	Seqno     int32 `tl:"int"`
}

type WaitMasterChainSeqno struct {
	Seqno   int32 `tl:"int"`
	TimeOut int32 `tl:"int"`
}

type GetAllShardsInfo struct {
	ID *tlb.BlockInfo `tl:"struct"`
}

type GetMasterchainInf struct{}

type BlockTransactionsList struct {
	ID    *tlb.BlockInfo     `tl:"struct"`
	Mode  int32              `tl:"int"`
	Count int32              `tl:"int"`
	After *tlb.TransactionID `tl:"struct"`
}

type GetBlockData struct {
	ID *tlb.BlockInfo `tl:"struct"`
}

var ErrBlockNotFound = errors.New("block not found")
var ErrNoNewBlocks = errors.New("no new blocks in a given timeout or in 10 seconds")

func (c *APIClient) Client() LiteClient {
	return c.client
}

// CurrentMasterchainInfo - cached version of GetMasterchainInfo to not do it in parallel many times
func (c *APIClient) CurrentMasterchainInfo(ctx context.Context) (_ *tlb.BlockInfo, err error) {
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

		var block *tlb.BlockInfo
		block, err = c.GetMasterchainInfo(ctx)
		if err != nil {
			return nil, err
		}

		err = c.waitMasterBlock(ctx, uint32(block.SeqNo))
		if err != nil {
			return nil, err
		}

		master.updatedAt = time.Now()
		master.block = block
	}

	return master.block, nil
}

// GetMasterchainInfo - gets the latest state of master chain
func (c *APIClient) GetMasterchainInfo(ctx context.Context) (*tlb.BlockInfo, error) {
	resp, err := c.client.DoRequest(ctx, GetMasterchainInf{})
	if err != nil {
		return nil, err
	}

	block := new(tlb.BlockInfo)
	_, err = tl.Parse(block, resp.Data, false)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response to blockInfo, err: %w", err)
	}

	return block, nil
}

// LookupBlock - find block information by seqno, shard and chain
func (c *APIClient) LookupBlock(ctx context.Context, workchain int32, shard int64, seqno uint32) (*tlb.BlockInfo, error) {
	resp, err := c.client.DoRequest(ctx, LookupBlock{
		Mod: 1,
		ID: &BlockInfoShort{
			Workchain: workchain,
			Shard:     shard,
			Seqno:     int32(seqno),
		},
	})
	if err != nil {
		return nil, err
	}

	switch resp.TypeID {
	case _BlockHeader:
		b := new(tlb.BlockInfo)
		_, err = tl.Parse(b, resp.Data, false)
		if err != nil {
			return nil, fmt.Errorf("failed to parse response to blockInfo, err: %w", err)
		}

		return b, nil
	case _LSError:
		lsErr := new(LSError)
		_, err = tl.Parse(lsErr, resp.Data, false)
		if err != nil {
			return nil, fmt.Errorf("failed to parse error, err: %w", err)
		}

		// 651 = block not found code
		if lsErr.Code == 651 {
			return nil, ErrBlockNotFound
		}

		return nil, lsErr
	}

	return nil, errors.New("unknown response type")
}

// GetBlockData - get block detailed information
func (c *APIClient) GetBlockData(ctx context.Context, block *tlb.BlockInfo) (*tlb.Block, error) {
	resp, err := c.client.DoRequest(ctx, GetBlockData{ID: block})
	if err != nil {
		return nil, err
	}

	switch resp.TypeID {
	case _BlockData:
		b := new(tlb.BlockInfo)
		resp.Data, err = tl.Parse(b, resp.Data, false)
		if err != nil {
			return nil, fmt.Errorf("failed to parse response in BlockInfo, err: %w", err)
		}

		var payload []byte
		payload, resp.Data, err = tl.FromBytes(resp.Data)
		if err != nil {
			return nil, err
		}

		cl, err := cell.FromBOC(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to parse block boc: %w", err)
		}

		var bData tlb.Block
		if err = tlb.LoadFromCell(&bData, cl.BeginParse()); err != nil {
			return nil, fmt.Errorf("failed to parse block data: %w", err)
		}

		return &bData, nil
	case _LSError:
		lsErr := new(LSError)
		_, err = tl.Parse(lsErr, resp.Data, false)
		if err != nil {
			return nil, fmt.Errorf("failed to parse error, err: %w", err)
		}
		return nil, lsErr
	}

	return nil, errors.New("unknown response type")
}

// GetBlockTransactions - list of block transactions
func (c *APIClient) GetBlockTransactions(ctx context.Context, block *tlb.BlockInfo, count uint32, after ...*tlb.TransactionID) ([]*tlb.TransactionID, bool, error) {
	blockData, err := tl.Serialize(block, false)
	if err != nil {
		return nil, false, err
	}
	req := append(blockData, make([]byte, 8)...)

	mode := uint32(0b111)
	if after != nil && after[0] != nil {
		mode |= 1 << 7
	}

	binary.LittleEndian.PutUint32(req[len(req)-8:], mode)
	binary.LittleEndian.PutUint32(req[len(req)-4:], count)
	if len(after) > 0 && after[0] != nil {
		req = append(req, after[0].AccountID...)

		ltBts := make([]byte, 8)
		binary.LittleEndian.PutUint64(ltBts, after[0].LT)
		req = append(req, ltBts...)
	}

	resp, err := c.client.Do(ctx, _ListBlockTransactions, req)
	if err != nil {
		return nil, false, err
	}

	switch resp.TypeID {
	case _BlockTransactions:
		b := new(tlb.BlockInfo)
		resp.Data, err = tl.Parse(b, resp.Data, false)
		if err != nil {
			return nil, false, err
		}

		_ = binary.LittleEndian.Uint32(resp.Data)
		resp.Data = resp.Data[4:]

		incomplete := int32(binary.LittleEndian.Uint32(resp.Data)) == _BoolTrue
		resp.Data = resp.Data[4:]

		vecLn := binary.LittleEndian.Uint32(resp.Data)
		resp.Data = resp.Data[4:]

		txList := make([]*tlb.TransactionID, vecLn)
		for i := 0; i < int(vecLn); i++ {
			mode := binary.LittleEndian.Uint32(resp.Data)
			resp.Data = resp.Data[4:]

			tid := &tlb.TransactionID{}

			if mode&0b1 != 0 {
				tid.AccountID = resp.Data[:32]
				resp.Data = resp.Data[32:]
			}

			if mode&0b10 != 0 {
				tid.LT = binary.LittleEndian.Uint64(resp.Data)
				resp.Data = resp.Data[8:]
			}

			if mode&0b100 != 0 {
				tid.Hash = resp.Data[:32]
				resp.Data = resp.Data[32:]
			}

			txList[i] = tid
		}

		var proof []byte
		proof, resp.Data, err = tl.FromBytes(resp.Data)
		if err != nil {
			return nil, false, err
		}
		_ = proof

		return txList, incomplete, nil
	case _LSError:
		lsErr := new(LSError)
		_, err = tl.Parse(lsErr, resp.Data, false)
		if err != nil {
			return nil, false, fmt.Errorf("failed to parse error, err: %w", err)
		}
		return nil, false, lsErr
	}

	return nil, false, errors.New("unknown response type")
}

// GetBlockShardsInfo - gets the information about workchains and its shards at given masterchain state
func (c *APIClient) GetBlockShardsInfo(ctx context.Context, master *tlb.BlockInfo) ([]*tlb.BlockInfo, error) {
	resp, err := c.client.DoRequest(ctx, GetAllShardsInfo{ID: master})
	if err != nil {
		return nil, err
	}

	switch resp.TypeID {
	case _AllShardsInfo:
		shardsInfo := new(AllShardsInfo)
		_, err := tl.Parse(shardsInfo, resp.Data, false)
		if err != nil {
			return nil, fmt.Errorf("failed to parse response to allShrdsInfo, err: %w", err)
		}

		c, err := cell.FromBOC(shardsInfo.Data)
		if err != nil {
			return nil, err
		}

		var inf tlb.AllShardsInfo
		err = tlb.LoadFromCell(&inf, c.BeginParse())
		if err != nil {
			return nil, err
		}

		var shards []*tlb.BlockInfo

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

				shards = append(shards, &tlb.BlockInfo{
					Workchain: int32(workchain),
					Shard:     shardDesc.NextValidatorShard,
					SeqNo:     shardDesc.SeqNo,
					RootHash:  shardDesc.RootHash,
					FileHash:  shardDesc.FileHash,
				})
			}
		}

		return shards, nil
	case _LSError:
		lsErr := new(LSError)
		_, err = tl.Parse(lsErr, resp.Data, false)
		if err != nil {
			return nil, fmt.Errorf("failed to parse error, err: %w", err)
		}
		return nil, lsErr
	}

	return nil, errors.New("unknown response type")
}

// WaitNextMasterBlock - wait for the next block of master chain
func (c *APIClient) waitMasterBlock(ctx context.Context, seqno uint32) error {
	var timeout = 10 * time.Second

	deadline, ok := ctx.Deadline()
	if ok {
		t := deadline.Sub(time.Now())
		if t < timeout {
			timeout = t
		}
	}

	_, err := c.client.DoRequest(ctx, WaitMasterChainSeqno{
		Seqno:   int32(seqno),
		TimeOut: int32(timeout / time.Millisecond),
	})
	if err != nil {
		return err
	}

	return nil
}

func (c *APIClient) WaitNextMasterBlock(ctx context.Context, master *tlb.BlockInfo) (*tlb.BlockInfo, error) {
	if master.Workchain != -1 {
		return nil, errors.New("not a master block passed")
	}

	ctx = c.client.StickyContext(ctx)

	err := c.waitMasterBlock(ctx, uint32(master.SeqNo+1))
	if err != nil {
		return nil, err
	}

	m, err := c.GetMasterchainInfo(ctx)
	if err != nil {
		return nil, err
	}

	if master.SeqNo == m.SeqNo {
		return nil, ErrNoNewBlocks
	}

	return m, nil
}
