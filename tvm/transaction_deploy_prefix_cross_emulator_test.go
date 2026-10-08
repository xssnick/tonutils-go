//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
)

func TestTVMCrossEmulatorTransactionDeploymentPrefixAddress(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}
	outgoing := mustTransactionMsgCell(t, &tlb.InternalMessage{IHRDisabled: true,
		SrcAddr: address.NewAddressNone(), DstAddr: tonopsTestAddr, Amount: tlb.FromNanoTONU(1000)})
	code := codeFromBuilders(t, stackop.PUSHREF(outgoing).Serialize(), stackop.PUSHINT(big.NewInt(1)).Serialize(),
		funcsop.SENDRAWMSG().Serialize(), funcsop.MYADDR().Serialize(), cellsliceop.NEWC().Serialize(),
		cellsliceop.STSLICE().Serialize(), cellsliceop.ENDC().Serialize(), execop.POPCTR(4).Serialize())
	depth := uint64(1)
	state := &tlb.StateInit{Depth: &depth, Code: code, Data: cell.BeginCell().EndCell()}
	stateCell, err := tlb.ToCell(state)
	if err != nil {
		t.Fatal(err)
	}
	addrBytes := append([]byte(nil), stateCell.Hash()...)
	addrBytes[0] ^= 0x80
	addr := address.NewAddress(0, 0, addrBytes)
	now := uint32(tonopsTestTime.Unix())
	shard := buildTransactionTestUninitShardAccount(t, addr, walletSendTestBalance, tlb.StorageInfo{
		StorageUsed:  tlb.StorageUsed{CellsUsed: big.NewInt(1), BitsUsed: big.NewInt(0)},
		StorageExtra: tlb.StorageExtraNone{}, LastPaid: now,
	})
	message := mustTransactionMsgCell(t, &tlb.InternalMessage{IHRDisabled: true, SrcAddr: internalEmulationSrcAddr,
		DstAddr: addr, Amount: tlb.FromNanoTONU(500_000_000), StateInit: state, Body: cell.BeginCell().EndCell()})
	baseConfig := mustReferenceTransactionConfigRoot(t)
	for version := uint32(0); version <= 10; version++ {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			config := referenceTransactionConfigRootWithGlobalVersion(t, baseConfig, version)
			ref, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, message, now, uint64(transactionTestLogicalTime), tonopsTestSeed, config)
			if err != nil {
				t.Fatal(err)
			}
			result, err := testEmulateTransaction(NewTVM(), shard, message, testTxParams{
				Address: addr, Now: now, BlockLT: transactionTestLogicalTime, LogicalTime: transactionTestLogicalTime,
				RandSeed: tonopsTestSeed, ConfigRoot: config,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(result.TransactionCell.Hash(), ref.txCell.Hash()) ||
				!bytes.Equal(result.NextAccount.ShardAccountCell().Hash(), ref.shardCell.Hash()) {
				t.Fatalf("deployment address mismatch: Go gas=%d exit=%d tx=%x; C++ gas=%d exit=%d tx=%x",
					result.GasUsed, result.ExitCode, result.TransactionCell.Hash(), ref.gasUsed, ref.exitCode, ref.txCell.Hash())
			}
			if result.NextAccount.runtime.addrVM != nil || result.NextAccount.State().Address.Anycast() != nil {
				t.Fatal("temporary deployment address leaked into the next account")
			}
			var refShard tlb.ShardAccount
			if err := tlb.Parse(&refShard, ref.shardCell); err != nil {
				t.Fatal(err)
			}
			secondRef, err := runReferenceOrdinaryTransactionWithConfigRoot(&refShard, message, now,
				uint64(transactionTestLogicalTime+10), tonopsTestSeed, config)
			if err != nil {
				t.Fatal(err)
			}
			second, err := testEmulateTransaction(NewTVM(), result.NextAccount.ShardAccount(), message, testTxParams{
				Address: addr, Now: now, BlockLT: transactionTestLogicalTime, LogicalTime: transactionTestLogicalTime + 10,
				RandSeed: tonopsTestSeed, ConfigRoot: config, AccountStorageStat: result.AccountStorageStat,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(second.TransactionCell.Hash(), secondRef.txCell.Hash()) ||
				!bytes.Equal(second.NextAccount.ShardAccountCell().Hash(), secondRef.shardCell.Hash()) {
				t.Fatal("subsequent active transaction differs from reference")
			}
		})
	}
}
