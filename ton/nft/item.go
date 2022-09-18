package nft

import (
	"context"
	"fmt"
	"math/big"
	"math/rand"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type TransferPayload struct {
	_                   tlb.Magic        `tlb:"#5fcc3d14"`
	QueryID             uint64           `tlb:"## 64"`
	NewOwner            *address.Address `tlb:"addr"`
	ResponseDestination *address.Address `tlb:"addr"`
	CustomPayload       *cell.Cell       `tlb:"maybe ^"`
	ForwardAmount       tlb.Coins        `tlb:"."`
	ForwardPayload      *cell.Cell       `tlb:"either . ^"`
}

type ItemData struct {
	Initialized       bool
	Index             *big.Int
	CollectionAddress *address.Address
	OwnerAddress      *address.Address
	Content           ContentAny
}

type ItemClient struct {
	addr *address.Address
	api  TonApi
}

func NewItemClient(api TonApi, nftAddr *address.Address) *ItemClient {
	return &ItemClient{
		addr: nftAddr,
		api:  api,
	}
}

func (c *ItemClient) GetNFTData(ctx context.Context) (*ItemData, error) {
	b, err := c.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get masterchain info: %w", err)
	}

	res, err := c.api.RunGetMethod(ctx, b, c.addr, "get_nft_data")
	if err != nil {
		return nil, fmt.Errorf("failed to run get_collection_data method: %w", err)
	}

	init, err := res.Int(0)
	if err != nil {
		return nil, fmt.Errorf("err get init value: %w", err)
	}

	index, err := res.Int(1)
	if err != nil {
		return nil, fmt.Errorf("err get index value: %w", err)
	}

	collectionRes, err := res.Slice(2)
	if err != nil {
		return nil, fmt.Errorf("err get collection slice value: %w", err)
	}

	collectionAddr, err := collectionRes.LoadAddr()
	if err != nil {
		return nil, fmt.Errorf("failed to load collection address from result slice: %w", err)
	}

	var ownerAddr *address.Address

	nilOwner, err := res.IsNil(3)
	if err != nil {
		return nil, fmt.Errorf("err check for nil owner slice value: %w", err)
	}

	if !nilOwner {
		ownerRes, err := res.Slice(3)
		if err != nil {
			return nil, fmt.Errorf("err get owner slice value: %w", err)
		}

		ownerAddr, err = ownerRes.LoadAddr()
		if err != nil {
			return nil, fmt.Errorf("failed to load owner address from result slice: %w", err)
		}
	} else {
		ownerAddr = address.NewAddressNone()
	}

	var cnt ContentAny

	nilContent, err := res.IsNil(4)
	if err != nil {
		return nil, fmt.Errorf("err check for nil content cell value: %w", err)
	}

	if !nilContent {
		content, err := res.Cell(4)
		if err != nil {
			return nil, fmt.Errorf("err get content cell value: %w", err)
		}

		cnt, err = ContentFromCell(content)
		if err != nil {
			return nil, fmt.Errorf("failed to parse content: %w", err)
		}
	}

	return &ItemData{
		Initialized:       init.Cmp(big.NewInt(0)) != 0,
		Index:             index,
		CollectionAddress: collectionAddr,
		OwnerAddress:      ownerAddr,
		Content:           cnt,
	}, nil
}

func (c *ItemClient) BuildTransferPayload(newOwner *address.Address, amountForward tlb.Coins, payloadForward *cell.Cell) (*cell.Cell, error) {
	if payloadForward == nil {
		payloadForward = cell.BeginCell().EndCell()
	}

	body, err := tlb.ToCell(TransferPayload{
		QueryID:             rand.Uint64(),
		NewOwner:            newOwner,
		ResponseDestination: newOwner,
		CustomPayload:       nil,
		ForwardAmount:       amountForward,
		ForwardPayload:      payloadForward,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to convert TransferPayload to cell: %w", err)
	}

	return body, nil
}
