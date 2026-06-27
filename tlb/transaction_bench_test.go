package tlb

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func BenchmarkTransactionLoadFromCell(b *testing.B) {
	txCell := benchmarkTransactionCell(b)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var tx Transaction
		if err := LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTransactionToCell(b *testing.B) {
	tx := benchmarkTransaction(b)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := tx.ToCell(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestTransactionFastRoundTripFixture(t *testing.T) {
	txCell := benchmarkTransactionCell(t)

	var tx Transaction
	if err := LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
		t.Fatal(err)
	}

	roundTrip, err := tx.ToCell()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(roundTrip.Hash(), txCell.Hash()) {
		t.Fatal("transaction round-trip hash mismatch")
	}
}

func benchmarkTransactionCell(tb testing.TB) *cell.Cell {
	tb.Helper()

	txCell, err := benchmarkTransaction(tb).ToCell()
	if err != nil {
		tb.Fatal(err)
	}

	return txCell
}

func benchmarkTransaction(tb testing.TB) *Transaction {
	tb.Helper()

	account := benchmarkBytes(0x10)
	prevHash := benchmarkBytes(0x20)
	oldHash := benchmarkBytes(0x30)
	newHash := benchmarkBytes(0x40)
	initHash := benchmarkBytes(0x50)
	finalHash := benchmarkBytes(0x60)
	actionHash := benchmarkBytes(0x70)
	src := address.NewAddress(0, 0, benchmarkBytes(0x80))
	dst := address.NewAddress(0, 0, benchmarkBytes(0x90))
	body := cell.BeginCell().
		MustStoreUInt(0, 32).
		MustStoreSlice([]byte("benchmark transaction"), 21*8).
		EndCell()

	msg := &Message{
		MsgType: MsgTypeInternal,
		Msg: &InternalMessage{
			IHRDisabled: true,
			Bounce:      true,
			SrcAddr:     src,
			DstAddr:     dst,
			Amount:      FromNanoTONU(2_100_000_000),
			IHRFee:      FromNanoTONU(1_000),
			FwdFee:      FromNanoTONU(2_000),
			CreatedLT:   1000,
			CreatedAt:   1_700_000_000,
			Body:        body,
		},
	}

	outDict := cell.NewDict(15)
	msgCell, err := msg.ToCell()
	if err != nil {
		tb.Fatal(err)
	}
	if err = outDict.SetIntKey(big.NewInt(0), cell.BeginCell().MustStoreRef(msgCell).EndCell()); err != nil {
		tb.Fatal(err)
	}

	exitArg := int32(-14)
	dueFees := FromNanoTONU(777)
	totalFwdFees := FromNanoTONU(333)
	totalActionFees := FromNanoTONU(111)
	gasCredit := big.NewInt(512)

	return &Transaction{
		AccountAddr: account,
		LT:          35290576000004,
		PrevTxHash:  prevHash,
		PrevTxLT:    35290576000003,
		Now:         1_700_000_001,
		OutMsgCount: 1,
		OrigStatus:  AccountStatusActive,
		EndStatus:   AccountStatusActive,
		TotalFees: CurrencyCollection{
			Coins: FromNanoTONU(42_000),
		},
		StateUpdate: HashUpdate{
			OldHash: oldHash,
			NewHash: newHash,
		},
		Description: TransactionDescriptionOrdinary{
			CreditFirst: true,
			StoragePhase: &StoragePhase{
				StorageFeesCollected: FromNanoTONU(123),
				StatusChange:         AccStatusChange{Type: AccStatusChangeUnchanged},
			},
			CreditPhase: &CreditPhase{
				DueFeesCollected: &dueFees,
				Credit: CurrencyCollection{
					Coins: FromNanoTONU(2_100_000_000),
				},
			},
			ComputePhase: ComputePhase{
				Phase: ComputePhaseVM{
					Success:          true,
					MsgStateUsed:     true,
					AccountActivated: false,
					GasFees:          FromNanoTONU(10_000),
					Details: ComputePhaseVMDetails{
						GasUsed:          big.NewInt(12_345),
						GasLimit:         big.NewInt(50_000),
						GasCredit:        gasCredit,
						Mode:             0,
						ExitCode:         0,
						ExitArg:          &exitArg,
						VMSteps:          777,
						VMInitStateHash:  initHash,
						VMFinalStateHash: finalHash,
					},
				},
			},
			ActionPhase: &ActionPhase{
				Success:         true,
				Valid:           true,
				NoFunds:         false,
				StatusChange:    AccStatusChange{Type: AccStatusChangeUnchanged},
				TotalFwdFees:    &totalFwdFees,
				TotalActionFees: &totalActionFees,
				ResultCode:      0,
				ResultArg:       &exitArg,
				TotalActions:    1,
				SpecActions:     0,
				SkippedActions:  0,
				MessagesCreated: 1,
				ActionListHash:  actionHash,
				TotalMsgSize: StorageUsedShort{
					Cells: big.NewInt(3),
					Bits:  big.NewInt(456),
				},
			},
		},
		IO: struct {
			In  *Message      `tlb:"maybe ^"`
			Out *MessagesList `tlb:"maybe ^"`
		}{
			In:  msg,
			Out: &MessagesList{List: outDict},
		},
	}
}

func benchmarkBytes(seed byte) []byte {
	data := make([]byte, 32)
	for i := range data {
		data[i] = seed + byte(i)
	}
	return data
}
