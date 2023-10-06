package ton

import (
	"context"
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func init() {
	tl.Register(GetConfigAll{}, "liteServer.getConfigAll mode:# id:tonNode.blockIdExt = liteServer.ConfigInfo")
	tl.Register(GetConfigParams{}, "liteServer.getConfigParams mode:# id:tonNode.blockIdExt param_list:(vector int) = liteServer.ConfigInfo")
	tl.Register(ConfigAll{}, "liteServer.configInfo mode:# id:tonNode.blockIdExt state_proof:bytes config_proof:bytes = liteServer.ConfigInfo")

	tl.Register(GetLibraries{}, "liteServer.getLibraries library_list:(vector int256) = liteServer.LibraryResult")
	tl.Register(LibraryEntry{}, "liteServer.libraryEntry hash:int256 data:bytes = liteServer.LibraryEntry")
	tl.Register(LibraryResult{}, "liteServer.libraryResult result:(vector liteServer.libraryEntry) = liteServer.LibraryResult")
}

type GetLibraries struct {
	LibraryList [][]byte `tl:"vector int256"`
}

type LibraryEntry struct {
	Hash []byte `tl:"int256"`
	Data []byte `tl:"bytes"`
}

type LibraryResult struct {
	Result []*LibraryEntry `tl:"vector struct"`
}

type ConfigAll struct {
	Mode        int         `tl:"int"`
	ID          *BlockIDExt `tl:"struct"`
	StateProof  *cell.Cell  `tl:"cell"`
	ConfigProof *cell.Cell  `tl:"cell"`
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

func (c *APIClient) GetLibraries(ctx context.Context, list ...[]byte) ([]*cell.Cell, error) {
	var (
		resp tl.Serializable
		err  error
	)

	if err = c.client.QueryLiteserver(ctx, GetLibraries{LibraryList: list}, &resp); err != nil {
		return nil, err
	}

	switch t := resp.(type) {
	case LibraryResult:
		libList := make([]*cell.Cell, 0)

		for _, t := range t.Result {
			libList = append(libList, cell.BeginCell().MustStoreBinarySnake(t.Data).EndCell())
		}

		return libList, err
	case LSError:
		return nil, t
	}

	return nil, errUnexpectedResponse(resp)
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
		stateExtra, err := CheckShardMcStateExtraProof(block, []*cell.Cell{t.ConfigProof, t.StateProof})
		if err != nil {
			return nil, fmt.Errorf("incorrect proof: %w", err)
		}

		result := &BlockchainConfig{data: map[int32]*cell.Cell{}}

		if len(onlyParams) > 0 {
			// we need it because lite server may add some unwanted keys
			for _, param := range onlyParams {
				res := stateExtra.ConfigParams.Config.Params.GetByIntKey(big.NewInt(int64(param)))
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
			for _, kv := range stateExtra.ConfigParams.Config.Params.All() {
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
