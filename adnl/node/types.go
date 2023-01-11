package node

import "github.com/xssnick/tonutils-go/tl"

func init() {
	tl.Register(ShardPublicOverlayID{}, "tonNode.shardPublicOverlayId workchain:int shard:long zero_state_file_hash:int256 = tonNode.ShardPublicOverlayId")
}

type ShardPublicOverlayID struct {
	Workchain         int32  `tl:"int"`
	Shard             int64  `tl:"long"`
	ZeroStateFileHash []byte `tl:"int256"`
}
