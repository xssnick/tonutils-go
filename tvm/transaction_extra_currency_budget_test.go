package tvm

import (
	"fmt"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTransactionActionExtraCurrencyValidationBudget(t *testing.T) {
	for _, count := range []int{511, 512, 513} {
		extra := transactionHistoricalBudgetCurrencies(t, count)
		for _, referenced := range []bool{false, true} {
			// Modern prepass treats this oversized library dictionary as opaque.
			message := transactionHistoricalBudgetMessage(t, transactionStateInitBudgetLibraries(t, 513), extra, referenced, false)
			for _, version := range []uint32{0, 7, 8, 13, 16} {
				for _, mode := range []uint8{1, 3} {
					t.Run(fmt.Sprintf("extra%d/ref%t/v%d/mode%d", count, referenced, version, mode), func(t *testing.T) {
						actions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: mode, Msg: message})
						got, err := transactionLoadActions(actions, version, false)
						if err != nil {
							t.Fatal(err)
						}
						wantCode := int32(0)
						wantSkipped := uint16(0)
						if count > 511 {
							if version >= 8 && mode&2 != 0 {
								wantSkipped = 1
							} else {
								wantCode = 34
							}
						}
						if got.resultCode != wantCode || got.skippedActions != wantSkipped {
							t.Fatalf("prepass code=%d skipped=%d, want code=%d skipped=%d", got.resultCode, got.skippedActions, wantCode, wantSkipped)
						}
					})
				}
			}
		}
	}
}

func TestTransactionActionExtraCurrencyBudgetPreservesCanonicalityStage(t *testing.T) {
	for _, value := range []*cell.Cell{
		cell.BeginCell().MustStoreUInt(0, 5).EndCell(),
		cell.BeginCell().MustStoreUInt(1, 5).MustStoreUInt(0, 8).EndCell(),
		cell.BeginCell().MustStoreUInt(2, 5).MustStoreUInt(1, 16).EndCell(),
	} {
		extra := cell.NewDict(32)
		if err := extra.Set(cell.BeginCell().MustStoreUInt(0, 32).EndCell(), value); err != nil {
			t.Fatal(err)
		}
		msg := transactionHistoricalBudgetMessage(t, nil, extra, false, false)
		for _, historical := range []bool{false, true} {
			if !transactionOutboundActionMessageStructureValid(msg, historical) {
				t.Fatalf("structurally valid currency rejected before SEND canonicality checks: historical=%t", historical)
			}
		}
	}
}

func TestTransactionActionReferencedBodyStrongValidation(t *testing.T) {
	proof, err := cell.CreateMerkleProof(cell.BeginCell().MustStoreUInt(1, 8).EndCell())
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]*cell.Cell{
		"ordinary": cell.BeginCell().MustStoreRef(proof).EndCell(),
		"proof":    proof,
		"library":  transactionSpecialMessageBodies(t)["library"],
	} {
		t.Run(name, func(t *testing.T) {
			msg := transactionMessageWithBodyLayout(t, body, true)
			for _, historical := range []bool{false, true} {
				if got := transactionOutboundActionMessageStructureValid(msg, historical); got != (name == "ordinary") {
					t.Fatalf("historical=%t: referenced body valid=%t", historical, got)
				}
			}
		})
	}
}
