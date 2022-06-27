package tlb

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type ShardState struct {
	_               Magic      `tlb:"#9023afe2"`
	GlobalID        int32      `tlb:"## 32"`
	ShardIdent      ShardIdent `tlb:"."`
	Seqno           uint32     `tlb:"## 32"`
	OutMsgQueueInfo *cell.Cell `tlb:"^"`
	Accounts        struct {
		ShardAccounts *cell.Dictionary `tlb:"maybe ^dict 256"`
	} `tlb:"^"`
}

type ShardIdent struct {
	PrefixBits  int8   `tlb:"## 6"`
	WorkchainID int32  `tlb:"## 32"`
	ShardPrefix uint64 `tlb:"## 64"`
}

/*
shard_descr_new#a seq_no:uint32 reg_mc_seqno:uint32
  start_lt:uint64 end_lt:uint64
  root_hash:bits256 file_hash:bits256
  before_split:Bool before_merge:Bool
  want_split:Bool want_merge:Bool
  nx_cc_updated:Bool flags:(## 3) { flags = 0 }
  next_catchain_seqno:uint32 next_validator_shard:uint64
  min_ref_mc_seqno:uint32 gen_utime:uint32
  split_merge_at:FutureSplitMerge
  ^[ fees_collected:CurrencyCollection
     funds_created:CurrencyCollection ] = ShardDescr;
*/

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
	NextValidatorShard uint64 `tlb:"## 64"`
	MinRefMcSeqNo      uint32 `tlb:"## 32"`
	GenUTime           uint32 `tlb:"## 32"`
}
