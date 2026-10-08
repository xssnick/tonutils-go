//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// The transaction code casts the uint64 gas fields loaded from config to
// signed long long when it constructs vm::GasLimits. MaxUint64 consequently
// becomes -1; adding the external gas credit gives the contract enough gas to
// reach ACCEPT. Saturating the limit to MaxInt64 instead overflows that same
// addition to a negative value and rejects the message before ACCEPT.
func TestTVMCrossEmulatorTransactionGasLimitSignedCast(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	specialAddr := address.NewAddress(0, 0xFF, bytes.Repeat([]byte{0x83}, 32))
	configAddrCell, err := tlb.ToCell(&tlb.ConfigParamAddress{Address: specialAddr.Data()})
	if err != nil {
		t.Fatalf("failed to build config address cell: %v", err)
	}
	gasCell, err := tlb.ToCell(&tlb.ConfigGasLimitsPrices{
		HasFlatPricing:          true,
		FlatGasLimit:            10,
		FlatGasPrice:            100,
		HasSeparateSpecialLimit: true,
		GasPrice:                1 << 16,
		GasLimit:                1_000_000,
		SpecialGasLimit:         ^uint64(0),
		GasCredit:               10_000,
		BlockGasLimit:           ^uint64(0),
	})
	if err != nil {
		t.Fatalf("failed to build gas prices cell: %v", err)
	}
	configRoot := referenceTransactionConfigRootWithOverrides(t, mustReferenceTransactionConfigRoot(t), map[int32]*cell.Cell{
		int32(tlb.ConfigParamConfigAddress):        configAddrCell,
		int32(tlb.ConfigParamGasPricesBasechain):   gasCell,
		int32(tlb.ConfigParamGasPricesMasterchain): gasCell,
	})
	data := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	code := makeTransactionExternalSuccessCode(t, data)
	shard := buildTransactionTestShardAccount(t, specialAddr, code, data, walletSendTestBalance, now)
	msg := mustTransactionMsgCell(t, &tlb.ExternalMessage{
		DstAddr: specialAddr,
		Body:    cell.BeginCell().EndCell(),
	})

	_, refErr := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	// The reference reaches ACCEPT and proceeds to transaction serialization.
	// Serialization then rejects the deliberately non-representable VarUInt7
	// gas_limit, which distinguishes this path from external-message rejection.
	if refErr == nil || !strings.Contains(refErr.Error(), "cannot serialize new transaction") {
		t.Fatalf("reference error = %v, want post-accept serialization rejection", refErr)
	}

	accepted, err := testCheckExternalMessageAccepted(NewTVM(), shard, msg, testTxParams{
		Address:     specialAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go acceptance check failed: %v", err)
	}
	if !accepted {
		t.Fatal("go rejected external message accepted by the signed C++ gas limit")
	}
}
