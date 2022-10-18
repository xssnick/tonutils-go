package tlb

import (
	"fmt"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type ShardState struct {
	_               Magic      `tlb:"#9023afe2"`
	GlobalID        int32      `tlb:"## 32"`
	ShardIdent      ShardIdent `tlb:"."`
	Seqno           uint32     `tlb:"## 32"`
	VertSeqno       uint32     `tlb:"## 32"`
	GenUTime        uint32     `tlb:"## 32"`
	GenLT           uint64     `tlb:"## 64"`
	MinRefMCSeqno   uint32     `tlb:"## 32"`
	OutMsgQueueInfo *cell.Cell `tlb:"^"`
	BeforeSplit     bool       `tlb:"bool"`
	Accounts        struct {
		ShardAccounts *cell.Dictionary `tlb:"dict 256"`
	} `tlb:"^"`
	Stats        *cell.Cell    `tlb:"^"`
	McStateExtra *McStateExtra `tlb:"maybe ^"`
}

type McStateExtra struct {
	_             Magic              `tlb:"#cc26"`
	ShardHashes   *cell.Dictionary   `tlb:"dict 32"`
	ConfigParams  ConfigParams       `tlb:"."`
	Info          *cell.Cell         `tlb:"^"`
	GlobalBalance CurrencyCollection `tlb:"."`
}

type ConfigParams struct {
	ConfigAddr *address.Address
	Config     *cell.Dictionary
}

type ShardIdent struct {
	_           Magic  `tlb:"$00"`
	PrefixBits  int8   `tlb:"## 6"`
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

func (p *ConfigParams) LoadFromCell(loader *cell.Slice) error {
	addrBits, err := loader.LoadSlice(256)
	if err != nil {
		return fmt.Errorf("failed to load bits of config addr: %w", err)
	}

	dictRef, err := loader.LoadRef()
	if err != nil {
		return fmt.Errorf("failed to load config dict ref: %w", err)
	}

	dict, err := dictRef.ToDict(32)
	if err != nil {
		return fmt.Errorf("failed to load config dict: %w", err)
	}

	p.ConfigAddr = address.NewAddress(0, 255, addrBits)
	p.Config = dict

	return nil
}
