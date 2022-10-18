package jetton

import (
	"context"
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/nft"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type TonApi interface {
	CurrentMasterchainInfo(ctx context.Context) (_ *tlb.BlockInfo, err error)
	RunGetMethod(ctx context.Context, blockInfo *tlb.BlockInfo, addr *address.Address, method string, params ...any) (*ton.ExecutionResult, error)
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
	return c.GetJettonWalletAtBlock(ctx, ownerAddr, b)
}

func (c *Client) GetJettonWalletAtBlock(ctx context.Context, ownerAddr *address.Address, b *tlb.BlockInfo) (*WalletClient, error) {
	res, err := c.api.RunGetMethod(ctx, b, c.addr, "get_wallet_address",
		cell.BeginCell().MustStoreAddr(ownerAddr).EndCell().BeginParse())
	if err != nil {
		return nil, fmt.Errorf("failed to run get_wallet_address method: %w", err)
	}

	x, err := res.Slice(0)
	if err != nil {
		return nil, err
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
	return c.GetJettonDataAtBlock(ctx, b)
}

func (c *Client) GetJettonDataAtBlock(ctx context.Context, b *tlb.BlockInfo) (*Data, error) {
	res, err := c.api.RunGetMethod(ctx, b, c.addr, "get_jetton_data")
	if err != nil {
		return nil, fmt.Errorf("failed to run get_jetton_data method: %w", err)
	}

	supply, err := res.Int(0)
	if err != nil {
		return nil, fmt.Errorf("supply get err: %w", err)
	}

	mintable, err := res.Int(1)
	if err != nil {
		return nil, fmt.Errorf("mintable get err: %w", err)
	}

	adminAddr, err := res.Slice(2)
	if err != nil {
		return nil, fmt.Errorf("admin addr get err: %w", err)
	}
	addr, err := adminAddr.LoadAddr()
	if err != nil {
		return nil, fmt.Errorf("failed to load address from adminAddr slice: %w", err)
	}

	contentCell, err := res.Cell(3)
	if err != nil {
		return nil, fmt.Errorf("content cell get err: %w", err)
	}

	content, err := nft.ContentFromCell(contentCell)
	if err != nil {
		return nil, fmt.Errorf("failed to load content from contentCell: %w", err)
	}

	walletCode, err := res.Cell(4)
	if err != nil {
		return nil, fmt.Errorf("wallet code get err: %w", err)
	}

	return &Data{
		TotalSupply: supply,
		Mintable:    mintable.Cmp(big.NewInt(0)) != 0,
		AdminAddr:   addr,
		Content:     content,
		WalletCode:  walletCode,
	}, nil
}
