package nft

import (
	"context"
	"fmt"
	"github.com/xssnick/tonutils-go/ton"
	"math/big"
	"math/rand"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type TonApi interface {
	CurrentMasterchainInfo(ctx context.Context) (_ *tlb.BlockInfo, err error)
	RunGetMethod(ctx context.Context, blockInfo *tlb.BlockInfo, addr *address.Address, method string, params ...any) (*ton.ExecutionResult, error)
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
	Factor  uint16
	Base    uint16
	Address *address.Address
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

func (c *CollectionClient) GetNFTAddressByIndex(ctx context.Context, index *big.Int) (*address.Address, error) {
	b, err := c.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get masterchain info: %w", err)
	}

	res, err := c.api.RunGetMethod(ctx, b, c.addr, "get_nft_address_by_index", index)
	if err != nil {
		return nil, fmt.Errorf("failed to run get_nft_address_by_index method: %w", err)
	}

	x, err := res.Slice(0)
	if err != nil {
		return nil, fmt.Errorf("result get err: %w", err)
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

	factor, err := res.Int(0)
	if err != nil {
		return nil, fmt.Errorf("factor get err: %w", err)
	}

	base, err := res.Int(1)
	if err != nil {
		return nil, fmt.Errorf("base get err: %w", err)
	}

	addrSlice, err := res.Slice(2)
	if err != nil {
		return nil, fmt.Errorf("addr slice get err: %w", err)
	}

	addr, err := addrSlice.LoadAddr()
	if err != nil {
		return nil, fmt.Errorf("failed to load address from result slice: %w", err)
	}

	return &CollectionRoyaltyParams{
		Factor:  uint16(factor.Uint64()),
		Base:    uint16(base.Uint64()),
		Address: addr,
	}, nil
}

func (c *CollectionClient) GetNFTContent(ctx context.Context, index *big.Int, individualNFTContent ContentAny) (ContentAny, error) {
	con, err := toNftContent(individualNFTContent)
	if err != nil {
		return nil, fmt.Errorf("failed to convert nft content to cell: %w", err)
	}

	b, err := c.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get masterchain info: %w", err)
	}

	res, err := c.api.RunGetMethod(ctx, b, c.addr, "get_nft_content", index, con)
	if err != nil {
		return nil, fmt.Errorf("failed to run get_nft_content method: %w", err)
	}

	x, err := res.Cell(0)
	if err != nil {
		return nil, fmt.Errorf("result get err: %w", err)
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

	nextIndex, err := res.Int(0)
	if err != nil {
		return nil, fmt.Errorf("next index get err: %w", err)
	}

	content, err := res.Cell(1)
	if err != nil {
		return nil, fmt.Errorf("content get err: %w", err)
	}

	ownerRes, err := res.Slice(2)
	if err != nil {
		return nil, fmt.Errorf("owner get err: %w", err)
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

func (c *CollectionClient) BuildMintPayload(index *big.Int, owner *address.Address, amountForward tlb.Coins, content ContentAny) (_ *cell.Cell, err error) {
	con, err := toNftContent(content)
	if err != nil {
		return nil, fmt.Errorf("failed to convert nft content to cell: %w", err)
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

func (c *CollectionClient) BuildMintEditablePayload(index *big.Int, owner, editor *address.Address, amountForward tlb.Coins, content ContentAny) (_ *cell.Cell, err error) {
	con, err := toNftContent(content)
	if err != nil {
		return nil, fmt.Errorf("failed to convert nft content to cell: %w", err)
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

func toNftContent(content ContentAny) (*cell.Cell, error) {
	if off, ok := content.(*ContentOffchain); ok {
		// https://github.com/ton-blockchain/TIPs/issues/64
		// Standard says that prefix should be 0x01, but looks like it was misunderstanding in other implementations and 0x01 was dropped
		// so, we make compatibility
		return cell.BeginCell().MustStoreStringSnake(off.URI).EndCell(), nil
	}
	return content.ContentCell()
}
