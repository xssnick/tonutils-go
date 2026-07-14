package tlb

import (
	"encoding/binary"
	"fmt"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func init() {
	Register(FutureSplit{})
	Register(FutureMerge{})
	Register(FutureSplitMergeNone{})

	Register(ShardStateSplit{})
	Register(ShardStateUnsplit{})
}

type ShardID uint64

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
		ShardAccounts *ShardAccountsAugDict `tlb:"."`
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
	Flags            uint16                  `tlb:"## 16"`
	ValidatorInfo    ValidatorInfo           `tlb:"."`
	PrevBlocks       *OldMcBlocksInfoAugDict `tlb:"."`
	AfterKeyBlock    bool                    `tlb:"bool"`
	LastKeyBlock     *ExtBlkRef              `tlb:"maybe ."`
	BlockCreateStats *cell.Cell              `tlb:"-"`
}

func (e *McStateExtraBlockInfo) LoadFromCell(loader *cell.Slice) error {
	return e.loadFromCell(loader, false)
}

func (e *McStateExtraBlockInfo) LoadFromCellAsProof(loader *cell.Slice) error {
	return e.loadFromCell(loader, true)
}

func (e *McStateExtraBlockInfo) loadFromCell(loader *cell.Slice, asProof bool) error {
	flags, err := loader.LoadUInt(16)
	if err != nil {
		return err
	}
	e.Flags = uint16(flags)
	if e.Flags&^1 != 0 {
		return fmt.Errorf("unsupported masterchain state extra flags: %d", e.Flags)
	}

	if err = LoadFromCell(&e.ValidatorInfo, loader); err != nil {
		return err
	}

	var prevBlocks OldMcBlocksInfoAugDict
	if asProof {
		err = prevBlocks.LoadFromCellAsProof(loader)
	} else {
		err = prevBlocks.LoadFromCell(loader)
	}
	if err != nil {
		return err
	}
	e.PrevBlocks = &prevBlocks

	e.AfterKeyBlock, err = loader.LoadBoolBit()
	if err != nil {
		return err
	}

	hasLastKeyBlock, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}
	if hasLastKeyBlock {
		var lastKeyBlock ExtBlkRef
		if err = LoadFromCell(&lastKeyBlock, loader); err != nil {
			return err
		}
		e.LastKeyBlock = &lastKeyBlock
	} else {
		e.LastKeyBlock = nil
	}

	if e.Flags&1 == 0 {
		e.BlockCreateStats = nil
		return nil
	}

	e.BlockCreateStats, err = loader.ToCell()
	return err
}

func (e McStateExtraBlockInfo) ToCell() (*cell.Cell, error) {
	flags := e.Flags
	if flags&^1 != 0 {
		return nil, fmt.Errorf("unsupported masterchain state extra flags: %d", flags)
	}
	if e.BlockCreateStats == nil {
		flags &^= 1
	} else {
		flags |= 1
	}

	b := cell.BeginCell()
	if err := b.StoreUInt(uint64(flags), 16); err != nil {
		return nil, err
	}

	validatorInfo, err := ToCell(e.ValidatorInfo)
	if err != nil {
		return nil, err
	}
	if err = b.StoreBuilder(validatorInfo.ToBuilder()); err != nil {
		return nil, err
	}

	prevBlocks, err := ToCell(e.PrevBlocks)
	if err != nil {
		return nil, err
	}
	if err = b.StoreBuilder(prevBlocks.ToBuilder()); err != nil {
		return nil, err
	}

	if err = b.StoreBoolBit(e.AfterKeyBlock); err != nil {
		return nil, err
	}

	if e.LastKeyBlock == nil {
		if err = b.StoreBoolBit(false); err != nil {
			return nil, err
		}
	} else {
		if err = b.StoreBoolBit(true); err != nil {
			return nil, err
		}

		lastKeyBlock, err := ToCell(e.LastKeyBlock)
		if err != nil {
			return nil, err
		}
		if err = b.StoreBuilder(lastKeyBlock.ToBuilder()); err != nil {
			return nil, err
		}
	}

	if e.BlockCreateStats != nil {
		if err = b.StoreBuilder(e.BlockCreateStats.ToBuilder()); err != nil {
			return nil, err
		}
	}

	return b.EndCell(), nil
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

func ShardChild(shard uint64, left bool) uint64 {
	x := lowerBit64(shard) >> 1
	if left {
		return shard - x
	}
	return shard + x
}

func ShardParent(shard uint64) uint64 {
	x := lowerBit64(shard)
	return (shard - x) | (x << 1)
}

func lowerBit64(x uint64) uint64 {
	return x & bitsNegate64(x)
}

func bitsNegate64(x uint64) uint64 {
	return ^x + 1
}

func (s ShardID) IsSibling(with ShardID) bool {
	return (s^with) != 0 && ((s ^ with) == ((s & ShardID(bitsNegate64(uint64(s)))) << 1))
}

func (s ShardID) IsParent(of ShardID) bool {
	y := lowerBit64(uint64(s))
	return y > 0 && of.GetParent() == s
}

func (s ShardID) GetParent() ShardID {
	y := lowerBit64(uint64(s))
	return ShardID((uint64(s) - y) | (y << 1))
}

func (s ShardID) GetChild(left bool) ShardID {
	y := lowerBit64(uint64(s)) >> 1
	if left {
		return s - ShardID(y)
	}
	return s + ShardID(y)
}

func (s ShardID) ContainsAddress(addr *address.Address) bool {
	x := lowerBit64(uint64(s))
	return ((uint64(s) ^ binary.BigEndian.Uint64(addr.Data())) & (bitsNegate64(x) << 1)) == 0
}

func (s ShardID) IsAncestor(of ShardID) bool {
	x := lowerBit64(uint64(s))
	y := lowerBit64(uint64(of))
	return x >= y && uint64(s^of)&(bitsNegate64(x)<<1) == 0
}

func (s ShardIdent) IsSibling(with ShardIdent) bool {
	return s.WorkchainID == with.WorkchainID && ShardID(s.ShardPrefix).IsSibling(ShardID(with.ShardPrefix))
}

func (s ShardIdent) IsAncestor(of ShardIdent) bool {
	return s.WorkchainID == of.WorkchainID && ShardID(s.ShardPrefix).IsAncestor(ShardID(of.ShardPrefix))
}

func (s ShardIdent) IsParent(of ShardIdent) bool {
	return s.WorkchainID == of.WorkchainID && ShardID(s.ShardPrefix).IsParent(ShardID(of.ShardPrefix))
}

func (s ShardIdent) GetShardID() ShardID {
	if s.PrefixBits > 63 {
		return ShardID(0)
	}
	return ShardID(s.ShardPrefix | 1<<(63-s.PrefixBits))
}
