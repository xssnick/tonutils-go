package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestEmulateTransactionPreservesNonExistAccountEndLT(t *testing.T) {
	now := uint32(tonopsTestTime.Unix())
	priceCell := buildTransactionMsgForwardPricesCell(t, 100, 1<<15)
	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: 13, Capabilities: 4})
	if err != nil {
		t.Fatalf("failed to build global version config: %v", err)
	}
	configRoot := buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion:               versionCell,
		tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
		tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
	})

	for _, buildProof := range []bool{false, true} {
		t.Run(proofModeName(buildProof), func(t *testing.T) {
			params := testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				ConfigRoot:  configRoot,
				BuildProof:  buildProof,
			}
			block, err := params.blockContext()
			if err != nil {
				t.Fatalf("failed to prepare block context: %v", err)
			}
			account, err := PrepareAccount(buildTransactionTestNoneShardAccount(t), tonopsTestAddr)
			if err != nil {
				t.Fatalf("failed to prepare account: %v", err)
			}

			firstMessage := prepareNonExistBounceMessage(t, uint64(transactionTestLogicalTime-1))
			first, err := NewTVM().EmulateTransaction(block, account, firstMessage, params.txOptions())
			if err != nil {
				t.Fatalf("first transaction failed: %v", err)
			}
			if first.NextAccount.State().IsValid {
				t.Fatal("first transaction unexpectedly created an account")
			}
			if first.EndLT != first.StartLT+2 {
				t.Fatalf("first logical time range = [%d, %d), want one outbound message", first.StartLT, first.EndLT)
			}

			secondMessage := prepareNonExistBounceMessage(t, uint64(transactionTestLogicalTime))
			second, err := NewTVM().EmulateTransaction(block, first.NextAccount, secondMessage, params.txOptions())
			if err != nil {
				t.Fatalf("second transaction failed: %v", err)
			}
			if second.StartLT != first.EndLT {
				t.Fatalf("second transaction starts at %d before previous end LT %d", second.StartLT, first.EndLT)
			}
		})
	}
}

func prepareNonExistBounceMessage(t *testing.T, createdLT uint64) *PreparedMessage {
	t.Helper()

	msgCell, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1000),
		CreatedLT:   createdLT,
		Body:        cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build internal message: %v", err)
	}
	message, err := PrepareMessage(msgCell)
	if err != nil {
		t.Fatalf("failed to prepare internal message: %v", err)
	}
	return message
}

func proofModeName(buildProof bool) string {
	if buildProof {
		return "proof"
	}
	return "plain"
}
