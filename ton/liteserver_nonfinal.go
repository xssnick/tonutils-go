package ton

import "github.com/xssnick/tonutils-go/tl"

func init() {
	tl.Register(NonfinalCandidateID{}, "liteServer.nonfinal.candidateId block_id:tonNode.blockIdExt creator:int256 collated_data_hash:int256 = liteServer.nonfinal.CandidateId")
	tl.Register(NonfinalCandidate{}, "liteServer.nonfinal.candidate id:liteServer.nonfinal.candidateId data:bytes collated_data:bytes = liteServer.nonfinal.Candidate")
	tl.Register(NonfinalCandidateInfo{}, "liteServer.nonfinal.candidateInfo id:liteServer.nonfinal.candidateId available:Bool approved_weight:long signed_weight:long total_weight:long = liteServer.nonfinal.CandidateInfo")
	tl.Register(NonfinalValidatorGroupInfo{}, "liteServer.nonfinal.validatorGroupInfo next_block_id:tonNode.blockId cc_seqno:int prev:(vector tonNode.blockIdExt) candidates:(vector liteServer.nonfinal.candidateInfo) = liteServer.nonfinal.ValidatorGroupInfo")
	tl.Register(NonfinalValidatorGroups{}, "liteServer.nonfinal.validatorGroups groups:(vector liteServer.nonfinal.validatorGroupInfo) = liteServer.nonfinal.ValidatorGroups")
	tl.Register(NonfinalPendingShardBlocks{}, "liteServer.nonfinal.pendingShardBlocks signed_blocks:(vector tonNode.blockIdExt) candidates:(vector tonNode.blockIdExt) = liteServer.nonfinal.PendingShardBlocks")

	tl.Register(NonfinalGetValidatorGroups{}, "liteServer.nonfinal.getValidatorGroups mode:# wc:mode.0?int shard:mode.0?long = liteServer.nonfinal.ValidatorGroups")
	tl.Register(NonfinalGetCandidate{}, "liteServer.nonfinal.getCandidate id:liteServer.nonfinal.candidateId = liteServer.nonfinal.Candidate")
	tl.Register(NonfinalGetPendingShardBlocks{}, "liteServer.nonfinal.getPendingShardBlocks mode:# wc:mode.0?int shard:mode.0?long = liteServer.nonfinal.PendingShardBlocks")
}

type NonfinalCandidateID struct {
	BlockID          *BlockIDExt `tl:"struct"`
	Creator          []byte      `tl:"int256"`
	CollatedDataHash []byte      `tl:"int256"`
}

type NonfinalCandidate struct {
	ID           *NonfinalCandidateID `tl:"struct"`
	Data         []byte               `tl:"bytes"`
	CollatedData []byte               `tl:"bytes"`
}

type NonfinalCandidateInfo struct {
	ID             *NonfinalCandidateID `tl:"struct"`
	Available      bool                 `tl:"bool"`
	ApprovedWeight int64                `tl:"long"`
	SignedWeight   int64                `tl:"long"`
	TotalWeight    int64                `tl:"long"`
}

type NonfinalValidatorGroupInfo struct {
	NextBlockID *BlockInfoShort         `tl:"struct"`
	CCSeqno     int32                   `tl:"int"`
	Prev        []*BlockIDExt           `tl:"vector struct"`
	Candidates  []NonfinalCandidateInfo `tl:"vector struct"`
}

type NonfinalValidatorGroups struct {
	Groups []NonfinalValidatorGroupInfo `tl:"vector struct"`
}

type NonfinalPendingShardBlocks struct {
	SignedBlocks []*BlockIDExt `tl:"vector struct"`
	Candidates   []*BlockIDExt `tl:"vector struct"`
}

type NonfinalGetValidatorGroups struct {
	Mode  uint32 `tl:"flags"`
	WC    int32  `tl:"?0 int"`
	Shard int64  `tl:"?0 long"`
}

type NonfinalGetCandidate struct {
	ID *NonfinalCandidateID `tl:"struct"`
}

type NonfinalGetPendingShardBlocks struct {
	Mode  uint32 `tl:"flags"`
	WC    int32  `tl:"?0 int"`
	Shard int64  `tl:"?0 long"`
}
