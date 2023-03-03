package wallet

import (
	"context"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type RegularBuilder interface {
	BuildMessage(ctx context.Context, isInitialized bool, block *ton.BlockIDExt, messages []*Message) (*cell.Cell, error)
}

type SpecRegular struct {
	wallet *Wallet

	// TTL of the messages that were sent from this wallet.
	// In normal cases it is not needed, as I know it can only
	// expire transaction if it not confirms too long.
	// use SetMessagesTTL if you want to change.
	messagesTTL uint32
}

func (s *SpecRegular) SetMessagesTTL(ttl uint32) {
	s.messagesTTL = ttl
}
