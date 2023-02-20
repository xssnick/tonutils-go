package tlb

import (
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

	if intMsg.Amount.NanoTON().Uint64() != intMsg2.Amount.NanoTON().Uint64() {
		t.Fatal("not eq ton", intMsg.Amount.NanoTON(), intMsg2.Amount.NanoTON())
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
