package jetton

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/nft"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type TonApi interface {
	WaitForBlock(seqno uint32) ton.APIClientWrapped
	CurrentMasterchainInfo(ctx context.Context) (_ *ton.BlockIDExt, err error)
	RunGetMethod(ctx context.Context, blockInfo *ton.BlockIDExt, addr *address.Address, method string, params ...any) (*ton.ExecutionResult, error)
	SubscribeOnTransactions(workerCtx context.Context, addr *address.Address, lastProcessedLT uint64, channel chan<- *tlb.Transaction)
}

var ErrInvalidTransfer = errors.New("transfer is not verified")

type MintPayloadMasterMsg struct {
	Opcode       uint32     `tlb:"## 32"`
	QueryID      uint64     `tlb:"## 64"`
	JettonAmount tlb.Coins  `tlb:"."`
	RestData     *cell.Cell `tlb:"."`
}

type MintPayload struct {
	_         tlb.Magic            `tlb:"#00000015"`
	QueryID   uint64               `tlb:"## 64"`
	ToAddress *address.Address     `tlb:"addr"`
	Amount    tlb.Coins            `tlb:"."`
	MasterMsg MintPayloadMasterMsg `tlb:"^"`
}

type TransferNotification struct {
	_              tlb.Magic        `tlb:"#7362d09c"`
	QueryID        uint64           `tlb:"## 64"`
	Amount         tlb.Coins        `tlb:"."`
	Sender         *address.Address `tlb:"addr"`
	ForwardPayload *cell.Cell       `tlb:"either . ^"`
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

func (c *Client) GetJettonWalletAtBlock(ctx context.Context, ownerAddr *address.Address, b *ton.BlockIDExt) (*WalletClient, error) {
	res, err := c.api.WaitForBlock(b.SeqNo).RunGetMethod(ctx, b, c.addr, "get_wallet_address",
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

func (c *Client) GetJettonDataAtBlock(ctx context.Context, b *ton.BlockIDExt) (*Data, error) {
	res, err := c.api.WaitForBlock(b.SeqNo).RunGetMethod(ctx, b, c.addr, "get_jetton_data")
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
