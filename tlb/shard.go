package tlb

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type ShardStateUnsplit struct {
	_               Magic      `tlb:"#9023afe2"`
	GlobalID        int32      `tlb:"## 32"`
	ShardIdent      ShardIdent `tlb:"."`
	Seqno           uint32     `tlb:"## 32"`
	OutMsgQueueInfo *cell.Cell `tlb:"^"`
	Accounts        struct {
		ShardAccounts *cell.Dictionary `tlb:"dict 256"`
	} `tlb:"^"`
}

type ShardState struct {
	Left  ShardStateUnsplit
	Right *ShardStateUnsplit
}

type ShardIdent struct {
	_           Magic  `tlb:"$00"`
	PrefixBits  int8   `tlb:"## 6"` // #<= 60
	WorkchainID int32  `tlb:"## 32"`
	ShardPrefix uint64 `tlb:"## 64"`
}

type ShardDesc struct {
	_                  Magic  `tlb:"#a"`
	SeqNo              uint32 `tlb:"## 32"`
	RegMcSeqno         uint32 `tlb:"## 32"`
	StartLT            uint64 `tlb:"## 64"`
	EndLT              uint64 `tlb:"## 64"`
	RootHash           []byte `tlb:"bits 256"`
	FileHash           []byte `tlb:"bits 256"`
	BeforeSplit        bool   `tlb:"bool"`
	BeforeMerge        bool   `tlb:"bool"`
	WantSplit          bool   `tlb:"bool"`
	WantMerge          bool   `tlb:"bool"`
	NXCCUpdated        bool   `tlb:"bool"`
	Flags              uint8  `tlb:"## 3"`
	NextCatchainSeqNo  uint32 `tlb:"## 32"`
	NextValidatorShard int64  `tlb:"## 64"`
	MinRefMcSeqNo      uint32 `tlb:"## 32"`
	GenUTime           uint32 `tlb:"## 32"`
}

func (s *ShardState) LoadFromCell(loader *cell.Slice) error {
	preloader := loader.Copy()
	tag, err := preloader.LoadUInt(32)
	if err != nil {
		return err
	}
	switch tag {
	case 0x5f327da5:
		var left, right ShardStateUnsplit
		leftRef, err := loader.LoadRef()
		if err != nil {
			return err
		}
		rightRef, err := loader.LoadRef()
		if err != nil {
			return err
		}
		err = LoadFromCell(&left, leftRef)
		if err != nil {
			return err
		}
		err = LoadFromCell(&right, rightRef)
		if err != nil {
			return err
		}
		s.Left = left
		s.Right = &right
	case 0x9023afe2:
		var state ShardStateUnsplit
		err = LoadFromCell(&state, loader)
		if err != nil {
			return err
		}
		s.Left = state
	}
	return nil
}
