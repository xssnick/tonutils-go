package dht

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
	"reflect"
)

func init() {
	tl.Register(FindNode{}, "dht.findNode key:int256 k:int = dht.Nodes")
	tl.Register(FindValue{}, "dht.findValue key:int256 k:int = dht.ValueResult")
	tl.Register(SignedAddressListQuery{}, "dht.getSignedAddressList = dht.Node")
	tl.Register(Node{}, "dht.node id:PublicKey addr_list:adnl.addressList version:int signature:bytes = dht.Node")
	tl.Register(NodesList{}, "dht.nodes nodes:(vector dht.node) = dht.Nodes")
	tl.Register(ValueFoundResult{}, "dht.valueFound value:dht.Value = dht.ValueResult")
	tl.Register(ValueNotFoundResult{}, "dht.valueNotFound nodes:dht.nodes = dht.ValueResult")
	tl.Register(Value{}, "dht.value key:dht.keyDescription value:bytes ttl:int signature:bytes = dht.Value")
	tl.Register(Key{}, "dht.key id:int256 name:bytes idx:int = dht.Key")
	tl.Register(KeyDescription{}, "dht.keyDescription key:dht.key id:PublicKey update_rule:dht.UpdateRule signature:bytes = dht.KeyDescription")
	tl.Register(UpdateRuleSignature{}, "dht.updateRule.signature = dht.UpdateRule")
	tl.Register(UpdateRuleAnybody{}, "dht.updateRule.anybody = dht.UpdateRule")
	tl.Register(UpdateRuleOverlayNodes{}, "dht.updateRule.overlayNodes = dht.UpdateRule")
	tl.Register(Query{}, "dht.query node:dht.node = True")
	tl.Register(Store{}, "dht.store value:dht.value = dht.Stored")
	tl.Register(Stored{}, "dht.stored = dht.Stored")
	tl.Register(Ping{}, "dht.ping random_id:long = dht.Pong")
	tl.Register(Pong{}, "dht.pong random_id:long = dht.Pong")
}

type FindNode struct {
	Key []byte `tl:"int256"`
	K   int32  `tl:"int"`
}

type FindValue struct {
	Key []byte `tl:"int256"`
	K   int32  `tl:"int"`
}

type ValueFoundResult struct {
	Value Value `tl:"struct boxed"`
}

type ValueNotFoundResult struct {
	Nodes NodesList `tl:"struct"`
}

type Value struct {
	KeyDescription KeyDescription `tl:"struct"`
	Data           []byte         `tl:"bytes"`
	TTL            int32          `tl:"int"`
	Signature      []byte         `tl:"bytes"`
}

type Key struct {
	ID    []byte `tl:"int256"`
	Name  []byte `tl:"bytes"`
	Index int32  `tl:"int"`
}

type KeyDescription struct {
	Key        Key    `tl:"struct"`
	ID         any    `tl:"struct boxed [pub.ed25519,pub.aes,pub.unenc,pub.overlay]"`
	UpdateRule any    `tl:"struct boxed [dht.updateRule.signature,dht.updateRule.anybody,dht.updateRule.overlayNodes]"`
	Signature  []byte `tl:"bytes"`
}

type SignedAddressListQuery struct{}

type Node struct {
	ID        any           `tl:"struct boxed [pub.ed25519,pub.aes]"`
	AddrList  *address.List `tl:"struct"`
	Version   int32         `tl:"int"`
	Signature []byte        `tl:"bytes"`
}

type NodesList struct {
	List []*Node `tl:"vector struct"`
}

type UpdateRuleSignature struct{}
type UpdateRuleAnybody struct{}
type UpdateRuleOverlayNodes struct{}

type Query struct {
	Node *Node `tl:"struct"`
}

type Store struct {
	Value *Value `tl:"struct"`
}

type Stored struct{}

type Ping struct {
	ID int64 `tl:"long"`
}

type Pong struct {
	ID int64 `tl:"long"`
}

func (n *Node) CheckSignature() error {
	pub, ok := n.ID.(keys.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("unsupported id type %s", reflect.TypeOf(n.ID).String())
	}

	signature := n.Signature
	n.Signature = nil
	toVerify, err := tl.Serialize(n, true)
	if err != nil {
		return fmt.Errorf("failed to serialize node: %w", err)
	}
	if !ed25519.Verify(pub.Key, toVerify, signature) {
		return fmt.Errorf("bad signature for node: %s", hex.EncodeToString(pub.Key))
	}
	n.Signature = signature
	return nil
}
