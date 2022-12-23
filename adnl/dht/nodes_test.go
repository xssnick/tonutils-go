package dht

import (
	"encoding/hex"
	"github.com/xssnick/tonutils-go/adnl"
	"math/big"
	"testing"
)

func TestPriorityList_addNode(t *testing.T) {
	adnlAddr := "516618cf6cbe9004f6883e742c9a2e3ca53ed02e3e36f4cef62a98ee1e449174"
	siteAddr, err := hex.DecodeString(adnlAddr)
	if err != nil {
		t.Fatal("failed to prepare test site address, err: ", err.Error())
	}
	k := Key{
		ID:    siteAddr,
		Name:  []byte("address"),
		Index: 0,
	}
	keyId, err := adnl.ToKeyID(k)
	if err != nil {
		t.Fatal("failed to prepare test key id")
	}

	pubKey1 := "a87e430f621471f0b1ad8f9004d81909ec55cb3a6efbfc4da326ec5e004eecf5"
	tPubKey1, err := hex.DecodeString(pubKey1)
	if err != nil {
		t.Fatal("failed to prepare test public key")
	}

	kId1, err := adnl.ToKeyID(adnl.PublicKeyED25519{tPubKey1})
	if err != nil {
		t.Fatal("failed to prepare test key ID")
	}

	pubKey2 := "3d496fbb1ed8d395e7b31969f9f33cce8530631d499ecec70c7c54ecdf1ca47e"
	tPubKey2, err := hex.DecodeString(pubKey2)
	if err != nil {
		t.Fatal("failed to prepare test public key")
	}

	kId2, err := adnl.ToKeyID(adnl.PublicKeyED25519{tPubKey2})
	if err != nil {
		t.Fatal("failed to prepare test key ID")
	}

	pubKey3 := "d67fb87bb90d765ff09178cde04d8d8cca5f63146e0eb882ebddf53559c6716a"
	tPubKey3, err := hex.DecodeString(pubKey3)
	if err != nil {
		t.Fatal("failed to prepare test public key")
	}

	kId3, err := adnl.ToKeyID(adnl.PublicKeyED25519{tPubKey3})
	if err != nil {
		t.Fatal("failed to prepare test key ID")
	}

	node1 := dhtNode{
		id:   kId1,
		adnl: nil,
		ping: 0,
	}
	priority1 := new(big.Int).SetBytes(xor(keyId, node1.id))

	node2 := dhtNode{
		id:   kId2,
		adnl: nil,
		ping: 0,
	}
	priority2 := new(big.Int).SetBytes(xor(keyId, node2.id))

	node3 := dhtNode{
		id:   kId3,
		adnl: nil,
		ping: 0,
	}
	priority3 := new(big.Int).SetBytes(xor(keyId, node3.id))

	pList := newPriorityList(12)
	ok := pList.addNode(&node1, priority1)
	if !ok {
		t.Fatal()
	}
	ok = pList.addNode(&node2, priority2)
	if !ok {
		t.Fatal()
	}
	ok = pList.addNode(&node3, priority3)
	if !ok {
		t.Fatal()
	}
	nodeOrder := make(map[string]uint8)
	nodeOrder["f6b6a3b295993727f1853d94a6cc39fc5ae20b79e06d665e753562084f234c7e"] = 1
	nodeOrder["c614d4796a11c74b16e9870bf4bbeda4f13a9e9c9f87087fe87300e22961b9db"] = 2
	nodeOrder["0a76318cf057724469b6b0e55c997b42c7c7bb5d88f68b40ee3c0f0af3e8e6d5"] = 3

	t.Run("nodes priority order", func(t *testing.T) {
		for nodeNo := 1; nodeNo < 4; nodeNo++ {
			node, _ := pList.getNode()
			if nodeOrder[hex.EncodeToString(node.id)] != uint8(nodeNo) {
				t.Errorf("want node index '%d', got '%d'", nodeOrder[hex.EncodeToString(node.id)], uint8(nodeNo))
			}
		}
	})

}
