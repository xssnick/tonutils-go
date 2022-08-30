package jetton

import (
	"context"
	"fmt"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton/nft"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"math/big"
)

type TonApi interface {
	CurrentMasterchainInfo(ctx context.Context) (_ *tlb.BlockInfo, err error)
	RunGetMethod(ctx context.Context, blockInfo *tlb.BlockInfo, addr *address.Address, method string, params ...any) ([]interface{}, error)
}

type MintPayload struct {
	_         tlb.Magic  `tlb:"#00000001"`
	QueryID   uint64     `tlb:"## 64"`
	Index     uint64     `tlb:"## 64"`
	TonAmount tlb.Coins  `tlb:"."`
	Content   *cell.Cell `tlb:"^"`
}

type Data struct {
	TotalSupply *big.Int
	Mintable    bool
	AdminAddr   *address.Address
	Content     nft.ContentAny
	WalletCode  *cell.Cell
}

type Client struct {
	addr *address.Address
	api  TonApi
}

func NewJettonMasterClient(api TonApi, masterContractAddr *address.Address) *Client {
	return &Client{
		addr: masterContractAddr,
		api:  api,
	}
}

func (c *Client) GetJettonWallet(ctx context.Context, ownerAddr *address.Address) (*WalletClient, error) {
	b, err := c.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get masterchain info: %w", err)
	}

	res, err := c.api.RunGetMethod(ctx, b, c.addr, "get_wallet_address",
		cell.BeginCell().MustStoreAddr(ownerAddr).EndCell().BeginParse())
	if err != nil {
		return nil, fmt.Errorf("failed to run get_wallet_address method: %w", err)
	}

	x, ok := res[0].(*cell.Slice)
	if !ok {
		return nil, fmt.Errorf("result is not slice")
	}

	addr, err := x.LoadAddr()
	if err != nil {
		return nil, fmt.Errorf("failed to load address from result slice: %w", err)
	}

	return &WalletClient{
		master: c,
		addr:   addr,
	}, nil
}

func (c *Client) GetJettonData(ctx context.Context) (*Data, error) {
	b, err := c.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get masterchain info: %w", err)
	}

	res, err := c.api.RunGetMethod(ctx, b, c.addr, "get_jetton_data")
	if err != nil {
		return nil, fmt.Errorf("failed to run get_jetton_data method: %w", err)
	}

	supply, ok := res[0].(*big.Int)
	if !ok {
		supplyI, ok := res[0].(int64)
		if !ok {
			return nil, fmt.Errorf("supply is not integer")
		}
		supply = big.NewInt(supplyI)
	}

	mintable, ok := res[1].(int64)
	if !ok {
		return nil, fmt.Errorf("mintable is not integer")
	}

	adminAddr, ok := res[2].(*cell.Slice)
	if !ok {
		return nil, fmt.Errorf("adminAddr is not slice")
	}

	addr, err := adminAddr.LoadAddr()
	if err != nil {
		return nil, fmt.Errorf("failed to load address from adminAddr slice: %w", err)
	}

	contentCell, ok := res[3].(*cell.Cell)
	if !ok {
		return nil, fmt.Errorf("contentCell is not cell")
	}

	content, err := nft.ContentFromCell(contentCell)
	if err != nil {
		return nil, fmt.Errorf("failed to load content from contentCell: %w", err)
	}

	walletCode, ok := res[4].(*cell.Cell)
	if !ok {
		return nil, fmt.Errorf("walletCode is not cell")
	}

	return &Data{
		TotalSupply: supply,
		Mintable:    mintable != 0,
		AdminAddr:   addr,
		Content:     content,
		WalletCode:  walletCode,
	}, nil
}
