package wallet

import (
	"context"
	"crypto/ed25519"
	"fmt"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type Version int

const (
	V1 Version = 1
	V2 Version = 2
	V3 Version = 3
	V4 Version = 4
)

type TonAPI interface {
	GetBlockInfo(ctx context.Context) (*tlb.BlockInfo, error)
	GetAccount(ctx context.Context, block *tlb.BlockInfo, addr *address.Address) (*ton.Account, error)
	SendExternalMessage(ctx context.Context, addr *address.Address, msg *cell.Cell) error
	SendExternalInitMessage(ctx context.Context, addr *address.Address, msg *cell.Cell, state *tlb.StateInit) error
	RunGetMethod(ctx context.Context, blockInfo *tlb.BlockInfo, addr *address.Address, method string, params ...interface{}) ([]interface{}, error)
}

type Wallet struct {
	api  TonAPI
	key  ed25519.PrivateKey
	addr *address.Address
	ver  Version
}

func FromPrivateKey(api TonAPI, key ed25519.PrivateKey, version Version) (*Wallet, error) {
	addr, err := AddressFromPubKey(key.Public().(ed25519.PublicKey), version)
	if err != nil {
		return nil, err
	}

	return &Wallet{
		api:  api,
		key:  key,
		addr: addr,
		ver:  version,
	}, nil
}

func (w *Wallet) Address() *address.Address {
	return w.addr
}

func (w *Wallet) GetBalance(ctx context.Context, block *tlb.BlockInfo) (*tlb.Grams, error) {
	acc, err := w.api.GetAccount(ctx, block, w.addr)
	if err != nil {
		return nil, fmt.Errorf("failed to get account state: %w", err)
	}

	if !acc.IsActive {
		return new(tlb.Grams), nil
	}

	return acc.State.Balance, nil
}

func (w *Wallet) Send(ctx context.Context, mode byte, message *tlb.InternalMessage) error {
	msg := cell.BeginCell()
	var stateInit *tlb.StateInit

	switch w.ver {
	case V3:
		block, err := w.api.GetBlockInfo(ctx)
		if err != nil {
			return fmt.Errorf("failed to get block: %w", err)
		}

		var seq uint64
		resp, err := w.api.RunGetMethod(ctx, block, w.addr, "seqno")
		if err != nil {
			// TODO: make it better
			// not initialized
			if err.Error() != "contract exit code: 4294967040" {
				return fmt.Errorf("failed to get seqno: %w", err)
			}

			stateInit, err = GetStateInit(w.key.Public().(ed25519.PublicKey), w.ver)
			if err != nil {
				return fmt.Errorf("failed to get state init: %w", err)
			}
		} else {
			var ok bool
			seq, ok = resp[0].(uint64)
			if !ok {
				return fmt.Errorf("seqno is not int")
			}
		}

		intMsg, err := message.ToCell()
		if err != nil {
			return fmt.Errorf("failed to convert internal message to cell: %w", err)
		}

		payload := cell.BeginCell().MustStoreUInt(_SubWalletV3, 32).
			MustStoreUInt(uint64(0xFFFFFFFF), 32). // ttl, I took it from wallet's transactions
			MustStoreUInt(seq, 32).MustStoreUInt(uint64(mode), 8).MustStoreRef(intMsg)

		sign := payload.EndCell().Sign(w.key)

		msg.MustStoreSlice(sign, 512).MustStoreBuilder(payload)
	default:
		return fmt.Errorf("send is not yet supported for wallet with this version")
	}

	err := w.api.SendExternalInitMessage(ctx, w.addr, msg.EndCell(), stateInit)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

func (w *Wallet) Transfer(ctx context.Context, to *address.Address, amount *tlb.Grams, comment string) error {
	var body *cell.Cell
	if comment != "" {
		// comment ident
		c := cell.BeginCell().MustStoreUInt(0, 32)

		data := []byte(comment)
		if err := c.StoreSlice(data, len(data)*8); err != nil {
			return fmt.Errorf("failed to encode comment: %w", err)
		}

		body = c.EndCell()
	}

	return w.Send(ctx, 1, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		DstAddr:     to,
		Amount:      amount,
		Body:        body,
	})
}

func (w *Wallet) DeployContract(ctx context.Context, amount *tlb.Grams, body, code, data *cell.Cell) (*address.Address, error) {
	state := &tlb.StateInit{
		Data: data,
		Code: code,
	}

	stateCell, err := state.ToCell()
	if err != nil {
		return nil, err
	}

	addr := address.NewAddress(0, 0, stateCell.Hash())

	err = w.Send(ctx, 1, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      false,
		DstAddr:     addr,
		Amount:      amount,
		Body:        body,
		StateInit:   state,
	})
	if err != nil {
		return nil, err
	}

	return addr, nil
}
