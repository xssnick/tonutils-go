package node

import "github.com/xssnick/tonutils-go/tl"

func init() {
	tl.Register(BlockIDExt{}, "tonNode.blockIdExt workchain:int shard:long seqno:int root_hash:int256 file_hash:int256 = tonNode.BlockIdExt")
	tl.Register(DownloadBlock{}, "tonNode.downloadBlock block:tonNode.blockIdExt = tonNode.Data")
	tl.Register(DownloadBlockFull{}, "tonNode.downloadBlockFull block:tonNode.blockIdExt = tonNode.DataFull")
	tl.Register(DataFull{}, "tonNode.dataFull id:tonNode.blockIdExt proof:bytes block:bytes is_link:Bool = tonNode.DataFull")
	tl.Register(DataFullEmpty{}, "tonNode.dataFullEmpty = tonNode.DataFull")

}

type DownloadBlock struct {
	Block BlockIDExt `tl:"struct"`
}

type DownloadBlockFull struct {
	Block BlockIDExt `tl:"struct"`
}

type BlockIDExt struct {
	Workchain int32  `tl:"int"`
	Shard     int64  `tl:"long"`
	Seqno     int32  `tl:"int"`
	RootHash  []byte `tl:"int256"`
	FileHash  []byte `tl:"int256"`
}

type DataFull struct {
	ID     BlockIDExt `tl:"struct"`
	Proof  []byte     `tl:"bytes"`
	Block  []byte     `tl:"bytes"`
	IsLink bool       `tl:"bool"`
}

type DataFullEmpty struct{}
