package dht

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/liteclient"
	"net"
	"testing"
	"time"
)

func TestClient_FindAddresses(t *testing.T) {
	testAddr := "516618cf6cbe9004f6883e742c9a2e3ca53ed02e3e36f4cef62a98ee1e449174" // ADNL address of foundation.ton

	cfg, err := liteclient.GetConfigFromUrl(context.Background(), "https://ton-blockchain.github.io/global.config.json")
	if err != nil {
		t.Fatalf("cannot fetch network config, error: %s", err)
	}

	var nodes []NodeInfo
	for i, node := range cfg.DHT.StaticNodes.Nodes {
		ip := make(net.IP, 4)
		ii := int32(node.AddrList.Addrs[0].IP)
		binary.BigEndian.PutUint32(ip, uint32(ii))

		pp, err := base64.StdEncoding.DecodeString(node.ID.Key)
		if err != nil {
			continue
		}

		nodes = append(nodes, NodeInfo{
			Address: ip.String() + ":" + fmt.Sprint(node.AddrList.Addrs[0].Port),
			Key:     pp,
		})
		fmt.Println("LOOOL", nodes[i])
	}

	dhtClient, err := NewClient(10*time.Second, nodes)
	if err != nil {
		t.Fatalf("failed to init DHT client: %s", err.Error())
	}

	time.Sleep(time.Second)
	fmt.Println("ACTIVE ")
	for k, v := range dhtClient.activeNodes {

		fmt.Println("key", k, "val", hex.EncodeToString(v.id), "adnl", v.adnl)
	}
	fmt.Println("KNOWN ")
	for k, v := range dhtClient.knownNodesInfo {
		fmt.Println(k, v.ID, v.AddrList, v.Signature, v.Version)
	}

	fmt.Println(dhtClient.queryTimeout, dhtClient.minNodeMx, dhtClient.mx)
	siteAddr, err := hex.DecodeString(testAddr)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = dhtClient.FindAddresses(context.Background(), siteAddr)
	if err != nil {
		t.Fatal(err)
	}
}
