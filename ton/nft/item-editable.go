package nft

import (
	"context"
	"fmt"
	"github.com/xssnick/tonutils-go/ton"
	"math/rand"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type ItemEditPayload struct {
	_       tlb.Magic  `tlb:"#1a0b9d51"`
	QueryID uint64     `tlb:"## 64"`
	Content *cell.Cell `tlb:"^"`
}

type ItemEditableClient struct {
	*ItemClient
}

func NewItemEditableClient(api TonApi, nftAddr *address.Address) *ItemEditableClient {
	return &ItemEditableClient{
		ItemClient: NewItemClient(api, nftAddr),
	}
}

func (c *ItemEditableClient) GetEditor(ctx context.Context) (*address.Address, error) {
	b, err := c.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get masterchain info: %w", err)
	}
	return c.GetEditorAtBlock(ctx, b)
}

func (c *ItemEditableClient) GetEditorAtBlock(ctx context.Context, b *ton.BlockIDExt) (*address.Address, error) {
	res, err := c.api.WaitForBlock(b.SeqNo).RunGetMethod(ctx, b, c.addr, "get_editor")
	if err != nil {
		return nil, fmt.Errorf("failed to run get_editor method: %w", err)
	}

	x, err := res.Slice(0)
	if err != nil {
		return nil, fmt.Errorf("result is not slice, err: %w", err)
	}

	addr, err := x.LoadAddr()
	if err != nil {
		return nil, fmt.Errorf("failed to load address from result slice: %w", err)
	}

	return addr, nil
}

func (c *ItemEditableClient) BuildEditPayload(content ContentAny) (*cell.Cell, error) {
	var con *cell.Cell
	switch cnt := content.(type) {
	case *ContentOffchain:
		// we have exception for offchain, it is without prefix
		con = cell.BeginCell().MustStoreStringSnake(cnt.URI).EndCell()
	default:
		var err error
		con, err = content.ContentCell()
		if err != nil {
			return nil, err
		}
	}

	body, err := tlb.ToCell(ItemEditPayload{
		QueryID: rand.Uint64(),
		Content: con,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to convert ItemEditPayload to cell: %w", err)
	}

	return body, nil
}
