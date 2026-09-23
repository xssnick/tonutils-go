package tvm

import (
	"fmt"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTransactionActionReferencedLazyBody(t *testing.T) {
	library := transactionSpecialMessageBodies(t)["library"]
	for _, body := range []*cell.Cell{library, cell.BeginCell().MustStoreRef(library).EndCell()} {
		for _, external := range []bool{false, true} {
			t.Run(fmt.Sprintf("special_%t/external_%t", body.IsSpecial(), external), func(t *testing.T) {
				loads := 0
				parent := cell.BeginCell().MustStoreRef(body).EndCell()
				lazy, err := cell.CreateWithLazyRefsUnsafe(
					0x0100, nil, parent.Hash(), []uint16{parent.Depth()},
					[]cell.LazyRef{{Hashes: body.Hash(), Depths: []uint16{body.Depth()}}},
					func(hash cell.Hash) (*cell.Cell, error) {
						loads++
						if hash != body.HashKey() {
							t.Fatal("validation loaded an opaque body descendant")
						}
						return body, nil
					})
				if err != nil {
					t.Fatal(err)
				}
				payload := lazy.MustPeekRef(0)
				msg := &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: &tlb.InternalMessage{
					IHRDisabled: true, SrcAddr: tonopsTestAddr, DstAddr: tonopsTestAddr, Body: payload,
				}}
				if external {
					msg = &tlb.Message{MsgType: tlb.MsgTypeExternalOut, Msg: &tlb.ExternalMessageOut{
						SrcAddr: tonopsTestAddr, DstAddr: address.NewAddressNone(), Body: payload,
					}}
				}
				builder := cell.BeginCell()
				if err := tlb.StoreMessageWithLayout(builder, msg, tlb.MessageLayout{BodyInRef: true}); err != nil {
					t.Fatal(err)
				}
				message := builder.EndCell()
				if got := transactionOutboundActionMessageStructureValid(message, false); got != !body.IsSpecial() {
					t.Fatalf("lazy body validity=%t, want %t", got, !body.IsSpecial())
				}
				if loads != 1 {
					t.Fatalf("body loads=%d, want one root load", loads)
				}
				// The normal Message schema keeps its body reference opaque.
				if _, err := validateBuiltTransactionMessage(message); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
