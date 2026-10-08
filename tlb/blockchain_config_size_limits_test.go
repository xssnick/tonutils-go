package tlb

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestSizeLimitsConfigV3HistoricalQueueLimits(t *testing.T) {
	maxLibraryLoadsValue := uint32(17)
	for _, maxLibraryLoads := range []*uint32{nil, &maxLibraryLoadsValue} {
		t.Run(fmt.Sprintf("library_load_limit_%v", maxLibraryLoads != nil), func(t *testing.T) {
			current := SizeLimitsConfigV3{
				MaxMsgBits:                 1000,
				MaxMsgCells:                101,
				MaxTransactionLibraryLoads: maxLibraryLoads,
				MaxTotalMsgBits:            2000,
				MaxTotalMsgCells:           202,
				OutMsgQueueSizeHardLimit:   18001,
				OutMsgQueueSizeSoftLimit:   12001,
			}
			currentCell, err := ToCell(&current)
			if err != nil {
				t.Fatal(err)
			}
			prefixBits := currentCell.BitsSize() - 64
			historicalCell := cell.BeginCell().MustStoreSlice(currentCell.MustBeginParse().MustLoadSlice(prefixBits), prefixBits).EndCell()
			cfg := BlockchainConfig{Root: mustBlockchainConfigRoot(t, map[uint32]*cell.Cell{
				ConfigParamSizeLimits: historicalCell,
			})}
			limits, err := cfg.GetSizeLimitsConfig()
			if err != nil {
				t.Fatal(err)
			}
			historical := limits.Config.(SizeLimitsConfigV3)
			want := current
			want.OutMsgQueueSizeHardLimit = 18000
			want.OutMsgQueueSizeSoftLimit = 12000
			if !reflect.DeepEqual(historical, want) {
				t.Fatalf("historical size limits = %+v, want %+v", historical, want)
			}

			serialized, err := ToCell(&historical)
			if err != nil {
				t.Fatal(err)
			}
			wantCell := historicalCell.ToBuilder().MustStoreUInt(18000, 32).MustStoreUInt(12000, 32).EndCell()
			if serialized.HashKey() != wantCell.HashKey() {
				t.Fatal("historical size limits were not serialized in the current format")
			}
		})
	}
}

func TestSizeLimitsConfigV3QueueLimitsTail(t *testing.T) {
	currentCell, err := ToCell(&SizeLimitsConfigV3{OutMsgQueueSizeSoftLimit: ^uint32(0)})
	if err != nil {
		t.Fatal(err)
	}
	var current SizeLimitsConfigV3
	loader := currentCell.MustBeginParse()
	if err = LoadFromCell(&current, loader); err != nil {
		t.Fatal(err)
	}
	if current.OutMsgQueueSizeHardLimit != 0 || current.OutMsgQueueSizeSoftLimit != ^uint32(0) || loader.BitsLeft() != 0 {
		t.Fatalf("current queue limits or consumed length changed: %+v; remaining=%d", current, loader.BitsLeft())
	}

	prefixBits := currentCell.BitsSize() - 64
	prefix := cell.BeginCell().MustStoreSlice(currentCell.MustBeginParse().MustLoadSlice(prefixBits), prefixBits).EndCell()
	for _, bits := range []uint{1, 31, 32, 33, 63, 65, 96} {
		t.Run(fmt.Sprintf("tail_bits_%d", bits), func(t *testing.T) {
			candidate := prefix.ToBuilder().MustStoreSlice(make([]byte, 12), bits).EndCell()
			var limits SizeLimitsConfig
			if err := Parse(&limits, candidate); err == nil {
				t.Fatalf("accepted malformed queue limits tail with %d bits", bits)
			}
		})
	}
	for _, record := range []*cell.Cell{prefix, currentCell} {
		candidate := record.ToBuilder().MustStoreRef(cell.BeginCell().EndCell()).EndCell()
		var limits SizeLimitsConfig
		if err := Parse(&limits, candidate); err == nil {
			t.Fatal("accepted a size limits record with an unexpected reference")
		}
	}
}

func TestSizeLimitsConfigV3SkipMagic(t *testing.T) {
	maxLibraryLoads := uint32(17)
	want := SizeLimitsConfigV3{
		MaxMsgBits:                 1000,
		MaxMsgCells:                101,
		MaxTransactionLibraryLoads: &maxLibraryLoads,
		MaxTotalMsgBits:            2000,
		MaxTotalMsgCells:           202,
		OutMsgQueueSizeHardLimit:   18001,
		OutMsgQueueSizeSoftLimit:   12001,
	}
	current, err := ToCell(&want)
	if err != nil {
		t.Fatal(err)
	}
	prefixBits := current.BitsSize() - 64
	historical := cell.BeginCell().MustStoreSlice(current.MustBeginParse().MustLoadSlice(prefixBits), prefixBits).EndCell()

	for _, oldFormat := range []bool{false, true} {
		for _, proof := range []bool{false, true} {
			for _, skipMagic := range []bool{false, true} {
				t.Run(fmt.Sprintf("historical_%t/proof_%t/skip_magic_%t", oldFormat, proof, skipMagic), func(t *testing.T) {
					record := current
					expected := want
					if oldFormat {
						record = historical
						expected.OutMsgQueueSizeHardLimit = 18000
						expected.OutMsgQueueSizeSoftLimit = 12000
					}
					loader := record.MustBeginParse()
					if skipMagic {
						if tag := loader.MustLoadUInt(8); tag != 0x03 {
							t.Fatalf("record tag = %x, want 03", tag)
						}
					}

					var got SizeLimitsConfigV3
					var err error
					if proof {
						err = LoadFromCellAsProof(&got, loader, skipMagic)
					} else {
						err = LoadFromCell(&got, loader, skipMagic)
					}
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, expected) || loader.BitsLeft() != 0 || loader.RefsNum() != 0 {
						t.Fatalf("decoded = %+v, want %+v; remaining bits=%d refs=%d", got, expected, loader.BitsLeft(), loader.RefsNum())
					}
				})
			}
		}
	}
}
