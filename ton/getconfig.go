package ton

import (
	"bytes"
	"context"
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

	tl.Register(GetLibraries{}, "liteServer.getLibraries library_list:(vector int256) = liteServer.LibraryResult")
	tl.Register(GetLibrariesWithProof{}, "liteServer.getLibrariesWithProof id:tonNode.blockIdExt mode:# library_list:(vector int256) = liteServer.LibraryResultWithProof")
	tl.Register(LibraryEntry{}, "liteServer.libraryEntry hash:int256 data:bytes = liteServer.LibraryEntry")
	tl.Register(LibraryResult{}, "liteServer.libraryResult result:(vector liteServer.libraryEntry) = liteServer.LibraryResult")
	tl.Register(LibraryResultWithProof{}, "liteServer.libraryResultWithProof id:tonNode.blockIdExt mode:# result:(vector liteServer.libraryEntry) state_proof:bytes data_proof:bytes = liteServer.LibraryResultWithProof")
}

type GetLibraries struct {
	LibraryList [][]byte `tl:"vector int256"`
}

type GetLibrariesWithProof struct {
	ID          *BlockIDExt `tl:"struct"`
	Mode        uint32      `tl:"flags"`
	LibraryList [][]byte    `tl:"vector int256"`
}

type LibraryEntry struct {
	Hash []byte `tl:"int256"`
	Data []byte `tl:"bytes"`
}

type LibraryResult struct {
	Result []*LibraryEntry `tl:"vector struct"`
}

type LibraryResultWithProof struct {
	ID         *BlockIDExt     `tl:"struct"`
	Mode       uint32          `tl:"flags"`
	Result     []*LibraryEntry `tl:"vector struct"`
	StateProof []byte          `tl:"bytes"`
	DataProof  []byte          `tl:"bytes"`
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

// Deprecated: use tlb.BlockchainConfig.
type BlockchainConfig = tlb.BlockchainConfig

func unwrapLibraryResultCell(data []byte, hash []byte) *cell.Cell {
	root, err := cell.FromBOC(data)
	if err != nil {
		return nil
	}

	for root != nil {
		rootHash := root.HashKey()
		if bytes.Equal(hash, rootHash[:]) {
			return root
		}

		// Some liteservers wrap the actual library root into an empty root cell.
		// Keep walking that wrapper chain, but only through the canonical 0-bit/1-ref form.
		if root.BitsSize() != 0 || root.RefsNum() != 1 {
			return nil
		}

		root = root.MustPeekRef(0)
	}

	return nil
}

func (c *APIClient) GetLibraries(ctx context.Context, hashes ...[]byte) ([]*cell.Cell, error) {
	var (
		resp tl.Serializable
		err  error
	)

	if err = c.client.QueryLiteserver(ctx, GetLibraries{LibraryList: hashes}, &resp); err != nil {
		return nil, err
	}

	switch t := resp.(type) {
	case LibraryResult:
		libList := make([]*cell.Cell, len(hashes))

		for i := 0; i < len(hashes); i++ {
			for _, e := range t.Result {
				// Calculate hashes by ourselves to make sure that LS is not cheating.
				// Some LS responses wrap the actual library root into an empty root cell,
				// so we only unwrap that canonical wrapper form before matching.
				if lib := unwrapLibraryResultCell(e.Data, hashes[i]); lib != nil {
					libList[i] = lib
					break
				}
			}
		}

		return libList, nil
	case LSError:
		return nil, t
	}

	return nil, errUnexpectedResponse(resp)
}

func (c *APIClient) GetBlockchainConfig(ctx context.Context, block *BlockIDExt, onlyParams ...int32) (*tlb.BlockchainConfig, error) {
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
		stateProof, err := cell.FromBOC(t.StateProof)
		if err != nil {
			return nil, fmt.Errorf("incorrect state proof: %w", err)
		}
		configProof, err := cell.FromBOC(t.ConfigProof)
		if err != nil {
			return nil, fmt.Errorf("incorrect config proof: %w", err)
		}
		stateExtra, err := CheckShardMcStateExtraProof(block, []*cell.Cell{stateProof, configProof})
		if err != nil {
			return nil, fmt.Errorf("incorrect proof: %w", err)
		}

		if len(onlyParams) > 0 {
			dict := cell.NewDict(32)

			// we need it because lite server may add some unwanted keys
			for _, param := range onlyParams {
				res, err := stateExtra.ConfigParams.Config.Params.LoadValueByIntKey(big.NewInt(int64(param)))
				if err != nil {
					return nil, fmt.Errorf("config param %d not found", param)
				}

				v, err := res.LoadRefCell()
				if err != nil {
					return nil, fmt.Errorf("failed to load config param %d, err: %w", param, err)
				}

				if err = dict.SetIntKey(big.NewInt(int64(param)), cell.BeginCell().MustStoreRef(v).EndCell()); err != nil {
					return nil, fmt.Errorf("failed to store config param %d: %w", param, err)
				}
			}

			return &tlb.BlockchainConfig{Root: dict.AsCell()}, nil
		}

		return &tlb.BlockchainConfig{Root: stateExtra.ConfigParams.Config.Params.AsCell()}, nil
	case LSError:
		return nil, t
	}
	return nil, errUnexpectedResponse(resp)
}
