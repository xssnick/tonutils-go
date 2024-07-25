package ton

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"reflect"
	"time"

	"github.com/xssnick/tonutils-go/tl"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func init() {
	tl.Register(MasterchainInfo{}, "liteServer.masterchainInfo last:tonNode.blockIdExt state_root_hash:int256 init:tonNode.zeroStateIdExt = liteServer.MasterchainInfo")
	tl.Register(BlockIDExt{}, "tonNode.blockIdExt workchain:int shard:long seqno:int root_hash:int256 file_hash:int256 = tonNode.BlockIdExt")
	tl.Register(ZeroStateIDExt{}, "tonNode.zeroStateIdExt workchain:int root_hash:int256 file_hash:int256 = tonNode.ZeroStateIdExt")
	tl.Register(GetBlockData{}, "liteServer.getBlock id:tonNode.blockIdExt = liteServer.BlockData")
	tl.Register(ListBlockTransactions{}, "liteServer.listBlockTransactions id:tonNode.blockIdExt mode:# count:# after:mode.7?liteServer.transactionId3 reverse_order:mode.6?true want_proof:mode.5?true = liteServer.BlockTransactions")
	tl.Register(ListBlockTransactionsExt{}, "liteServer.listBlockTransactionsExt id:tonNode.blockIdExt mode:# count:# after:mode.7?liteServer.transactionId3 reverse_order:mode.6?true want_proof:mode.5?true = liteServer.BlockTransactionsExt")
	tl.Register(GetAllShardsInfo{}, "liteServer.getAllShardsInfo id:tonNode.blockIdExt = liteServer.AllShardsInfo")
	tl.Register(GetMasterchainInf{}, "liteServer.getMasterchainInfo = liteServer.MasterchainInfo")
	tl.Register(WaitMasterchainSeqno{}, "liteServer.waitMasterchainSeqno seqno:int timeout_ms:int = Object")
	tl.Register(LookupBlock{}, "liteServer.lookupBlock mode:# id:tonNode.blockId lt:mode.1?long utime:mode.2?int = liteServer.BlockHeader")
	tl.Register(BlockInfoShort{}, "tonNode.blockId workchain:int shard:long seqno:int = tonNode.BlockId")
	tl.Register(BlockData{}, "liteServer.blockData id:tonNode.blockIdExt data:bytes = liteServer.BlockData")
	tl.Register(BlockHeader{}, "liteServer.blockHeader id:tonNode.blockIdExt mode:# header_proof:bytes = liteServer.BlockHeader")
	tl.Register(BlockTransactions{}, "liteServer.blockTransactions id:tonNode.blockIdExt req_count:# incomplete:Bool ids:(vector liteServer.transactionId) proof:bytes = liteServer.BlockTransactions")
	tl.Register(BlockTransactionsExt{}, "liteServer.blockTransactionsExt id:tonNode.blockIdExt req_count:# incomplete:Bool transactions:bytes proof:bytes = liteServer.BlockTransactionsExt")
	tl.Register(AllShardsInfo{}, "liteServer.allShardsInfo id:tonNode.blockIdExt proof:bytes data:bytes = liteServer.AllShardsInfo")
	tl.Register(ShardInfo{}, "liteServer.shardInfo id:tonNode.blockIdExt shardblk:tonNode.blockIdExt shard_proof:bytes shard_descr:bytes = liteServer.ShardInfo")
	tl.Register(ShardBlockProof{}, "liteServer.shardBlockProof masterchain_id:tonNode.blockIdExt links:(vector liteServer.shardBlockLink) = liteServer.ShardBlockProof")
	tl.Register(ShardBlockLink{}, "liteServer.shardBlockLink id:tonNode.blockIdExt proof:bytes = liteServer.ShardBlockLink")
	tl.Register(Object{}, "object ? = Object")
	tl.Register(True{}, "true = True")
	tl.Register(TransactionID3{}, "liteServer.transactionId3 account:int256 lt:long = liteServer.TransactionId3")
	tl.Register(TransactionID{}, "liteServer.transactionId mode:# account:mode.0?int256 lt:mode.1?long hash:mode.2?int256 = liteServer.TransactionId")

	tl.Register(GetState{}, "liteServer.getState id:tonNode.blockIdExt = liteServer.BlockState")
	tl.Register(BlockState{}, "liteServer.blockState id:tonNode.blockIdExt root_hash:int256 file_hash:int256 data:bytes = liteServer.BlockState")

	tl.Register(GetBlockProof{}, "liteServer.getBlockProof mode:# known_block:tonNode.blockIdExt target_block:mode.0?tonNode.blockIdExt = liteServer.PartialBlockProof")
	tl.Register(PartialBlockProof{}, "liteServer.partialBlockProof complete:Bool from:tonNode.blockIdExt to:tonNode.blockIdExt steps:(vector liteServer.BlockLink) = liteServer.PartialBlockProof")
	tl.Register(BlockLinkBackward{}, "liteServer.blockLinkBack to_key_block:Bool from:tonNode.blockIdExt to:tonNode.blockIdExt dest_proof:bytes proof:bytes state_proof:bytes = liteServer.BlockLink")
	tl.Register(BlockLinkForward{}, "liteServer.blockLinkForward to_key_block:Bool from:tonNode.blockIdExt to:tonNode.blockIdExt dest_proof:bytes config_proof:bytes signatures:liteServer.SignatureSet = liteServer.BlockLink")
	tl.Register(SignatureSet{}, "liteServer.signatureSet validator_set_hash:int catchain_seqno:int signatures:(vector liteServer.signature) = liteServer.SignatureSet")
	tl.Register(Signature{}, "liteServer.signature node_id_short:int256 signature:bytes = liteServer.Signature")
	tl.Register(BlockID{}, "ton.blockId root_cell_hash:int256 file_hash:int256 = ton.BlockId")

	tl.Register(GetVersion{}, "liteServer.getVersion = liteServer.Version")
	tl.Register(Version{}, "liteServer.version mode:# version:int capabilities:long now:int = liteServer.Version")

	tl.Register(GetShardBlockProof{}, "liteServer.getShardBlockProof id:tonNode.blockIdExt = liteServer.ShardBlockProof")
	tl.Register(GetShardInfo{}, "liteServer.getShardInfo id:tonNode.blockIdExt workchain:int shard:long exact:Bool = liteServer.ShardInfo")
	tl.Register(GetBlockHeader{}, "liteServer.getBlockHeader id:tonNode.blockIdExt mode:# = liteServer.BlockHeader")
	tl.Register(GetMasterchainInfoExt{}, "liteServer.getMasterchainInfoExt mode:# = liteServer.MasterchainInfoExt")
	tl.Register(MasterchainInfoExt{}, "liteServer.masterchainInfoExt mode:# version:int capabilities:long last:tonNode.blockIdExt last_utime:int now:int state_root_hash:int256 init:tonNode.zeroStateIdExt = liteServer.MasterchainInfoExt")
}

type GetVersion struct{}

type Version struct {
	Mode         uint32 `tl:"flags"`
	Version      int32  `tl:"int"`
	Capabilities int64  `tl:"long"`
	Now          uint32 `tl:"int"`
}

type GetState struct {
	ID       *BlockIDExt `tl:"struct"`
	RootHash []byte      `tl:"int256"`
	FileHash []byte      `tl:"int256"`
	Data     *cell.Cell  `tl:"cell"`
}

type BlockState struct {
	ID *BlockIDExt `tl:"struct"`
}

type GetShardBlockProof struct {
	ID *BlockIDExt `tl:"struct"`
}

type ShardBlockProof struct {
	MasterchainID *BlockIDExt      `tl:"struct"`
	Links         []ShardBlockLink `tl:"vector struct"`
}

type ShardBlockLink struct {
	ID    *BlockIDExt `tl:"struct"`
	Proof []byte      `tl:"bytes"`
}

type BlockID struct {
	RootHash []byte `tl:"int256"`
	FileHash []byte `tl:"int256"`
}

type PartialBlockProof struct {
	Complete bool        `tl:"bool"`
	From     *BlockIDExt `tl:"struct"`
	To       *BlockIDExt `tl:"struct"`
	Steps    []any       `tl:"vector struct boxed [liteServer.blockLinkForward, liteServer.blockLinkBack]"`
}

type BlockLinkBackward struct {
	ToKeyBlock bool        `tl:"bool"`
	From       *BlockIDExt `tl:"struct"`
	To         *BlockIDExt `tl:"struct"`
	DestProof  []byte      `tl:"bytes"`
	Proof      []byte      `tl:"bytes"`
	StateProof []byte      `tl:"bytes"`
}

type BlockLinkForward struct {
	ToKeyBlock   bool          `tl:"bool"`
	From         *BlockIDExt   `tl:"struct"`
	To           *BlockIDExt   `tl:"struct"`
	DestProof    []byte        `tl:"bytes"`
	ConfigProof  []byte        `tl:"bytes"`
	SignatureSet *SignatureSet `tl:"struct boxed"`
}

type SignatureSet struct {
	ValidatorSetHash int32       `tl:"int"`
	CatchainSeqno    int32       `tl:"int"`
	Signatures       []Signature `tl:"vector struct"`
}

type Signature struct {
	NodeIDShort []byte `tl:"int256"`
	Signature   []byte `tl:"bytes"`
}

type Object struct{}
type True struct{}

// TODO: will be moved here in the next version
type BlockIDExt = tlb.BlockInfo

type MasterchainInfo struct {
	Last          *BlockIDExt     `tl:"struct"`
	StateRootHash []byte          `tl:"int256"`
	Init          *ZeroStateIDExt `tl:"struct"`
}

type MasterchainInfoExt struct {
	Mode          uint32          `tl:"flags"`
	Version       int32           `tl:"int"`
	Capabilities  int64           `tl:"long"`
	Last          *BlockIDExt     `tl:"struct"`
	LastUTime     uint32          `tl:"int"`
	Now           uint32          `tl:"int"`
	StateRootHash []byte          `tl:"int256"`
	Init          *ZeroStateIDExt `tl:"struct"`
}

type BlockHeader struct {
	ID          *BlockIDExt `tl:"struct"`
	Mode        uint32      `tl:"flags"`
	HeaderProof []byte      `tl:"bytes"`
}

type ZeroStateIDExt struct {
	Workchain int32  `tl:"int"`
	RootHash  []byte `tl:"int256"`
	FileHash  []byte `tl:"int256"`
}

type AllShardsInfo struct {
	ID    *BlockIDExt  `tl:"struct"`
	Proof []*cell.Cell `tl:"cell"`
	Data  *cell.Cell   `tl:"cell"`
}

type ShardInfo struct {
	ID               *BlockIDExt  `tl:"struct"`
	ShardBlock       *BlockIDExt  `tl:"struct"`
	ShardProof       []*cell.Cell `tl:"cell optional 2"`
	ShardDescription *cell.Cell   `tl:"cell optional"`
}

type BlockTransactions struct {
	ID             *BlockIDExt     `tl:"struct"`
	ReqCount       int32           `tl:"int"`
	Incomplete     bool            `tl:"bool"`
	TransactionIds []TransactionID `tl:"vector struct"`
	Proof          *cell.Cell      `tl:"cell optional"`
}

type BlockTransactionsExt struct {
	ID           *BlockIDExt  `tl:"struct"`
	ReqCount     int32        `tl:"int"`
	Incomplete   bool         `tl:"bool"`
	Transactions []*cell.Cell `tl:"cell optional"`
	Proof        []byte       `tl:"bytes"`
}

type BlockData struct {
	ID      *BlockIDExt `tl:"struct"`
	Payload []byte      `tl:"bytes"`
}

type LookupBlock struct {
	Mode  uint32          `tl:"flags"`
	ID    *BlockInfoShort `tl:"struct"`
	LT    uint64          `tl:"?1 long"`
	UTime uint32          `tl:"?2 int"`
}

type GetBlockHeader struct {
	ID   *BlockIDExt `tl:"struct"`
	Mode uint32      `tl:"flags"`
}

type BlockInfoShort struct {
	Workchain int32 `tl:"int"`
	Shard     int64 `tl:"long"`
	Seqno     int32 `tl:"int"`
}

type WaitMasterchainSeqno struct {
	Seqno   int32 `tl:"int"`
	Timeout int32 `tl:"int"`
}

type GetAllShardsInfo struct {
	ID *BlockIDExt `tl:"struct"`
}

type GetShardInfo struct {
	ID        *BlockIDExt `tl:"struct"`
	Workchain int32       `tl:"int"`
	Shard     int64       `tl:"long"`
	Exact     bool        `tl:"bool"`
}

type GetMasterchainInf struct{}

type GetMasterchainInfoExt struct {
	Mode uint32 `tl:"flags"`
}

type ListBlockTransactions struct {
	ID           *BlockIDExt     `tl:"struct"`
	Mode         uint32          `tl:"flags"`
	Count        uint32          `tl:"int"`
	After        *TransactionID3 `tl:"?7 struct"`
	ReverseOrder *True           `tl:"?6 struct"`
	WantProof    *True           `tl:"?5 struct"`
}

type ListBlockTransactionsExt struct {
	ID           *BlockIDExt     `tl:"struct"`
	Mode         uint32          `tl:"flags"`
	Count        uint32          `tl:"int"`
	After        *TransactionID3 `tl:"?7 struct"`
	ReverseOrder *True           `tl:"?6 struct"`
	WantProof    *True           `tl:"?5 struct"`
}

type TransactionShortInfo struct {
	Account []byte
	LT      uint64
	Hash    []byte
}

type GetBlockProof struct {
	Mode        uint32      `tl:"flags"`
	KnownBlock  *BlockIDExt `tl:"struct"`
	TargetBlock *BlockIDExt `tl:"?0 struct"`
}

func (t *TransactionShortInfo) ID3() *TransactionID3 {
	return &TransactionID3{
		Account: t.Account,
		LT:      t.LT,
	}
}

type TransactionID struct {
	Flags   uint32 `tl:"flags"`
	Account []byte `tl:"?0 int256"`
	LT      uint64 `tl:"?1 long"`
	Hash    []byte `tl:"?2 int256"`
}

type TransactionID3 struct {
	Account []byte `tl:"int256"`
	LT      uint64 `tl:"long"`
}

type GetBlockData struct {
	ID *BlockIDExt `tl:"struct"`
}

var ErrBlockNotFound = errors.New("block not found")
var ErrNoNewBlocks = errors.New("no new blocks in a given timeout or in 10 seconds")

func (c *APIClient) Client() LiteClient {
	return c.client
}

// CurrentMasterchainInfo - cached version of GetMasterchainInfo to not do it in parallel many times
func (c *APIClient) CurrentMasterchainInfo(ctx context.Context) (_ *BlockIDExt, err error) {
	root := c.root() // this method should use root level props, to share curMasters and lock.

	// if not sticky - id will be 0
	nodeID := c.client.StickyNodeID(ctx)

	root.curMastersLock.Lock()
	master := root.curMasters[nodeID]
	if master == nil {
		master = &masterInfo{}
		root.curMasters[nodeID] = master
	}
	root.curMastersLock.Unlock()

	master.mx.Lock()
	defer master.mx.Unlock()

	if time.Now().After(master.updatedAt.Add(5 * time.Second)) {
		ctx = c.client.StickyContext(ctx)

		var block *BlockIDExt
		block, err = c.GetMasterchainInfo(ctx)
		if err != nil {
			return nil, fmt.Errorf("get masterchain info error (%s): %w", reflect.TypeOf(c.client).String(), err)
		}

		master.updatedAt = time.Now()
		master.block = block
	}

	return master.block, nil
}

// GetMasterchainInfo - gets the latest state of master chain
func (c *APIClient) GetMasterchainInfo(ctx context.Context) (*BlockIDExt, error) {
	var resp tl.Serializable
	err := c.client.QueryLiteserver(ctx, GetMasterchainInf{}, &resp)
	if err != nil {
		return nil, err
	}

	switch t := resp.(type) {
	case MasterchainInfo:
		if c.proofCheckPolicy == ProofCheckPolicySecure {
			root := c.root()
			root.trustedLock.Lock()
			defer root.trustedLock.Unlock()

			if root.trustedBlock == nil {
				if root.trustedBlock == nil {
					// we have no block to trust, so trust first block we get
					root.trustedBlock = t.Last.Copy()
					log.Println("[WARNING] trusted block was not set on initialization, so first block we got was considered as trusted. " +
						"For better security you should use SetTrustedBlock(block) method and pass there init block from config on start")
				}
			} else {
				if err := c.VerifyProofChain(ctx, root.trustedBlock, t.Last); err != nil {
					return nil, fmt.Errorf("failed to verify proof chain: %w", err)
				}

				if t.Last.SeqNo > root.trustedBlock.SeqNo {
					root.trustedBlock = t.Last.Copy()
				}
			}
		}
		return t.Last, nil
	case LSError:
		return nil, t
	}
	return nil, errUnexpectedResponse(resp)
}

// LookupBlock - find block information by seqno, shard and chain
func (c *APIClient) LookupBlock(ctx context.Context, workchain int32, shard int64, seqno uint32) (*BlockIDExt, error) {
	var resp tl.Serializable
	err := c.client.QueryLiteserver(ctx, LookupBlock{
		Mode: 1,
		ID: &BlockInfoShort{
			Workchain: workchain,
			Shard:     shard,
			Seqno:     int32(seqno),
		},
	}, &resp)
	if err != nil {
		return nil, err
	}

	switch t := resp.(type) {
	case BlockHeader:
		return t.ID, nil
	case LSError:
		// 651 = block not found code
		if t.Code == 651 {
			return nil, ErrBlockNotFound
		}
		return nil, t
	}
	return nil, errUnexpectedResponse(resp)
}

// GetBlockData - get block detailed information
func (c *APIClient) GetBlockData(ctx context.Context, block *BlockIDExt) (*tlb.Block, error) {
	var resp tl.Serializable
	err := c.client.QueryLiteserver(ctx, GetBlockData{ID: block}, &resp)
	if err != nil {
		return nil, err
	}

	switch t := resp.(type) {
	case BlockData:
		pl, err := cell.FromBOC(t.Payload)
		if err != nil {
			return nil, err
		}

		if !bytes.Equal(pl.Hash(), block.RootHash) {
			return nil, fmt.Errorf("incorrect block")
		}

		var bData tlb.Block
		if err = tlb.LoadFromCell(&bData, pl.BeginParse()); err != nil {
			return nil, fmt.Errorf("failed to parse block data: %w", err)
		}
		return &bData, nil
	case LSError:
		return nil, t
	}
	return nil, errUnexpectedResponse(resp)
}

// GetBlockTransactionsV2 - list of block transactions
func (c *APIClient) GetBlockTransactionsV2(ctx context.Context, block *BlockIDExt, count uint32, after ...*TransactionID3) ([]TransactionShortInfo, bool, error) {
	withAfter := uint32(0)
	var afterTx *TransactionID3
	if len(after) > 0 && after[0] != nil {
		afterTx = after[0]
		withAfter = 1
	}

	mode := 0b111 | (withAfter << 7)
	if c.proofCheckPolicy != ProofCheckPolicyUnsafe {
		mode |= 1 << 5
	}

	var resp tl.Serializable
	err := c.client.QueryLiteserver(ctx, ListBlockTransactions{
		Mode:      mode,
		ID:        block,
		Count:     count,
		After:     afterTx,
		WantProof: &True{},
	}, &resp)
	if err != nil {
		return nil, false, err
	}

	switch t := resp.(type) {
	case BlockTransactions:
		var shardAccounts tlb.ShardAccountBlocks

		if c.proofCheckPolicy != ProofCheckPolicyUnsafe {
			if t.Proof == nil {
				return nil, false, fmt.Errorf("no proof passed by ls")
			}

			blockProof, err := CheckBlockProof(t.Proof, block.RootHash)
			if err != nil {
				return nil, false, fmt.Errorf("failed to check block proof: %w", err)
			}

			if err = tlb.LoadFromCellAsProof(&shardAccounts, blockProof.Extra.ShardAccountBlocks.BeginParse()); err != nil {
				return nil, false, fmt.Errorf("failed to load shard accounts from proof: %w", err)
			}
		}

		txIds := make([]TransactionShortInfo, 0, len(t.TransactionIds))
		for _, id := range t.TransactionIds {
			if id.LT == 0 || id.Hash == nil || id.Account == nil {
				return nil, false, fmt.Errorf("invalid ls response, fields are nil")
			}

			if c.proofCheckPolicy != ProofCheckPolicyUnsafe {
				if err = CheckTransactionProof(id.Hash, id.LT, id.Account, &shardAccounts); err != nil {
					return nil, false, fmt.Errorf("incorrect tx %s proof: %w", hex.EncodeToString(id.Hash), err)
				}
			}

			txIds = append(txIds, TransactionShortInfo{
				Account: id.Account,
				LT:      id.LT,
				Hash:    id.Hash,
			})
		}
		return txIds, t.Incomplete, nil
	case LSError:
		return nil, false, t
	}
	return nil, false, errUnexpectedResponse(resp)
}

// GetBlockShardsInfo - gets the information about workchains and its shards at given masterchain state
func (c *APIClient) GetBlockShardsInfo(ctx context.Context, master *BlockIDExt) ([]*BlockIDExt, error) {
	var resp tl.Serializable
	err := c.client.QueryLiteserver(ctx, GetAllShardsInfo{ID: master}, &resp)
	if err != nil {
		return nil, err
	}

	switch t := resp.(type) {
	case AllShardsInfo:
		var inf tlb.AllShardsInfo
		err = tlb.LoadFromCell(&inf, t.Data.BeginParse())
		if err != nil {
			return nil, err
		}

		if c.proofCheckPolicy != ProofCheckPolicyUnsafe {
			if len(t.Proof) == 0 {
				return nil, fmt.Errorf("empty proof")
			}

			switch len(t.Proof) {
			case 1:
				blockProof, err := CheckBlockProof(t.Proof[0], master.RootHash)
				if err != nil {
					return nil, fmt.Errorf("failed to check proof: %w", err)
				}

				if blockProof.Extra == nil || blockProof.Extra.Custom == nil || !bytes.Equal(blockProof.Extra.Custom.ShardHashes.AsCell().Hash(0), t.Data.MustPeekRef(0).Hash()) {
					return nil, fmt.Errorf("incorrect proof")
				}
			case 2: // old LS compatibility
				shardState, err := CheckBlockShardStateProof(t.Proof, master.RootHash)
				if err != nil {
					return nil, fmt.Errorf("failed to check proof: %w", err)
				}

				mcShort := shardState.McStateExtra.BeginParse()
				if v, err := mcShort.LoadUInt(16); err != nil || v != 0xcc26 {
					return nil, fmt.Errorf("invalic mc extra in proof")
				}

				dictProof, err := mcShort.LoadMaybeRef()
				if err != nil {
					return nil, fmt.Errorf("failed to load dict proof: %w", err)
				}

				if dictProof == nil && inf.ShardHashes.IsEmpty() {
					return []*BlockIDExt{}, nil
				}

				if (dictProof == nil) != inf.ShardHashes.IsEmpty() ||
					!bytes.Equal(dictProof.MustToCell().Hash(0), t.Data.MustPeekRef(0).Hash()) {
					return nil, fmt.Errorf("incorrect proof")
				}
			default:
				return nil, fmt.Errorf("incorrect proof roots num")
			}
		}

		return LoadShardsFromHashes(inf.ShardHashes, false)
	case LSError:
		return nil, t
	}
	return nil, errUnexpectedResponse(resp)
}

func LoadShardsFromHashes(shardHashes *cell.Dictionary, skipPruned bool) (shards []*BlockIDExt, err error) {
	if shardHashes == nil {
		return []*BlockIDExt{}, nil
	}

	kvs, err := shardHashes.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to load shard hashes dict: %w", err)
	}

	for _, kv := range kvs {
		workchain, err := kv.Key.LoadInt(32)
		if err != nil {
			return nil, fmt.Errorf("failed to load workchain: %w", err)
		}

		binTreeRef, err := kv.Value.LoadRef()
		if err != nil {
			return nil, fmt.Errorf("failed to load bin tree ref: %w", err)
		}

		var binTree tlb.BinTree
		if err = tlb.LoadFromCellAsProof(&binTree, binTreeRef); err != nil {
			return nil, fmt.Errorf("load BinTree err: %w", err)
		}

		for _, bk := range binTree.All() {
			if skipPruned && bk.Value.GetType() != cell.OrdinaryCellType {
				// in case of split we have list with only needed shard,
				// and pruned branch for others.
				continue
			}

			loader := bk.Value.BeginParse()

			ab, err := loader.LoadUInt(4)
			if err != nil {
				return nil, fmt.Errorf("load ShardDesc magic err: %w", err)
			}

			switch ab {
			case 0xa:
				var shardDesc tlb.ShardDesc
				if err = tlb.LoadFromCell(&shardDesc, loader, true); err != nil {
					return nil, fmt.Errorf("load ShardDesc err: %w", err)
				}
				shards = append(shards, &BlockIDExt{
					Workchain: int32(workchain),
					Shard:     shardDesc.NextValidatorShard,
					SeqNo:     shardDesc.SeqNo,
					RootHash:  shardDesc.RootHash,
					FileHash:  shardDesc.FileHash,
				})
			case 0xb:
				var shardDesc tlb.ShardDescB
				if err = tlb.LoadFromCell(&shardDesc, loader, true); err != nil {
					return nil, fmt.Errorf("load ShardDescB err: %w", err)
				}
				shards = append(shards, &BlockIDExt{
					Workchain: int32(workchain),
					Shard:     shardDesc.NextValidatorShard,
					SeqNo:     shardDesc.SeqNo,
					RootHash:  shardDesc.RootHash,
					FileHash:  shardDesc.FileHash,
				})
			default:
				return nil, fmt.Errorf("wrong ShardDesc magic: %x", ab)
			}
		}
	}
	return
}

// GetBlockProof - gets proof chain for the block
func (c *APIClient) GetBlockProof(ctx context.Context, known, target *BlockIDExt) (*PartialBlockProof, error) {
	var resp tl.Serializable
	err := c.client.QueryLiteserver(ctx, GetBlockProof{
		Mode:        1,
		KnownBlock:  known,
		TargetBlock: target,
	}, &resp)
	if err != nil {
		return nil, err
	}

	switch t := resp.(type) {
	case PartialBlockProof:
		return &t, nil
	case LSError:
		return nil, t
	}
	return nil, errUnexpectedResponse(resp)
}
