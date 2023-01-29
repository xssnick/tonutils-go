package ton

import (
	"context"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"math/big"
)

func init() {
	tl.Register(GetConfigAll{}, "liteServer.getConfigAll mode:# id:tonNode.blockIdExt = liteServer.ConfigInfo")
	tl.Register(GetConfigParams{}, "liteServer.getConfigParams mode:# id:tonNode.blockIdExt param_list:(vector int) = liteServer.ConfigInfo")
	tl.Register(ConfigAll{}, "liteServer.configInfo mode:# id:tonNode.blockIdExt state_proof:bytes config_proof:bytes = liteServer.ConfigInfo")
}

type ConfigAll struct {
	Mod         int            `tl:"int"`
	ID          *tlb.BlockInfo `tl:"struct"`
	StateProof  []byte         `tl:"bytes"`
	ConfigProof []byte         `tl:"bytes"`
}

type GetConfigAll struct {
	Mod     int32          `tl:"int"`
	BlockID *tlb.BlockInfo `tl:"struct"`
}

type GetConfigParams struct {
	Mod     int32          `tl:"int"`
	BlockID *tlb.BlockInfo `tl:"struct"`
	Params  []int32        `tl:"vector int"`
}

type BlockchainConfig struct {
	data map[int32]*cell.Cell
}

func (c *APIClient) GetBlockchainConfig(ctx context.Context, block *tlb.BlockInfo, onlyParams ...int32) (*BlockchainConfig, error) {
	var resp *liteclient.LiteResponse
	var err error
	if len(onlyParams) > 0 {
		resp, err = c.client.DoRequest(ctx, GetConfigParams{
			Mod:     0,
			BlockID: block,
			Params:  onlyParams,
		})
		if err != nil {
			return nil, err
		}
	} else {
		resp, err = c.client.DoRequest(ctx, GetConfigAll{
			Mod:     0,
			BlockID: block,
		})
		if err != nil {
			return nil, err
		}
	}

	switch resp.TypeID {
	case _ConfigParams:
		config := new(ConfigAll)
		_, err = tl.Parse(config, resp.Data, false)
		if err != nil {
			return nil, fmt.Errorf("failed to parse response to configAll, err: %w", err)
		}

		c, err := cell.FromBOC(config.ConfigProof)
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

		result := &BlockchainConfig{data: map[int32]*cell.Cell{}}

		if len(onlyParams) > 0 {
			// we need it because lite server adds some unwanted keys
			for _, param := range onlyParams {
				res := state.McStateExtra.ConfigParams.Config.GetByIntKey(big.NewInt(int64(param)))
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
			for _, kv := range state.McStateExtra.ConfigParams.Config.All() {
				v, err := kv.Value.BeginParse().LoadRef()
				if err != nil {
					return nil, fmt.Errorf("failed to load config param %d, err: %w", kv.Key.BeginParse().MustLoadInt(32), err)
				}

				result.data[int32(kv.Key.BeginParse().MustLoadInt(32))] = v.MustToCell()
			}
		}

		return result, nil
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

// TODO: add methods to BlockchainConfig to easily get gas price and etc

func (b *BlockchainConfig) Get(id int32) *cell.Cell {
	return b.data[id]
}

func (b *BlockchainConfig) All() map[int32]*cell.Cell {
	return b.data
}
