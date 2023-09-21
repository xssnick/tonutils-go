package payments

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"time"
)

type TonApi interface {
	WaitForBlock(seqno uint32) ton.APIClientWrapped
	CurrentMasterchainInfo(ctx context.Context) (_ *ton.BlockIDExt, err error)
	RunGetMethod(ctx context.Context, blockInfo *ton.BlockIDExt, addr *address.Address, method string, params ...any) (*ton.ExecutionResult, error)
	SendExternalMessage(ctx context.Context, msg *tlb.ExternalMessage) error
	GetAccount(ctx context.Context, block *ton.BlockIDExt, addr *address.Address) (*tlb.Account, error)
}

type Client struct {
	api TonApi
}

type ChannelStatus int8

const (
	ChannelStatusUninitialized ChannelStatus = iota
	ChannelStatusOpen
	ChannelStatusClosureStarted
	ChannelStatusSettlingConditionals
	ChannelStatusAwaitingFinalization
)

type AsyncChannel struct {
	Status  ChannelStatus
	Storage AsyncChannelStorageData
	addr    *address.Address
	client  *Client
}

type ChannelID []byte

func NewPaymentChannelClient(api TonApi) *Client {
	return &Client{
		api: api,
	}
}

var ErrVerificationNotPassed = fmt.Errorf("verification not passed")

func (c *Client) GetAsyncChannel(ctx context.Context, block *ton.BlockIDExt, addr *address.Address, verify bool) (*AsyncChannel, error) {
	acc, err := c.api.GetAccount(ctx, block, addr)
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	if !acc.IsActive || !acc.State.IsValid || acc.State.Status != tlb.AccountStatusActive {
		return nil, fmt.Errorf("channel account is not active")
	}

	return c.ParseAsyncChannel(addr, acc.Code, acc.Data, verify)
}

func (c *Client) ParseAsyncChannel(addr *address.Address, code, data *cell.Cell, verify bool) (*AsyncChannel, error) {
	if verify {
		if !bytes.Equal(code.Hash(), AsyncPaymentChannelCodeHash) {
			return nil, ErrVerificationNotPassed
		}
	}

	ch := &AsyncChannel{
		addr:   addr,
		client: c,
		Status: ChannelStatusUninitialized,
	}

	err := tlb.LoadFromCell(&ch.Storage, data.BeginParse())
	if err != nil {
		return nil, fmt.Errorf("failed to load storage: %w", err)
	}

	if verify {
		storageData := AsyncChannelStorageData{
			KeyA:          ch.Storage.KeyA,
			KeyB:          ch.Storage.KeyB,
			ChannelID:     ch.Storage.ChannelID,
			ClosingConfig: ch.Storage.ClosingConfig,
			Payments:      ch.Storage.Payments,
		}

		data, err = tlb.ToCell(storageData)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize storage data: %w", err)
		}

		si, err := tlb.ToCell(tlb.StateInit{
			Code: AsyncPaymentChannelCode,
			Data: data,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to serialize state init: %w", err)
		}

		if !bytes.Equal(si.Hash(), ch.addr.Data()) {
			return nil, ErrVerificationNotPassed
		}
	}

	ch.Status = ch.Storage.calcState()

	return ch, nil
}

func (c *Client) GetDeployAsyncChannelParams(channelId ChannelID, isA bool, initialBalance tlb.Coins, ourKey ed25519.PrivateKey, theirKey ed25519.PublicKey, closingConfig ClosingConfig, paymentConfig PaymentConfig) (body, code, data *cell.Cell, err error) {
	if len(channelId) != 16 {
		return nil, nil, nil, fmt.Errorf("channelId len should be 16 bytes")
	}

	storageData := AsyncChannelStorageData{
		KeyA:          ourKey.Public().(ed25519.PublicKey),
		KeyB:          theirKey,
		ChannelID:     channelId,
		ClosingConfig: closingConfig,
		Payments:      paymentConfig,
	}

	if !isA {
		storageData.KeyA, storageData.KeyB = storageData.KeyB, storageData.KeyA
	}

	data, err = tlb.ToCell(storageData)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to serialize storage data: %w", err)
	}

	initCh := InitChannel{}
	initCh.IsA = isA

	if isA {
		initCh.Signed.BalanceA = initialBalance
	} else {
		initCh.Signed.BalanceB = initialBalance
	}
	initCh.Signed.ChannelID = channelId
	initCh.Signature, err = toSignature(initCh.Signed, ourKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to sign data: %w", err)
	}

	body, err = tlb.ToCell(initCh)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to serialize message: %w", err)
	}
	return body, AsyncPaymentChannelCode, data, nil
}

func (c *AsyncChannel) Address() *address.Address {
	return c.addr
}

// calcState - it repeats get_channel_state method of contract,
// we do this because we cannot prove method execution for now,
// but can proof contract data and code, so this approach is safe
func (s *AsyncChannelStorageData) calcState() ChannelStatus {
	if !s.Initialized {
		return ChannelStatusUninitialized
	}
	if s.Quarantine == nil {
		return ChannelStatusOpen
	}
	now := time.Now().Unix()
	quarantineEnds := int64(s.Quarantine.QuarantineStarts) + int64(s.ClosingConfig.QuarantineDuration)
	if quarantineEnds > now {
		return ChannelStatusClosureStarted
	}
	if quarantineEnds+int64(s.ClosingConfig.ConditionalCloseDuration) > now {
		return ChannelStatusSettlingConditionals
	}
	return ChannelStatusAwaitingFinalization
}

func toSignature(obj any, key ed25519.PrivateKey) (Signature, error) {
	toSign, err := tlb.ToCell(obj)
	if err != nil {
		return Signature{}, fmt.Errorf("failed to serialize body to sign: %w", err)
	}
	return Signature{Value: toSign.Sign(key)}, nil
}

func RandomChannelID() (ChannelID, error) {
	id := make(ChannelID, 16)
	_, err := rand.Read(id)
	if err != nil {
		return nil, err
	}
	return id, nil
}
