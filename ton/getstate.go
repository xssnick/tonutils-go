package ton

import (
	"context"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func init() {
	tl.Register(GetAccountState{}, "liteServer.getAccountState id:tonNode.blockIdExt account:liteServer.accountId = liteServer.AccountState")
	tl.Register(AccountState{}, "liteServer.accountState id:tonNode.blockIdExt shardblk:tonNode.blockIdExt shard_proof:bytes proof:bytes state:bytes = liteServer.AccountState")
}

type AccountState struct {
	ID         *BlockIDExt `tl:"struct"`
	Shard      *BlockIDExt `tl:"struct"`
	ShardProof []byte      `tl:"bytes"`
	Proof      []byte      `tl:"bytes"`
	State      []byte      `tl:"bytes"`
}

type GetAccountState struct {
	ID      *BlockIDExt `tl:"struct"`
	Account AccountID   `tl:"struct"`
}

type AccountID struct {
	Workchain int32  `tl:"int"`
	ID        []byte `tl:"int256"`
}

func (c *APIClient) GetAccount(ctx context.Context, block *BlockIDExt, addr *address.Address) (*tlb.Account, error) {
	var resp tl.Serializable
	err := c.client.QueryLiteserver(ctx, GetAccountState{
		ID: block,
		Account: AccountID{
			Workchain: addr.Workchain(),
			ID:        addr.Data(),
		},
	}, &resp)
	if err != nil {
		return nil, err
	}

	switch t := resp.(type) {
	case AccountState:
		if len(t.State) == 0 {
			return &tlb.Account{
				IsActive: false,
			}, nil
		}

		acc := &tlb.Account{
			IsActive: true,
		}

		cls, err := cell.FromBOCMultiRoot(t.Proof)
		if err != nil {
			return nil, fmt.Errorf("failed to parse proof boc: %w", err)
		}

		bp := cls[0].BeginParse()

		merkle, err := bp.LoadRef()
		if err != nil {
			return nil, fmt.Errorf("failed to load ref ShardStateUnsplit: %w", err)
		}

		_, err = merkle.LoadRef()
		if err != nil {
			return nil, fmt.Errorf("failed to load ref ShardState: %w", err)
		}

		shardAccounts, err := merkle.LoadRef()
		if err != nil {
			return nil, fmt.Errorf("failed to load ref ShardState: %w", err)
		}
		shardAccountsDict, err := shardAccounts.LoadDict(256)

		if shardAccountsDict != nil {
			addrKey := cell.BeginCell().MustStoreSlice(addr.Data(), 256).EndCell()
			val := shardAccountsDict.Get(addrKey)
			if val == nil {
				return nil, errors.New("no addr info in proof hashmap")
			}

			loadVal := val.BeginParse()

			// skip it
			err = tlb.LoadFromCell(new(tlb.DepthBalanceInfo), loadVal)
			if err != nil {
				return nil, fmt.Errorf("failed to load DepthBalanceInfo: %w", err)
			}

			acc.LastTxHash, err = loadVal.LoadSlice(256)
			if err != nil {
				return nil, fmt.Errorf("failed to load LastTxHash: %w", err)
			}

			acc.LastTxLT, err = loadVal.LoadUInt(64)
			if err != nil {
				return nil, fmt.Errorf("failed to load LastTxLT: %w", err)
			}
		}

		stateCell, err := cell.FromBOC(t.State)
		if err != nil {
			return nil, fmt.Errorf("failed to parse state boc: %w", err)
		}

		var st tlb.AccountState
		err = st.LoadFromCell(stateCell.BeginParse())
		if err != nil {
			return nil, fmt.Errorf("failed to load account state: %w", err)
		}

		if st.Status == tlb.AccountStatusActive {
			acc.Code = st.StateInit.Code
			acc.Data = st.StateInit.Data
		}

		acc.State = &st

		return acc, nil
	case LSError:
		return nil, t
	}
	return nil, errUnexpectedResponse(resp)
}
