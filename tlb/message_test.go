package tlb

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestInternalMessage_ToCell(t *testing.T) { // need to deploy contract on test-net - > than change config to test-net.
	src := address.MustParseAddr("EQAOp1zuKuX4zY6L9rEdSLam7J3gogIHhfRu_gH70u2MQnmd") // new address from test net
	dst := address.MustParseAddr("EQA_B407fiLIlE5VYZCaI2rki0in6kLyjdhhwitvZNfpe7eY") // new address from test net
	amount := MustFromTON("0.05")

	intMsg := InternalMessage{
		IHRDisabled: false,
		Bounce:      true,
		Bounced:     false,
		SrcAddr:     src,
		DstAddr:     dst,
		Amount:      amount,
		StateInit: &StateInit{
			Data: cell.BeginCell().EndCell(),
			Code: cell.BeginCell().EndCell(),
		},
		Body: cell.BeginCell().EndCell(),
	}

	c, err := intMsg.ToCell()
	if err != nil {
		t.Fatal("to cell err", err)
	}

	var intMsg2 InternalMessage
	err = LoadFromCell(&intMsg2, c.BeginParse())
	if err != nil {
		t.Fatal("from cell err", err)
	}

	if intMsg.SrcAddr.String() != intMsg2.SrcAddr.String() {
		t.Fatal("not eq src")
	}

	if intMsg.DstAddr.String() != intMsg2.DstAddr.String() {
		t.Fatal("not eq dst")
	}

	if intMsg.Amount.Nano().Uint64() != intMsg2.Amount.Nano().Uint64() {
		t.Fatal("not eq ton", intMsg.Amount.Nano(), intMsg2.Amount.Nano())
	}
}

func TestCornerMessage(t *testing.T) {
	msgBoc, _ := hex.DecodeString("b5ee9c724101020100860001b36800bf4c6bdca25797e55d700c1a5448e2af5d1ac16f9a9628719a4e1eb2b44d85e33fd104a366f6fb17799871f82e00e4f2eb8ae6aaf6d3e0b3fb346cd0208e23725e14094ba15d20071f12260000446ee17a9b0cc8c028d8c001004d8002b374733831aac3455708e8f1d2c7f129540b982d3a5de8325bf781083a8a3d2a04a7f943813277f3ea")

	c, err := cell.FromBOC(msgBoc)
	if err != nil {
		t.Fatal(err)
	}

	var m InternalMessage
	err = LoadFromCell(&m, c.BeginParse())
	if err != nil {
		t.Fatal(err)
	}

	c2, err := ToCell(m)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(c.Hash(), c2.Hash()) {
		t.Fatal("hash not match")
	}
}

func TestMessage_LoadFromCell(t *testing.T) {
	t.Run("internal msg case", func(t *testing.T) {
		var msg Message
		tIntMsg := &InternalMessage{
			IHRDisabled: false,
			Bounce:      true,
			Bounced:     false,
			SrcAddr:     nil,
			DstAddr:     nil,
			CreatedLT:   0,
			CreatedAt:   0,
			StateInit:   nil,
			Body:        cell.BeginCell().MustStoreUInt(777, 27).EndCell(),
		}
		_cell, err := tIntMsg.ToCell()
		if err != nil {
			t.Fatal(err)
		}
		err = msg.LoadFromCell(_cell.BeginParse())
		if err != nil {
			t.Fatal(err)
		}
		if msg.MsgType != "INTERNAL" {
			t.Errorf("wrong msg type, want INTERNAL, got %s", msg.MsgType)
		}
	})

	t.Run("external in msg case", func(t *testing.T) {
		var msg Message
		tExMsg := &ExternalMessage{
			SrcAddr:   nil,
			DstAddr:   nil,
			ImportFee: Coins{},
			StateInit: nil,
			Body:      cell.BeginCell().MustStoreUInt(777, 27).EndCell(),
		}
		_cell, err := tExMsg.ToCell()
		if err != nil {
			t.Fatal(err)
		}
		err = msg.LoadFromCell(_cell.BeginParse())
		if err != nil {
			t.Fatal(err)
		}
		if msg.MsgType != "EXTERNAL_IN" {
			t.Errorf("wrong msg type, want EXTERNAL_IN, got %s", msg.MsgType)
		}
	})

	t.Run("external out msg case", func(t *testing.T) {
		var msg Message
		tExMsg := &ExternalMessageOut{
			SrcAddr:   nil,
			DstAddr:   nil,
			CreatedLT: 0,
			CreatedAt: 0,
			StateInit: nil,
			Body:      cell.BeginCell().MustStoreUInt(777, 27).EndCell(),
		}
		_cell, err := ToCell(tExMsg)
		if err != nil {
			t.Fatal(err)
		}

		err = msg.LoadFromCell(_cell.BeginParse())
		if err != nil {
			t.Fatal(err)
		}
		if msg.MsgType != "EXTERNAL_OUT" {
			t.Errorf("wrong msg type, want EXTERNAL_OUT, got %s", msg.MsgType)
		}
	})
}
