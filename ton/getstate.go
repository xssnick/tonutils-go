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
	tl.Register(GetAccountState{}, "liteServer.getAccountState id:tonNode.blockIdExt account:liteServer.accountId = liteServer.AccountState")
	tl.Register(AccountState{}, "liteServer.accountState id:tonNode.blockIdExt shardblk:tonNode.blockIdExt shard_proof:bytes proof:bytes state:bytes = liteServer.AccountState")
}

type AccountState struct {
	ID         *tlb.BlockInfo `tl:"struct"`
	Shard      *tlb.BlockInfo `tl:"struct"`
	ShardProof []byte         `tl:"bytes"`
	Proof      []byte         `tl:"bytes"`
	State      []byte         `tl:"bytes"`
}

type GetAccountState struct {
	ID    *tlb.BlockInfo `tl:"struct"`
	AccID *AccountID     `tl:"struct"`
}

type AccountID struct {
	WorkChain int32  `tl:"int"`
	ID        []byte `tl:"int256"`
}

func (c *APIClient) GetAccount(ctx context.Context, block *tlb.BlockInfo, addr *address.Address) (*tlb.Account, error) {
	resp, err := c.client.DoRequest(ctx, GetAccountState{
		ID: block,
		AccID: &AccountID{
			WorkChain: addr.Workchain(),
			ID:        addr.Data(),
		},
	})
	if err != nil {
		return nil, err
	}

	switch resp.TypeID {
	case _AccountState:
		accState := new(AccountState)
		_, err = tl.Parse(accState, resp.Data, false)
		if err != nil {
			return nil, fmt.Errorf("failed to parse response to AccountState, err: %w", err)
		}

		if len(accState.State) == 0 {
			return &tlb.Account{
				IsActive: false,
			}, nil
		}

		acc := &tlb.Account{
			IsActive: true,
		}

		cls, err := cell.FromBOCMultiRoot(accState.Proof)
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

		stateCell, err := cell.FromBOC(accState.State)
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
