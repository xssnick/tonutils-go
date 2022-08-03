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

type ItemMintPayload struct {
	_         tlb.Magic  `tlb:"#00000001"`
	QueryID   uint64     `tlb:"## 64"`
	Index     uint64     `tlb:"## 64"`
	TonAmount tlb.Coins  `tlb:"."`
	Content   *cell.Cell `tlb:"^"`
}

type CollectionChangeOwner struct {
	_        tlb.Magic        `tlb:"#00000003"`
	QueryID  uint64           `tlb:"## 64"`
	NewOwner *address.Address `tlb:"addr"`
}

type CollectionData struct {
	NextItemIndex uint64
	Content       ContentAny
	OwnerAddress  *address.Address
}

type CollectionRoyaltyParams struct {
	Factor  uint16
	Base    uint16
	Address *address.Address
}

type CollectionClient struct {
	addr *address.Address
	api  *ton.APIClient
}

func NewCollectionClient(api *ton.APIClient, collectionAddr *address.Address) *CollectionClient {
	return &CollectionClient{
		addr: collectionAddr,
		api:  api,
	}
}

func (c *CollectionClient) GetNFTAddressByIndex(ctx context.Context, index uint64) (*address.Address, error) {
	b, err := c.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get masterchain info: %w", err)
	}

	res, err := c.api.RunGetMethod(ctx, b, c.addr, "get_nft_address_by_index", index)
	if err != nil {
		return nil, fmt.Errorf("failed to run get_nft_address_by_index method: %w", err)
	}

	x, ok := res[0].(*cell.Slice)
	if !ok {
		return nil, fmt.Errorf("result is not slice")
	}

	addr, err := x.LoadAddr()
	if err != nil {
		return nil, fmt.Errorf("failed to load address from result slice: %w", err)
	}

	return addr, nil
}

func (c *CollectionClient) RoyaltyParams(ctx context.Context) (*CollectionRoyaltyParams, error) {
	b, err := c.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get masterchain info: %w", err)
	}

	res, err := c.api.RunGetMethod(ctx, b, c.addr, "royalty_params")
	if err != nil {
		return nil, fmt.Errorf("failed to run royalty_params method: %w", err)
	}

	factor, ok := res[0].(int64)
	if !ok {
		return nil, fmt.Errorf("factor is not int64")
	}

	base, ok := res[0].(int64)
	if !ok {
		return nil, fmt.Errorf("base is not int64")
	}

	addrSlice, ok := res[2].(*cell.Slice)
	if !ok {
		return nil, fmt.Errorf("addrSlice is not slice")
	}

	addr, err := addrSlice.LoadAddr()
	if err != nil {
		return nil, fmt.Errorf("failed to load address from result slice: %w", err)
	}

	return &CollectionRoyaltyParams{
		Factor:  uint16(factor),
		Base:    uint16(base),
		Address: addr,
	}, nil
}

func (c *CollectionClient) GetNFTContent(ctx context.Context, index uint64, individualNFTContent *cell.Cell) (ContentAny, error) {
	b, err := c.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get masterchain info: %w", err)
	}

	res, err := c.api.RunGetMethod(ctx, b, c.addr, "get_nft_content", index, individualNFTContent)
	if err != nil {
		return nil, fmt.Errorf("failed to run get_nft_content method: %w", err)
	}

	x, ok := res[0].(*cell.Cell)
	if !ok {
		return nil, fmt.Errorf("result is not cell")
	}

	cnt, err := ContentFromCell(x)
	if err != nil {
		return nil, fmt.Errorf("failed to parse content: %w", err)
	}

	return cnt, nil
}

func (c *CollectionClient) GetCollectionData(ctx context.Context) (*CollectionData, error) {
	b, err := c.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get masterchain info: %w", err)
	}

	res, err := c.api.RunGetMethod(ctx, b, c.addr, "get_collection_data")
	if err != nil {
		return nil, fmt.Errorf("failed to run get_collection_data method: %w", err)
	}

	nextIndex, ok := res[0].(int64)
	if !ok {
		return nil, fmt.Errorf("nextIndex is not int64")
	}

	content, ok := res[1].(*cell.Cell)
	if !ok {
		return nil, fmt.Errorf("content is not cell")
	}

	ownerRes, ok := res[2].(*cell.Slice)
	if !ok {
		return nil, fmt.Errorf("ownerRes is not slice")
	}

	addr, err := ownerRes.LoadAddr()
	if err != nil {
		return nil, fmt.Errorf("failed to load owner address from result slice: %w", err)
	}

	cnt, err := ContentFromCell(content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse content: %w", err)
	}

	return &CollectionData{
		NextItemIndex: uint64(nextIndex),
		Content:       cnt,
		OwnerAddress:  addr,
	}, nil
}

func (c *CollectionClient) BuildMintPayload(index uint64, owner *address.Address, amountForward tlb.Coins, content ContentAny) (*cell.Cell, error) {
	con, err := content.ContentCell()
	if err != nil {
		return nil, err
	}

	con = cell.BeginCell().MustStoreAddr(owner).MustStoreRef(con).EndCell()

	body, err := tlb.ToCell(ItemMintPayload{
		QueryID:   rand.Uint64(),
		Index:     index,
		TonAmount: amountForward,
		Content:   con,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to convert ItemMintPayload to cell: %w", err)
	}

	return body, nil
}

func (c *CollectionClient) BuildMintEditablePayload(index uint64, owner, editor *address.Address, amountForward tlb.Coins, content ContentAny) (*cell.Cell, error) {
	con, err := content.ContentCell()
	if err != nil {
		return nil, err
	}

	con = cell.BeginCell().MustStoreAddr(owner).MustStoreRef(con).MustStoreAddr(editor).EndCell()

	body, err := tlb.ToCell(ItemMintPayload{
		QueryID:   rand.Uint64(),
		Index:     index,
		TonAmount: amountForward,
		Content:   con,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to convert ItemMintPayload to cell: %w", err)
	}

	return body, nil
}
