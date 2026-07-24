package tlb

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestShardStateStatsRoundTrip(t *testing.T) {
	extra := cell.NewDict(32)
	if err := extra.SetIntKey(big.NewInt(7), cell.BeginCell().MustStoreBigVarUInt(big.NewInt(99), 32).EndCell()); err != nil {
		t.Fatal(err)
	}
	libraries := cell.NewDict(256)
	publishers := cell.NewDict(256)
	if err := publishers.SetIntKey(big.NewInt(2), cell.BeginCell().EndCell()); err != nil {
		t.Fatal(err)
	}
	library := cell.BeginCell().MustStoreUInt(0xaa, 8).EndCell()
	descriptor := cell.BeginCell().MustStoreUInt(0, 2).MustStoreRef(library).
		MustStoreBuilder(publishers.AsCell().ToBuilder()).EndCell()
	if err := libraries.SetIntKey(big.NewInt(1), descriptor); err != nil {
		t.Fatal(err)
	}

	stats := ShardStateStats{
		OverloadHistory:  0x1020304050607080,
		UnderloadHistory: 0x8877665544332211,
		TotalBalance: CurrencyCollection{
			Coins:           FromNanoTONU(123456789),
			ExtraCurrencies: extra,
		},
		TotalValidatorFees: CurrencyCollection{Coins: FromNanoTONU(777)},
		Libraries:          libraries,
		MasterRef: &ExtBlkRef{
			EndLt:    9001,
			SeqNo:    42,
			RootHash: shardStateStatsHash(0x11),
			FileHash: shardStateStatsHash(0x22),
		},
	}

	root, err := stats.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	var parsed ShardStateStats
	loader := root.MustBeginParse()
	if err = parsed.LoadFromCell(loader); err != nil {
		t.Fatal(err)
	}
	if loader.BitsLeft() != 0 || loader.RefsNum() != 0 {
		t.Fatalf("trailing data: %d bits, %d refs", loader.BitsLeft(), loader.RefsNum())
	}

	rebuilt, err := parsed.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if root.HashKey() != rebuilt.HashKey() {
		t.Fatalf("roundtrip hash mismatch: got %x, want %x", rebuilt.Hash(), root.Hash())
	}
	if parsed.MasterRef == nil || parsed.MasterRef.SeqNo != stats.MasterRef.SeqNo {
		t.Fatalf("master ref was not preserved: %+v", parsed.MasterRef)
	}
	if !parsed.TotalBalance.Equals(stats.TotalBalance) || !parsed.TotalValidatorFees.Equals(stats.TotalValidatorFees) {
		t.Fatal("balances were not preserved")
	}
}

func TestShardStateStatsRoundTripWithoutMasterRef(t *testing.T) {
	stats := ShardStateStats{
		TotalBalance:       CurrencyCollection{Coins: FromNanoTONU(1)},
		TotalValidatorFees: CurrencyCollection{Coins: FromNanoTONU(2)},
	}

	root, err := stats.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	var parsed ShardStateStats
	if err = parsed.LoadFromCell(root.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	if parsed.MasterRef != nil {
		t.Fatalf("unexpected master ref: %+v", parsed.MasterRef)
	}
	if parsed.Libraries != nil && !parsed.Libraries.IsEmpty() {
		t.Fatal("empty libraries dictionary was not preserved")
	}
}

func TestShardStateStatsRejectsInvalidLibrariesKeySize(t *testing.T) {
	libraries := cell.NewDict(32)
	if err := libraries.SetIntKey(big.NewInt(1), cell.BeginCell().EndCell()); err != nil {
		t.Fatal(err)
	}

	if _, err := (ShardStateStats{Libraries: libraries}).ToCell(); err == nil {
		t.Fatal("non-empty libraries dictionary with a non-256-bit key must be rejected")
	}
}

func shardStateStatsHash(fill byte) []byte {
	value := make([]byte, 32)
	for i := range value {
		value[i] = fill
	}
	return value
}
