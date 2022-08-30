package wallet

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type Version int

const (
	V3           Version = 3
	V4R2         Version = 42
	HighloadV2R2 Version = 122
)

// defining some funcs this way to mock for tests
var randUint32 = rand.Uint32
var timeNow = time.Now

var ErrTxWasNotConfirmed = errors.New("transaction was not confirmed in a given deadline, but it may still be confirmed later")
var ErrTxWasNotFound = errors.New("requested transaction is not found")

type TonAPI interface {
	CurrentMasterchainInfo(ctx context.Context) (*tlb.BlockInfo, error)
	GetAccount(ctx context.Context, block *tlb.BlockInfo, addr *address.Address) (*tlb.Account, error)
	SendExternalMessage(ctx context.Context, msg *tlb.ExternalMessage) error
	RunGetMethod(ctx context.Context, blockInfo *tlb.BlockInfo, addr *address.Address, method string, params ...interface{}) ([]interface{}, error)
	ListTransactions(ctx context.Context, addr *address.Address, num uint32, lt uint64, txHash []byte) ([]*tlb.Transaction, error)
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

	w.spec, err = getSpec(w)
	if err != nil {
		return nil, err
	}

	return w, nil
}

func getSpec(w *Wallet) (any, error) {
	regular := SpecRegular{
		wallet:      w,
		messagesTTL: 60 * 3, // default ttl 3 min
	}

	switch w.ver {
	case V3:
		return &SpecV3{regular}, nil
	case V4R2:
		return &SpecV4R2{regular}, nil
	case HighloadV2R2:
		return &SpecHighloadV2R2{regular}, nil
	}

	return nil, errors.New("cannot init spec: unknown version")
}

func (w *Wallet) Address() *address.Address {
	return w.addr
}

func (w *Wallet) PrivateKey() ed25519.PrivateKey {
	return w.key
}

func (w *Wallet) GetSubwallet(subwallet uint32) (*Wallet, error) {
	addr, err := AddressFromPubKey(w.key.Public().(ed25519.PublicKey), w.ver, subwallet)
	if err != nil {
		return nil, err
	}

	sub := &Wallet{
		api:       w.api,
		key:       w.key,
		addr:      addr,
		ver:       w.ver,
		subwallet: subwallet,
	}

	sub.spec, err = getSpec(sub)
	if err != nil {
		return nil, err
	}

	return sub, nil
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

func (w *Wallet) BuildMessageForMany(ctx context.Context, messages []*Message) (*tlb.ExternalMessage, error) {
	var stateInit *tlb.StateInit

	block, err := w.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	acc, err := w.api.GetAccount(ctx, block, w.addr)
	if err != nil {
		return nil, fmt.Errorf("failed to get account state: %w", err)
	}

	initialized := true
	if !acc.IsActive || acc.State.Status != tlb.AccountStatusActive {
		initialized = false

		stateInit, err = GetStateInit(w.key.Public().(ed25519.PublicKey), w.ver, w.subwallet)
		if err != nil {
			return nil, fmt.Errorf("failed to get state init: %w", err)
		}
	}

	var msg *cell.Cell
	switch w.ver {
	case V3, V4R2:
		msg, err = w.spec.(RegularBuilder).BuildMessage(ctx, initialized, block, messages)
		if err != nil {
			return nil, fmt.Errorf("build message err: %w", err)
		}
	case HighloadV2R2:
		msg, err = w.spec.(*SpecHighloadV2R2).BuildMessage(ctx, randUint32(), messages)
		if err != nil {
			return nil, fmt.Errorf("build message err: %w", err)
		}
	default:
		return nil, fmt.Errorf("send is not yet supported for wallet with this version")
	}

	return &tlb.ExternalMessage{
		DstAddr:   w.addr,
		StateInit: stateInit,
		Body:      msg,
	}, nil
}

func (w *Wallet) Send(ctx context.Context, message *Message, waitConfirmation ...bool) error {
	return w.SendMany(ctx, []*Message{message}, waitConfirmation...)
}

func (w *Wallet) SendMany(ctx context.Context, messages []*Message, waitConfirmation ...bool) error {
	_, _, err := w.sendMany(ctx, messages, waitConfirmation...)
	return err
}

// SendManyGetInMsgHash returns hash of external incoming message payload.
func (w *Wallet) SendManyGetInMsgHash(ctx context.Context, messages []*Message, waitConfirmation ...bool) ([]byte, error) {
	_, inMsgHash, err := w.sendMany(ctx, messages, waitConfirmation...)
	return inMsgHash, err
}

// SendManyWaitTxHash always waits for tx block confirmation and returns found tx hash in block.
func (w *Wallet) SendManyWaitTxHash(ctx context.Context, messages []*Message) ([]byte, error) {
	txHash, _, err := w.sendMany(ctx, messages, true)
	return txHash, err
}

func (w *Wallet) sendMany(ctx context.Context, messages []*Message, waitConfirmation ...bool) (txHash []byte, inMsgHash []byte, err error) {
	block, err := w.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get block: %w", err)
	}

	acc, err := w.api.GetAccount(ctx, block, w.addr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get account state: %w", err)
	}

	ext, err := w.BuildMessageForMany(ctx, messages)
	if err != nil {
		return nil, nil, err
	}
	inMsgHash = ext.Body.Hash()

	if err = w.api.SendExternalMessage(ctx, ext); err != nil {
		return nil, nil, fmt.Errorf("failed to send message: %w", err)
	}

	if len(waitConfirmation) > 0 && waitConfirmation[0] {
		txHash, err = w.waitConfirmation(ctx, block, acc, ext)
		if err != nil {
			return nil, nil, err
		}
	}

	return txHash, inMsgHash, nil
}

func (w *Wallet) waitConfirmation(ctx context.Context, block *tlb.BlockInfo, acc *tlb.Account, ext *tlb.ExternalMessage) ([]byte, error) {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		// fallback timeout to not stuck forever with background context
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
	}
	till, _ := ctx.Deadline()

	for time.Now().Before(till) {
		time.Sleep(1 * time.Second)
		blockNew, err := w.api.CurrentMasterchainInfo(ctx)
		if err != nil {
			continue
		}

		if blockNew.SeqNo == block.SeqNo {
			continue
		}

		accNew, err := w.api.GetAccount(ctx, block, w.addr)
		if err != nil {
			continue
		}
		block = blockNew

		if accNew.LastTxLT == acc.LastTxLT {
			// if not in block, maybe LS lost our message, send it again
			if err = w.api.SendExternalMessage(ctx, ext); err != nil {
				continue
			}

			continue
		}

		lastLt, lastHash := accNew.LastTxLT, accNew.LastTxHash

		// it is possible that > 5 new not related transactions will happen, and we should not lose our scan offset,
		// to prevent this we will scan till we reach last seen offset.
		for time.Now().Before(till) {
			// we try to get last 5 transactions, and check if we have our new there.
			txList, err := w.api.ListTransactions(ctx, w.addr, 5, lastLt, lastHash)
			if err != nil {
				continue
			}

			sawLastTx := false
			for i, transaction := range txList {
				if i == 0 {
					// get previous of the oldest tx, in case if we need to scan deeper
					lastLt, lastHash = txList[0].PrevTxLT, txList[0].PrevTxHash
				}

				if !sawLastTx && transaction.PrevTxLT == acc.LastTxLT &&
					bytes.Equal(transaction.PrevTxHash, acc.LastTxHash) {
					sawLastTx = true
				}

				if transaction.IO.In != nil && transaction.IO.In.MsgType == tlb.MsgTypeExternalIn {
					extIn := transaction.IO.In.AsExternalIn()
					if ext.StateInit != nil {
						if extIn.StateInit == nil {
							continue
						}

						if !bytes.Equal(ext.StateInit.Data.Hash(), extIn.StateInit.Data.Hash()) {
							continue
						}

						if !bytes.Equal(ext.StateInit.Code.Hash(), extIn.StateInit.Code.Hash()) {
							continue
						}
					}

					if !bytes.Equal(extIn.Body.Hash(), ext.Body.Hash()) {
						continue
					}

					return transaction.Hash, nil
				}
			}

			if sawLastTx {
				break
			}
		}
		acc = accNew
	}

	return nil, ErrTxWasNotConfirmed
}

// TransferNoBounce - can be used to transfer TON to not yet initialized contract/wallet
func (w *Wallet) TransferNoBounce(ctx context.Context, to *address.Address, amount tlb.Coins, comment string, waitConfirmation ...bool) error {
	return w.transfer(ctx, to, amount, comment, false, waitConfirmation...)
}

// Transfer - safe transfer, in case of error on smart contract side, you will get coins back,
// cannot be used to transfer TON to not yet initialized contract/wallet
func (w *Wallet) Transfer(ctx context.Context, to *address.Address, amount tlb.Coins, comment string, waitConfirmation ...bool) error {
	return w.transfer(ctx, to, amount, comment, true, waitConfirmation...)
}

func (w *Wallet) transfer(ctx context.Context, to *address.Address, amount tlb.Coins, comment string, bounce bool, waitConfirmation ...bool) error {
	var body *cell.Cell
	if comment != "" {
		// comment ident
		root := cell.BeginCell().MustStoreUInt(0, 32)

		if err := root.StoreStringSnake(comment); err != nil {
			return fmt.Errorf("failed to build comment: %w", err)
		}

		body = root.EndCell()
	}

	return w.Send(ctx, &Message{
		Mode: 1,
		InternalMessage: &tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      bounce,
			DstAddr:     to,
			Amount:      amount,
			Body:        body,
		},
	}, waitConfirmation...)
}

func (w *Wallet) DeployContract(ctx context.Context, amount tlb.Coins, msgBody, contractCode, contractData *cell.Cell, waitConfirmation ...bool) (*address.Address, error) {
	state := &tlb.StateInit{
		Data: contractData,
		Code: contractCode,
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
			Body:        msgBody,
			StateInit:   state,
		},
	}, waitConfirmation...); err != nil {
		return nil, err
	}

	return addr, nil
}

// FindTransactionByInMsgHash returns transaction in wallet account with incoming message hash equal to msgHash.
func (w *Wallet) FindTransactionByInMsgHash(ctx context.Context, msgHash []byte) (*tlb.Transaction, error) {
	block, err := w.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot get masterchain info: %w", err)
	}

	acc, err := w.api.GetAccount(ctx, block, w.addr)
	if err != nil {
		return nil, fmt.Errorf("cannot get account: %w", err)
	}
	if !acc.IsActive { // no tx is made from this account
		return nil, fmt.Errorf("account is inactive: %w", ErrTxWasNotFound)
	}

	for lastLt, lastHash := acc.LastTxLT, acc.LastTxHash; ; {
		if lastLt == 0 { // no older transactions
			return nil, ErrTxWasNotFound
		}

		txList, err := w.api.ListTransactions(ctx, w.addr, 15, lastLt, lastHash)
		if err != nil && strings.Contains(err.Error(), "cannot compute block with specified transaction: lt not in db") {
			return nil, fmt.Errorf("archive node is needed: %w", ErrTxWasNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("cannot list transactions: %w", err)
		}

		for i, transaction := range txList {
			if i == 0 {
				// get previous of the oldest tx, in case if we need to scan deeper
				lastLt, lastHash = txList[0].PrevTxLT, txList[0].PrevTxHash
			}

			if transaction.IO.In == nil {
				continue
			}
			if !bytes.Equal(transaction.IO.In.Msg.Payload().Hash(), msgHash) {
				continue
			}

			return transaction, nil
		}
	}
}

func SimpleMessage(to *address.Address, amount tlb.Coins, payload *cell.Cell) *Message {
	return &Message{
		Mode: 1,
		InternalMessage: &tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      true,
			DstAddr:     to,
			Amount:      amount,
			Body:        payload,
		},
	}
}
