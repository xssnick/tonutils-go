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
