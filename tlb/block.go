package tlb

import (
	"bytes"
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

// Deprecated: use ton.BlockIDExt
type BlockInfo struct {
	Workchain int32  `tl:"int"`
	Shard     int64  `tl:"long"`
	SeqNo     uint32 `tl:"int"`
	RootHash  []byte `tl:"int256"`
	FileHash  []byte `tl:"int256"`
}

type StateUpdate struct {
	Old any        `tlb:"^ [ShardStateUnsplit,ShardStateSplit]"`
	New *cell.Cell `tlb:"^"`
}

type McBlockExtra struct {
	_           Magic            `tlb:"#cca5"`
	KeyBlock    bool             `tlb:"bool"`
	ShardHashes *cell.Dictionary `tlb:"dict 32"`
	ShardFees   *cell.Dictionary `tlb:"dict 96"`
	Details     struct {
		PrevBlockSignatures *cell.Dictionary `tlb:"dict 16"`
		RecoverCreateMsg    *cell.Cell       `tlb:"maybe ^"`
		MintMsg             *cell.Cell       `tlb:"maybe ^"`
	} `tlb:"^"`
	ConfigParams *ConfigParams `tlb:"?KeyBlock ."`
}

type BlockExtra struct {
	_                  Magic         `tlb:"#4a33f6fd"`
	InMsgDesc          *cell.Cell    `tlb:"^"`
	OutMsgDesc         *cell.Cell    `tlb:"^"`
	ShardAccountBlocks *cell.Cell    `tlb:"^"`
	RandSeed           []byte        `tlb:"bits 256"`
	CreatedBy          []byte        `tlb:"bits 256"`
	Custom             *McBlockExtra `tlb:"maybe ^"`
}

type ShardAccountBlocks struct {
	Accounts *cell.Dictionary `tlb:"dict 256"`
}

type AccountBlock struct {
	_            Magic            `tlb:"#5"`
	Addr         []byte           `tlb:"bits 256"`
	Transactions *cell.Dictionary `tlb:"dict inline 64"`
	StateUpdate  *cell.Cell       `tlb:"^"`
}

type Block struct {
	_           Magic       `tlb:"#11ef55aa"`
	GlobalID    int32       `tlb:"## 32"`
	BlockInfo   BlockHeader `tlb:"^"`
	ValueFlow   *cell.Cell  `tlb:"^"`
	StateUpdate *cell.Cell  `tlb:"^"`
	Extra       *BlockExtra `tlb:"^"`
}

type AllShardsInfo struct {
	ShardHashes *cell.Dictionary `tlb:"dict 32"`
}

type BlockHeader struct { // BlockIDExt from block.tlb
	blockInfoPart
	GenSoftware *GlobalVersion
	MasterRef   *ExtBlkRef
	PrevRef     BlkPrevInfo
	PrevVertRef *BlkPrevInfo
}

type blockInfoPart struct {
	_                         Magic      `tlb:"#9bc7a987"`
	Version                   uint32     `tlb:"## 32"`
	NotMaster                 bool       `tlb:"bool"`
	AfterMerge                bool       `tlb:"bool"`
	BeforeSplit               bool       `tlb:"bool"`
	AfterSplit                bool       `tlb:"bool"`
	WantSplit                 bool       `tlb:"bool"`
	WantMerge                 bool       `tlb:"bool"`
	KeyBlock                  bool       `tlb:"bool"`
	VertSeqnoIncr             bool       `tlb:"bool"`
	Flags                     uint32     `tlb:"## 8"`
	SeqNo                     uint32     `tlb:"## 32"`
	VertSeqNo                 uint32     `tlb:"## 32"`
	Shard                     ShardIdent `tlb:"."`
	GenUtime                  uint32     `tlb:"## 32"`
	StartLt                   uint64     `tlb:"## 64"`
	EndLt                     uint64     `tlb:"## 64"`
	GenValidatorListHashShort uint32     `tlb:"## 32"`
	GenCatchainSeqno          uint32     `tlb:"## 32"`
	MinRefMcSeqno             uint32     `tlb:"## 32"`
	PrevKeyBlockSeqno         uint32     `tlb:"## 32"`
}

type ExtBlkRef struct {
	EndLt    uint64 `tlb:"## 64"`
	SeqNo    uint32 `tlb:"## 32"`
	RootHash []byte `tlb:"bits 256"`
	FileHash []byte `tlb:"bits 256"`
}

type GlobalVersion struct {
	_            Magic  `tlb:"#c4"`
	Version      uint32 `tlb:"## 32"`
	Capabilities uint64 `tlb:"## 64"`
}

type BlkPrevInfo struct {
	Prev1 ExtBlkRef
	Prev2 *ExtBlkRef
}

func (h *BlockInfo) Equals(h2 *BlockInfo) bool {
	return h.Shard == h2.Shard && h.SeqNo == h2.SeqNo && h.Workchain == h2.Workchain &&
		bytes.Equal(h.FileHash, h2.FileHash) && bytes.Equal(h.RootHash, h2.RootHash)
}

func (h *BlockInfo) Copy() *BlockInfo {
	root := make([]byte, len(h.RootHash))
	file := make([]byte, len(h.FileHash))
	copy(root, h.RootHash)
	copy(file, h.FileHash)

	return &BlockInfo{
		Workchain: h.Workchain,
		Shard:     h.Shard,
		SeqNo:     h.SeqNo,
		RootHash:  root,
		FileHash:  file,
	}
}

func (h *BlockHeader) LoadFromCell(loader *cell.Slice) error {
	var infoPart blockInfoPart
	err := LoadFromCell(&infoPart, loader)
	if err != nil {
		return fmt.Errorf("failed to load blockInfoPart: %w", err)
	}
	h.blockInfoPart = infoPart

	if infoPart.Flags&1 == 1 {
		var globalVer GlobalVersion
		err = LoadFromCell(&globalVer, loader)
		if err != nil {
			return fmt.Errorf("failed to load GlobalVersion: %w", err)
		}
		h.GenSoftware = &globalVer
	}

	if infoPart.NotMaster {
		var masterRef ExtBlkRef
		l, err := loader.LoadRef()
		if err != nil {
			return err
		}
		err = LoadFromCell(&masterRef, l)
		if err != nil {
			return fmt.Errorf("failed to load ExtBlkRef: %w", err)
		}
		h.MasterRef = &masterRef
	}

	l, err := loader.LoadRef()
	if err != nil {
		return fmt.Errorf("failed to load ref for after merge: %w", err)
	}
	prevRef, err := loadBlkPrevInfo(l, infoPart.AfterMerge)
	if err != nil {
		return fmt.Errorf("failed to loadBlkPrevInfo for after merge: %w", err)
	}
	h.PrevRef = *prevRef

	if infoPart.VertSeqnoIncr {
		l, err := loader.LoadRef()
		if err != nil {
			return fmt.Errorf("failed to load ref for vert incr: %w", err)
		}
		prevVertRef, err := loadBlkPrevInfo(l, false)
		if err != nil {
			return fmt.Errorf("failed to loadBlkPrevInfo for prev vert ref: %w", err)
		}
		h.PrevVertRef = prevVertRef
	}
	return nil
}

func loadBlkPrevInfo(loader *cell.Slice, afterMerge bool) (*BlkPrevInfo, error) {
	var res BlkPrevInfo

	if loader.IsSpecial() {
		// TODO: rewrite BlockHeader to pure tlb loader
		// if it is a proof we skip load
		return &res, nil
	}

	if !afterMerge {
		var blkRef ExtBlkRef
		err := LoadFromCell(&blkRef, loader)
		if err != nil {
			return nil, err
		}
		res.Prev1 = blkRef
		return &res, nil
	}

	var blkRef1, blkRef2 ExtBlkRef
	prev1, err := loader.LoadRef()
	if err != nil {
		return nil, err
	}
	prev2, err := loader.LoadRef()
	if err != nil {
		return nil, err
	}
	err = LoadFromCell(&blkRef1, prev1)
	if err != nil {
		return nil, err
	}
	err = LoadFromCell(&blkRef2, prev2)
	if err != nil {
		return nil, err
	}

	res.Prev1 = blkRef1
	res.Prev2 = &blkRef2
	return &res, nil
}

func ConvertShardIdentToShard(si ShardIdent) (workchain int32, shard uint64) {
	shard = si.ShardPrefix
	pow2 := uint64(1) << (63 - si.PrefixBits)
	shard |= pow2
	return si.WorkchainID, shard
}

func shardChild(shard uint64, left bool) uint64 {
	x := lowerBit64(shard) >> 1
	if left {
		return shard - x
	}
	return shard + x
}

func shardParent(shard uint64) uint64 {
	x := lowerBit64(shard)
	return (shard - x) | (x << 1)
}

func lowerBit64(x uint64) uint64 {
	return x & bitsNegate64(x)
}

func bitsNegate64(x uint64) uint64 {
	return ^x + 1
}

func (h *BlockHeader) GetParentBlocks() ([]*BlockInfo, error) {
	var parents []*BlockInfo
	workchain, shard := ConvertShardIdentToShard(h.Shard)

	if !h.AfterMerge && !h.AfterSplit {
		return []*BlockInfo{{
			Workchain: workchain,
			SeqNo:     h.PrevRef.Prev1.SeqNo,
			RootHash:  h.PrevRef.Prev1.RootHash,
			FileHash:  h.PrevRef.Prev1.FileHash,
			Shard:     int64(shard),
		}}, nil
	} else if !h.AfterMerge && h.AfterSplit {
		return []*BlockInfo{{
			Workchain: workchain,
			SeqNo:     h.PrevRef.Prev1.SeqNo,
			RootHash:  h.PrevRef.Prev1.RootHash,
			FileHash:  h.PrevRef.Prev1.FileHash,
			Shard:     int64(shardParent(shard)),
		}}, nil
	}

	if h.PrevRef.Prev2 == nil {
		return nil, fmt.Errorf("must be 2 parent blocks after merge")
	}
	parents = append(parents, &BlockInfo{
		Workchain: workchain,
		SeqNo:     h.PrevRef.Prev1.SeqNo,
		RootHash:  h.PrevRef.Prev1.RootHash,
		FileHash:  h.PrevRef.Prev1.FileHash,
		Shard:     int64(shardChild(shard, true)),
	})
	parents = append(parents, &BlockInfo{
		Workchain: workchain,
		SeqNo:     h.PrevRef.Prev2.SeqNo,
		RootHash:  h.PrevRef.Prev2.RootHash,
		FileHash:  h.PrevRef.Prev2.FileHash,
		Shard:     int64(shardChild(shard, false)),
	})
	return parents, nil
}
