package wallet

import (
	"crypto/ed25519"
	"github.com/xssnick/tonutils-go/tlb"
)

type ConfigCustom interface {
	StateIniter
	GetSpec(w *Wallet) RegularBuilder
}

type StateIniter interface {
	GetStateInit(pubKey ed25519.PublicKey, subWallet uint32) (*tlb.StateInit, error)
}
