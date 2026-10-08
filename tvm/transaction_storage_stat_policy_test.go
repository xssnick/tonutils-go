package tvm

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

// C++ compute_state materializes the storage dictionary only when its hash
// must be persisted. With the default threshold, 27 -> 26 cells therefore
// requires a sufficient dictionary proof, while 26 -> 25 may retain deferred
// changes. See TON 3d478cb, crypto/block/transaction.cpp, compute_state.
func TestTransactionStorageStatProofRequirement(t *testing.T) {
	for version := uint32(10); version <= uint32(vmcore.MaxSupportedGlobalVersion); version++ {
		for _, tc := range []struct {
			name      string
			depth     int
			pruned    int
			threshold uint32
			workchain int
		}{
			{name: "27_to_26", depth: 22, pruned: 13, threshold: 26},
			{name: "26_to_25", depth: 21, pruned: 9, threshold: 26},
			{name: "masterchain", depth: 22, pruned: 13, threshold: 26, workchain: -1},
			{name: "raised_threshold", depth: 22, pruned: 13, threshold: 27},
			{name: "lowered_threshold", depth: 21, pruned: 9, threshold: 25},
		} {
			t.Run(fmt.Sprintf("v%d/%s", version, tc.name), func(t *testing.T) {
				chain := func(depth int) *cell.Cell {
					c := cell.BeginCell().MustStoreUInt(0x1000, 64).EndCell()
					for i := depth - 1; i >= 0; i-- {
						c = cell.BeginCell().MustStoreUInt(uint64(0x1000+i), 64).MustStoreRef(c).EndCell()
					}
					return c
				}
				oldData := cell.BeginCell().MustStoreUInt(1, 8).MustStoreRef(chain(0)).EndCell()
				newData := cell.BeginCell().MustStoreUInt(2, 8).MustStoreRef(chain(tc.depth)).EndCell()
				code := makeTransactionExternalSuccessCode(t, newData)
				storage := storageStatProofAccountStorage(t, code, oldData)
				usage, dict, err := transactionComputeAccountStorageStat(storage, 0)
				if err != nil {
					t.Fatal(err)
				}
				addr := address.MustParseRawAddr(fmt.Sprintf("%d:2b63f898590aa9e300fd0e696bc834b9ebe3ab75ec16dbc2a90955d960cc6a85", tc.workchain))
				before := &tlb.ShardAccount{
					Account:       storageStatProofAccount(t, addr, code, oldData, usage, dict),
					LastTransHash: make([]byte, 32),
				}
				messageCell, err := tlb.ToCell(&tlb.ExternalMessage{DstAddr: addr, Body: cell.BeginCell().EndCell()})
				if err != nil {
					t.Fatal(err)
				}
				message, err := PrepareMessage(messageCell)
				if err != nil {
					t.Fatal(err)
				}

				var nodes []cell.Hash
				seen := map[cell.Hash]bool{}
				var visit func(*cell.Cell)
				visit = func(c *cell.Cell) {
					h := c.HashKey()
					if seen[h] {
						return
					}
					seen[h] = true
					nodes = append(nodes, h)
					for i := 0; i < int(c.RefsNum()); i++ {
						visit(c.MustPeekRef(i))
					}
				}
				visit(dict)
				excluded := nodes[tc.pruned]
				proof, err := dict.CreateHashUsageProof(func(h cell.Hash) bool { return h != excluded })
				if err != nil {
					t.Fatal(err)
				}
				partial, err := cell.UnwrapProofVirtualized(proof, dict.Hash())
				if err != nil {
					t.Fatal(err)
				}
				cfg := transactionFuzzStorageDictConfig(t, version, tc.threshold)
				run := func(bound *cell.Cell) (*TransactionExecutionResult, error) {
					t.Helper()
					account, err := PrepareAccount(before, addr)
					if err != nil {
						t.Fatal(err)
					}
					block, err := cfg.NewBlockContext(BlockOptions{
						Now:      uint32(tonopsTestTime.Unix()),
						BlockLT:  transactionTestLogicalTime,
						RandSeed: make([]byte, 32),
					})
					if err != nil {
						t.Fatal(err)
					}
					if bound != nil {
						if err := block.BindAccountStorageStat(account, bound); err != nil {
							t.Fatalf("bind authenticated dictionary: %v", err)
						}
					}
					return NewTVM().EmulateTransaction(block, account, message, TransactionOptions{
						LogicalTime: transactionTestLogicalTime,
					})
				}
				full, err := run(dict)
				if err != nil {
					t.Fatalf("full dictionary: %v", err)
				}
				if !full.Accepted || full.TransactionCell == nil || full.NextAccount == nil || full.StorageStatRecomputed {
					t.Fatal("full dictionary must commit without recovery")
				}
				nextCells := full.NextAccount.runtime.storageInfo.StorageUsed.CellsUsed.Uint64()
				if usage.cells != uint64(tc.depth+5) || nextCells != uint64(tc.depth+4) {
					t.Fatalf("unexpected storage size %d -> %d", usage.cells, nextCells)
				}
				compare := func(result *TransactionExecutionResult) {
					t.Helper()
					if !result.Accepted || result.TransactionCell == nil || result.NextAccount == nil {
						t.Fatal("expected committed transaction")
					}
					if !bytes.Equal(full.TransactionCell.Hash(), result.TransactionCell.Hash()) ||
						!bytes.Equal(full.NextAccount.ShardAccountCell().Hash(), result.NextAccount.ShardAccountCell().Hash()) {
						t.Fatal("result differs from the full dictionary control")
					}
				}

				// An omitted dictionary requests a direct state walk, rather than
				// claiming that an authenticated partial dictionary is sufficient.
				direct, err := run(nil)
				if err != nil {
					t.Fatalf("omitted dictionary: %v", err)
				}
				compare(direct)
				if direct.StorageStatRecomputed {
					t.Fatal("omitted dictionary must not report proof recovery")
				}

				result, err := run(partial)
				requiresHash := version >= 11 && tc.workchain == 0 && nextCells >= uint64(tc.threshold)
				if requiresHash {
					if !errors.Is(err, cell.ErrDictHasSpecialCells) {
						t.Fatalf("insufficient persisted-dictionary proof: got %v, want %v", err, cell.ErrDictHasSpecialCells)
					}
					return
				}
				if err != nil {
					t.Fatalf("non-persisted dictionary: %v", err)
				}
				compare(result)
				if !result.StorageStatRecomputed {
					t.Fatal("partial dictionary did not exercise recovery")
				}
			})
		}
	}
}
