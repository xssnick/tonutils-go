//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

func TestTVMCrossEmulatorPrecompiledExternalGas(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	data := cell.BeginCell().EndCell()
	base := mustReferenceTransactionConfigRoot(t)
	specialAddr := address.NewAddress(0, 0xFF, bytes.Repeat([]byte{0x89}, 32))
	configAddress, err := tlb.ToCell(&tlb.ConfigParamAddress{Address: specialAddr.Data()})
	if err != nil {
		t.Fatal(err)
	}

	for _, version := range []uint32{0, 13, vmcore.MaxSupportedGlobalVersion} {
		for _, tc := range []struct {
			name       string
			balance    uint64
			usage      uint64
			nops       int
			accept     bool
			special    bool
			noCredit   bool
			wideLimit  bool
			wantAccept bool
			wantOOG    bool
		}{
			{name: "positive usage against zero initial limit", balance: 400, usage: 1, accept: true, wantAccept: true},
			{name: "equal to account maximum", balance: 400, usage: 400, accept: true, wantAccept: true},
			{name: "above account maximum", balance: 400, usage: 401, accept: true},
			{name: "accept beyond original credit", balance: 5_000, usage: 1, nops: 26, accept: true, wantAccept: true},
			{name: "return without accept", balance: 5_000, usage: 1, nops: 26},
			{name: "exhaust fallback credit before accept", balance: 5_000, usage: 1, nops: 28, wantOOG: true},
			{name: "special fallback limit", balance: 5_000, usage: 800, nops: 40, accept: true, special: true, wantAccept: true},
			{name: "special without credit", balance: 5_000, usage: 800, accept: true, special: true, noCredit: true, wantAccept: version >= 5},
			{name: "special unsigned maximum", balance: 5_000, usage: 1, accept: true, special: true, noCredit: true, wideLimit: true, wantOOG: version >= 5},
		} {
			t.Run(fmt.Sprintf("v%d/%s", version, tc.name), func(t *testing.T) {
				addr := tonopsTestAddr
				if tc.special {
					addr = specialAddr
				}
				credit := uint64(100)
				if tc.noCredit {
					credit = 0
				}
				prices := buildTransactionPrecompiledExternalGasPrices(t, credit)
				if tc.wideLimit {
					var limits tlb.ConfigGasLimitsPrices
					if err := tlb.Parse(&limits, prices); err != nil {
						t.Fatal(err)
					}
					limits.SpecialGasLimit = math.MaxUint64
					prices, err = tlb.ToCell(&limits)
					if err != nil {
						t.Fatal(err)
					}
				}
				code := makeTransactionPrecompiledExternalCode(t, tc.nops, tc.accept)
				root := referenceTransactionConfigRootWithOverrides(t,
					referenceTransactionConfigRootWithGlobalVersion(t, base, version), map[int32]*cell.Cell{
						int32(tlb.ConfigParamConfigAddress):               configAddress,
						int32(tlb.ConfigParamGasPricesBasechain):          prices,
						int32(tlb.ConfigParamGasPricesMasterchain):        prices,
						int32(tlb.ConfigParamMsgForwardPricesBasechain):   buildTransactionMsgForwardPricesCell(t, 0, 0),
						int32(tlb.ConfigParamMsgForwardPricesMasterchain): buildTransactionMsgForwardPricesCell(t, 0, 0),
						int32(tlb.ConfigParamPrecompiledContracts):        buildTransactionV13PrecompiledConfig(t, code, tc.usage),
					})
				storage, err := tlb.ToCell(&tlb.AccountStorage{
					Status:    tlb.AccountStatusActive,
					Balance:   tlb.FromNanoTONU(tc.balance),
					StateInit: &tlb.StateInit{Code: code, Data: data},
				})
				if err != nil {
					t.Fatal(err)
				}
				usage, err := transactionCollectUsage(storage)
				if err != nil {
					t.Fatal(err)
				}
				shard := buildTransactionTestShardAccountWithStorageInfo(t, addr, code, data, tc.balance, tlb.StorageInfo{
					StorageUsed: tlb.StorageUsed{
						CellsUsed: new(big.Int).SetUint64(usage.cells),
						BitsUsed:  new(big.Int).SetUint64(usage.bits),
					},
					StorageExtra: tlb.StorageExtraNone{},
					LastPaid:     now,
				})
				msg := mustTransactionMsgCell(t, &tlb.ExternalMessage{DstAddr: addr, Body: data})
				params := testTxParams{
					Address:     addr,
					Now:         now,
					BlockLT:     transactionTestLogicalTime,
					LogicalTime: transactionTestLogicalTime,
					RandSeed:    tonopsTestSeed,
					ConfigRoot:  root,
				}
				accepted, acceptErr := testCheckExternalMessageAccepted(NewTVM(), shard, msg, params)
				res, txErr := testEmulateTransaction(NewTVM(), shard, msg, params)
				ref, refErr := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, root)
				if tc.wantOOG {
					if !errors.Is(acceptErr, errPrecompiledOutOfGas) || !errors.Is(txErr, errPrecompiledOutOfGas) || accepted || res != nil {
						t.Fatalf("accept check = %t/%v, transaction = %+v/%v, reference = %+v/%v, want precompiled out-of-gas", accepted, acceptErr, res, txErr, ref, refErr)
					}
					if ref != nil || refErr == nil || !strings.Contains(refErr.Error(), "cannot create compute phase") {
						t.Fatalf("reference = %+v/%v, want compute-phase creation failure", ref, refErr)
					}
					return
				}
				if acceptErr != nil || txErr != nil || accepted != tc.wantAccept || res.Accepted != tc.wantAccept {
					t.Fatalf("accept check = %t/%v, transaction = %+v/%v, reference = %+v/%v, want accepted %t", accepted, acceptErr, res, txErr, ref, refErr, tc.wantAccept)
				}
				if !tc.wantAccept {
					if res.TransactionCell != nil || ref != nil || refErr == nil || !strings.Contains(refErr.Error(), "External message not accepted") {
						t.Fatalf("rejected transaction = %+v, reference = %+v/%v, want external rejection without transaction", res, ref, refErr)
					}
					return
				}
				if refErr != nil {
					t.Fatal(refErr)
				}
				if res.GasUsed != int64(tc.usage) || ref.gasUsed != int64(tc.usage) || res.Steps != 0 || res.ExitCode != ref.exitCode {
					t.Fatalf("Go exit/gas/steps = %d/%d/%d, reference exit/gas = %d/%d, want configured gas %d and steps 0", res.ExitCode, res.GasUsed, res.Steps, ref.exitCode, ref.gasUsed, tc.usage)
				}
				assertTransactionComputePhaseParity(t, res.TransactionCell, ref.txCell)
				assertTransactionNonComputeParity(t, res.TransactionCell, ref.txCell)
				assertShardAccountNonComputeParity(t, res.NextAccount.ShardAccountCell(), ref.shardCell)
				if !bytes.Equal(res.TransactionCell.Hash(), ref.txCell.Hash()) || !bytes.Equal(res.NextAccount.ShardAccountCell().Hash(), ref.shardCell.Hash()) {
					t.Fatal("transaction or shard account hash differs from reference")
				}
			})
		}
	}
}
