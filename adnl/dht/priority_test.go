package dht

import (
	"bytes"
	"encoding/hex"
	"github.com/xssnick/tonutils-go/adnl"
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
		adnlId:       kId1,
		currentState: _StateActive,
	}

	node2 := dhtNode{
		adnlId:       kId2,
		currentState: _StateFail,
	}

	node3 := dhtNode{
		adnlId:       kId3,
		currentState: _StateActive,
	}

	pList := newPriorityList(12, keyId)
	ok := pList.addNode(&node1)
	if !ok {
		t.Fatal()
	}
	ok = pList.addNode(&node2)
	if !ok {
		t.Fatal()
	}
	ok = pList.addNode(&node3)
	if !ok {
		t.Fatal()
	}
	nodeOrder := make(map[string]uint8)
	nodeOrder["f6b6a3b295993727f1853d94a6cc39fc5ae20b79e06d665e753562084f234c7e"] = 3
	nodeOrder["c614d4796a11c74b16e9870bf4bbeda4f13a9e9c9f87087fe87300e22961b9db"] = 1
	nodeOrder["0a76318cf057724469b6b0e55c997b42c7c7bb5d88f68b40ee3c0f0af3e8e6d5"] = 2

	t.Run("nodes priority order", func(t *testing.T) {
		for nodeNo := 1; nodeNo < 4; nodeNo++ {
			node, _ := pList.getNode()
			if nodeOrder[node.id()] != uint8(nodeNo) {
				t.Errorf("want node index '%d', got '%d'", nodeOrder[node.id()], uint8(nodeNo))
			}
		}
	})
}

func TestPriorityList_markNotUsed(t *testing.T) {
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
		adnlId: kId1,
	}

	node2 := dhtNode{
		adnlId: kId2,
	}

	node3 := dhtNode{
		adnlId: kId3,
	}

	pList := newPriorityList(12, keyId)
	ok := pList.addNode(&node1)
	if !ok {
		t.Fatal()
	}
	ok = pList.addNode(&node2)
	if !ok {
		t.Fatal()
	}
	ok = pList.addNode(&node3)
	if !ok {
		t.Fatal()
	}
	curNode := pList.list
	for curNode != nil {
		if curNode.used {
			t.Fatal("find used node before use for some reason")
		}
		curNode = curNode.next
	}

	usedNode, _ := pList.getNode()

	t.Run("markNotUsed test", func(t *testing.T) {
		pList.markUsed(usedNode, false)

		curNode = pList.list
		for curNode != nil {
			if bytes.Equal(curNode.node.adnlId, usedNode.adnlId) {
				if curNode.used != false {
					t.Errorf("want 'false' use status, got '%v'", curNode.used)
				}
			} else {
				if curNode.used == true {
					t.Errorf("find used node for some reason")
				}
			}
			curNode = curNode.next
		}
	})
}
