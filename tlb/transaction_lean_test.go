package tlb

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTransactionLeanMatchesFullTransaction(t *testing.T) {
	txCell := benchmarkTransactionCell(t)

	var full Transaction
	if err := full.LoadFromCell(txCell.MustBeginParse()); err != nil {
		t.Fatalf("load full transaction: %v", err)
	}

	var lean TransactionLean
	if err := lean.LoadFromCell(txCell.MustBeginParse()); err != nil {
		t.Fatalf("load lean transaction: %v", err)
	}

	if !bytes.Equal(lean.AccountAddr[:], full.AccountAddr) {
		t.Fatal("account address mismatch")
	}
	if lean.LT != full.LT || lean.PrevTxLT != full.PrevTxLT || lean.Now != full.Now {
		t.Fatal("transaction header mismatch")
	}
	if !bytes.Equal(lean.PrevTxHash[:], full.PrevTxHash) {
		t.Fatal("previous transaction hash mismatch")
	}
	if lean.OutMsgCount != full.OutMsgCount || lean.OrigStatus != full.OrigStatus || lean.EndStatus != full.EndStatus {
		t.Fatal("transaction status or output count mismatch")
	}
	if lean.InMsg == nil || full.IO.In == nil {
		t.Fatal("inbound message was not retained")
	}
	fullIn, err := full.IO.In.ToCell()
	if err != nil {
		t.Fatalf("serialize full inbound message: %v", err)
	}
	if !bytes.Equal(lean.InMsg.Hash(), fullIn.Hash()) {
		t.Fatal("inbound message hash mismatch")
	}
	if !lean.HasOutMsgs || full.IO.Out == nil {
		t.Fatal("output dictionary presence mismatch")
	}
	if !lean.TotalFees.Equals(full.TotalFees) {
		t.Fatal("total fees mismatch")
	}
	if !bytes.Equal(lean.OldHash[:], full.StateUpdate.OldHash) || !bytes.Equal(lean.NewHash[:], full.StateUpdate.NewHash) {
		t.Fatal("state update mismatch")
	}
	if lean.Kind != TransactionKindOrdinary || lean.IsTock {
		t.Fatalf("unexpected description: kind=%s is_tock=%v", lean.Kind, lean.IsTock)
	}
}

func TestTransactionLeanDescriptionKinds(t *testing.T) {
	tests := []struct {
		name   string
		tag    uint64
		kind   TransactionKind
		isTock bool
	}{
		{name: "ordinary", tag: 0b0000, kind: TransactionKindOrdinary},
		{name: "storage", tag: 0b0001, kind: TransactionKindStorage},
		{name: "tick", tag: 0b0010, kind: TransactionKindTickTock},
		{name: "tock", tag: 0b0011, kind: TransactionKindTickTock, isTock: true},
		{name: "split_prepare", tag: 0b0100, kind: TransactionKindSplitPrepare},
		{name: "split_install", tag: 0b0101, kind: TransactionKindSplitInstall},
		{name: "merge_prepare", tag: 0b0110, kind: TransactionKindMergePrepare},
		{name: "merge_install", tag: 0b0111, kind: TransactionKindMergeInstall},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var transaction TransactionLean
			err := transaction.LoadFromCell(transactionLeanCell(t, cell.BeginCell().MustStoreUInt(tt.tag, 4).EndCell()).MustBeginParse())
			if err != nil {
				t.Fatalf("load transaction: %v", err)
			}
			if transaction.Kind != tt.kind || transaction.IsTock != tt.isTock {
				t.Fatalf("unexpected description: kind=%s is_tock=%v", transaction.Kind, transaction.IsTock)
			}
		})
	}
}

func TestTransactionLeanReuseClearsOptionalFields(t *testing.T) {
	var transaction TransactionLean
	if err := transaction.LoadFromCell(benchmarkTransactionCell(t).MustBeginParse()); err != nil {
		t.Fatalf("load populated transaction: %v", err)
	}
	if transaction.InMsg == nil || !transaction.HasOutMsgs {
		t.Fatal("populated fixture did not set optional fields")
	}

	empty := transactionLeanCell(t, cell.BeginCell().MustStoreUInt(0b0001, 4).EndCell())
	if err := transaction.LoadFromCell(empty.MustBeginParse()); err != nil {
		t.Fatalf("reuse transaction parser: %v", err)
	}
	if transaction.InMsg != nil || transaction.HasOutMsgs {
		t.Fatal("optional fields leaked from the previous transaction")
	}
	if transaction.Kind != TransactionKindStorage || transaction.IsTock {
		t.Fatalf("description leaked from the previous transaction: kind=%s is_tock=%v", transaction.Kind, transaction.IsTock)
	}
}

func TestTransactionLeanRejectsInvalidBoundaries(t *testing.T) {
	t.Run("transaction magic", func(t *testing.T) {
		var transaction TransactionLean
		err := transaction.LoadFromCell(transactionLeanCellWithMagic(t, 0, transactionLeanStateUpdate(), cell.BeginCell().MustStoreUInt(0, 4).EndCell()).MustBeginParse())
		if err == nil || !strings.Contains(err.Error(), "invalid transaction magic") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("state update magic", func(t *testing.T) {
		root := transactionLeanCellWithStateUpdate(t,
			cell.BeginCell().MustStoreUInt(0x73, 8).MustStoreSlice(make([]byte, 64), 512).EndCell(),
			cell.BeginCell().MustStoreUInt(0, 4).EndCell(),
		)

		var transaction TransactionLean
		err := transaction.LoadFromCell(root.MustBeginParse())
		if err == nil || !strings.Contains(err.Error(), "invalid hash update magic") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("description prefix", func(t *testing.T) {
		var transaction TransactionLean
		err := transaction.LoadFromCell(transactionLeanCell(t, cell.BeginCell().MustStoreUInt(0b100, 3).EndCell()).MustBeginParse())
		if err == nil || !strings.Contains(err.Error(), "unknown transaction description magic prefix") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func BenchmarkTransactionLeanLoadFromCell(b *testing.B) {
	txCell := benchmarkTransactionCell(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var transaction TransactionLean
		if err := transaction.LoadFromCell(txCell.MustBeginParse()); err != nil {
			b.Fatal(err)
		}
	}
}

func transactionLeanCell(t testing.TB, description *cell.Cell) *cell.Cell {
	t.Helper()

	return transactionLeanCellWithStateUpdate(t, transactionLeanStateUpdate(), description)
}

func transactionLeanCellWithStateUpdate(t testing.TB, stateUpdate, description *cell.Cell) *cell.Cell {
	return transactionLeanCellWithMagic(t, 0b0111, stateUpdate, description)
}

func transactionLeanCellWithMagic(t testing.TB, magic uint64, stateUpdate, description *cell.Cell) *cell.Cell {
	t.Helper()

	io := cell.BeginCell().MustStoreBoolBit(false).MustStoreBoolBit(false).EndCell()
	builder := cell.BeginCell().
		MustStoreUInt(magic, 4).
		MustStoreSlice(benchmarkBytes(0x10), 256).
		MustStoreUInt(100, 64).
		MustStoreSlice(benchmarkBytes(0x20), 256).
		MustStoreUInt(99, 64).
		MustStoreUInt(1_700_000_000, 32).
		MustStoreUInt(0, 15)
	if err := storeAccountStatus(builder, AccountStatusActive); err != nil {
		t.Fatalf("store original status: %v", err)
	}
	if err := storeAccountStatus(builder, AccountStatusActive); err != nil {
		t.Fatalf("store end status: %v", err)
	}
	builder.MustStoreRef(io)
	if err := storeCurrencyCollection(builder, CurrencyCollection{}); err != nil {
		t.Fatalf("store total fees: %v", err)
	}

	return builder.MustStoreRef(stateUpdate).MustStoreRef(description).EndCell()
}

func transactionLeanStateUpdate() *cell.Cell {
	return cell.BeginCell().
		MustStoreUInt(0x72, 8).
		MustStoreSlice(benchmarkBytes(0x30), 256).
		MustStoreSlice(benchmarkBytes(0x40), 256).
		EndCell()
}
