package wallet

import (
	"context"
	"errors"
	"fmt"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type RegularBuilder interface {
	BuildRegularMessage(ctx context.Context, isInitialized bool, block *tlb.BlockInfo, messages []*Message) (*cell.Cell, error)
}

type SpecRegular struct {
	wallet *Wallet

	// TTL of the messages that were sent from this wallet.
	// Applicable to non highload wallets only
	// In normal cases it is not needed, as I know it can only
	// expire transaction if it not confirms too long.
	// use SetMessagesTTL if you want to change.
	messagesTTL uint32
}

func (s *SpecRegular) SetMessagesTTL(ttl uint32) {
	s.messagesTTL = ttl
}

func (s *SpecRegular) BuildRegularMessage(ctx context.Context, isInitialized bool, block *tlb.BlockInfo, messages []*Message) (*cell.Cell, error) {
	if len(messages) > 4 {
		return nil, errors.New("for this type of wallet max 4 messages can be sent in the same time")
	}

	var seq uint64

	if isInitialized {
		resp, err := s.wallet.api.RunGetMethod(ctx, block, s.wallet.addr, "seqno")
		if err != nil {
			return nil, fmt.Errorf("get seqno err: %w", err)
		}

		var ok bool
		seq, ok = resp[0].(uint64)
		if !ok {
			return nil, fmt.Errorf("seqno is not an integer")
		}
	}

	payload := cell.BeginCell().MustStoreUInt(uint64(s.wallet.subwallet), 32).
		MustStoreUInt(uint64(s.messagesTTL), 32).
		MustStoreUInt(seq, 32)

	for i, message := range messages {
		intMsg, err := message.InternalMessage.ToCell()
		if err != nil {
			return nil, fmt.Errorf("failed to convert internal message %d to cell: %w", i, err)
		}

		payload.MustStoreUInt(uint64(message.Mode), 8).MustStoreRef(intMsg)
	}

	sign := payload.EndCell().Sign(s.wallet.key)
	msg := cell.BeginCell().MustStoreSlice(sign, 512).MustStoreBuilder(payload).EndCell()

	return msg, nil
}
