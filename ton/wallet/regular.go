package wallet

import (
	"context"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type RegularBuilder interface {
	BuildMessage(ctx context.Context, isInitialized bool, _ *ton.BlockIDExt, messages []*Message) (*cell.Cell, error)
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

type SpecSeqno struct {
	// Instead of calling contract 'seqno' method,
	// this function wil be used (if not nil) to get seqno for new transaction.
	// You may use it to set seqno according to your own logic,
	// for example for additional idempotency,
	// if build message is not enough, or for additional security
	seqnoFetcher func(ctx context.Context, subWallet uint32) (uint32, error)
}

// Deprecated: Use SetSeqnoFetcher
func (s *SpecSeqno) SetCustomSeqnoFetcher(fetcher func() uint32) {
	s.seqnoFetcher = func(ctx context.Context, subWallet uint32) (uint32, error) {
		return fetcher(), nil
	}
}

func (s *SpecSeqno) SetSeqnoFetcher(fetcher func(ctx context.Context, subWallet uint32) (uint32, error)) {
	s.seqnoFetcher = fetcher
}

type SpecQuery struct {
	// Instead of generating random query id with message ttl,
	// this function wil be used (if not nil) to get query id for new transaction.
	// You may use it to set query id according to your own logic,
	// for example for additional idempotency,
	// if build message is not enough, or for additional security
	//
	// Do not set ttl to high if you are sending many messages,
	// unexpired executed messages will be cached in contract,
	// and it may become too expensive to make transactions.
	customQueryIDFetcher func(ctx context.Context, subWalletId uint32) (ttl uint32, randPart uint32, err error)
}

func (s *SpecQuery) SetCustomQueryIDFetcher(fetcher func() (ttl uint32, randPart uint32)) {
	s.SetCustomQueryIDFetcherWithContext(func(ctx context.Context, subWalletId uint32) (ttl uint32, randPart uint32, err error) {
		ttl, randPart = fetcher()
		return ttl, randPart, nil
	})
}

func (s *SpecQuery) SetCustomQueryIDFetcherWithContext(fetcher func(ctx context.Context, subWalletId uint32) (ttl uint32, randPart uint32, err error)) {
	s.customQueryIDFetcher = fetcher
}
