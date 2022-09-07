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

type TonApi interface {
	GetAccount(ctx context.Context, block *tlb.BlockInfo, addr *address.Address) (*tlb.Account, error)
	CurrentMasterchainInfo(ctx context.Context) (_ *tlb.BlockInfo, err error)
	RunGetMethod(ctx context.Context, blockInfo *tlb.BlockInfo, addr *address.Address, method string, params ...any) ([]interface{}, error)
}

type CollectionRawData struct {
	OwnerAddress  *address.Address `tlb:"addr"`
	NextItemIndex *big.Int         `tlb:"## 64"`
	Content       *cell.Cell       `tlb:"^"`
	ItemCode      *cell.Cell       `tlb:"^"`
	RoyaltyParams *cell.Cell       `tlb:"^"`
}

type ItemMintPayload struct {
	_         tlb.Magic  `tlb:"#00000001"`
	QueryID   uint64     `tlb:"## 64"`
	Index     *big.Int   `tlb:"## 64"`
	TonAmount tlb.Coins  `tlb:"."`
	Content   *cell.Cell `tlb:"^"`
}

type CollectionChangeOwner struct {
	_        tlb.Magic        `tlb:"#00000003"`
	QueryID  uint64           `tlb:"## 64"`
	NewOwner *address.Address `tlb:"addr"`
}

type CollectionData struct {
	NextItemIndex *big.Int
	Content       ContentAny
	OwnerAddress  *address.Address
}

type CollectionRoyaltyParams struct {
	Factor  uint16           `tlb:"## 16"`
	Base    uint16           `tlb:"## 16"`
	Address *address.Address `tlb:"addr"`
}

type CollectionClient struct {
	addr *address.Address
	api  TonApi
}

func NewCollectionClient(api TonApi, collectionAddr *address.Address) *CollectionClient {
	return &CollectionClient{
		addr: collectionAddr,
		api:  api,
	}
}

func (c *CollectionClient) ParseAccountData(ctx context.Context) (*CollectionRawData, error) {
	b, err := c.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get masterchain info: %w", err)
	}
	return c.ParseAccountDataAtBlock(ctx, b)
}

func (c *CollectionClient) ParseAccountDataAtBlock(ctx context.Context, block *tlb.BlockInfo) (*CollectionRawData, error) {
	raw := new(CollectionRawData)

	a, err := c.api.GetAccount(ctx, block, c.addr)
	if err != nil {
		return nil, err
	}
	if !a.IsActive || a.State.Status != tlb.AccountStatusActive {
		return nil, fmt.Errorf("account is not active")
	}

	if err := tlb.LoadFromCell(raw, a.Data.BeginParse()); err != nil {
		return nil, fmt.Errorf("load from cell: %w", err)
	}

	return raw, nil
}

func (c *CollectionClient) GetNFTAddressByIndex(ctx context.Context, index *big.Int) (*address.Address, error) {
	b, err := c.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get masterchain info: %w", err)
	}
	return c.GetNFTAddressByIndexAtBlock(ctx, b, index)
}

func (c *CollectionClient) GetNFTAddressByIndexAtBlock(ctx context.Context, b *tlb.BlockInfo, index *big.Int) (*address.Address, error) {
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
	return c.RoyaltyParamsAtBlock(ctx, b)
}

func (c *CollectionClient) RoyaltyParamsAtBlock(ctx context.Context, b *tlb.BlockInfo) (*CollectionRoyaltyParams, error) {
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

func (c *CollectionClient) GetNFTContent(ctx context.Context, index *big.Int, individualNFTContent *cell.Cell) (ContentAny, error) {
	b, err := c.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get masterchain info: %w", err)
	}
	return c.GetNFTContentAtBlock(ctx, b, index, individualNFTContent)
}

func (c *CollectionClient) GetNFTContentAtBlock(ctx context.Context, b *tlb.BlockInfo, index *big.Int, individualNFTContent *cell.Cell) (ContentAny, error) {
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
	return c.GetCollectionDataAtBlock(ctx, b)
}

func (c *CollectionClient) GetCollectionDataAtBlock(ctx context.Context, b *tlb.BlockInfo) (*CollectionData, error) {
	res, err := c.api.RunGetMethod(ctx, b, c.addr, "get_collection_data")
	if err != nil {
		return nil, fmt.Errorf("failed to run get_collection_data method: %w", err)
	}

	nextIndex, ok := res[0].(*big.Int)
	if !ok {
		nextIndexI, ok := res[0].(int64)
		if !ok {
			return nil, fmt.Errorf("nextIndex is not int")
		}
		nextIndex = big.NewInt(nextIndexI)
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
		NextItemIndex: nextIndex,
		Content:       cnt,
		OwnerAddress:  addr,
	}, nil
}

func (c *CollectionClient) BuildMintPayload(index *big.Int, owner *address.Address, amountForward tlb.Coins, content ContentAny) (*cell.Cell, error) {
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

func (c *CollectionClient) BuildMintEditablePayload(index *big.Int, owner, editor *address.Address, amountForward tlb.Coins, content ContentAny) (*cell.Cell, error) {
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

func ParseItemMintPayload(payload *cell.Cell) (*ItemMintPayload, error) {
	ret := new(ItemMintPayload)

	if err := tlb.LoadFromCell(ret, payload.BeginParse()); err != nil {
		return nil, err
	}

	return ret, nil
}

func ParseItemMintPayloadContent(payloadContent *cell.Cell) (owner, editor *address.Address, content ContentAny, err error) {
	s := payloadContent.BeginParse()

	owner, err = s.LoadAddr()
	if err != nil {
		return nil, nil, nil, err
	}

	contentSlice, err := s.LoadRef()
	if err != nil {
		return nil, nil, nil, err
	}

	content, err = ContentFromSlice(contentSlice)
	if err != nil {
		return nil, nil, nil, err
	}

	editor, err = s.LoadAddr()
	if err != nil {
		editor = nil
	}

	return owner, editor, content, nil
}
