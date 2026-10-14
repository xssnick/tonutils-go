package wallet

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// https://github.com/ton-blockchain/tg-wallet-contract/blob/master/contracts/WalletTrampoline.boc
// Only the immutable trampoline is deployed; the implementation is loaded from config[-123].
const _TelegramCodeHex = "b5ee9c7241010101001a000030ff00209821d7498308b9f240df8085f833d0ed1e20ed53d969427e39"

const (
	// TelegramMainnetSubwallet is the default subwallet ID for Telegram wallets.
	TelegramMainnetSubwallet uint32 = 0x7FFF7F11
	// TelegramTestnetSubwallet can be selected with Wallet.GetSubwallet on testnet.
	TelegramTestnetSubwallet uint32 = 0x7FFF7FFD
)

type SpecTelegram struct {
	SpecRegular
	SpecSeqno
}

// TelegramWalletData is the revision 0 storage layout.
type TelegramWalletData struct {
	Revision    uint8  `tlb:"## 8"`
	Seqno       uint32 `tlb:"## 32"`
	SubwalletID uint32 `tlb:"## 32"`
	PublicKey   []byte `tlb:"bits 256"`
}

// TelegramSeqnoHeader identifies the subwallet, request expiry and sequence number.
type TelegramSeqnoHeader struct {
	SubwalletID uint32 `tlb:"## 32"`
	ValidUntil  uint32 `tlb:"## 32"`
	Seqno       uint32 `tlb:"## 32"`
}

// TelegramMessageToSend describes an outgoing message and its send mode.
type TelegramMessageToSend struct {
	SendMode uint8                `tlb:"## 8"`
	Message  *tlb.InternalMessage `tlb:"^"`
}

// TelegramSendOneRequest is an external request to send one message.
// Its prefix is explicitly defined in the contract's messages.tolk.
// https://github.com/ton-blockchain/tg-wallet-contract/blob/master/contracts/WalletTg/messages.tolk
type TelegramSendOneRequest struct {
	_       tlb.Magic             `tlb:"#63896e75"`
	Header  TelegramSeqnoHeader   `tlb:"."`
	Message TelegramMessageToSend `tlb:"."`
}

// TelegramSendBulkRequest is an external request to send multiple messages.
type TelegramSendBulkRequest struct {
	_        tlb.Magic               `tlb:"#73896e75"`
	Header   TelegramSeqnoHeader     `tlb:"."`
	Messages []TelegramMessageToSend `tlb:"tolk_array ."`
}

// TelegramSignedRequest stores a signature followed by the inline request.
type TelegramSignedRequest struct {
	Signature []byte     `tlb:"bits 512"`
	Request   *cell.Cell `tlb:"."`
}

func (s *SpecTelegram) BuildMessage(ctx context.Context, _ bool, _ *ton.BlockIDExt, messages []*Message) (*cell.Cell, error) {
	if len(messages) == 0 || len(messages) > 255 {
		return nil, errors.New("Telegram wallet requires between 1 and 255 messages")
	}

	toSend := make([]TelegramMessageToSend, len(messages))
	for i, message := range messages {
		if message.Mode&IgnoreErrors == 0 {
			return nil, fmt.Errorf("Telegram wallet message %d must have IgnoreErrors send mode", i)
		}

		toSend[i] = TelegramMessageToSend{SendMode: message.Mode, Message: message.InternalMessage}
	}

	seq, err := s.seqnoFetcher(ctx, s.wallet.subwallet)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch seqno: %w", err)
	}

	header := TelegramSeqnoHeader{
		SubwalletID: s.wallet.subwallet,
		ValidUntil:  uint32(timeNow().Add(time.Duration(s.messagesTTL) * time.Second).Unix()),
		Seqno:       seq,
	}

	var request *cell.Cell
	if len(messages) == 1 {
		request, err = tlb.ToCell(TelegramSendOneRequest{Header: header, Message: toSend[0]})
	} else {
		request, err = tlb.ToCell(TelegramSendBulkRequest{Header: header, Messages: toSend})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to build Telegram wallet request: %w", err)
	}

	signature, err := s.wallet.signer(ctx, request, s.wallet.subwallet)
	if err != nil {
		return nil, fmt.Errorf("failed to sign: %w", err)
	}

	return tlb.ToCell(TelegramSignedRequest{Signature: signature, Request: request})
}
