package ton

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/xssnick/tonutils-go/liteclient/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func (c *APIClient) GetBlockInfo(ctx context.Context) (*tlb.BlockInfo, error) {
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
		return nil, LSError{
			Code: binary.LittleEndian.Uint32(resp.Data),
			Text: string(resp.Data[4:]),
		}
	}

	return nil, errors.New("unknown response type")
}

func (c *APIClient) GetBlockTransactions(ctx context.Context, block *tlb.BlockInfo, count uint32, after ...*tlb.TransactionID) ([]*tlb.TransactionID, bool, error) {
	req := append(block.Serialize(), make([]byte, 8)...)

	mode := uint32(0b111)
	if after != nil {
		mode |= 1 << 7
	}

	binary.LittleEndian.PutUint32(req[len(req)-8:], mode)
	binary.LittleEndian.PutUint32(req[len(req)-4:], count)
	if after != nil {
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
		return nil, false, LSError{
			Code: binary.LittleEndian.Uint32(resp.Data),
			Text: string(resp.Data[4:]),
		}
	}

	return nil, false, errors.New("unknown response type")
}
