package ton

import (
	"context"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/tl"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func init() {
	tl.Register(GetOneTransaction{}, "liteServer.getOneTransaction id:tonNode.blockIdExt account:liteServer.accountId lt:long = liteServer.TransactionInfo")
	tl.Register(GetTransactions{}, "liteServer.getTransactions count:# account:liteServer.accountId lt:long hash:int256 = liteServer.TransactionList")
	tl.Register(TransactionList{}, "liteServer.transactionList ids:(vector tonNode.blockIdExt) transactions:bytes = liteServer.TransactionList")
	tl.Register(TransactionInfo{}, "liteServer.transactionInfo id:tonNode.blockIdExt proof:bytes transaction:bytes = liteServer.TransactionInfo")
}

type TransactionInfo struct {
	ID          *tlb.BlockInfo `tl:"struct"`
	Proof       []byte         `tl:"bytes"`
	Transaction []byte         `tl:"bytes"`
}

type TransactionList struct {
	IDs          []*tlb.BlockInfo `tl:"vector struct"`
	Transactions []byte           `tl:"bytes"`
}

type GetOneTransaction struct {
	ID    *tlb.BlockInfo `tl:"struct"`
	AccID *AccountID     `tl:"struct"`
	LT    int64          `tl:"long"`
}

type GetTransactions struct {
	Limit  int32      `tl:"int"`
	AccID  *AccountID `tl:"struct"`
	LT     int64      `tl:"long"`
	TxHash []byte     `tl:"int256"`
}

// ListTransactions - returns list of transactions before (including) passed lt and hash, the oldest one is first in result slice
func (c *APIClient) ListTransactions(ctx context.Context, addr *address.Address, limit uint32, lt uint64, txHash []byte) ([]*tlb.Transaction, error) {
	resp, err := c.client.DoRequest(ctx, GetTransactions{
		Limit: int32(limit),
		AccID: &AccountID{
			WorkChain: addr.Workchain(),
			ID:        addr.Data(),
		},
		LT:     int64(lt),
		TxHash: txHash,
	})
	if err != nil {
		return nil, err
	}

	switch resp.TypeID {
	case _TransactionsList:
		if len(resp.Data) <= 4 {
			return nil, errors.New("too short response")
		}

		txs := new(TransactionList)
		_, err = tl.Parse(txs, resp.Data, false)
		if err != nil {
			return nil, fmt.Errorf("failed to parse response to transactionList, err: %w", err)
		}

		txList, err := cell.FromBOCMultiRoot(txs.Transactions)
		if err != nil {
			return nil, fmt.Errorf("failed to parse cell from transaction bytes: %w", err)
		}

		res := make([]*tlb.Transaction, 0, len(txList))
		for _, txCell := range txList {
			loader := txCell.BeginParse()

			var tx tlb.Transaction
			err = tlb.LoadFromCell(&tx, loader)
			if err != nil {
				return nil, fmt.Errorf("failed to load transaction from cell: %w", err)
			}
			tx.Hash = txCell.Hash()

			res = append(res, &tx)
		}

		return res, nil
	case _LSError:
		lsErr := new(LSError)
		_, err = tl.Parse(lsErr, resp.Data, false)
		if err != nil {
			return nil, fmt.Errorf("failed to parse error, err: %w", err)
		}

		if lsErr.Code == 0 {
			return nil, ErrMessageNotAccepted
		}

		return nil, lsErr
	}

	return nil, errors.New("unknown response type")
}

func (c *APIClient) GetTransaction(ctx context.Context, block *tlb.BlockInfo, addr *address.Address, lt uint64) (*tlb.Transaction, error) {
	resp, err := c.client.DoRequest(ctx, GetOneTransaction{
		ID: block,
		AccID: &AccountID{
			WorkChain: addr.Workchain(),
			ID:        addr.Data(),
		},
		LT: int64(lt),
	})
	if err != nil {
		return nil, err
	}

	switch resp.TypeID {
	case _TransactionInfo:
		txInfo := new(TransactionInfo)
		_, err = tl.Parse(txInfo, resp.Data, false)
		if err != nil {
			return nil, fmt.Errorf("failed to parse respons to TransactionInfo, err: %w", err)
		}

		txCell, err := cell.FromBOC(txInfo.Transaction)
		if err != nil {
			return nil, fmt.Errorf("failed to parrse cell from transaction bytes: %w", err)
		}

		var tx tlb.Transaction
		err = tlb.LoadFromCell(&tx, txCell.BeginParse())
		if err != nil {
			return nil, fmt.Errorf("failed to load transaction from cell: %w", err)
		}
		tx.Hash = txCell.Hash()

		return &tx, nil
	case _LSError:
		lsErr := new(LSError)
		_, err = tl.Parse(lsErr, resp.Data, false)
		if err != nil {
			return nil, fmt.Errorf("failed to parse error, err: %w", err)
		}

		if lsErr.Code == 0 {
			return nil, ErrMessageNotAccepted
		}

		return nil, lsErr
	}

	return nil, errors.New("unknown response type")
}
