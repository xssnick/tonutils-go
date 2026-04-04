package adnl

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"

	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

func init() {
	tl.Register(Node{}, "adnl.node id:PublicKey addr_list:adnl.addressList version:int signature:bytes = adnl.Node")
}

type Node struct {
	ID        any           `tl:"struct boxed [pub.ed25519,pub.aes]"`
	AddrList  *address.List `tl:"struct"`
	Version   int32         `tl:"int"`
	Signature []byte        `tl:"bytes"`
}

func (n *Node) CheckSignature() error {
	pub, ok := n.ID.(keys.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("unsupported id type %T", n.ID)
	}

	nodeCopy := *n
	signature := append([]byte{}, n.Signature...)
	nodeCopy.Signature = nil

	data, err := tl.Serialize(nodeCopy, true)
	if err != nil {
		return fmt.Errorf("failed to serialize node: %w", err)
	}
	if !ed25519.Verify(pub.Key, data, signature) {
		return fmt.Errorf("bad signature for node: %s", hex.EncodeToString(pub.Key))
	}
	return nil
}
