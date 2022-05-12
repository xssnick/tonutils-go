package ton

import (
	"context"

	"github.com/xssnick/tonutils-go/liteclient/tlb"
)

func (c *APIClient) GetBlockInfo(ctx context.Context) (*tlb.BlockInfo, error) {
	resp, err := c.client.Do(ctx, _GetMasterchainInfo, nil)
	if err != nil {
		return nil, err
	}

	block := new(tlb.BlockInfo)
	_, err = block.Load(resp.Data)
	if err != nil {
		return nil, err
	}

	return block, nil
}
