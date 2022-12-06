package overlay

import "github.com/xssnick/tonutils-go/tl"

func init() {
	tl.Register(Node{}, "overlay.node id:PublicKey overlay:int256 version:int signature:bytes = overlay.Node")
	tl.Register(NodesList{}, "overlay.nodes nodes:(vector overlay.node) = overlay.Nodes")
	tl.Register(GetRandomPeers{}, "overlay.getRandomPeers peers:overlay.nodes = overlay.Nodes")
	tl.Register(Query{}, "overlay.query overlay:int256 = True")
}

type Node struct {
	ID        any    `tl:"struct boxed [pub.ed25519,pub.aes]"`
	Overlay   []byte `tl:"int256"`
	Version   int32  `tl:"int"`
	Signature []byte `tl:"bytes"`
}

type NodesList struct {
	List []Node `tl:"vector struct"`
}

type GetRandomPeers struct {
	List NodesList `tl:"struct"`
}

type Query struct {
	Overlay []byte `tl:"int256"`
}
