package dht

import (
	"encoding/base64"
	"fmt"

	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/liteclient"
)

// DHT is kept as an alias for backwards compatibility with older examples.
type DHT = Client

// BootstrapNodesFromConfig is kept for backwards compatibility with older examples.
func BootstrapNodesFromConfig(cfg *liteclient.GlobalConfig) ([]*Node, error) {
	return nodesFromConfig(cfg)
}

func nodesFromConfig(cfg *liteclient.GlobalConfig) ([]*Node, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}

	nodes := make([]*Node, 0, len(cfg.DHT.StaticNodes.Nodes))
	for _, node := range cfg.DHT.StaticNodes.Nodes {
		key, err := base64.StdEncoding.DecodeString(node.ID.Key)
		if err != nil {
			continue
		}

		sign, err := base64.StdEncoding.DecodeString(node.Signature)
		if err != nil {
			continue
		}

		n := &Node{
			ID: keys.PublicKeyED25519{
				Key: key,
			},
			AddrList: &address.List{
				Version:    int32(node.AddrList.Version),
				ReinitDate: int32(node.AddrList.ReinitDate),
				Priority:   int32(node.AddrList.Priority),
				ExpireAt:   int32(node.AddrList.ExpireAt),
			},
			Version:   int32(node.Version),
			Signature: sign,
		}

		for _, addr := range node.AddrList.Addrs {
			adnlAddr, err := addr.ToADNLAddress()
			if err != nil {
				continue
			}
			n.AddrList.Addresses = append(n.AddrList.Addresses, adnlAddr)
		}

		nodes = append(nodes, n)
	}

	return nodes, nil
}
