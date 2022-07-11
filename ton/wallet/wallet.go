package wallet

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type Version int

const (
	V3         Version = 3
	V4R2       Version = 42
	HighloadV2 Version = 102
)

type TonAPI interface {
	GetMasterchainInfo(ctx context.Context) (*tlb.BlockInfo, error)
	GetAccount(ctx context.Context, block *tlb.BlockInfo, addr *address.Address) (*tlb.Account, error)
	SendExternalMessage(ctx context.Context, msg *tlb.ExternalMessage) error
	RunGetMethod(ctx context.Context, blockInfo *tlb.BlockInfo, addr *address.Address, method string, params ...interface{}) ([]interface{}, error)
}

type Message struct {
	Mode            uint8
	InternalMessage *tlb.InternalMessage
}

type Wallet struct {
	api  TonAPI
	key  ed25519.PrivateKey
	addr *address.Address
	ver  Version

	// Can be used to operate multiple wallets with the same key and version.
	// use GetSubwallet if you need it.
	subwallet uint32

	// Stores a pointer to implementation of the version related functionality
	spec any
}

func FromPrivateKey(api TonAPI, key ed25519.PrivateKey, version Version) (*Wallet, error) {
	addr, err := AddressFromPubKey(key.Public().(ed25519.PublicKey), version, DefaultSubwallet)
	if err != nil {
		return nil, err
	}

	w := &Wallet{
		api:       api,
		key:       key,
		addr:      addr,
		ver:       version,
		subwallet: DefaultSubwallet,
	}

	switch version {
	case V3, V4R2:
		regular := SpecRegular{
			wallet:      w,
			messagesTTL: 0xFFFFFFFF, // no expire
		}

		switch version {
		case V3:
			w.spec = &SpecV3{regular}
		case V4R2:
			w.spec = &SpecV4R2{regular}
		}
	default:
		return nil, errors.New("cannot init spec: unknown version")
	}

	return w, nil
}

func (w *Wallet) Address() *address.Address {
	return w.addr
}

func (w *Wallet) GetSubwallet(subwallet uint32) (*Wallet, error) {
	addr, err := AddressFromPubKey(w.key.Public().(ed25519.PublicKey), w.ver, subwallet)
	if err != nil {
		return nil, err
	}

	return &Wallet{
		api:       w.api,
		key:       w.key,
		addr:      addr,
		ver:       w.ver,
		subwallet: subwallet,
	}, nil
}

func (w *Wallet) GetBalance(ctx context.Context, block *tlb.BlockInfo) (tlb.Coins, error) {
	acc, err := w.api.GetAccount(ctx, block, w.addr)
	if err != nil {
		return tlb.Coins{}, fmt.Errorf("failed to get account state: %w", err)
	}

	if !acc.IsActive {
		return tlb.Coins{}, nil
	}

	return acc.State.Balance, nil
}

func (w *Wallet) GetSpec() any {
	return w.spec
}

func (w *Wallet) Send(ctx context.Context, message *Message) error {
	return w.SendMany(ctx, []*Message{message})
}

func (w *Wallet) SendMany(ctx context.Context, messages []*Message) error {
	var stateInit *tlb.StateInit

	block, err := w.api.GetMasterchainInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to get block: %w", err)
	}

	acc, err := w.api.GetAccount(ctx, block, w.addr)
	if err != nil {
		return fmt.Errorf("failed to get account state: %w", err)
	}

	initialized := true
	if !acc.IsActive || acc.State.Status != tlb.AccountStatusActive {
		initialized = false

		stateInit, err = GetStateInit(w.key.Public().(ed25519.PublicKey), w.ver, w.subwallet)
		if err != nil {
			return fmt.Errorf("failed to get state init: %w", err)
		}
	}

	var msg *cell.Cell
	switch w.ver {
	case V3, V4R2:
		msg, err = w.spec.(RegularBuilder).BuildRegularMessage(ctx, initialized, block, messages)
		if err != nil {
			return fmt.Errorf("build message err: %w", err)
		}
	default:
		return fmt.Errorf("send is not yet supported for wallet with this version")
	}

	err = w.api.SendExternalMessage(ctx, &tlb.ExternalMessage{
		DstAddr:   w.addr,
		StateInit: stateInit,
		Body:      msg,
	})
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

func (w *Wallet) Transfer(ctx context.Context, to *address.Address, amount tlb.Coins, comment string) error {
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

	return w.Send(ctx, &Message{
		Mode: 1,
		InternalMessage: &tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      true,
			DstAddr:     to,
			Amount:      amount,
			Body:        body,
		},
	})
}

func (w *Wallet) DeployContract(ctx context.Context, amount tlb.Coins, body, code, data *cell.Cell) (*address.Address, error) {
	state := &tlb.StateInit{
		Data: data,
		Code: code,
	}

	stateCell, err := state.ToCell()
	if err != nil {
		return nil, err
	}

	addr := address.NewAddress(0, 0, stateCell.Hash())

	if err = w.Send(ctx, &Message{
		Mode: 1,
		InternalMessage: &tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      false,
			DstAddr:     addr,
			Amount:      amount,
			Body:        body,
			StateInit:   state,
		},
	}); err != nil {
		return nil, err
	}

	return addr, nil
}
