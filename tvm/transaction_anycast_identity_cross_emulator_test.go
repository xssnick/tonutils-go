//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
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

func TestTVMCrossEmulatorTransactionAnycastIdentityV7V8V9V10(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	raw, exact := transactionAnycastIdentityAddresses(t)

	outMsg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     address.NewAddressNone(),
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell(),
	})
	if err != nil {
		t.Fatal(err)
	}
	code := codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHREF(outMsg).Serialize(),
		stackop.PUSHINT(big.NewInt(0)).Serialize(),
		funcsop.SENDRAWMSG().Serialize(),
		funcsop.MYADDR().Serialize(),
		cellsliceop.NEWC().Serialize(),
		cellsliceop.STSLICE().Serialize(),
		cellsliceop.ENDC().Serialize(),
		execop.POPCTR(4).Serialize(),
	)
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     raw,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})
	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())

	for _, tc := range []struct {
		name    string
		version uint32
		want    *address.Address
	}{
		{name: "v7_raw", version: 7, want: raw},
		{name: "v8_raw", version: 8, want: raw},
		{name: "v9_raw", version: 9, want: raw},
		{name: "v10_exact", version: 10, want: exact},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, tc.version)
			shard := buildTransactionTestShardAccount(t, raw, code, cell.BeginCell().EndCell(), walletSendTestBalance, now)

			goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     raw,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}
			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(
				shard,
				msg,
				now,
				uint64(transactionTestLogicalTime),
				tonopsTestSeed,
				configRoot,
			)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}
			wantData := cell.BeginCell().MustStoreAddr(tc.want).EndCell()
			assertTransactionAnycastIdentityData(t, "go", goRes.NextAccount.ShardAccountCell(), wantData)
			assertTransactionAnycastIdentityData(t, "reference", refRes.shardCell, wantData)
			assertTransactionAnycastIdentitySource(t, "go", goRes.TransactionCell, tc.want)
			assertTransactionAnycastIdentitySource(t, "reference", refRes.txCell, tc.want)

			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}
		})
	}
}

func TestTVMCrossEmulatorTransactionAnycastExplicitSourceV7V8V9V10(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	raw, exact := transactionAnycastIdentityAddresses(t)
	wrongData := append([]byte(nil), exact.Data()...)
	wrongData[len(wrongData)-1] ^= 1
	wrong := address.NewAddress(0, byte(exact.Workchain()), wrongData)
	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	inMsg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     raw,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	for _, tc := range []struct {
		name       string
		version    uint32
		source     *address.Address
		resultCode int32
	}{
		{name: "v7_raw_preserved", version: 7, source: raw},
		{name: "v8_raw_preserved", version: 8, source: raw},
		{name: "v9_raw_preserved", version: 9, source: raw},
		{name: "v9_exact_preserved", version: 9, source: exact},
		{name: "v9_wrong_rejected", version: 9, source: wrong, resultCode: 35},
		{name: "v10_exact_preserved", version: 10, source: exact},
		{name: "v10_raw_rejected", version: 10, source: raw, resultCode: 35},
		{name: "v10_wrong_rejected", version: 10, source: wrong, resultCode: 35},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outMsg, err := tlb.ToCell(&tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     tc.source,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(1_000_000_000),
				Body:        cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell(),
			})
			if err != nil {
				t.Fatal(err)
			}
			newData := cell.BeginCell().MustStoreUInt(0xA55A, 16).EndCell()
			code := makeTransactionInternalSendCode(t, outMsg, newData, 0)
			shard := buildTransactionTestShardAccount(t, raw, code, cell.BeginCell().EndCell(), walletSendTestBalance, now)
			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, tc.version)

			goRes, err := testEmulateTransaction(NewTVM(), shard, inMsg, testTxParams{
				Address:     raw,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}
			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(
				shard,
				inMsg,
				now,
				uint64(transactionTestLogicalTime),
				tonopsTestSeed,
				configRoot,
			)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			assertTransactionAnycastActionResult(t, "go", goRes.TransactionCell, tc.resultCode)
			assertTransactionAnycastActionResult(t, "reference", refRes.txCell, tc.resultCode)
			if tc.resultCode == 0 {
				assertTransactionAnycastIdentitySource(t, "go", goRes.TransactionCell, tc.source)
				assertTransactionAnycastIdentitySource(t, "reference", refRes.txCell, tc.source)
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}
		})
	}
}

func TestTVMCrossEmulatorTransactionAnycastFrozenHashUsesOriginalAddress(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	depth := uint64(1)
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	code := makeTransactionInternalSuccessCode(t, newData)
	stateInit := &tlb.StateInit{
		Depth: &depth,
		Code:  code,
		Data:  origData,
	}
	stateInitCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatalf("failed to serialize state init: %v", err)
	}

	rewritePrefix := byte(0)
	if stateInitCell.Hash()[0]&0x80 == 0 {
		rewritePrefix = 0x80
	}
	rawAddr := address.NewAddress(0, 0, stateInitCell.Hash()).
		WithAnycast(address.NewAnycast(1, []byte{rewritePrefix}))
	exactAddr, err := transactionAccountIDAddr(rawAddr)
	if err != nil {
		t.Fatalf("failed to rewrite anycast account address: %v", err)
	}
	if bytes.Equal(rawAddr.Data(), exactAddr.Data()) {
		t.Fatal("anycast test address did not rewrite the account id")
	}

	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      false,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     rawAddr,
		Amount:      tlb.FromNanoTONU(0),
		Body:        cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell(),
	})
	storageInfo := tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(1),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now - 1,
	}
	shard := buildTransactionTestStoredShardAccount(t, rawAddr, tlb.AccountStatusActive, 50, storageInfo, stateInit, nil)
	storagePrices := transactionVersionStoragePricesCell(t, 100)
	gasLimits := buildTransactionGasLimitsCell(t, 10, 1_000_000)

	for _, version := range []uint32{9, 10, 12, 13, 15} {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			wantTxStatus := tlb.AccountStatus(tlb.AccountStatusFrozen)
			if version >= 13 {
				wantTxStatus = tlb.AccountStatusUninit
			}

			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
			configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
				int32(tlb.ConfigParamStoragePrices):        storagePrices,
				int32(tlb.ConfigParamGasPricesBasechain):   gasLimits,
				int32(tlb.ConfigParamGasPricesMasterchain): gasLimits,
			})
			goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     rawAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}
			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(
				shard,
				msg,
				now,
				uint64(transactionTestLogicalTime),
				tonopsTestSeed,
				configRoot,
			)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			goTxStatus := transactionCrossEndStatus(t, goRes.TransactionCell)
			refTxStatus := transactionCrossEndStatus(t, refRes.txCell)
			if goTxStatus != wantTxStatus || refTxStatus != wantTxStatus {
				t.Fatalf("tx status mismatch: go=%s reference=%s want=%s", goTxStatus, refTxStatus, wantTxStatus)
			}
			goAccountStatus := transactionCrossShardStatus(t, goRes.NextAccount.ShardAccountCell())
			refAccountStatus := transactionCrossShardStatus(t, refRes.shardCell)
			if goAccountStatus != tlb.AccountStatusUninit || refAccountStatus != tlb.AccountStatusUninit {
				t.Fatalf("account status mismatch: go=%s reference=%s want=%s", goAccountStatus, refAccountStatus, tlb.AccountStatusUninit)
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}

			// C++ forgets the anycast rewrite metadata whenever this uninitialized
			// account is loaded again. Keep the second transaction in the same test
			// so both the serialized state and the prepared-account lane are covered.
			secondMsg := mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				Bounce:      false,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     exactAddr,
				Amount:      tlb.FromNanoTONU(1_000_000_000),
				Body:        cell.BeginCell().MustStoreUInt(0xFACE, 16).EndCell(),
			})
			secondLT := transactionTestLogicalTime + 10
			secondParams := testTxParams{
				Address:            exactAddr,
				Now:                now,
				BlockLT:            secondLT,
				LogicalTime:        secondLT,
				RandSeed:           append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:         configRoot,
				AccountStorageStat: goRes.AccountStorageStat,
			}
			block, err := secondParams.blockContext()
			if err != nil {
				t.Fatalf("failed to prepare second Go block context: %v", err)
			}
			preparedMsg, err := PrepareMessage(secondMsg)
			if err != nil {
				t.Fatalf("failed to prepare second Go message: %v", err)
			}
			goSecond, err := NewTVM().EmulateTransaction(block, goRes.NextAccount, preparedMsg, secondParams.txOptions())
			if err != nil {
				t.Fatalf("second Go transaction emulation failed: %v", err)
			}

			var refNext tlb.ShardAccount
			if err = tlb.Parse(&refNext, refRes.shardCell); err != nil {
				t.Fatalf("failed to parse first reference shard account: %v", err)
			}
			refSecond, err := runReferenceOrdinaryTransactionWithConfigRoot(
				&refNext,
				secondMsg,
				now,
				uint64(secondLT),
				tonopsTestSeed,
				configRoot,
			)
			if err != nil {
				t.Fatalf("second reference transaction emulation failed: %v", err)
			}

			for side, shardCell := range map[string]*cell.Cell{
				"go":        goSecond.NextAccount.ShardAccountCell(),
				"reference": refSecond.shardCell,
			} {
				var nextShard tlb.ShardAccount
				if err = tlb.Parse(&nextShard, shardCell); err != nil {
					t.Fatalf("failed to parse second %s shard account: %v", side, err)
				}
				var nextAccount tlb.AccountState
				if err = tlb.Parse(&nextAccount, nextShard.Account); err != nil {
					t.Fatalf("failed to parse second %s account: %v", side, err)
				}
				if nextAccount.Address.Anycast() != nil || !bytes.Equal(nextAccount.Address.Data(), exactAddr.Data()) {
					t.Fatalf("second %s account address = %v, want exact %s", side, nextAccount.Address, exactAddr)
				}
			}
			if !bytes.Equal(goSecond.TransactionCell.Hash(), refSecond.txCell.Hash()) {
				t.Fatalf("second transaction hash mismatch:\ngo=%s\nreference=%s", goSecond.TransactionCell.Dump(), refSecond.txCell.Dump())
			}
			if !bytes.Equal(goSecond.NextAccount.ShardAccountCell().Hash(), refSecond.shardCell.Hash()) {
				t.Fatalf("second shard account hash mismatch:\ngo=%s\nreference=%s", goSecond.NextAccount.ShardAccountCell().Dump(), refSecond.shardCell.Dump())
			}
		})
	}
}

func transactionAnycastIdentityAddresses(t *testing.T) (*address.Address, *address.Address) {
	t.Helper()

	data := append([]byte(nil), tonopsTestAddr.Data()...)
	data[0] &^= 0x80
	raw := address.NewAddress(0, byte(tonopsTestAddr.Workchain()), data).
		WithAnycast(address.NewAnycast(1, []byte{0x80}))
	exact, err := transactionAccountIDAddr(raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw, exact
}

func assertTransactionAnycastIdentityData(t *testing.T, side string, shardCell, want *cell.Cell) {
	t.Helper()

	var shard tlb.ShardAccount
	if err := tlb.Parse(&shard, shardCell); err != nil {
		t.Fatalf("failed to parse %s shard account: %v", side, err)
	}
	var account tlb.AccountState
	if err := tlb.Parse(&account, shard.Account); err != nil {
		t.Fatalf("failed to parse %s account: %v", side, err)
	}
	if account.StateInit == nil || account.StateInit.Data == nil {
		t.Fatalf("%s account has no state data", side)
	}
	if account.StateInit.Data.HashKey() != want.HashKey() {
		t.Fatalf("%s c7.myself data = %s, want %s", side, account.StateInit.Data.Dump(), want.Dump())
	}
}

func assertTransactionAnycastIdentitySource(t *testing.T, side string, txCell *cell.Cell, want *address.Address) {
	t.Helper()

	var tx tlb.Transaction
	if err := tlb.LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
		t.Fatalf("failed to parse %s transaction: %v", side, err)
	}
	out, err := tx.IO.Out.ToSlice()
	if err != nil {
		t.Fatalf("failed to parse %s outbound messages: %v", side, err)
	}
	if len(out) != 1 || out[0].MsgType != tlb.MsgTypeInternal {
		t.Fatalf("%s outbound messages = %+v, want one internal message", side, out)
	}
	if got := out[0].AsInternal().SrcAddr; got == nil || !got.Equals(want) {
		t.Fatalf("%s outbound source = %v, want %v", side, got, want)
	}
}

func assertTransactionAnycastActionResult(t *testing.T, side string, txCell *cell.Cell, want int32) {
	t.Helper()

	var tx tlb.Transaction
	if err := tlb.LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
		t.Fatalf("failed to parse %s transaction: %v", side, err)
	}
	desc, ok := tx.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok || desc.ActionPhase == nil {
		t.Fatalf("%s transaction action phase = %+v", side, tx.Description)
	}
	if desc.ActionPhase.ResultCode != want {
		t.Fatalf("%s action result = %d, want %d", side, desc.ActionPhase.ResultCode, want)
	}
}
