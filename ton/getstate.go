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
	tl.Register(GetAccountStatePruned{}, "liteServer.getAccountStatePrunned id:tonNode.blockIdExt account:liteServer.accountId = liteServer.AccountState")
	tl.Register(GetAccountState{}, "liteServer.getAccountState id:tonNode.blockIdExt account:liteServer.accountId = liteServer.AccountState")
	tl.Register(AccountState{}, "liteServer.accountState id:tonNode.blockIdExt shardblk:tonNode.blockIdExt shard_proof:bytes proof:bytes state:bytes = liteServer.AccountState")
}

type AccountState struct {
	ID         *BlockIDExt  `tl:"struct"`
	Shard      *BlockIDExt  `tl:"struct"`
	ShardProof []*cell.Cell `tl:"cell optional 2"`
	Proof      []*cell.Cell `tl:"cell optional 2"`
	State      *cell.Cell   `tl:"cell optional"`
}

type GetAccountStatePruned struct {
	ID      *BlockIDExt `tl:"struct"`
	Account AccountID   `tl:"struct"`
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

		if t.State == nil {
			return &tlb.Account{
				IsActive: false,
			}, nil
		}

		if t.Proof == nil {
			return nil, fmt.Errorf("no proof")
		}

		acc := &tlb.Account{
			IsActive: true,
		}

		var shardHash []byte
		if c.proofCheckPolicy != ProofCheckPolicyUnsafe && addr.Workchain() != address.MasterchainID {
			if len(t.ShardProof) == 0 {
				return nil, ErrNoProof
			}

			if t.Shard == nil || len(t.Shard.RootHash) != 32 {
				return nil, fmt.Errorf("shard block not passed")
			}

			shardHash = t.Shard.RootHash
		}

		shardAcc, balanceInfo, err := CheckAccountStateProof(addr, block, t.Proof, t.ShardProof, shardHash, c.proofCheckPolicy == ProofCheckPolicyUnsafe)
		if err != nil {
			return nil, fmt.Errorf("failed to check acc state proof: %w", err)
		}

		if !bytes.Equal(shardAcc.Account.Hash(0), t.State.Hash()) {
			return nil, fmt.Errorf("proof hash not match state account hash")
		}

		var st tlb.AccountState
		if err = st.LoadFromCell(t.State.BeginParse()); err != nil {
			return nil, fmt.Errorf("failed to load account state: %w", err)
		}

		if st.Balance.Nano().Cmp(balanceInfo.Currencies.Coins.Nano()) != 0 {
			return nil, fmt.Errorf("proof balance not match state balance")
		}

		acc.ShardBlock = t.Shard
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
