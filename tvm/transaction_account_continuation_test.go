package tvm

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestAccountNoneContinuationStorageContext(t *testing.T) {
	for _, debt := range []uint64{0, 17} {
		t.Run(fmt.Sprintf("debt=%d", debt), func(t *testing.T) {
			previous, err := PrepareAccount(buildTransactionTestNoneShardAccount(t), tonopsTestAddr)
			if err != nil {
				t.Fatal(err)
			}
			const now, startLT, endLT = 1234567, 200, 208
			due := transactionCoinsPtr(new(big.Int).SetUint64(debt))
			next, err := buildTransactionAccountCell(&previous.runtime, tlb.AccountStatusNonExist,
				big.NewInt(0), nil, endLT, now, due, nil, nil, nil, nil, false, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			var result TransactionExecutionResult
			tx := cell.BeginCell().MustStoreUInt(42, 8).EndCell()
			if err = fillTransactionExecutionResult(&result, tx, previous, next, nil, startLT, endLT); err != nil {
				t.Fatal(err)
			}
			account := result.NextAccount
			if account.ShardAccount().Account.HashKey() != previous.ShardAccount().Account.HashKey() ||
				account.State().IsValid || account.State().StorageInfo.DuePayment != nil || account.State().StorageInfo.LastPaid != 0 {
				t.Fatal("transient storage context changed the serialized account_none")
			}
			for _, buildProof := range []bool{false, true} {
				t.Run(proofModeName(buildProof), func(t *testing.T) {
					runtime, proof, err := account.runtimeForExecution(buildProof)
					if err != nil {
						t.Fatal(err)
					}
					if (proof != nil) != buildProof {
						t.Fatal("unexpected account proof mode")
					}
					if runtime.storageInfo.LastPaid != now || runtime.storageLT != endLT ||
						accountContinuationDebt(runtime.storageInfo.DuePayment) != debt {
						t.Fatalf("continuation context: last_paid=%d end_lt=%d due=%v; want %d, %d, %d",
							runtime.storageInfo.LastPaid, runtime.storageLT, runtime.storageInfo.DuePayment, now, endLT, debt)
					}
					fee, err := transactionComputeStorageFee(nil, runtime, now, false)
					if err != nil {
						t.Fatal(err)
					}
					if accountContinuationDebt(transactionCoinsPtr(fee)) != debt {
						t.Fatalf("next storage phase charges %v, want debt %d", fee, debt)
					}
					if _, err = transactionComputeStorageFee(nil, runtime, now-1, false); err == nil {
						t.Fatal("continuation lost the last_paid time bound")
					}
				})
			}

			// Reopening the serialized state starts a new block, where account_none
			// has neither storage debt nor the previous block's in-memory end LT.
			for _, parsed := range []bool{false, true} {
				var reopened *PreparedAccount
				if parsed {
					reopened, err = PrepareParsedAccount(account.ShardAccount(), account.State(), tonopsTestAddr)
				} else {
					reopened, err = PrepareAccount(account.ShardAccount(), tonopsTestAddr)
				}
				if err != nil {
					t.Fatal(err)
				}
				if reopened.runtime.storageInfo.DuePayment != nil || reopened.runtime.storageInfo.LastPaid != 0 || reopened.runtime.storageLT != 0 {
					t.Fatalf("parsed=%t: transient context survived preparation from serialized account_none", parsed)
				}
			}
			if previous.runtime.storageInfo.DuePayment != nil || previous.runtime.storageInfo.LastPaid != 0 || previous.runtime.storageLT != 0 {
				t.Fatal("previous prepared account was mutated")
			}
		})
	}
}

func TestDeletedAccountContinuationStorageDebt(t *testing.T) {
	now := uint32(tonopsTestTime.Unix())
	for _, version := range []uint32{4, 7, 16} {
		cfg := transactionTestConfigWithGlobalVersion(t, version)
		block, err := cfg.NewBlockContext(BlockOptions{Now: now, BlockLT: transactionTestLogicalTime, RandSeed: tonopsTestSeed})
		if err != nil {
			t.Fatal(err)
		}
		initialDebt := cfg.storageDueLimitsFor(false).deleteDue.Uint64() + 10
		for _, kind := range []string{"ordinary", "tick", "tock"} {
			for _, buildProof := range []bool{false, true} {
				t.Run(fmt.Sprintf("v%d/%s/%s", version, kind, proofModeName(buildProof)), func(t *testing.T) {
					debt := tlb.FromNanoTONU(initialDebt)
					account, err := PrepareAccount(buildTransactionTestUninitShardAccount(t, tonopsTestAddr, 1,
						tlb.StorageInfo{LastPaid: now, DuePayment: &debt}), tonopsTestAddr)
					if err != nil {
						t.Fatal(err)
					}
					machine := NewTVM()
					opts := TransactionOptions{LogicalTime: transactionTestLogicalTime, BuildProof: buildProof}
					var first *TransactionExecutionResult
					firstCollected := uint64(1)
					if kind == "ordinary" {
						firstCollected++
						first, err = machine.EmulateTransaction(block, account, accountContinuationMessage(t, 1), opts)
					} else {
						first, err = machine.EmulateTickTockTransaction(block, account, kind == "tock", opts)
					}
					if err != nil {
						t.Fatal(err)
					}
					remainingDebt := initialDebt - firstCollected
					phase := accountContinuationStoragePhase(t, first)
					if first.NextAccount.State().IsValid || phase.StatusChange.Type != tlb.AccStatusChangeDeleted ||
						phase.StorageFeesCollected.NanoRef().Uint64() != firstCollected || accountContinuationDebt(phase.StorageFeesDue) != remainingDebt {
						t.Fatalf("first transaction did not delete the account with unpaid debt: state=%+v phase=%+v", first.NextAccount.State(), phase)
					}

					remainingBalance := initialDebt + 1_000_000
					message := accountContinuationMessage(t, remainingDebt+remainingBalance)
					opts.AccountStorageStat = first.AccountStorageStat
					second, err := machine.EmulateTransaction(block, first.NextAccount, message, opts)
					if err != nil {
						t.Fatal(err)
					}
					phase = accountContinuationStoragePhase(t, second)
					state := second.NextAccount.State()
					if !state.IsValid || state.Status != tlb.AccountStatusUninit || state.Balance.NanoRef().Uint64() != remainingBalance ||
						phase.StorageFeesCollected.NanoRef().Uint64() != remainingDebt || second.StartLT != first.EndLT {
						t.Fatalf("continuation lost debt or end LT: state=%+v phase=%+v start=%d want=%d", state, phase, second.StartLT, first.EndLT)
					}
					wantDebt := uint64(0)
					if version < 7 {
						// Before version 7, paying the full debt did not clear it.
						wantDebt = remainingDebt
					}
					if state.StorageInfo.LastPaid != now || accountContinuationDebt(state.StorageInfo.DuePayment) != wantDebt {
						t.Fatalf("recreated account storage info=%+v, want last_paid=%d due=%d", state.StorageInfo, now, wantDebt)
					}
					if accountContinuationDebt(first.NextAccount.runtime.storageInfo.DuePayment) != remainingDebt ||
						accountContinuationDebt(account.runtime.storageInfo.DuePayment) != initialDebt {
						t.Fatal("following transaction mutated an earlier prepared account")
					}

					// The same serialized account_none, reopened for a later block,
					// must receive the full credit without the previous block's debt.
					reopened, err := PrepareAccount(first.NextAccount.ShardAccount(), tonopsTestAddr)
					if err != nil {
						t.Fatal(err)
					}
					nextBlock, err := cfg.NewBlockContext(BlockOptions{
						Now: now + 1, BlockLT: transactionTestLogicalTime + 100, RandSeed: tonopsTestSeed,
					})
					if err != nil {
						t.Fatal(err)
					}
					reset, err := machine.EmulateTransaction(nextBlock, reopened, message,
						TransactionOptions{LogicalTime: transactionTestLogicalTime + 100, BuildProof: buildProof})
					if err != nil {
						t.Fatal(err)
					}
					if accountContinuationStoragePhase(t, reset).StorageFeesCollected.NanoRef().Sign() != 0 ||
						reset.NextAccount.State().Balance.NanoRef().Uint64() != remainingDebt+remainingBalance || reset.NextAccount.State().StorageInfo.DuePayment != nil {
						t.Fatal("a new block retained transient debt from account_none")
					}
				})
			}
		}
	}
}

func accountContinuationMessage(t *testing.T, amount uint64) *PreparedMessage {
	t.Helper()
	message, err := PrepareMessage(buildTransactionOutboundInternalCellWithAddresses(t,
		internalEmulationSrcAddr, tonopsTestAddr, amount, cell.BeginCell().EndCell()))
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func accountContinuationStoragePhase(t *testing.T, result *TransactionExecutionResult) *tlb.StoragePhase {
	t.Helper()
	transaction, err := result.ParseTransaction()
	if err != nil {
		t.Fatal(err)
	}
	switch desc := transaction.Description.(type) {
	case tlb.TransactionDescriptionOrdinary:
		return desc.StoragePhase
	case tlb.TransactionDescriptionTickTock:
		return &desc.StoragePhase
	default:
		t.Fatalf("unexpected transaction description %T", desc)
		return nil
	}
}

func accountContinuationDebt(coins *tlb.Coins) uint64 {
	if coins == nil {
		return 0
	}
	return coins.NanoRef().Uint64()
}
