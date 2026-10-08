package tlb

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

type StateUpdate struct {
	Old any        `tlb:"^ [ShardStateUnsplit,ShardStateSplit]"`
	New *cell.Cell `tlb:"^"`
}

type McBlockExtra struct {
	_           Magic             `tlb:"#cca5"`
	KeyBlock    bool              `tlb:"bool"`
	ShardHashes *cell.Dictionary  `tlb:"dict 32"`
	ShardFees   *ShardFeesAugDict `tlb:"."`
	Details     struct {
		PrevBlockSignatures *cell.Dictionary `tlb:"dict 16"`
		RecoverCreateMsg    *cell.Cell       `tlb:"maybe ^"`
		MintMsg             *cell.Cell       `tlb:"maybe ^"`
	} `tlb:"^"`
	ConfigParams *ConfigParams `tlb:"?KeyBlock ."`
}

func (e *McBlockExtra) ToCell() (*cell.Cell, error) {
	//TODO: implement augmented dict
	return nil, fmt.Errorf("serialization of McBlockExtra is not supported yet, because of aug dicts")
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
	Accounts *ShardAccountBlocksAugDict `tlb:"."`
}

type AccountBlock struct {
	_            Magic                       `tlb:"#5"`
	Addr         []byte                      `tlb:"bits 256"`
	Transactions *AccountTransactionsAugDict `tlb:"."`
	StateUpdate  *cell.Cell                  `tlb:"^"`
}

func (b AccountBlock) ToCell() (*cell.Cell, error) {
	if len(b.Addr) != 32 {
		return nil, fmt.Errorf("account block address must be 256 bits")
	}
	if b.Transactions == nil || b.Transactions.AugmentedDictionary == nil {
		return nil, fmt.Errorf("account block transactions dict is nil")
	}
	if b.Transactions.GetKeySize() != 64 {
		return nil, fmt.Errorf("account block transactions have key size %d", b.Transactions.GetKeySize())
	}

	transactions, err := b.Transactions.InlineCell()
	if err != nil {
		return nil, fmt.Errorf("failed to store account block transactions: %w", err)
	}

	builder := cell.BeginCell().
		MustStoreUInt(5, 4).
		MustStoreSlice(b.Addr, 256)
	if err = builder.StoreBuilder(transactions.ToBuilder()); err != nil {
		return nil, fmt.Errorf("failed to store account block transactions: %w", err)
	}
	if err = builder.StoreRef(b.StateUpdate); err != nil {
		return nil, fmt.Errorf("failed to store account block state update: %w", err)
	}
	return builder.EndCell(), nil
}

type Block struct {
	_           Magic       `tlb:"#11ef55aa"`
	GlobalID    int32       `tlb:"## 32"`
	BlockInfo   BlockHeader `tlb:"^"`
	ValueFlow   *cell.Cell  `tlb:"^"`
	StateUpdate *cell.Cell  `tlb:"^"`
	Extra       *BlockExtra `tlb:"^"`
}

// ListTransactions returns all transactions stored in this block.
func (b *Block) ListTransactions() ([]*Transaction, error) {
	if b == nil {
		return nil, fmt.Errorf("block is nil")
	}
	if b.Extra == nil || b.Extra.ShardAccountBlocks == nil {
		return nil, fmt.Errorf("block has no shard account blocks")
	}

	accounts, err := b.Extra.ShardAccountBlocks.BeginParse()
	if err != nil {
		return nil, fmt.Errorf("failed to load shard account blocks: %w", err)
	}

	hasAccounts, err := accounts.LoadBoolBit()
	if err != nil {
		return nil, fmt.Errorf("failed to load shard account blocks root flag: %w", err)
	}
	if !hasAccounts {
		return []*Transaction{}, nil
	}

	root, err := accounts.LoadRefCell()
	if err != nil {
		return nil, fmt.Errorf("failed to load shard account blocks root: %w", err)
	}

	accountList, err := root.AsDict(256).LoadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to load account blocks: %w", err)
	}

	txList := make([]*Transaction, 0)
	for _, accountKV := range accountList {
		if err = skipCurrencyCollectionBoundary(accountKV.Value); err != nil {
			return nil, fmt.Errorf("failed to load account block fees: %w", err)
		}

		var accountBlock AccountBlock
		if err = LoadFromCell(&accountBlock, accountKV.Value); err != nil {
			return nil, fmt.Errorf("failed to load account block: %w", err)
		}

		if accountBlock.Transactions == nil || accountBlock.Transactions.IsEmpty() {
			continue
		}

		txRoot := accountBlock.Transactions.AsCell()
		txKVList, err := txRoot.AsDict(64).LoadAll()
		if err != nil {
			return nil, fmt.Errorf("failed to load account transactions: %w", err)
		}

		for _, txKV := range txKVList {
			if err = skipCurrencyCollectionBoundary(txKV.Value); err != nil {
				return nil, fmt.Errorf("failed to load tx fees: %w", err)
			}

			txCell, err := txKV.Value.LoadRefCell()
			if err != nil {
				return nil, fmt.Errorf("failed to load tx ref: %w", err)
			}

			var tx Transaction
			if err = Parse(&tx, txCell); err != nil {
				return nil, fmt.Errorf("failed to load tx from cell: %w", err)
			}

			tx.Hash = txCell.Hash()
			txList = append(txList, &tx)
		}
	}

	return txList, nil
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

	// Pruned reports that the prev-block references were pruned away in the
	// Merkle proof this header was parsed from, so Prev1/Prev2 carry NO data.
	// Block proofs may legally prune these cells. Callers that need parent block
	// IDs MUST check this flag before using Prev1/Prev2.
	Pruned bool
}

func (h BlockHeader) ToCell() (*cell.Cell, error) {
	b := cell.BeginCell()

	info, err := ToCell(h.blockInfoPart)
	if err != nil {
		return nil, fmt.Errorf("failed to convert blockInfoPart: %w", err)
	}
	b.MustStoreBuilder(info.ToBuilder())

	if h.Flags&1 == 1 {
		if h.GenSoftware == nil {
			return nil, fmt.Errorf("GenSoftware is nil but flag specified")
		}

		g, err := ToCell(h.GenSoftware)
		if err != nil {
			return nil, fmt.Errorf("failed to convert GenSoftware: %w", err)
		}
		b.MustStoreBuilder(g.ToBuilder())
	}

	if h.NotMaster {
		if h.MasterRef == nil {
			return nil, fmt.Errorf("MasterRef is nil but flag specified")
		}

		m, err := ToCell(h.MasterRef)
		if err != nil {
			return nil, fmt.Errorf("failed to convert MasterRef: %w", err)
		}
		b.MustStoreRef(m)
	}

	bIn := cell.BeginCell()
	if err = storeBlkPrevInfo(&h.PrevRef, bIn, h.AfterMerge); err != nil {
		return nil, fmt.Errorf("failed to store prev ref: %w", err)
	}
	b.MustStoreRef(bIn.EndCell())

	if h.VertSeqnoIncr {
		if h.PrevVertRef == nil {
			return nil, fmt.Errorf("PrevVertRef is nil but flag specified")
		}

		bIn = cell.BeginCell()
		if err = storeBlkPrevInfo(h.PrevVertRef, bIn, false); err != nil {
			return nil, fmt.Errorf("failed to store prev ref: %w", err)
		}
		b.MustStoreRef(bIn.EndCell())
	}

	return b.EndCell(), nil
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
		// The prev cell was pruned away in a Merkle proof: the references are
		// unavailable. Mark the result so callers cannot mistake zero values for
		// decoded references.
		res.Pruned = true
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

	// prev_blks_info$_ prev1:^ExtBlkRef prev2:^ExtBlkRef = BlkPrevInfo 1;
	// the two ExtBlkRef refs may themselves be pruned in a proof.
	var blkRef1, blkRef2 ExtBlkRef
	prev1, err := loader.LoadRef()
	if err != nil {
		return nil, err
	}
	prev2, err := loader.LoadRef()
	if err != nil {
		return nil, err
	}

	if prev1.IsSpecial() || prev2.IsSpecial() {
		res.Pruned = true
		return &res, nil
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

func storeBlkPrevInfo(info *BlkPrevInfo, b *cell.Builder, afterMerge bool) error {
	if info.Pruned {
		return fmt.Errorf("cannot serialize pruned prev block info: the original references were pruned away in a proof")
	}
	if !afterMerge {
		c, err := ToCell(info.Prev1)
		if err != nil {
			return err
		}
		b.MustStoreBuilder(c.ToBuilder())
		return nil
	}

	c1, err := ToCell(info.Prev1)
	if err != nil {
		return err
	}
	c2, err := ToCell(info.Prev2)
	if err != nil {
		return err
	}

	b.MustStoreRef(c1)
	b.MustStoreRef(c2)
	return err
}

func ConvertShardIdentToShard(si ShardIdent) (workchain int32, shard uint64) {
	shard = si.ShardPrefix
	pow2 := uint64(1) << (63 - si.PrefixBits)
	shard |= pow2
	return si.WorkchainID, shard
}
