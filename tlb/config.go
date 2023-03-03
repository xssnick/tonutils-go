package tlb

import "github.com/xssnick/tonutils-go/tvm/cell"

type ValidatorSet struct {
	_          Magic            `tlb:"#11"`
	UTimeSince uint32           `tlb:"## 32"`
	UTimeUntil uint32           `tlb:"## 32"`
	Total      uint16           `tlb:"## 16"`
	Main       uint16           `tlb:"## 16"`
	List       *cell.Dictionary `tlb:"dict 16"`
}

type ValidatorSetExt struct {
	_           Magic            `tlb:"#12"`
	UTimeSince  uint32           `tlb:"## 32"`
	UTimeUntil  uint32           `tlb:"## 32"`
	Total       uint16           `tlb:"## 16"`
	Main        uint16           `tlb:"## 16"`
	TotalWeight uint64           `tlb:"## 64"`
	List        *cell.Dictionary `tlb:"dict 16"`
}

type Validator struct {
	_         Magic            `tlb:"#53"`
	PublicKey SigPubKeyED25519 `tlb:"."`
	Weight    uint64           `tlb:"## 64"`
}

type ValidatorAddr struct {
	_         Magic            `tlb:"#73"`
	PublicKey SigPubKeyED25519 `tlb:"."`
	Weight    uint64           `tlb:"## 64"`
	ADNLAddr  []byte           `tlb:"bits 256"`
}

type SigPubKeyED25519 struct {
	_   Magic  `tlb:"#8e81278a"`
	Key []byte `tlb:"bits 256"`
}
