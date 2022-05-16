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

type Account struct {
	IsActive bool
	State    *tlb.AccountState
	Data     *cell.Cell
	Code     *cell.Cell
}

func (c *APIClient) GetAccount(ctx context.Context, block *tlb.BlockInfo, addr *address.Address) (*Account, error) {
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
			return &Account{
				IsActive: false,
			}, nil
		}

		cl, err := cell.FromBOC(state)
		if err != nil {
			return nil, err
		}

		loader := cl.BeginParse()

		contractCode, err := loader.LoadRef()
		if err != nil {
			return nil, err
		}
		contractData, err := loader.LoadRef()
		if err != nil {
			return nil, err
		}

		contractCodeCell, err := contractCode.ToCell()
		if err != nil {
			return nil, err
		}
		contractDataCell, err := contractData.ToCell()
		if err != nil {
			return nil, err
		}

		var st tlb.AccountState
		err = st.LoadFromCell(loader)
		if err != nil {
			return nil, err
		}

		return &Account{
			IsActive: true,
			State:    &st,
			Data:     contractDataCell,
			Code:     contractCodeCell,
		}, nil
	case _LSError:
		return nil, fmt.Errorf("lite server error, code %d: %s", binary.LittleEndian.Uint32(resp.Data), string(resp.Data[5:]))
	}

	return nil, errors.New("unknown response type")
}
