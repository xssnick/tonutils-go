package wallet

import (
	"crypto/ed25519"
	"github.com/xssnick/tonutils-go/tlb"
)

type ConfigCustom interface {
	GetStateInit(pubKey ed25519.PublicKey, subWallet uint32) (*tlb.StateInit, error)
	GetSpec(w *Wallet) RegularBuilder
}
