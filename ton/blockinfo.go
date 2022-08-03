package ton

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

var ErrBlockNotFound = errors.New("block not found")

// CurrentMasterchainInfo - cached version of GetMasterchainInfo to not do it in parallel many times
func (c *APIClient) CurrentMasterchainInfo(ctx context.Context) (_ *tlb.BlockInfo, err error) {
	c.curMasterLock.RLock()
	master := c.curMaster
	tm := c.curMasterUpdateTime
	c.curMasterLock.RUnlock()

	if master == nil || time.Now().After(tm.Add(3*time.Second)) {
		c.curMasterLock.Lock()
		defer c.curMasterLock.Unlock()

		// update values to latest in case update happen between previous check
		master = c.curMaster
		tm = c.curMasterUpdateTime

		// second check to avoid concurrent update
		if master == nil || time.Now().After(tm.Add(3*time.Second)) {
			master, err = c.GetMasterchainInfo(ctx)
			if err != nil {
				return nil, err
			}

			for {
				// we should check if block is already accessible due to interesting LS behavior, if not - we will wait for it.
				_, err := c.LookupBlock(ctx, master.Workchain, master.Shard, master.SeqNo)
				if err != nil {
					if err == ErrBlockNotFound {
						time.Sleep(200 * time.Millisecond)
						continue
					}
					return nil, err
				}
				break
			}

			c.curMasterUpdateTime = time.Now()
			c.curMaster = master
		}
	}

	return master, nil
}

// GetMasterchainInfo - gets the latest state of master chain
func (c *APIClient) GetMasterchainInfo(ctx context.Context) (*tlb.BlockInfo, error) {
	resp, err := c.client.Do(ctx, _GetMasterchainInfo, nil)
	if err != nil {
		return nil, err
	}

	block := new(tlb.BlockInfo)
	_, err = block.Load(resp.Data)
	if err != nil {
		return nil, err
	}

	return block, nil
}

// LookupBlock - find block information by seqno, shard and chain
func (c *APIClient) LookupBlock(ctx context.Context, workchain int32, shard int64, seqno uint32) (*tlb.BlockInfo, error) {
	data := make([]byte, 20)
	binary.LittleEndian.PutUint32(data, 1)
	binary.LittleEndian.PutUint32(data[4:], uint32(workchain))
	binary.LittleEndian.PutUint64(data[8:], uint64(shard))
	binary.LittleEndian.PutUint32(data[16:], seqno)

	resp, err := c.client.Do(ctx, _LookupBlock, data)
	if err != nil {
		return nil, err
	}

	switch resp.TypeID {
	case _BlockHeader:
		b := new(tlb.BlockInfo)
		resp.Data, err = b.Load(resp.Data)
		if err != nil {
			return nil, err
		}

		return b, nil
	case _LSError:
		var lsErr LSError
		resp.Data, err = lsErr.Load(resp.Data)
		if err != nil {
			return nil, err
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
	resp, err := c.client.Do(ctx, _GetBlock, block.Serialize())
	if err != nil {
		return nil, err
	}

	switch resp.TypeID {
	case _BlockData:
		b := new(tlb.BlockInfo)
		resp.Data, err = b.Load(resp.Data)
		if err != nil {
			return nil, err
		}

		var payload []byte
		payload, resp.Data = loadBytes(resp.Data)

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
		var lsErr LSError
		resp.Data, err = lsErr.Load(resp.Data)
		if err != nil {
			return nil, err
		}
		return nil, lsErr
	}

	return nil, errors.New("unknown response type")
}

// GetBlockTransactions - list of block transactions
func (c *APIClient) GetBlockTransactions(ctx context.Context, block *tlb.BlockInfo, count uint32, after ...*tlb.TransactionID) ([]*tlb.TransactionID, bool, error) {
	req := append(block.Serialize(), make([]byte, 8)...)

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
		resp.Data, err = b.Load(resp.Data)
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
		proof, resp.Data = loadBytes(resp.Data)
		_ = proof

		return txList, incomplete, nil
	case _LSError:
		var lsErr LSError
		resp.Data, err = lsErr.Load(resp.Data)
		if err != nil {
			return nil, false, err
		}
		return nil, false, lsErr
	}

	return nil, false, errors.New("unknown response type")
}

// GetBlockShardsInfo - gets the information about workchains and its shards at given masterchain state
func (c *APIClient) GetBlockShardsInfo(ctx context.Context, master *tlb.BlockInfo) ([]*tlb.BlockInfo, error) {
	resp, err := c.client.Do(ctx, _GetAllShardsInfo, master.Serialize())
	if err != nil {
		return nil, err
	}

	switch resp.TypeID {
	case _AllShardsInfo:
		b := new(tlb.BlockInfo)
		resp.Data, err = b.Load(resp.Data)
		if err != nil {
			return nil, err
		}

		var proof []byte
		proof, resp.Data = loadBytes(resp.Data)
		_ = proof

		var data []byte
		data, resp.Data = loadBytes(resp.Data)

		c, err := cell.FromBOC(data)
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
		var lsErr LSError
		resp.Data, err = lsErr.Load(resp.Data)
		if err != nil {
			return nil, err
		}
		return nil, lsErr
	}

	return nil, errors.New("unknown response type")
}
