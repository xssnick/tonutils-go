package dht

import (
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
	"reflect"
)

const (
	_UnknownNetworkID = int32(-1)
	_MaxValueSize     = 768
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
	tl.Register(ClientNotFoundResult{}, "dht.clientNotFound nodes:dht.nodes = dht.ReversePingResult")
	tl.Register(ReversePingOKResult{}, "dht.reversePingOk = dht.ReversePingResult")
	tl.Register(Message{}, "dht.message node:dht.node = dht.Message")
	tl.Register(RequestReversePingCont{}, "dht.requestReversePingCont target:adnl.Node signature:bytes client:int256 = dht.RequestReversePingCont")
	tl.Register(RegisterReverseConnection{}, "dht.registerReverseConnection node:PublicKey ttl:int signature:bytes = dht.Stored")
	tl.Register(RequestReversePing{}, "dht.requestReversePing target:adnl.Node signature:bytes client:int256 k:int = dht.ReversePingResult")
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
type ClientNotFoundResult struct {
	Nodes NodesList `tl:"struct"`
}

type ReversePingOKResult struct{}

type Message struct {
	Node *Node `tl:"struct"`
}

type RegisterReverseConnection struct {
	Node      any    `tl:"struct boxed [pub.ed25519,pub.aes,pub.unenc,pub.overlay]"`
	TTL       int32  `tl:"int"`
	Signature []byte `tl:"bytes"`
}

type RequestReversePing struct {
	Target    adnl.Node `tl:"struct boxed"`
	Signature []byte    `tl:"bytes"`
	Client    []byte    `tl:"int256"`
	K         int32     `tl:"int"`
}

type RequestReversePingCont struct {
	Target    adnl.Node `tl:"struct boxed"`
	Signature []byte    `tl:"bytes"`
	Client    []byte    `tl:"int256"`
}

type Ping struct {
	ID int64 `tl:"long"`
}

type Pong struct {
	ID int64 `tl:"long"`
}

func (n *Node) CheckSignature() error {
	return n.CheckSignatureWithNetworkID(_UnknownNetworkID)
}

func (n *Node) CheckSignatureWithNetworkID(networkID int32) error {
	return n.checkSignature(networkID)
}

func (n *Node) validate(currentVersion, ourNetworkID int32) error {
	if n == nil {
		return fmt.Errorf("nil node")
	}
	if currentVersion != 0 && n.Version <= currentVersion {
		return fmt.Errorf("too old version")
	}
	if n.AddrList == nil || len(n.AddrList.Addresses) == 0 {
		return fmt.Errorf("dht node must have >0 addresses")
	}
	for i, addr := range n.AddrList.Addresses {
		if addr == nil {
			return fmt.Errorf("dht node address %d is nil", i)
		}
	}
	return n.CheckSignatureWithNetworkID(ourNetworkID)
}

func (n *Node) checkSignature(ourNetworkID int32) error {
	pub, ok := n.ID.(keys.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("unsupported id type %s", reflect.TypeOf(n.ID).String())
	}

	signature, err := splitNodeSignature(n.Signature, ourNetworkID)
	if err != nil {
		return err
	}

	nodeCopy := *n
	nodeCopy.Signature = nil
	toVerify, err := tl.Serialize(nodeCopy, true)
	if err != nil {
		return fmt.Errorf("failed to serialize node: %w", err)
	}
	if !ed25519.Verify(pub.Key, toVerify, signature) {
		return fmt.Errorf("bad signature for node: %s", hex.EncodeToString(pub.Key))
	}
	return nil
}

func splitNodeSignature(signature []byte, ourNetworkID int32) ([]byte, error) {
	switch len(signature) {
	case ed25519.SignatureSize:
		return signature, nil
	case ed25519.SignatureSize + 4:
		networkID := int32(binary.LittleEndian.Uint32(signature[:4]))
		if ourNetworkID != _UnknownNetworkID && networkID != _UnknownNetworkID && networkID != ourNetworkID {
			return nil, fmt.Errorf("wrong network id (expected %d, found %d)", ourNetworkID, networkID)
		}
		return signature[4:], nil
	default:
		return nil, fmt.Errorf("invalid length of signature")
	}
}

func cloneAddressList(list *address.List) *address.List {
	return address.CloneList(list)
}

func cloneNode(node *Node) *Node {
	if node == nil {
		return nil
	}

	return &Node{
		ID:        node.ID,
		AddrList:  cloneAddressList(node.AddrList),
		Version:   node.Version,
		Signature: append([]byte{}, node.Signature...),
	}
}

func buildSignedNode(id any, list *address.List, version, networkID int32, key ed25519.PrivateKey) (*Node, error) {
	node := &Node{
		ID:       id,
		AddrList: cloneAddressList(list),
		Version:  version,
	}

	signature, err := signNode(node, networkID, key)
	if err != nil {
		return nil, err
	}
	node.Signature = signature
	return node, nil
}

func signNode(node *Node, networkID int32, key ed25519.PrivateKey) ([]byte, error) {
	nodeCopy := cloneNode(node)
	nodeCopy.Signature = nil

	data, err := tl.Serialize(nodeCopy, true)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize node: %w", err)
	}

	signature := ed25519.Sign(key, data)
	if networkID == _UnknownNetworkID {
		return signature, nil
	}

	ext := make([]byte, 4+len(signature))
	binary.LittleEndian.PutUint32(ext[:4], uint32(networkID))
	copy(ext[4:], signature)
	return ext, nil
}
