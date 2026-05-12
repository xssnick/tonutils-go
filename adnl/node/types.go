package node

import "github.com/xssnick/tonutils-go/tl"

func init() {
	tl.Register(SessionID{}, "tonNode.sessionId workchain:int shard:long cc_seqno:int opts_hash:int256 = tonNode.SessionId")
	tl.Register(ShardPublicOverlayID{}, "tonNode.shardPublicOverlayId workchain:int shard:long zero_state_file_hash:int256 = tonNode.ShardPublicOverlayId")
	tl.Register(ShardID{}, "tonNode.shardId workchain:int shard:long = tonNode.ShardId")
	tl.Register(PrivateBlockOverlayID{}, "tonNode.privateBlockOverlayId zero_state_file_hash:int256 nodes:(vector int256) = tonNode.PrivateBlockOverlayId")
	tl.Register(CustomOverlayID{}, "tonNode.customOverlayId zero_state_file_hash:int256 name:string nodes:(vector int256) = tonNode.CustomOverlayId")
	tl.Register(FastSyncOverlayID{}, "tonNode.fastSyncOverlayId zero_state_file_hash:int256 shard:tonNode.shardId = tonNode.FastSyncOverlayId")
	tl.Register(ConsensusOverlayID{}, "consensus.overlayId session_id:int256 nodes:(vector int256) = consensus.OverlayId")
}

type ShardPublicOverlayID struct {
	Workchain         int32  `tl:"int"`
	Shard             int64  `tl:"long"`
	ZeroStateFileHash []byte `tl:"int256"`
}

type SessionID struct {
	Workchain int32  `tl:"int"`
	Shard     int64  `tl:"long"`
	CCSeqno   int32  `tl:"int"`
	OptsHash  []byte `tl:"int256"`
}

type ShardID struct {
	Workchain int32 `tl:"int"`
	Shard     int64 `tl:"long"`
}

type PrivateBlockOverlayID struct {
	ZeroStateFileHash []byte   `tl:"int256"`
	Nodes             [][]byte `tl:"vector int256"`
}

type CustomOverlayID struct {
	ZeroStateFileHash []byte   `tl:"int256"`
	Name              string   `tl:"string"`
	Nodes             [][]byte `tl:"vector int256"`
}

type FastSyncOverlayID struct {
	ZeroStateFileHash []byte  `tl:"int256"`
	Shard             ShardID `tl:"struct"`
}

type ConsensusOverlayID struct {
	SessionID []byte   `tl:"int256"`
	Nodes     [][]byte `tl:"vector int256"`
}
