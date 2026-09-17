package tvm

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
)

func TestHistoricalStorageFeeCPPVectors(t *testing.T) {
	data, err := os.ReadFile("testdata/historical-storage/cpp-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Cells   uint64 `json:"cells"`
		Bits    uint64 `json:"bits"`
		Windows []struct {
			BitPrice  uint64 `json:"bit_price"`
			CellPrice uint64 `json:"cell_price"`
			Delta     uint32 `json:"delta"`
		} `json:"windows"`
		NormalizedFee string `json:"normalized_fee"`
		CollectedFee  string `json:"collected_fee"`
	}
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 214 {
		t.Fatalf("incomplete C++ reference corpus: %d vectors", len(vectors))
	}

	for index, vector := range vectors {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			for _, masterchain := range []bool{false, true} {
				cfg := &PreparedBlockchainConfig{}
				now := uint32(1)
				for _, window := range vector.Windows {
					price := tlb.ConfigStoragePrices{ValidSince: now}
					if masterchain {
						price.MCBitPrice, price.MCCellPrice = window.BitPrice, window.CellPrice
					} else {
						price.BitPrice, price.CellPrice = window.BitPrice, window.CellPrice
					}
					cfg.storagePrices = append(cfg.storagePrices, preparedStoragePrice{price: price})
					now += window.Delta
				}

				for _, historical := range []bool{false, true} {
					want := vector.NormalizedFee
					if historical {
						want = vector.CollectedFee
					}
					got, err := cfg.computeStorageFee(masterchain, 1, now, vector.Bits, vector.Cells, historical)
					if err != nil || got.String() != want {
						t.Fatalf("masterchain=%t historical=%t: fee=%v, err=%v; want %s", masterchain, historical, got, err, want)
					}
				}
			}
		})
	}
}

func TestHistoricalStorageFeeWindowShape(t *testing.T) {
	// Equal prices in separate windows retain a single overflowing base-2^52
	// limb. Multiplying the combined duration creates a second limb instead.
	for _, split := range []bool{false, true} {
		cfg := &PreparedBlockchainConfig{storagePrices: []preparedStoragePrice{
			{price: tlb.ConfigStoragePrices{ValidSince: 1, CellPrice: 1}},
		}}
		if split {
			cfg.storagePrices = append(cfg.storagePrices, preparedStoragePrice{
				price: tlb.ConfigStoragePrices{ValidSince: 2, CellPrice: 1},
			})
		}
		for _, historical := range []bool{false, true} {
			want := int64(1 << 36)
			if historical && !split {
				want = 0
			}
			got, err := cfg.computeStorageFee(false, 1, 3, 0, 1<<51, historical)
			if err != nil || got.Cmp(big.NewInt(want)) != 0 {
				t.Fatalf("split=%t historical=%t: fee=%v, err=%v; want %d", split, historical, got, err, want)
			}
		}
	}
}

func TestHistoricalStorageFeeSignedBounds(t *testing.T) {
	for _, test := range []struct {
		name                             string
		bits, cells, bitPrice, cellPrice uint64
	}{
		{name: "unsigned bits", bits: math.MaxInt64 + 1, bitPrice: 1},
		{name: "unsigned cells", cells: math.MaxInt64 + 1, cellPrice: 1},
		{name: "unsigned bit price", bits: 1, bitPrice: math.MaxInt64 + 1},
		{name: "unsigned cell price", cells: 1, cellPrice: math.MaxInt64 + 1},
		{name: "signed product carry", cells: math.MaxInt64, cellPrice: math.MaxInt64},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, masterchain := range []bool{false, true} {
				price := tlb.ConfigStoragePrices{ValidSince: 1}
				if masterchain {
					price.MCBitPrice, price.MCCellPrice = test.bitPrice, test.cellPrice
				} else {
					price.BitPrice, price.CellPrice = test.bitPrice, test.cellPrice
				}
				cfg := &PreparedBlockchainConfig{storagePrices: []preparedStoragePrice{{price: price}}}
				if _, err := cfg.computeStorageFee(masterchain, 1, 2, test.bits, test.cells, true); !errors.Is(err, errHistoricalStorageOverflow) {
					t.Fatalf("masterchain=%t: accepted unsupported signed arithmetic: %v", masterchain, err)
				}
			}
		})
	}

	// Addition across windows can fill one unnormalized signed limb completely.
	// Rounded rshift(16, 1) must subtract before shifting, then add one.
	cfg := &PreparedBlockchainConfig{}
	for i := range 2048 {
		cfg.storagePrices = append(cfg.storagePrices, preparedStoragePrice{
			price: tlb.ConfigStoragePrices{ValidSince: uint32(i + 1), CellPrice: 1<<52 - 1},
		})
	}
	cfg.storagePrices = append(cfg.storagePrices, preparedStoragePrice{
		price: tlb.ConfigStoragePrices{ValidSince: 2049, CellPrice: 2047},
	})
	for _, historical := range []bool{false, true} {
		got, err := cfg.computeStorageFee(false, 1, 2050, 0, 1, historical)
		if err != nil || got.Cmp(big.NewInt(1<<47)) != 0 {
			t.Fatalf("historical=%t: rounding MaxInt64: fee=%v, err=%v", historical, got, err)
		}
	}

	cfg.storagePrices = append(cfg.storagePrices, preparedStoragePrice{
		price: tlb.ConfigStoragePrices{ValidSince: 2050, CellPrice: 1},
	})
	if _, err := cfg.computeStorageFee(false, 1, 2051, 0, 1, true); !errors.Is(err, errHistoricalStorageOverflow) {
		t.Fatalf("accepted signed addition overflow across windows: %v", err)
	}
	got, err := cfg.computeStorageFee(false, 1, 2051, 0, 1, false)
	if err != nil || got.Cmp(big.NewInt(1<<47)) != 0 {
		t.Fatalf("modern arithmetic at historical overflow: fee=%v, err=%v", got, err)
	}
}

func TestHistoricalStorageFeeSkipsInactivePrices(t *testing.T) {
	cfg := &PreparedBlockchainConfig{storagePrices: []preparedStoragePrice{
		{price: tlb.ConfigStoragePrices{ValidSince: 1, CellPrice: math.MaxUint64}},
		{price: tlb.ConfigStoragePrices{ValidSince: 5, CellPrice: 1}},
		{price: tlb.ConfigStoragePrices{ValidSince: 10, CellPrice: math.MaxUint64}},
	}}
	got, err := cfg.computeStorageFee(false, 5, 10, 0, 1, true)
	if err != nil || got.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("inactive prices affected fee: fee=%v, err=%v", got, err)
	}
	for _, interval := range [][2]uint32{{0, 20}, {5, 5}, {6, 5}, {1, 1}} {
		got, err := cfg.computeStorageFee(false, interval[0], interval[1], math.MaxUint64, math.MaxUint64, true)
		if err != nil || got.Sign() != 0 {
			t.Fatalf("empty interval %v affected fee: fee=%v, err=%v", interval, got, err)
		}
	}
}
