package ton

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func init() {
	tl.Register(GetConfigAll{}, "liteServer.getConfigAll mode:# id:tonNode.blockIdExt = liteServer.ConfigInfo")
	tl.Register(GetConfigParams{}, "liteServer.getConfigParams mode:# id:tonNode.blockIdExt param_list:(vector int) = liteServer.ConfigInfo")
	tl.Register(ConfigAll{}, "liteServer.configInfo mode:# id:tonNode.blockIdExt state_proof:bytes config_proof:bytes = liteServer.ConfigInfo")
}

type ConfigAll struct {
	Mode        int         `tl:"int"`
	ID          *BlockIDExt `tl:"struct"`
	StateProof  []byte      `tl:"bytes"`
	ConfigProof []byte      `tl:"bytes"`
}

type GetConfigAll struct {
	Mode    int32       `tl:"int"`
	BlockID *BlockIDExt `tl:"struct"`
}

type GetConfigParams struct {
	Mode    int32       `tl:"int"`
	BlockID *BlockIDExt `tl:"struct"`
	Params  []int32     `tl:"vector int"`
}

type BlockchainConfig struct {
	data map[int32]*cell.Cell
}

func (c *APIClient) GetBlockchainConfig(ctx context.Context, block *BlockIDExt, onlyParams ...int32) (*BlockchainConfig, error) {
	var resp tl.Serializable
	var err error
	if len(onlyParams) > 0 {
		err = c.client.QueryLiteserver(ctx, GetConfigParams{
			Mode:    0,
			BlockID: block,
			Params:  onlyParams,
		}, &resp)
		if err != nil {
			return nil, err
		}
	} else {
		err = c.client.QueryLiteserver(ctx, GetConfigAll{
			Mode:    0,
			BlockID: block,
		}, &resp)
		if err != nil {
			return nil, err
		}
	}

	switch t := resp.(type) {
	case ConfigAll:
		c, err := cell.FromBOC(t.ConfigProof)
		if err != nil {
			return nil, err
		}

		ref, err := c.BeginParse().LoadRef()
		if err != nil {
			return nil, err
		}

		var state tlb.ShardStateUnsplit
		err = tlb.LoadFromCell(&state, ref)
		if err != nil {
			return nil, err
		}

		if state.McStateExtra == nil {
			return nil, errors.New("no mc extra state found, something went wrong")
		}

		var stateExtra tlb.McStateExtra
		err = tlb.LoadFromCell(&stateExtra, state.McStateExtra.BeginParse())
		if err != nil {
			return nil, fmt.Errorf("load masterchain state extra: %w", err)
		}

		result := &BlockchainConfig{data: map[int32]*cell.Cell{}}

		if len(onlyParams) > 0 {
			// we need it because lite server may add some unwanted keys
			for _, param := range onlyParams {
				res := stateExtra.ConfigParams.Config.GetByIntKey(big.NewInt(int64(param)))
				if res == nil {
					return nil, fmt.Errorf("config param %d not found", param)
				}

				v, err := res.BeginParse().LoadRef()
				if err != nil {
					return nil, fmt.Errorf("failed to load config param %d, err: %w", param, err)
				}

				result.data[param] = v.MustToCell()
			}
		} else {
			for _, kv := range stateExtra.ConfigParams.Config.All() {
				v, err := kv.Value.BeginParse().LoadRef()
				if err != nil {
					return nil, fmt.Errorf("failed to load config param %d, err: %w", kv.Key.BeginParse().MustLoadInt(32), err)
				}

				result.data[int32(kv.Key.BeginParse().MustLoadInt(32))] = v.MustToCell()
			}
		}

		return result, nil
	case LSError:
		return nil, t
	}
	return nil, errUnexpectedResponse(resp)
}

// TODO: add methods to BlockchainConfig to easily get gas price and etc

func (b *BlockchainConfig) Get(id int32) *cell.Cell {
	return b.data[id]
}

func (b *BlockchainConfig) All() map[int32]*cell.Cell {
	return b.data
}
