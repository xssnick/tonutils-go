package ton

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func (c *APIClient) GetAccount(ctx context.Context, block *tlb.BlockInfo, addr *address.Address) (*tlb.Account, error) {
	data := block.Serialize()

	chain := make([]byte, 4)
	binary.LittleEndian.PutUint32(chain, uint32(addr.Workchain()))

	data = append(data, chain...)
	data = append(data, addr.Data()...)

	resp, err := c.client.Do(ctx, _GetAccountState, data)
	if err != nil {
		return nil, err
	}

	switch resp.TypeID {
	case _AccountState:

		b := new(tlb.BlockInfo)
		resp.Data, err = b.Load(resp.Data)
		if err != nil {
			return nil, err
		}

		shard := new(tlb.BlockInfo)
		resp.Data, err = shard.Load(resp.Data)
		if err != nil {
			return nil, err
		}

		var shardProof []byte
		shardProof, resp.Data = loadBytes(resp.Data)
		_ = shardProof

		var proof []byte
		proof, resp.Data = loadBytes(resp.Data)
		_ = proof

		var state []byte
		state, resp.Data = loadBytes(resp.Data)

		if len(state) == 0 {
			return &tlb.Account{
				IsActive: false,
			}, nil
		}

		acc := &tlb.Account{
			IsActive: true,
		}

		cls, err := cell.FromBOCMultiRoot(proof)
		if err != nil {
			return nil, fmt.Errorf("failed to parse proof boc: %w", err)
		}

		bp := cls[0].BeginParse()

		// ShardStateUnsplit
		ssuRef, err := bp.LoadRef()
		if err != nil {
			return nil, fmt.Errorf("failed to load ref ShardStateUnsplit: %w", err)
		}

		var shardState tlb.ShardStateUnsplit
		err = tlb.LoadFromCell(&shardState, ssuRef)
		if err != nil {
			return nil, fmt.Errorf("failed to load ref ShardState: %w", err)
		}

		if shardState.Accounts.ShardAccounts != nil {
			addrKey := cell.BeginCell().MustStoreSlice(addr.Data(), 256).EndCell()
			val := shardState.Accounts.ShardAccounts.Get(addrKey)
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

		stateCell, err := cell.FromBOC(state)
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
