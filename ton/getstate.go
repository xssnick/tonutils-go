package ton

import (
	"bytes"
	"context"
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
		if !t.ID.Equals(block) {
			return nil, fmt.Errorf("response with incorrect master block")
		}

		if len(t.State) == 0 {
			return &tlb.Account{
				IsActive: false,
			}, nil
		}

		acc := &tlb.Account{
			IsActive: true,
		}

		proof, err := cell.FromBOCMultiRoot(t.Proof)
		if err != nil {
			return nil, fmt.Errorf("failed to parse proof boc: %w", err)
		}

		var shardProof []*cell.Cell
		var shardHash []byte
		if !c.skipProofCheck && addr.Workchain() != address.MasterchainID {
			if len(t.ShardProof) == 0 {
				return nil, ErrNoProof
			}

			shardProof, err = cell.FromBOCMultiRoot(t.ShardProof)
			if err != nil {
				return nil, fmt.Errorf("failed to parse shard proof boc: %w", err)
			}

			if t.Shard == nil || len(t.Shard.RootHash) != 32 {
				return nil, fmt.Errorf("shard block not passed")
			}

			shardHash = t.Shard.RootHash
		}

		shardAcc, balanceInfo, err := CheckAccountStateProof(addr, block, proof, shardProof, shardHash, c.skipProofCheck)
		if err != nil {
			return nil, fmt.Errorf("failed to check acc state proof: %w", err)
		}

		stateCell, err := cell.FromBOC(t.State)
		if err != nil {
			return nil, fmt.Errorf("failed to parse state boc: %w", err)
		}

		if !bytes.Equal(shardAcc.Account.Hash(0), stateCell.Hash()) {
			return nil, fmt.Errorf("proof hash not match state account hash")
		}

		var st tlb.AccountState
		if err = st.LoadFromCell(stateCell.BeginParse()); err != nil {
			return nil, fmt.Errorf("failed to load account state: %w", err)
		}

		if st.Balance.NanoTON().Cmp(balanceInfo.Currencies.Coins.NanoTON()) != 0 {
			return nil, fmt.Errorf("proof balance not match state balance")
		}

		acc.LastTxHash = shardAcc.LastTransHash
		acc.LastTxLT = shardAcc.LastTransLT

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
