package tlb

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"reflect"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// Shadow structs carry the same tlb tags as StorageUsed/StorageInfo but no
// methods, so the generic loader/serializer takes the reflective struct-tag
// path. They pin the hand-rolled implementations to the struct-tag spec.
type shadowStorageUsed struct {
	CellsUsed *big.Int `tlb:"var uint 7"`
	BitsUsed  *big.Int `tlb:"var uint 7"`
}

type shadowStorageInfo struct {
	StorageUsed  shadowStorageUsed `tlb:"."`
	StorageExtra any               `tlb:"[StorageExtraNone,StorageExtraInfo]"`
	LastPaid     uint32            `tlb:"## 32"`
	DuePayment   *Coins            `tlb:"maybe ."`
}

func storageInfoTestCases() map[string]StorageInfo {
	due := FromNanoTONU(1_500_000_007)
	dictHash := benchmarkBytes(0xAB)

	return map[string]StorageInfo{
		"zero used, extra none, no due payment": {
			StorageUsed: StorageUsed{
				CellsUsed: big.NewInt(0),
				BitsUsed:  big.NewInt(0),
			},
			StorageExtra: StorageExtraNone{},
			LastPaid:     0,
		},
		"small used, extra none, no due payment": {
			StorageUsed: StorageUsed{
				CellsUsed: big.NewInt(25),
				BitsUsed:  big.NewInt(12_345),
			},
			StorageExtra: StorageExtraNone{},
			LastPaid:     1_700_000_000,
		},
		"big used, extra info, due payment": {
			StorageUsed: StorageUsed{
				CellsUsed: new(big.Int).SetUint64(0x1234_5678_9ABC),
				BitsUsed:  new(big.Int).SetUint64(0xFFFF_FFFF_FFFF),
			},
			StorageExtra: StorageExtraInfo{DictHash: dictHash},
			LastPaid:     1_234_567_890,
			DuePayment:   &due,
		},
	}
}

func assertStorageInfoEqual(t *testing.T, got, want StorageInfo) {
	t.Helper()

	if got.StorageUsed.CellsUsed.Cmp(want.StorageUsed.CellsUsed) != 0 {
		t.Fatalf("cells used mismatch: got %s, want %s", got.StorageUsed.CellsUsed, want.StorageUsed.CellsUsed)
	}
	if got.StorageUsed.BitsUsed.Cmp(want.StorageUsed.BitsUsed) != 0 {
		t.Fatalf("bits used mismatch: got %s, want %s", got.StorageUsed.BitsUsed, want.StorageUsed.BitsUsed)
	}
	if !reflect.DeepEqual(got.StorageExtra, want.StorageExtra) {
		t.Fatalf("storage extra mismatch: got %#v, want %#v", got.StorageExtra, want.StorageExtra)
	}
	if got.LastPaid != want.LastPaid {
		t.Fatalf("last paid mismatch: got %d, want %d", got.LastPaid, want.LastPaid)
	}

	if (got.DuePayment == nil) != (want.DuePayment == nil) {
		t.Fatalf("due payment presence mismatch: got %v, want %v", got.DuePayment, want.DuePayment)
	}
	if want.DuePayment != nil && got.DuePayment.Nano().Cmp(want.DuePayment.Nano()) != 0 {
		t.Fatalf("due payment mismatch: got %s, want %s", got.DuePayment.Nano(), want.DuePayment.Nano())
	}
}

func TestStorageInfoReflectiveParity(t *testing.T) {
	for name, info := range storageInfoTestCases() {
		t.Run(name, func(t *testing.T) {
			fastCell, err := info.ToCell()
			if err != nil {
				t.Fatal(err)
			}

			shadow := shadowStorageInfo{
				StorageUsed: shadowStorageUsed{
					CellsUsed: info.StorageUsed.CellsUsed,
					BitsUsed:  info.StorageUsed.BitsUsed,
				},
				StorageExtra: info.StorageExtra,
				LastPaid:     info.LastPaid,
				DuePayment:   info.DuePayment,
			}
			reflectiveCell, err := ToCell(&shadow)
			if err != nil {
				t.Fatal(err)
			}

			if !bytes.Equal(fastCell.Hash(), reflectiveCell.Hash()) {
				t.Fatal("hand-rolled and reflective serialization differ")
			}

			// hand-rolled parse of the reflectively-built cell
			var parsed StorageInfo
			if err = parsed.LoadFromCell(reflectiveCell.MustBeginParse()); err != nil {
				t.Fatal(err)
			}
			assertStorageInfoEqual(t, parsed, info)

			// reflective parse of the hand-rolled cell
			var parsedShadow shadowStorageInfo
			if err = LoadFromCell(&parsedShadow, fastCell.MustBeginParse()); err != nil {
				t.Fatal(err)
			}
			assertStorageInfoEqual(t, StorageInfo{
				StorageUsed: StorageUsed{
					CellsUsed: parsedShadow.StorageUsed.CellsUsed,
					BitsUsed:  parsedShadow.StorageUsed.BitsUsed,
				},
				StorageExtra: parsedShadow.StorageExtra,
				LastPaid:     parsedShadow.LastPaid,
				DuePayment:   parsedShadow.DuePayment,
			}, info)

			// round trip identity
			roundTrip, err := parsed.ToCell()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(roundTrip.Hash(), fastCell.Hash()) {
				t.Fatal("storage info round-trip hash mismatch")
			}
		})
	}
}

func TestStorageUsedReflectiveParity(t *testing.T) {
	values := []*big.Int{
		big.NewInt(0),
		big.NewInt(1),
		big.NewInt(255),
		big.NewInt(256),
		new(big.Int).SetUint64(0xFFFF_FFFF_FFFF), // 6 bytes, max width for var uint 7
	}

	for _, cells := range values {
		for _, bits := range values {
			used := StorageUsed{CellsUsed: cells, BitsUsed: bits}

			fastCell, err := used.ToCell()
			if err != nil {
				t.Fatal(err)
			}

			shadow := shadowStorageUsed{CellsUsed: cells, BitsUsed: bits}
			reflectiveCell, err := ToCell(&shadow)
			if err != nil {
				t.Fatal(err)
			}

			if !bytes.Equal(fastCell.Hash(), reflectiveCell.Hash()) {
				t.Fatalf("serialization mismatch for cells=%s bits=%s", cells, bits)
			}

			var parsed StorageUsed
			if err = parsed.LoadFromCell(reflectiveCell.MustBeginParse()); err != nil {
				t.Fatal(err)
			}
			if parsed.CellsUsed.Cmp(cells) != 0 || parsed.BitsUsed.Cmp(bits) != 0 {
				t.Fatalf("parse mismatch for cells=%s bits=%s: got %s/%s", cells, bits, parsed.CellsUsed, parsed.BitsUsed)
			}
		}
	}
}

func TestStorageInfoUnknownExtraMagic(t *testing.T) {
	corrupted := cell.BeginCell().
		MustStoreVarUInt(1, 7).
		MustStoreVarUInt(1, 7).
		MustStoreUInt(0b010, 3). // neither StorageExtraNone nor StorageExtraInfo
		MustStoreUInt(0, 32).
		MustStoreBoolBit(false).
		EndCell()

	var info StorageInfo
	if err := info.LoadFromCell(corrupted.MustBeginParse()); err == nil {
		t.Fatal("hand-rolled parse accepted unknown storage extra magic")
	}

	var shadow shadowStorageInfo
	if err := LoadFromCell(&shadow, corrupted.MustBeginParse()); err == nil {
		t.Fatal("reflective parse accepted unknown storage extra magic")
	}
}

func TestAccountStateFastRoundTripFixture(t *testing.T) {
	accStateBOC, _ := hex.DecodeString("b5ee9c724101030100d700026fc00c419e2b8a3b6cd81acd3967dbbaf4442e1870e99eaf32278b7814a6ccaac5f802068148c314b1854000006735d812370d00764ce8d340010200deff0020dd2082014c97ba218201339cbab19f71b0ed44d0d31fd31f31d70bffe304e0a4f2608308d71820d31fd31fd31ff82313bbf263ed44d0d31fd31fd3ffd15132baf2a15144baf2a204f901541055f910f2a3f8009320d74a96d307d402fb00e8d101a4c8cb1fcb1fcbffc9ed5400500000000229a9a317d78e2ef9e6572eeaa3f206ae5c3dd4d00ddd2ffa771196dc0ab985fa84daf451c340d7fa")
	acc, err := cell.FromBOC(accStateBOC)
	if err != nil {
		t.Fatal(err)
	}

	var state AccountState
	if err = state.LoadFromCell(acc.MustBeginParse()); err != nil {
		t.Fatal(err)
	}

	if _, ok := state.StorageInfo.StorageExtra.(StorageExtraNone); !ok {
		t.Fatalf("unexpected storage extra %#v", state.StorageInfo.StorageExtra)
	}

	roundTrip, err := state.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(roundTrip.Hash(), acc.Hash()) {
		t.Fatal("account state round-trip hash mismatch")
	}
}

func TestAccountStateRoundTripVariants(t *testing.T) {
	addr := address.NewAddress(0, 0, benchmarkBytes(0x11))
	due := FromNanoTONU(777)

	extraCurrencies := cell.NewDict(32)
	if err := extraCurrencies.SetIntKey(big.NewInt(1), cell.BeginCell().MustStoreVarUInt(1_000_000, 32).EndCell()); err != nil {
		t.Fatal(err)
	}

	depth := uint64(3)
	code := cell.BeginCell().MustStoreUInt(0xC0DE, 16).EndCell()
	data := cell.BeginCell().MustStoreUInt(0xDA7A, 16).EndCell()

	states := map[string]AccountState{
		"active with extras and due payment": {
			IsValid: true,
			Address: addr,
			StorageInfo: StorageInfo{
				StorageUsed: StorageUsed{
					CellsUsed: big.NewInt(3),
					BitsUsed:  big.NewInt(1024),
				},
				StorageExtra: StorageExtraInfo{DictHash: benchmarkBytes(0x22)},
				LastPaid:     1_700_000_100,
				DuePayment:   &due,
			},
			AccountStorage: AccountStorage{
				Status:            AccountStatusActive,
				LastTransactionLT: 12345,
				Balance:           FromNanoTONU(5_000_000_000),
				ExtraCurrencies:   extraCurrencies,
				StateInit: &StateInit{
					Depth: &depth,
					TickTock: &TickTock{
						Tick: true,
						Tock: false,
					},
					Code: code,
					Data: data,
				},
			},
		},
		"frozen": {
			IsValid: true,
			Address: addr,
			StorageInfo: StorageInfo{
				StorageUsed: StorageUsed{
					CellsUsed: big.NewInt(1),
					BitsUsed:  big.NewInt(64),
				},
				StorageExtra: StorageExtraNone{},
				LastPaid:     1_700_000_200,
			},
			AccountStorage: AccountStorage{
				Status:            AccountStatusFrozen,
				LastTransactionLT: 54321,
				Balance:           FromNanoTONU(1),
				StateHash:         benchmarkBytes(0x33),
			},
		},
		"uninit": {
			IsValid: true,
			Address: addr,
			StorageInfo: StorageInfo{
				StorageUsed: StorageUsed{
					CellsUsed: big.NewInt(0),
					BitsUsed:  big.NewInt(0),
				},
				StorageExtra: StorageExtraNone{},
			},
			AccountStorage: AccountStorage{
				Status:  AccountStatusUninit,
				Balance: FromNanoTONU(42),
			},
		},
		"non existing": {
			IsValid: false,
		},
	}

	for name, state := range states {
		t.Run(name, func(t *testing.T) {
			stateCell, err := state.ToCell()
			if err != nil {
				t.Fatal(err)
			}

			var parsed AccountState
			if err = parsed.LoadFromCell(stateCell.MustBeginParse()); err != nil {
				t.Fatal(err)
			}

			if parsed.IsValid != state.IsValid {
				t.Fatalf("IsValid mismatch: got %t, want %t", parsed.IsValid, state.IsValid)
			}
			if !state.IsValid {
				return
			}

			if parsed.Address.String() != state.Address.String() {
				t.Fatalf("address mismatch: got %s, want %s", parsed.Address, state.Address)
			}
			if parsed.Status != state.Status {
				t.Fatalf("status mismatch: got %s, want %s", parsed.Status, state.Status)
			}
			if parsed.LastTransactionLT != state.LastTransactionLT {
				t.Fatalf("last tx lt mismatch: got %d, want %d", parsed.LastTransactionLT, state.LastTransactionLT)
			}
			if parsed.Balance.Nano().Cmp(state.Balance.Nano()) != 0 {
				t.Fatalf("balance mismatch: got %s, want %s", parsed.Balance.Nano(), state.Balance.Nano())
			}
			assertStorageInfoEqual(t, parsed.StorageInfo, state.StorageInfo)

			roundTrip, err := parsed.ToCell()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(roundTrip.Hash(), stateCell.Hash()) {
				t.Fatal("account state round-trip hash mismatch")
			}
		})
	}
}

func TestTransactionDescriptionTickTockRoundTrip(t *testing.T) {
	for _, isTock := range []bool{false, true} {
		desc := TransactionDescriptionTickTock{
			IsTock: isTock,
			StoragePhase: StoragePhase{
				StorageFeesCollected: FromNanoTONU(123),
				StatusChange:         AccStatusChange{Type: AccStatusChangeUnchanged},
			},
			ComputePhase: ComputePhase{
				Phase: ComputePhaseSkipped{
					Reason: ComputeSkipReason{Type: ComputeSkipReasonNoGas},
				},
			},
			Aborted: true,
		}

		descCell, err := desc.ToCell()
		if err != nil {
			t.Fatal(err)
		}

		var parsed TransactionDescriptionTickTock
		if err = parsed.LoadFromCell(descCell.MustBeginParse()); err != nil {
			t.Fatal(err)
		}
		if parsed.IsTock != isTock {
			t.Fatalf("is_tock mismatch: got %t, want %t", parsed.IsTock, isTock)
		}

		roundTrip, err := parsed.ToCell()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(roundTrip.Hash(), descCell.Hash()) {
			t.Fatalf("tick-tock description round-trip hash mismatch (is_tock=%t)", isTock)
		}
	}
}
