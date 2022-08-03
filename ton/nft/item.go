package nft

import (
	"context"
	"fmt"
	"math/rand"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
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
	Index             uint64
	CollectionAddress *address.Address
	OwnerAddress      *address.Address
	Content           ContentAny
}

type ItemClient struct {
	addr *address.Address
	api  *ton.APIClient
}

func NewItemClient(api *ton.APIClient, nftAddr *address.Address) *ItemClient {
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

	init, ok := res[0].(int64)
	if !ok {
		return nil, fmt.Errorf("init is not int64")
	}

	index, ok := res[1].(int64)
	if !ok {
		return nil, fmt.Errorf("index is not int64")
	}

	collectionRes, ok := res[2].(*cell.Slice)
	if !ok {
		return nil, fmt.Errorf("collectionRes is not slice")
	}

	collectionAddr, err := collectionRes.LoadAddr()
	if err != nil {
		return nil, fmt.Errorf("failed to load collection address from result slice: %w", err)
	}

	var ownerAddr *address.Address

	if res[3] != nil {
		ownerRes, ok := res[3].(*cell.Slice)
		if !ok {
			return nil, fmt.Errorf("ownerRes is not slice")
		}

		ownerAddr, err = ownerRes.LoadAddr()
		if err != nil {
			return nil, fmt.Errorf("failed to load owner address from result slice: %w", err)
		}
	} else {
		ownerAddr = address.NewAddressNone()
	}

	var cnt ContentAny

	if res[4] != nil {
		content, ok := res[4].(*cell.Cell)
		if !ok {
			return nil, fmt.Errorf("content is not cell")
		}

		cnt, err = ContentFromCell(content)
		if err != nil {
			return nil, fmt.Errorf("failed to parse content: %w", err)
		}
	}

	return &ItemData{
		Initialized:       init != 0,
		Index:             uint64(index),
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
