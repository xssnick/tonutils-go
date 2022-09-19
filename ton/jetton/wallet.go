package jetton

import (
	"context"
	"fmt"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"math/rand"
)

type TransferPayload struct {
	_                   tlb.Magic        `tlb:"#0f8a7ea5"`
	QueryID             uint64           `tlb:"## 64"`
	Amount              tlb.Coins        `tlb:"."`
	Destination         *address.Address `tlb:"addr"`
	ResponseDestination *address.Address `tlb:"addr"`
	CustomPayload       *cell.Cell       `tlb:"maybe ^"`
	ForwardTONAmount    tlb.Coins        `tlb:"."`
	ForwardPayload      *cell.Cell       `tlb:"either . ^"`
}

type BurnPayload struct {
	_                   tlb.Magic        `tlb:"#595f07bc"`
	QueryID             uint64           `tlb:"## 64"`
	Amount              tlb.Coins        `tlb:"."`
	ResponseDestination *address.Address `tlb:"addr"`
	CustomPayload       *cell.Cell       `tlb:"maybe ^"`
}

type WalletClient struct {
	master *Client
	addr   *address.Address
}

func (c *WalletClient) Address() *address.Address {
	return c.addr
}

func (c *WalletClient) GetBalance(ctx context.Context) (tlb.Coins, error) {
	b, err := c.master.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return tlb.Coins{}, fmt.Errorf("failed to get masterchain info: %w", err)
	}

	res, err := c.master.api.RunGetMethod(ctx, b, c.addr, "get_wallet_data")
	if err != nil {
		if cErr, ok := err.(ton.ContractExecError); ok && cErr.Code == ton.ErrCodeContractNotInitialized {
			return tlb.Coins{}, nil
		}
		return tlb.Coins{}, fmt.Errorf("failed to run get_wallet_data method: %w", err)
	}

	balance, err := res.Int(0)
	if err != nil {
		return tlb.Coins{}, fmt.Errorf("failed to parse balance: %w", err)
	}

	return tlb.FromNanoTON(balance), nil
}

func (c *WalletClient) BuildTransferPayload(to *address.Address, amountCoins, amountForwardTON tlb.Coins, payloadForward *cell.Cell) (*cell.Cell, error) {
	if payloadForward == nil {
		payloadForward = cell.BeginCell().EndCell()
	}

	body, err := tlb.ToCell(TransferPayload{
		QueryID:             rand.Uint64(),
		Amount:              amountCoins,
		Destination:         to,
		ResponseDestination: to,
		CustomPayload:       nil,
		ForwardTONAmount:    amountForwardTON,
		ForwardPayload:      payloadForward,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to convert TransferPayload to cell: %w", err)
	}

	return body, nil
}

func (c *WalletClient) BuildBurnPayload(amountCoins tlb.Coins, notifyAddr *address.Address) (*cell.Cell, error) {
	body, err := tlb.ToCell(BurnPayload{
		QueryID:             rand.Uint64(),
		Amount:              amountCoins,
		ResponseDestination: notifyAddr,
		CustomPayload:       nil,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to convert BurnPayload to cell: %w", err)
	}

	return body, nil
}
