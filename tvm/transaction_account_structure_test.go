package tvm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestPrepareAccountRequiresStdAddressAndMatchingIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		addr *address.Address
	}{
		{name: "none", addr: address.NewAddressNone()},
		{name: "external", addr: address.NewAddressExt(0, 8, []byte{0xAA})},
		{name: "variable", addr: address.NewAddressVar(0, 0, 256, make([]byte, 32))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shard := transactionAccountStructureShard(t, tc.addr, tlb.StorageUsed{
				CellsUsed: big.NewInt(0),
				BitsUsed:  big.NewInt(0),
			}, nil, tlb.AccountStatusUninit, nil)
			if _, err := PrepareAccount(shard, tonopsTestAddr); err == nil {
				t.Fatal("non-addr_std existing account was accepted")
			}
		})
	}

	prefix := append([]byte(nil), tonopsTestAddr.Data()[:1]...)
	raw := address.NewAddress(0, byte(tonopsTestAddr.Workchain()), append([]byte(nil), tonopsTestAddr.Data()...)).
		WithAnycast(address.NewAnycast(5, prefix))
	shard := transactionAccountStructureShard(t, raw, tlb.StorageUsed{
		CellsUsed: big.NewInt(0),
		BitsUsed:  big.NewInt(0),
	}, nil, tlb.AccountStatusUninit, nil)
	exact, err := transactionAccountIDAddr(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = PrepareAccount(shard, exact); err != nil {
		t.Fatalf("matching anycast account identity was rejected: %v", err)
	}
	if _, err = PrepareAccount(shard, nil); err != nil {
		t.Fatalf("self-contained existing account was rejected: %v", err)
	}

	wrongData := append([]byte(nil), exact.Data()...)
	wrongData[len(wrongData)-1] ^= 1
	wrong := address.NewAddress(0, byte(exact.Workchain()), wrongData)
	if _, err = PrepareAccount(shard, wrong); err == nil {
		t.Fatal("existing account with a mismatching requested identity was accepted")
	}
}

func TestPrepareAccountRejectsNonCanonicalVarUIntegers(t *testing.T) {
	for _, field := range []string{"storage used", "due payment", "balance"} {
		t.Run(field, func(t *testing.T) {
			shard := &tlb.ShardAccount{
				Account:       transactionAccountWithPaddedVarUInteger(field),
				LastTransHash: make([]byte, 32),
			}
			if _, err := PrepareAccount(shard, tonopsTestAddr); err == nil {
				t.Fatal("account with a leading-zero VarUInteger was accepted")
			}
		})
	}
}

func TestPrepareAccountRecursivelyValidatesExtraCurrencies(t *testing.T) {
	zeroValue := cell.NewDict(32)
	if err := zeroValue.SetIntKey(big.NewInt(1), cell.BeginCell().MustStoreUInt(0, 5).EndCell()); err != nil {
		t.Fatal(err)
	}
	leadingZero := cell.NewDict(32)
	if err := leadingZero.SetIntKey(big.NewInt(1), cell.BeginCell().MustStoreUInt(1, 5).MustStoreUInt(0, 8).EndCell()); err != nil {
		t.Fatal(err)
	}
	trailing := cell.NewDict(32)
	if err := trailing.SetIntKey(big.NewInt(1), cell.BeginCell().MustStoreUInt(1, 5).MustStoreUInt(1, 8).MustStoreBoolBit(true).EndCell()); err != nil {
		t.Fatal(err)
	}
	malformedRoot := cell.BeginCell().
		MustStoreUInt(0, 2). // hml_short with an empty label: a fork for a 32-bit key
		MustStoreRef(cell.BeginCell().EndCell()).
		EndCell().AsDict(32)

	for _, tc := range []struct {
		name string
		dict *cell.Dictionary
	}{
		{name: "zero VarUIntegerPos", dict: zeroValue},
		{name: "leading zero", dict: leadingZero},
		{name: "trailing leaf data", dict: trailing},
		{name: "malformed topology", dict: malformedRoot},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shard := transactionAccountStructureShard(t, tonopsTestAddr, tlb.StorageUsed{
				CellsUsed: big.NewInt(0),
				BitsUsed:  big.NewInt(0),
			}, tc.dict, tlb.AccountStatusUninit, nil)
			if _, err := PrepareAccount(shard, tonopsTestAddr); err == nil {
				t.Fatal("account with invalid extra currencies was accepted")
			}
		})
	}
}

func TestPrepareParsedAccountCannotBypassStructuralValidation(t *testing.T) {
	badExtra := cell.NewDict(32)
	if err := badExtra.SetIntKey(big.NewInt(1), cell.BeginCell().MustStoreUInt(0, 5).EndCell()); err != nil {
		t.Fatal(err)
	}
	for _, shard := range []*tlb.ShardAccount{
		transactionAccountStructureShard(t, address.NewAddressVar(0, 0, 256, make([]byte, 32)), tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		}, nil, tlb.AccountStatusUninit, nil),
		transactionAccountStructureShard(t, tonopsTestAddr, tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		}, badExtra, tlb.AccountStatusUninit, nil),
	} {
		var parsed tlb.AccountState
		if err := tlb.Parse(&parsed, shard.Account); err != nil {
			t.Fatal(err)
		}
		if _, err := PrepareParsedAccount(shard, &parsed, nil); err == nil {
			t.Fatal("parsed account bypassed backing-cell structural validation")
		}
	}
}

func TestPrepareAccountTreatsExistingStateInitLibraryAsOpaqueCell(t *testing.T) {
	// Existing Account uses StateInit, whose library field is Maybe ^Cell in
	// the reference schema. StateInitWithLibs/SimpleLib validation applies to
	// messages, not to Account::unpack.
	opaque := cell.BeginCell().
		MustStoreUInt(0, 2).
		MustStoreRef(cell.BeginCell().EndCell()).
		EndCell().AsDict(256)
	shard := transactionAccountStructureShard(t, tonopsTestAddr, tlb.StorageUsed{
		CellsUsed: big.NewInt(0),
		BitsUsed:  big.NewInt(0),
	}, nil, tlb.AccountStatusActive, &tlb.StateInit{Lib: opaque})
	if _, err := PrepareAccount(shard, tonopsTestAddr); err != nil {
		t.Fatalf("opaque existing-account library cell was rejected: %v", err)
	}
}

func TestPrepareAccountAcceptsFullVarUInteger7Range(t *testing.T) {
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 48), big.NewInt(1))
	shard := transactionAccountStructureShard(t, tonopsTestAddr, tlb.StorageUsed{
		CellsUsed: new(big.Int).Set(max),
		BitsUsed:  new(big.Int).Set(max),
	}, nil, tlb.AccountStatusUninit, nil)
	if _, err := PrepareAccount(shard, tonopsTestAddr); err != nil {
		t.Fatalf("valid 6-byte VarUInteger 7 value was rejected: %v", err)
	}
}

func TestValidateTransactionAccountStructureCommonPathDoesNotAllocate(t *testing.T) {
	shard := transactionAccountStructureShard(t, tonopsTestAddr, tlb.StorageUsed{
		CellsUsed: big.NewInt(2),
		BitsUsed:  big.NewInt(64),
	}, nil, tlb.AccountStatusActive, &tlb.StateInit{
		Code: cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell(),
		Data: cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell(),
	})

	allocs := testing.AllocsPerRun(1_000, func() {
		if err := validateTransactionAccountStructure(shard.Account); err != nil {
			panic(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("structural validation allocs = %v, want 0", allocs)
	}
}

func transactionAccountStructureShard(t *testing.T, addr *address.Address, used tlb.StorageUsed, extra *cell.Dictionary, status tlb.AccountStatus, stateInit *tlb.StateInit) *tlb.ShardAccount {
	t.Helper()
	state := &tlb.AccountState{
		IsValid: true,
		Address: addr,
		StorageInfo: tlb.StorageInfo{
			StorageUsed:  used,
			StorageExtra: tlb.StorageExtraNone{},
		},
		AccountStorage: tlb.AccountStorage{
			LastTransactionLT: 1,
			Balance:           tlb.FromNanoTONU(1),
			ExtraCurrencies:   extra,
			Status:            status,
			StateInit:         stateInit,
		},
	}
	account, err := state.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	return &tlb.ShardAccount{
		Account:       account,
		LastTransHash: make([]byte, 32),
	}
}

func transactionAccountWithPaddedVarUInteger(field string) *cell.Cell {
	b := cell.BeginCell().
		MustStoreBoolBit(true).
		MustStoreAddr(tonopsTestAddr)
	if field == "storage used" {
		b.MustStoreUInt(1, 3).MustStoreUInt(0, 8)
	} else {
		b.MustStoreUInt(0, 3)
	}
	b.MustStoreUInt(0, 3). // storage bits used
				MustStoreUInt(0, 3). // storage_extra_none
				MustStoreUInt(0, 32)
	if field == "due payment" {
		b.MustStoreBoolBit(true).MustStoreUInt(1, 4).MustStoreUInt(0, 8)
	} else {
		b.MustStoreBoolBit(false)
	}
	b.MustStoreUInt(1, 64)
	if field == "balance" {
		b.MustStoreUInt(1, 4).MustStoreUInt(0, 8)
	} else {
		b.MustStoreBigCoins(big.NewInt(1))
	}
	b.MustStoreBoolBit(false) // empty extra currencies
	b.MustStoreUInt(0, 2)     // account_uninit
	return b.EndCell()
}

func BenchmarkPrepareAccountStructure(b *testing.B) {
	state := &tlb.AccountState{
		IsValid: true,
		Address: tonopsTestAddr,
		StorageInfo: tlb.StorageInfo{
			StorageUsed: tlb.StorageUsed{
				CellsUsed: big.NewInt(2),
				BitsUsed:  big.NewInt(64),
			},
			StorageExtra: tlb.StorageExtraNone{},
		},
		AccountStorage: tlb.AccountStorage{
			LastTransactionLT: 1,
			Balance:           tlb.FromNanoTONU(1_000_000_000),
			Status:            tlb.AccountStatusActive,
			StateInit: &tlb.StateInit{
				Code: cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell(),
				Data: cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell(),
			},
		},
	}
	account, err := state.ToCell()
	if err != nil {
		b.Fatal(err)
	}
	shard := &tlb.ShardAccount{
		Account:       account,
		LastTransHash: make([]byte, 32),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err = PrepareAccount(shard, tonopsTestAddr); err != nil {
			b.Fatal(err)
		}
	}
}
