package ton

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func (c *APIClient) ListTransactions(ctx context.Context, addr *address.Address, num uint32, lt uint64, txHash []byte) ([]*tlb.Transaction, error) {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, num)

	chain := make([]byte, 4)
	binary.LittleEndian.PutUint32(chain, uint32(addr.Workchain()))

	data = append(data, chain...)
	data = append(data, addr.Data()...)

	ltData := make([]byte, 8)
	binary.LittleEndian.PutUint64(ltData, lt)

	data = append(data, ltData...)

	// hash
	data = append(data, txHash[:]...)

	resp, err := c.client.Do(ctx, _GetTransactions, data)
	if err != nil {
		return nil, err
	}

	switch resp.TypeID {
	case _TransactionsList:
		if len(resp.Data) <= 4 {
			return nil, errors.New("too short response")
		}

		vecLn := binary.LittleEndian.Uint32(resp.Data)
		resp.Data = resp.Data[4:]

		for i := 0; i < int(vecLn); i++ {
			var block tlb.BlockInfo

			resp.Data, err = block.Load(resp.Data)
			if err != nil {
				return nil, fmt.Errorf("failed to load block from vector: %w", err)
			}
		}

		var txData []byte
		txData, resp.Data = loadBytes(resp.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to load transaction bytes: %w", err)
		}

		txList, err := cell.FromBOCMultiRoot(txData)
		if err != nil {
			return nil, fmt.Errorf("failed to parrse cell from transaction bytes: %w", err)
		}

		res := make([]*tlb.Transaction, 0, len(txList))
		for _, txCell := range txList {
			loader := txCell.BeginParse()

			var tx tlb.Transaction
			err = tlb.LoadFromCell(&tx, loader)
			if err != nil {
				return nil, fmt.Errorf("failed to load transaction from cell: %w", err)
			}

			res = append(res, &tx)
		}

		return res, nil
	case _LSError:
		code := binary.LittleEndian.Uint32(resp.Data)
		if code == 0 {
			return nil, ErrMessageNotAccepted
		}

		return nil, LSError{
			Code: binary.LittleEndian.Uint32(resp.Data),
			Text: string(resp.Data[4:]),
		}
	}

	return nil, errors.New("unknown response type")
}

func (c *APIClient) GetTransaction(ctx context.Context, block *tlb.BlockInfo, addr *address.Address, lt uint64) (*tlb.Transaction, error) {
	data := block.Serialize()

	chain := make([]byte, 4)
	binary.LittleEndian.PutUint32(chain, uint32(addr.Workchain()))

	data = append(data, chain...)
	data = append(data, addr.Data()...)

	ltData := make([]byte, 8)
	binary.LittleEndian.PutUint64(ltData, lt)

	data = append(data, ltData...)

	resp, err := c.client.Do(ctx, _GetOneTransaction, data)
	if err != nil {
		return nil, err
	}

	switch resp.TypeID {
	case _TransactionInfo:
		b := new(tlb.BlockInfo)
		resp.Data, err = b.Load(resp.Data)
		if err != nil {
			return nil, err
		}

		var proof []byte
		proof, resp.Data = loadBytes(resp.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to load proof bytes: %w", err)
		}
		_ = proof

		var txData []byte
		txData, resp.Data = loadBytes(resp.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to load transaction bytes: %w", err)
		}

		txCell, err := cell.FromBOC(txData)
		if err != nil {
			return nil, fmt.Errorf("failed to parrse cell from transaction bytes: %w", err)
		}

		var tx tlb.Transaction
		err = tlb.LoadFromCell(&tx, txCell.BeginParse())
		if err != nil {
			return nil, fmt.Errorf("failed to load transaction from cell: %w", err)
		}

		return &tx, nil
	case _LSError:
		code := binary.LittleEndian.Uint32(resp.Data)
		if code == 0 {
			return nil, ErrMessageNotAccepted
		}

		return nil, LSError{
			Code: binary.LittleEndian.Uint32(resp.Data),
			Text: string(resp.Data[4:]),
		}
	}

	return nil, errors.New("unknown response type")
}
