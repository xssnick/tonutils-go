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
	tl.Register(BlockBroadcast{}, "tonNode.blockBroadcast id:tonNode.blockIdExt catchain_seqno:int validator_set_hash:int signatures:(vector tonNode.blockSignature) proof:bytes data:bytes = tonNode.Broadcast")
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

type BlockBroadcast struct {
	ID               ton.BlockIDExt   `tl:"struct"`
	CatchainSeqno    int32            `tl:"int"`
	ValidatorSetHash int32            `tl:"int"`
	Signatures       []BlockSignature `tl:"vector struct"`
	Proof            []byte           `tl:"bytes"`
	Data             []byte           `tl:"bytes"`
}
