package tlb

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func init() {
	Register(FutureSplit{})
	Register(FutureMerge{})
	Register(FutureSplitMergeNone{})

	Register(ShardStateSplit{})
	Register(ShardStateUnsplit{})
}

type ShardStateUnsplit struct {
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
	Stats        *cell.Cell `tlb:"^"`
	McStateExtra *cell.Cell `tlb:"maybe ^"`
}

type McStateExtra struct {
	_             Magic              `tlb:"#cc26"`
	ShardHashes   *cell.Dictionary   `tlb:"dict 32"`
	ConfigParams  ConfigParams       `tlb:"."`
	Info          *cell.Cell         `tlb:"^"`
	GlobalBalance CurrencyCollection `tlb:"."`
}

/*
flags:(## 16) { flags <= 1 }
     validator_info:ValidatorInfo
     prev_blocks:OldMcBlocksInfo
     after_key_block:Bool
     last_key_block:(Maybe ExtBlkRef)
     block_create_stats:(flags . 0)?BlockCreateStats

validator_info$_
  validator_list_hash_short:uint32
  catchain_seqno:uint32
  nx_cc_updated:Bool
= ValidatorInfo;

ext_blk_ref$_ end_lt:uint64
  seq_no:uint32 root_hash:bits256 file_hash:bits256
  = ExtBlkRef;

_ key:Bool max_end_lt:uint64 = KeyMaxLt;
_ key:Bool blk_ref:ExtBlkRef = KeyExtBlkRef;

*/

type KeyExtBlkRef struct {
	IsKey  bool      `tlb:"bool"`
	BlkRef ExtBlkRef `tlb:"."`
}

type KeyMaxLt struct {
	IsKey    bool   `tlb:"bool"`
	MaxEndLT uint64 `tlb:"## 64"`
}

type ValidatorInfo struct {
	ValidatorListHashShort uint32 `tlb:"## 32"`
	CatchainSeqno          uint32 `tlb:"## 32"`
	NextCCUpdated          bool   `tlb:"bool"`
}

type McStateExtraBlockInfo struct {
	Flags            uint16           `tlb:"## 16"`
	ValidatorInfo    ValidatorInfo    `tlb:"."`
	PrevBlocks       *cell.Dictionary `tlb:"dict 32"`
	LastKeyBlock     *ExtBlkRef       `tlb:"maybe ."`
	BlockCreateStats *cell.Cell       `tlb:"."`
}

type ConfigParams struct {
	ConfigAddr []byte `tlb:"bits 256"`
	Config     struct {
		Params *cell.Dictionary `tlb:"dict inline 32"`
	} `tlb:"^"`
}

type ShardStateSplit struct {
	_     Magic             `tlb:"#5f327da5"`
	Left  ShardStateUnsplit `tlb:"^"`
	Right ShardStateUnsplit `tlb:"^"`
}

type ShardIdent struct {
	_           Magic  `tlb:"$00"`
	PrefixBits  int8   `tlb:"## 6"` // #<= 60
	WorkchainID int32  `tlb:"## 32"`
	ShardPrefix uint64 `tlb:"## 64"`
}

type FutureSplitMergeNone struct {
	_ Magic `tlb:"$0"`
}

type FutureSplit struct {
	_          Magic  `tlb:"$10"`
	SplitUtime uint32 `tlb:"## 32"`
	Interval   uint32 `tlb:"## 32"`
}

type FutureMerge struct {
	_          Magic  `tlb:"$11"`
	MergeUtime uint32 `tlb:"## 32"`
	Interval   uint32 `tlb:"## 32"`
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
	SplitMergeAt       any    `tlb:"[FutureMerge,FutureSplit,FutureSplitMergeNone]"`
	Currencies         struct {
		FeesCollected CurrencyCollection `tlb:"."`
		FundsCreated  CurrencyCollection `tlb:"."`
	} `tlb:"^"`
}

type ShardDescB struct {
	_                  Magic              `tlb:"#b"`
	SeqNo              uint32             `tlb:"## 32"`
	RegMcSeqno         uint32             `tlb:"## 32"`
	StartLT            uint64             `tlb:"## 64"`
	EndLT              uint64             `tlb:"## 64"`
	RootHash           []byte             `tlb:"bits 256"`
	FileHash           []byte             `tlb:"bits 256"`
	BeforeSplit        bool               `tlb:"bool"`
	BeforeMerge        bool               `tlb:"bool"`
	WantSplit          bool               `tlb:"bool"`
	WantMerge          bool               `tlb:"bool"`
	NXCCUpdated        bool               `tlb:"bool"`
	Flags              uint8              `tlb:"## 3"`
	NextCatchainSeqNo  uint32             `tlb:"## 32"`
	NextValidatorShard int64              `tlb:"## 64"`
	MinRefMcSeqNo      uint32             `tlb:"## 32"`
	GenUTime           uint32             `tlb:"## 32"`
	SplitMergeAt       any                `tlb:"[FutureMerge,FutureSplit,FutureSplitMergeNone]"`
	FeesCollected      CurrencyCollection `tlb:"."`
	FundsCreated       CurrencyCollection `tlb:"."`
}
