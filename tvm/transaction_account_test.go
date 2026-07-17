package tvm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestPrepareAccountRequiresExactAccountCell(t *testing.T) {
	valid := cell.BeginCell().MustStoreBoolBit(false).EndCell()
	shard := func(account *cell.Cell) *tlb.ShardAccount {
		return &tlb.ShardAccount{
			Account:       account,
			LastTransHash: make([]byte, 32),
		}
	}

	if _, err := PrepareAccount(shard(valid), tonopsTestAddr); err != nil {
		t.Fatalf("exact account cell was rejected: %v", err)
	}

	trailingBit := cell.BeginCell().
		MustStoreBoolBit(false).
		MustStoreBoolBit(true).
		EndCell()
	if _, err := PrepareAccount(shard(trailingBit), tonopsTestAddr); err == nil {
		t.Fatal("account cell with a trailing bit was accepted")
	}

	trailingRef := cell.BeginCell().
		MustStoreBoolBit(false).
		MustStoreRef(cell.BeginCell().EndCell()).
		EndCell()
	if _, err := PrepareAccount(shard(trailingRef), tonopsTestAddr); err == nil {
		t.Fatal("account cell with a trailing reference was accepted")
	}
}

func TestPrepareParsedAccountMatchesBackingCell(t *testing.T) {
	state := &tlb.AccountState{
		IsValid: true,
		Address: tonopsTestAddr,
		StorageInfo: tlb.StorageInfo{
			StorageUsed: tlb.StorageUsed{
				CellsUsed: big.NewInt(0),
				BitsUsed:  big.NewInt(0),
			},
			StorageExtra: tlb.StorageExtraNone{},
		},
		AccountStorage: tlb.AccountStorage{
			LastTransactionLT: 1,
			Balance:           tlb.FromNanoTONU(1),
			Status:            tlb.AccountStatusUninit,
		},
	}
	stateCell, err := state.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	shard := &tlb.ShardAccount{
		Account:       stateCell,
		LastTransHash: make([]byte, 32),
	}

	if _, err = PrepareParsedAccount(shard, state, tonopsTestAddr); err != nil {
		t.Fatalf("matching parsed account was rejected: %v", err)
	}

	mismatch := *state
	addrData := append([]byte(nil), state.Address.Data()...)
	addrData[len(addrData)-1] ^= 1
	mismatch.Address = address.NewAddress(0, 0, addrData)
	if _, err = PrepareParsedAccount(shard, &mismatch, tonopsTestAddr); err == nil {
		t.Fatal("parsed account with a different identity was accepted")
	}

	backingWithSuffix := cell.BeginCell().
		MustStoreBuilder(stateCell.ToBuilder()).
		MustStoreBoolBit(true).
		EndCell()
	shard.Account = backingWithSuffix
	if _, err = PrepareParsedAccount(shard, state, tonopsTestAddr); err == nil {
		t.Fatal("parsed account was accepted with a suffixed backing cell")
	}
}

func TestTransactionFrozenStateInitDepthMatchesExistingAnycast(t *testing.T) {
	for _, depth := range []uint64{1, 30} {
		t.Run(new(big.Int).SetUint64(depth).String(), func(t *testing.T) {
			stateInit := &tlb.StateInit{
				Depth: &depth,
				Code:  cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell(),
				Data:  cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell(),
			}
			stateCell, err := tlb.ToCell(stateInit)
			if err != nil {
				t.Fatal(err)
			}
			addr := address.NewAddress(0, 0, stateCell.Hash()).WithAnycast(
				address.NewAnycast(uint(depth), make([]byte, (depth+7)/8)),
			)
			msg := &tlb.Message{
				MsgType: tlb.MsgTypeExternalIn,
				Msg: &tlb.ExternalMessage{
					DstAddr:   addr,
					StateInit: stateInit,
				},
			}
			acc := &transactionRuntimeAccount{
				addr:      addr,
				status:    tlb.AccountStatusFrozen,
				stateHash: stateCell.Hash(),
			}

			next, used, skip, err := transactionPrepareComputeAccount(acc, tlb.AccountStatusFrozen, false, msg, false, transactionTestConfigWithGlobalVersion(t, 14))
			if err != nil {
				t.Fatal(err)
			}
			if skip != nil || !used || next.status != tlb.AccountStatusActive {
				t.Fatalf("matching depth was rejected: next=%+v used=%t skip=%+v", next, used, skip)
			}
		})
	}

	accountDepth := uint64(5)
	stateDepth := uint64(6)
	stateInit := &tlb.StateInit{
		Depth: &stateDepth,
		Code:  cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell(),
	}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatal(err)
	}
	addr := address.NewAddress(0, 0, stateCell.Hash()).WithAnycast(
		address.NewAnycast(uint(accountDepth), make([]byte, (accountDepth+7)/8)),
	)
	msg := &tlb.Message{
		MsgType: tlb.MsgTypeExternalIn,
		Msg: &tlb.ExternalMessage{
			DstAddr:   addr,
			StateInit: stateInit,
		},
	}
	_, used, skip, err := transactionPrepareComputeAccount(&transactionRuntimeAccount{
		addr:      addr,
		status:    tlb.AccountStatusFrozen,
		stateHash: stateCell.Hash(),
	}, tlb.AccountStatusFrozen, false, msg, false, transactionTestConfigWithGlobalVersion(t, 14))
	if err != nil {
		t.Fatal(err)
	}
	if used || skip == nil || skip.Type != tlb.ComputeSkipReasonBadState {
		t.Fatalf("mismatching depth result: used=%t skip=%+v, want bad_state", used, skip)
	}
}

func TestTransactionAccountStorageStatReplaceSharedRef(t *testing.T) {
	shared := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	oldStorage := cell.BeginCell().MustStoreUInt(0xA, 4).
		MustStoreRef(shared).
		MustStoreRef(shared).
		EndCell()
	newStorage := cell.BeginCell().MustStoreUInt(0xB, 4).
		MustStoreRef(shared).
		EndCell()

	oldUsage, oldDict, err := transactionComputeAccountStorageStat(oldStorage)
	if err != nil {
		t.Fatal(err)
	}
	if oldUsage != (transactionUsage{cells: 2, bits: 12}) {
		t.Fatalf("old storage usage = %+v, want 2 cells and 12 bits", oldUsage)
	}

	stat, err := transactionInitAccountStorageStat(oldDict, oldStorage, tlb.StorageUsed{
		CellsUsed: new(big.Int).SetUint64(oldUsage.cells),
		BitsUsed:  new(big.Int).SetUint64(oldUsage.bits),
	}, transactionAccountStorageStatRootHash(oldDict))
	if err != nil {
		t.Fatal(err)
	}

	gotUsage, gotDict, err := stat.replaceStorage(newStorage)
	if err != nil {
		t.Fatal(err)
	}

	wantUsage, wantDict, err := transactionComputeAccountStorageStat(newStorage)
	if err != nil {
		t.Fatal(err)
	}
	if gotUsage != wantUsage {
		t.Fatalf("replace storage usage = %+v, want %+v", gotUsage, wantUsage)
	}
	if !transactionCellEqual(gotDict, wantDict) {
		t.Fatal("replace storage stat dictionary does not match full recompute")
	}
}

func TestTransactionAccountStorageRefsUnchanged(t *testing.T) {
	left := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	right := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	oldStorage := cell.BeginCell().MustStoreUInt(0xA, 4).
		MustStoreRef(left).
		MustStoreRef(right).
		EndCell()
	newStorage := cell.BeginCell().MustStoreUInt(0xB, 4).
		MustStoreRef(left).
		MustStoreRef(right).
		EndCell()

	unchanged, err := transactionAccountStorageRefsUnchanged(oldStorage, newStorage)
	if err != nil {
		t.Fatal(err)
	}
	if !unchanged {
		t.Fatal("expected same storage refs to be unchanged")
	}

	reordered := cell.BeginCell().MustStoreUInt(0xC, 4).
		MustStoreRef(right).
		MustStoreRef(left).
		EndCell()
	unchanged, err = transactionAccountStorageRefsUnchanged(oldStorage, reordered)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged {
		t.Fatal("expected reordered storage refs to be changed")
	}
}

func TestTransactionAccountStorageRootDiffSortsByHash(t *testing.T) {
	keep := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	remove := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	add := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()

	oldRoots := [4]*cell.Cell{remove, keep}
	newRoots := [4]*cell.Cell{add, keep}
	toAdd, toAddNum, toDel, toDelNum := transactionAccountStorageRootDiff(oldRoots, 2, newRoots, 2)
	if toAddNum != 1 || toAdd[0].HashKey() != add.HashKey() {
		t.Fatalf("toAdd = %v, want only add root", toAddNum)
	}
	if toDelNum != 1 || toDel[0].HashKey() != remove.HashKey() {
		t.Fatalf("toDel = %v, want only remove root", toDelNum)
	}
}

func TestTransactionAccountStatusAndStorageHelperEdges(t *testing.T) {
	if got := transactionAccountStorageStatRootHash(nil); len(got) != 32 || !transactionHashIsZero(got) {
		t.Fatalf("nil storage stat hash = %x, want 32 zero bytes", got)
	}
	if transactionHashIsZero(nil) {
		t.Fatal("nil hash should not count as zero hash")
	}
	if transactionHashIsZero([]byte{0, 1}) {
		t.Fatal("non-zero hash should not count as zero hash")
	}

	if transactionCloneUint64(nil) != nil {
		t.Fatal("nil uint64 clone should stay nil")
	}
	depth := uint64(7)
	cloned := transactionCloneUint64(&depth)
	depth = 9
	if cloned == nil || *cloned != 7 {
		t.Fatalf("cloned depth = %v, want independent 7", cloned)
	}

	extra := makeTransactionExtraCurrencies(t, 7, 1)
	for _, tc := range []struct {
		name      string
		status    tlb.AccountStatus
		deleted   bool
		balance   *big.Int
		extra     *cell.Dictionary
		activated bool
		want      tlb.AccountStatus
	}{
		{name: "deleted empty", status: tlb.AccountStatusActive, deleted: true, balance: big.NewInt(0), want: tlb.AccountStatusNonExist},
		{name: "deleted grams remain", status: tlb.AccountStatusActive, deleted: true, balance: big.NewInt(1), want: tlb.AccountStatusUninit},
		{name: "deleted extra remain", status: tlb.AccountStatusActive, deleted: true, balance: big.NewInt(0), extra: extra, want: tlb.AccountStatusUninit},
		{name: "uninit empty not activated", status: tlb.AccountStatusUninit, balance: big.NewInt(0), want: tlb.AccountStatusNonExist},
		{name: "uninit empty activated", status: tlb.AccountStatusUninit, balance: big.NewInt(0), activated: true, want: tlb.AccountStatusUninit},
		{name: "active empty", status: tlb.AccountStatusActive, balance: big.NewInt(0), want: tlb.AccountStatusActive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := transactionFinalizeAccountStatus(tc.status, tc.deleted, tc.balance, tc.extra, tc.activated); got != tc.want {
				t.Fatalf("final status = %s, want %s", got, tc.want)
			}
		})
	}

	oldUsage := tlb.StorageUsed{
		CellsUsed: big.NewInt(2),
		BitsUsed:  big.NewInt(4),
	}
	got, err := transactionAccountStorageUsageWithSameRefs(oldUsage, nil, cell.BeginCell().MustStoreUInt(1, 1).EndCell())
	if err != nil {
		t.Fatal(err)
	}
	if got != (transactionUsage{cells: 2, bits: 4}) {
		t.Fatalf("nil old storage usage = %+v, want old usage", got)
	}

	oldStorage := cell.BeginCell().MustStoreUInt(0xffff, 16).EndCell()
	newStorage := cell.BeginCell().MustStoreUInt(1, 1).EndCell()
	got, err = transactionAccountStorageUsageWithSameRefs(oldUsage, oldStorage, newStorage)
	if err != nil {
		t.Fatal(err)
	}
	if got != (transactionUsage{cells: 2, bits: 0}) {
		t.Fatalf("shrunk storage usage = %+v, want old cells and zero bits", got)
	}

	if sorted := transactionSortedAccountStorageRoots([4]*cell.Cell{}, 0); sorted != ([4]transactionAccountStorageRootHash{}) {
		t.Fatalf("empty sorted roots = %+v, want zero array", sorted)
	}

	if got := transactionStorageUsedUint64(new(big.Int).Lsh(big.NewInt(1), 70)); got != ^uint64(0) {
		t.Fatalf("overflow storage used = %d, want max uint64", got)
	}
	if got := transactionStorageUsedUint64(big.NewInt(-1)); got != 0 {
		t.Fatalf("negative storage used = %d, want 0", got)
	}
}

func TestTransactionAddressSuspensionAndStateInitEdges(t *testing.T) {
	now := uint32(100)
	if suspended := emptyPreparedTestConfig().isAddressSuspended(now, address.NewAddressExt(0, 8, []byte{0xAB})); suspended {
		t.Fatalf("external address suspended = %t, want false", suspended)
	}
	if suspended := emptyPreparedTestConfig().isAddressSuspended(now, tonopsTestAddr); suspended {
		t.Fatalf("missing suspended config = %t, want false", suspended)
	}

	suspendedDict := cell.NewDict(288)
	suspendedKey := cell.BeginCell().
		MustStoreInt(int64(tonopsTestAddr.Workchain()), 32).
		MustStoreSlice(tonopsTestAddr.Data(), 256).
		EndCell()
	if err := suspendedDict.Set(suspendedKey, cell.BeginCell().EndCell()); err != nil {
		t.Fatalf("failed to store suspended address: %v", err)
	}
	activeList, err := tlb.ToCell(&tlb.SuspendedAddressList{
		Addresses:      suspendedDict,
		SuspendedUntil: now + 1,
	})
	if err != nil {
		t.Fatalf("failed to build suspended list: %v", err)
	}
	expiredList, err := tlb.ToCell(&tlb.SuspendedAddressList{
		Addresses:      suspendedDict,
		SuspendedUntil: now,
	})
	if err != nil {
		t.Fatalf("failed to build expired suspended list: %v", err)
	}

	activeCfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamSuspendedAddressList: activeList,
	})
	expiredCfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamSuspendedAddressList: expiredList,
	})
	if suspended := expiredCfg.isAddressSuspended(now, tonopsTestAddr); suspended {
		t.Fatalf("expired suspended list = %t, want false", suspended)
	}
	if suspended := activeCfg.isAddressSuspended(now, address.NewAddress(0, byte(tonopsTestAddr.Workchain()), bytesWithFirstBitFlipped(tonopsTestAddr.Data()))); suspended {
		t.Fatalf("missing address suspended = %t, want false", suspended)
	}
	if suspended := activeCfg.isAddressSuspended(now, tonopsTestAddr); !suspended {
		t.Fatalf("listed address suspended = %t, want true", suspended)
	}

	depth := uint64(31)
	if transactionStateInitMatchesAddress(make([]byte, 32), tonopsTestAddr, &depth) {
		t.Fatal("fixed prefix above 30 should not match")
	}
	if transactionStateInitMatchesAddress([]byte{1, 2}, tonopsTestAddr, nil) {
		t.Fatal("short state hash should not match")
	}
	if transactionStateInitMatchesAddress(make([]byte, 32), address.NewAddressExt(0, 8, []byte{0xAB}), nil) {
		t.Fatal("non-std address data should not match state hash")
	}
}

func FuzzTransactionFinalizeAccountStatusBoundaries(f *testing.F) {
	f.Add(byte(0), false, false, int64(0), false)
	f.Add(byte(1), true, false, int64(0), false)
	f.Add(byte(2), true, false, int64(1), false)
	f.Add(byte(1), false, true, int64(0), false)
	f.Add(byte(1), false, false, int64(0), true)

	statuses := []tlb.AccountStatus{
		tlb.AccountStatusUninit,
		tlb.AccountStatusActive,
		tlb.AccountStatusFrozen,
		tlb.AccountStatusNonExist,
	}

	f.Fuzz(func(t *testing.T, rawStatus byte, deleted, activated bool, rawBalance int64, hasExtra bool) {
		status := statuses[int(rawStatus)%len(statuses)]
		balance := big.NewInt(rawBalance)
		if rawBalance < 0 {
			balance.SetInt64(0)
		}
		var extra *cell.Dictionary
		if hasExtra {
			extra = makeTransactionExtraCurrencies(t, 7, 1)
		}

		want := status
		if deleted {
			want = tlb.AccountStatusUninit
			if balance.Sign() == 0 && transactionExtraDictIsEmpty(extra) {
				want = tlb.AccountStatusNonExist
			}
		} else if status == tlb.AccountStatusUninit && !activated && balance.Sign() == 0 && transactionExtraDictIsEmpty(extra) {
			want = tlb.AccountStatusNonExist
		}

		if got := transactionFinalizeAccountStatus(status, deleted, balance, extra, activated); got != want {
			t.Fatalf("status=%s deleted=%t activated=%t balance=%s extra=%t final=%s want=%s", status, deleted, activated, balance, hasExtra, got, want)
		}
	})
}

func bytesWithFirstBitFlipped(src []byte) []byte {
	out := append([]byte(nil), src...)
	out[0] ^= 0x80
	return out
}
