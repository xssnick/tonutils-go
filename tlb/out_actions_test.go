package tlb

import (
	"bytes"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestLoadOutListTypedActions(t *testing.T) {
	sendMsg := &InternalMessage{
		IHRDisabled: true,
		SrcAddr:     address.MustParseRawAddr("-1:0000000000000000000000000000000000000000000000000000000000000000"),
		DstAddr:     address.MustParseRawAddr("0:0000000000000000000000000000000000000000000000000000000000000000"),
		Amount:      FromNanoTONU(100),
		Body:        cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell(),
	}
	sendMsgCell, err := ToCell(sendMsg)
	if err != nil {
		t.Fatalf("failed to build send message: %v", err)
	}

	newCode := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	libCell := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	hash := bytes.Repeat([]byte{0x11}, 32)

	empty := cell.BeginCell().EndCell()
	first, err := ToCell(OutList{
		Prev: empty,
		Out: ActionSendMsg{
			Mode: 1,
			Msg:  sendMsgCell,
		},
	})
	if err != nil {
		t.Fatalf("failed to build send action: %v", err)
	}
	second, err := ToCell(OutList{
		Prev: first,
		Out: ActionSetCode{
			NewCode: newCode,
		},
	})
	if err != nil {
		t.Fatalf("failed to build setcode action: %v", err)
	}
	third, err := ToCell(OutList{
		Prev: second,
		Out: ActionReserveCurrency{
			Mode: 3,
			Currency: CurrencyCollection{
				Coins: FromNanoTONU(777),
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to build reserve action: %v", err)
	}
	fourth, err := ToCell(OutList{
		Prev: third,
		Out: ActionChangeLibrary{
			Mode: 2,
			LibRef: LibRefHash{
				LibHash: hash,
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to build changelib-hash action: %v", err)
	}
	root, err := ToCell(OutList{
		Prev: fourth,
		Out: ActionChangeLibrary{
			Mode: 1,
			LibRef: LibRefRef{
				Library: libCell,
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to build changelib-ref action: %v", err)
	}

	actions, err := LoadOutList(root)
	if err != nil {
		t.Fatalf("failed to load out list: %v", err)
	}
	if len(actions) != 5 {
		t.Fatalf("expected 5 actions, got %d", len(actions))
	}

	send, ok := actions[0].(ActionSendMsg)
	if !ok || send.Mode != 1 || !bytes.Equal(send.Msg.Hash(), sendMsgCell.Hash()) {
		t.Fatalf("unexpected send action: %#v", actions[0])
	}

	setCode, ok := actions[1].(ActionSetCode)
	if !ok || !bytes.Equal(setCode.NewCode.Hash(), newCode.Hash()) {
		t.Fatalf("unexpected setcode action: %#v", actions[1])
	}

	reserve, ok := actions[2].(ActionReserveCurrency)
	if !ok || reserve.Mode != 3 || reserve.Currency.Coins.Nano().Uint64() != 777 {
		t.Fatalf("unexpected reserve action: %#v", actions[2])
	}

	changeHash, ok := actions[3].(ActionChangeLibrary)
	if !ok || changeHash.Mode != 2 {
		t.Fatalf("unexpected changelib hash action: %#v", actions[3])
	}
	libHash, ok := changeHash.LibRef.(LibRefHash)
	if !ok || !bytes.Equal(libHash.LibHash, hash) {
		t.Fatalf("unexpected lib hash ref: %#v", changeHash.LibRef)
	}

	changeRef, ok := actions[4].(ActionChangeLibrary)
	if !ok || changeRef.Mode != 1 {
		t.Fatalf("unexpected changelib ref action: %#v", actions[4])
	}
	libRef, ok := changeRef.LibRef.(LibRefRef)
	if !ok || !bytes.Equal(libRef.Library.Hash(), libCell.Hash()) {
		t.Fatalf("unexpected lib cell ref: %#v", changeRef.LibRef)
	}
}
