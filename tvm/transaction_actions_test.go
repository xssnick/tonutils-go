package tvm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTransactionMessageToCellWithRetryKeepsStateInitRefWhenMovingBody(t *testing.T) {
	var attempts []transactionOutboundLayout
	root, used, err := transactionMessageToCellWithRetry(transactionOutboundLayout{}, true, func(layout transactionOutboundLayout) (*cell.Cell, error) {
		attempts = append(attempts, layout)
		if layout.stateInitInRef && layout.bodyInRef {
			return cell.BeginCell().EndCell(), nil
		}
		return nil, errors.New("does not fit")
	})
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if root == nil {
		t.Fatal("retry returned nil cell")
	}
	if !used.stateInitInRef || !used.bodyInRef {
		t.Fatalf("used layout = %+v, want state init and body in refs", used)
	}
	if len(attempts) != 3 {
		t.Fatalf("attempts = %d, want 3", len(attempts))
	}
	if attempts[2] != (transactionOutboundLayout{stateInitInRef: true, bodyInRef: true}) {
		t.Fatalf("final attempt = %+v, want progressive state init/body refs", attempts[2])
	}
}

func TestTransactionMessageToCellWithRetryCanSkipStateInitRef(t *testing.T) {
	var attempts []transactionOutboundLayout
	root, used, err := transactionMessageToCellWithRetry(transactionOutboundLayout{}, false, func(layout transactionOutboundLayout) (*cell.Cell, error) {
		attempts = append(attempts, layout)
		if layout.bodyInRef {
			return cell.BeginCell().EndCell(), nil
		}
		return nil, errors.New("does not fit")
	})
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if root == nil {
		t.Fatal("retry returned nil cell")
	}
	if used.stateInitInRef || !used.bodyInRef {
		t.Fatalf("used layout = %+v, want only body in ref", used)
	}
	if len(attempts) != 2 {
		t.Fatalf("attempts = %d, want 2", len(attempts))
	}
	if attempts[1] != (transactionOutboundLayout{bodyInRef: true}) {
		t.Fatalf("final attempt = %+v, want body ref only", attempts[1])
	}
}

func TestTransactionCollectUsageLoadsLazyRefs(t *testing.T) {
	leaf := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	child := cell.BeginCell().MustStoreUInt(0xBB, 8).MustStoreRef(leaf).EndCell()
	root := cell.BeginCell().MustStoreUInt(0xAA, 8).MustStoreRef(child).EndCell()

	lazyRoot := lazyTransactionTestRoot(t, root)
	lazyChild, err := lazyRoot.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if !lazyChild.IsLazy() {
		t.Fatal("test root should keep child behind lazy boundary")
	}

	want, err := transactionCollectUsage(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := transactionCollectUsage(lazyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("usage from lazy tree = %+v, want %+v", got, want)
	}
}

func TestTransactionValidateRelaxedActionMessageCurrencies(t *testing.T) {
	t.Run("rejects non canonical value", func(t *testing.T) {
		msg := transactionTestRelaxedInternalMessage(func(b *cell.Builder) {
			b.MustStoreUInt(4, 4).
				MustStoreSlice([]byte{0x00, 0x0a, 0xb1, 0x47}, 32)
		}, func(b *cell.Builder) {
			b.MustStoreBigCoins(big.NewInt(0))
		})

		if _, err := transactionValidateRelaxedActionMessageCurrencies(msg); err == nil {
			t.Fatal("expected non-canonical value to be rejected")
		}
	})

	t.Run("reports non canonical extra flags as skippable", func(t *testing.T) {
		msg := transactionTestRelaxedInternalMessage(func(b *cell.Builder) {
			b.MustStoreBigCoins(big.NewInt(700743))
		}, func(b *cell.Builder) {
			b.MustStoreUInt(1, 4).MustStoreUInt(0, 8)
		})

		ok, err := transactionValidateRelaxedActionMessageCurrencies(msg)
		if err != nil {
			t.Fatalf("extra flags should not be fatal: %v", err)
		}
		if ok {
			t.Fatal("expected non-canonical extra flags to be reported")
		}
	})
}

func transactionTestRelaxedInternalMessage(storeValue, storeExtraFlags func(*cell.Builder)) *cell.Cell {
	return cell.BeginCell().
		MustStoreBoolBit(false).
		MustStoreBoolBit(true).
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		MustStoreAddr(address.NewAddressNone()).
		MustStoreAddr(address.NewAddress(0, 0, make([]byte, 32))).
		MustStoreBuilder(transactionTestBuilder(storeValue)).
		MustStoreBoolBit(false).
		MustStoreBuilder(transactionTestBuilder(storeExtraFlags)).
		MustStoreBigCoins(big.NewInt(0)).
		MustStoreUInt(0, 64).
		MustStoreUInt(0, 32).
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		EndCell()
}

func transactionTestBuilder(fn func(*cell.Builder)) *cell.Builder {
	b := cell.BeginCell()
	fn(b)
	return b
}

func lazyTransactionTestRoot(t *testing.T, root *cell.Cell) *cell.Cell {
	t.Helper()

	roots, _, err := cell.FromBOCMultiRootReader(
		cell.NewBOCNoCopyReader(root.ToBOCWithOptions(cell.BOCSerializeOptions{WithIndex: true})),
		cell.BOCParseOptions{Lazy: true, TrustedHashes: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("lazy parse returned %d roots, want 1", len(roots))
	}
	return roots[0]
}
