package node

import (
	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/ton"
)

func init() {
	tl.Register(DownloadBlock{}, "tonNode.downloadBlock block:tonNode.blockIdExt = tonNode.Data")
	tl.Register(DownloadBlockFull{}, "tonNode.downloadBlockFull block:tonNode.blockIdExt = tonNode.DataFull")
	tl.Register(DataFull{}, "tonNode.dataFull id:tonNode.blockIdExt proof:bytes block:bytes is_link:Bool = tonNode.DataFull")
	tl.Register(DataFullEmpty{}, "tonNode.dataFullEmpty = tonNode.DataFull")
	tl.Register(NewShardBlock{}, "tonNode.newShardBlock block:tonNode.blockIdExt cc_seqno:int data:bytes = tonNode.NewShardBlock")
	tl.Register(NewShardBlockBroadcast{}, "tonNode.newShardBlockBroadcast block:tonNode.newShardBlock = tonNode.Broadcast")
	tl.Register(BlockSignature{}, "tonNode.blockSignature who:int256 signature:bytes = tonNode.BlockSignature")
	tl.Register(SignatureSetOrdinary{}, "tonNode.signatureSet.ordinary cc_seqno:int validator_set_hash:int signatures:(vector tonNode.blockSignature) = tonNode.SignatureSet")
	tl.Register(SignatureSetSimplex{}, "tonNode.signatureSet.simplex final:Bool cc_seqno:int validator_set_hash:int signatures:(vector tonNode.blockSignature) session_id:int256 slot:int candidate:consensus.CandidateHashData = tonNode.SignatureSet")
	tl.Register(BlockBroadcastCompressed{}, "tonNode.blockBroadcastCompressed id:tonNode.blockIdExt catchain_seqno:int validator_set_hash:int flags:# compressed:bytes = tonNode.Broadcast")
	tl.Register(BlockBroadcastCompressedData{}, "tonNode.blockBroadcastCompressed.data signatures:(vector tonNode.blockSignature) proof_data:bytes = tonNode.blockBroadcaseCompressed.Data")
	tl.Register(BlockBroadcastCompressedV2{}, "tonNode.blockBroadcastCompressedV2 id:tonNode.blockIdExt signature_set:tonNode.SignatureSet flags:# proof:bytes data_compressed:bytes = tonNode.Broadcast")
	tl.Register(BlockBroadcast{}, "tonNode.blockBroadcast id:tonNode.blockIdExt catchain_seqno:int validator_set_hash:int signatures:(vector tonNode.blockSignature) proof:bytes data:bytes = tonNode.Broadcast")
	tl.Register(NewBlockCandidateBroadcast{}, "tonNode.newBlockCandidateBroadcast id:tonNode.blockIdExt catchain_seqno:int validator_set_hash:int collator_signature:tonNode.blockSignature data:bytes = tonNode.Broadcast")
	tl.Register(NewBlockCandidateBroadcastCompressed{}, "tonNode.newBlockCandidateBroadcastCompressed id:tonNode.blockIdExt catchain_seqno:int validator_set_hash:int collator_signature:tonNode.blockSignature flags:# compressed:bytes = tonNode.Broadcast")
	tl.Register(NewBlockCandidateBroadcastCompressedV2{}, "tonNode.newBlockCandidateBroadcastCompressedV2 id:tonNode.blockIdExt catchain_seqno:int validator_set_hash:int collator_signature:tonNode.blockSignature flags:# compressed:bytes = tonNode.Broadcast")
}

type DownloadBlock struct {
	Block ton.BlockIDExt `tl:"struct"`
}

type DownloadBlockFull struct {
	Block ton.BlockIDExt `tl:"struct"`
}

type DataFull struct {
	ID     ton.BlockIDExt `tl:"struct"`
	Proof  []byte         `tl:"bytes"`
	Block  []byte         `tl:"bytes"`
	IsLink bool           `tl:"bool"`
}

type DataFullEmpty struct{}

type NewShardBlock struct {
	ID      ton.BlockIDExt `tl:"struct"`
	CCSeqno int32          `tl:"int"`
	Data    []byte         `tl:"bytes"`
}

type NewShardBlockBroadcast struct {
	Block NewShardBlock `tl:"struct"`
}

type BlockSignature struct {
	Who       []byte `tl:"int256"`
	Signature []byte `tl:"bytes"`
}

type SignatureSetOrdinary struct {
	CatchainSeqno    int32            `tl:"int"`
	ValidatorSetHash int32            `tl:"int"`
	Signatures       []BlockSignature `tl:"vector struct"`
}

type SignatureSetSimplex struct {
	Final            bool             `tl:"bool"`
	CatchainSeqno    int32            `tl:"int"`
	ValidatorSetHash int32            `tl:"int"`
	Signatures       []BlockSignature `tl:"vector struct"`
	SessionID        []byte           `tl:"int256"`
	Slot             int32            `tl:"int"`
	Candidate        any              `tl:"struct boxed [consensus.candidateHashDataOrdinary,consensus.candidateHashDataEmpty]"`
}

type BlockBroadcast struct {
	ID               ton.BlockIDExt   `tl:"struct"`
	CatchainSeqno    int32            `tl:"int"`
	ValidatorSetHash int32            `tl:"int"`
	Signatures       []BlockSignature `tl:"vector struct"`
	Proof            []byte           `tl:"bytes"`
	Data             []byte           `tl:"bytes"`
}

type BlockBroadcastCompressed struct {
	ID               ton.BlockIDExt `tl:"struct"`
	CatchainSeqno    int32          `tl:"int"`
	ValidatorSetHash int32          `tl:"int"`
	Flags            uint32         `tl:"flags"`
	Compressed       []byte         `tl:"bytes"`
}

type BlockBroadcastCompressedData struct {
	Signatures []BlockSignature `tl:"vector struct"`
	ProofData  []byte           `tl:"bytes"`
}

type BlockBroadcastCompressedV2 struct {
	ID             ton.BlockIDExt `tl:"struct"`
	SignatureSet   any            `tl:"struct boxed [tonNode.signatureSet.ordinary,tonNode.signatureSet.simplex]"`
	Flags          uint32         `tl:"flags"`
	Proof          []byte         `tl:"bytes"`
	DataCompressed []byte         `tl:"bytes"`
}

type NewBlockCandidateBroadcast struct {
	ID                ton.BlockIDExt `tl:"struct"`
	CatchainSeqno     int32          `tl:"int"`
	ValidatorSetHash  int32          `tl:"int"`
	CollatorSignature BlockSignature `tl:"struct"`
	Data              []byte         `tl:"bytes"`
}

type NewBlockCandidateBroadcastCompressed struct {
	ID                ton.BlockIDExt `tl:"struct"`
	CatchainSeqno     int32          `tl:"int"`
	ValidatorSetHash  int32          `tl:"int"`
	CollatorSignature BlockSignature `tl:"struct"`
	Flags             uint32         `tl:"flags"`
	Compressed        []byte         `tl:"bytes"`
}

type NewBlockCandidateBroadcastCompressedV2 struct {
	ID                ton.BlockIDExt `tl:"struct"`
	CatchainSeqno     int32          `tl:"int"`
	ValidatorSetHash  int32          `tl:"int"`
	CollatorSignature BlockSignature `tl:"struct"`
	Flags             uint32         `tl:"flags"`
	Compressed        []byte         `tl:"bytes"`
}
