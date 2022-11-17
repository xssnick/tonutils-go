package ton

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"math/big"
)

type BlockchainConfig struct {
	data map[int32]*cell.Cell
}

func (c *APIClient) GetBlockchainConfig(ctx context.Context, block *tlb.BlockInfo, onlyParams ...int32) (*BlockchainConfig, error) {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, 0) // mode

	data = append(data, block.Serialize()...)

	id := _GetConfigAll

	if len(onlyParams) > 0 {
		id = _GetConfigParams

		ln := make([]byte, 4)
		binary.LittleEndian.PutUint32(ln, uint32(len(onlyParams)))

		data = append(data, ln...)
		for _, p := range onlyParams {
			param := make([]byte, 4)
			binary.LittleEndian.PutUint32(param, uint32(p))
			data = append(data, param...)
		}
	}

	resp, err := c.client.Do(ctx, id, data)
	if err != nil {
		return nil, err
	}

	switch resp.TypeID {
	case _ConfigParams:
		_ = binary.LittleEndian.Uint32(resp.Data)
		resp.Data = resp.Data[4:]

		b := new(tlb.BlockInfo)
		resp.Data, err = b.Load(resp.Data)
		if err != nil {
			return nil, err
		}

		var shardProof []byte
		shardProof, resp.Data, err = tl.FromBytes(resp.Data)
		if err != nil {
			return nil, err
		}
		_ = shardProof

		var configProof []byte
		configProof, resp.Data, err = tl.FromBytes(resp.Data)
		if err != nil {
			return nil, err
		}
		_ = configProof

		c, err := cell.FromBOC(configProof)
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
