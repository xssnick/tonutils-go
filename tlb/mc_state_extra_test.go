package tlb

import (
	"bytes"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestMcStateExtraBlockInfoLoadAndStore(t *testing.T) {
	stats := cell.BeginCell().
		MustStoreUInt(0x17, 8).
		MustStoreUInt(0, 1).
		EndCell()

	tests := []struct {
		name          string
		flags         uint16
		afterKeyBlock bool
		stats         *cell.Cell
	}{
		{
			name:          "without block create stats",
			flags:         0,
			afterKeyBlock: true,
		},
		{
			name:          "with block create stats",
			flags:         1,
			afterKeyBlock: false,
			stats:         stats,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := makeMcStateExtraBlockInfoCell(tt.flags, tt.afterKeyBlock, tt.stats)

			var info McStateExtraBlockInfo
			err := LoadFromCell(&info, src.MustBeginParse())
			if err != nil {
				t.Fatal(err)
			}

			if info.Flags != tt.flags {
				t.Fatalf("unexpected flags: got %d want %d", info.Flags, tt.flags)
			}
			if info.ValidatorInfo.ValidatorListHashShort != 0x11223344 {
				t.Fatalf("unexpected validator list hash: got %x", info.ValidatorInfo.ValidatorListHashShort)
			}
			if info.ValidatorInfo.CatchainSeqno != 0x55667788 {
				t.Fatalf("unexpected catchain seqno: got %x", info.ValidatorInfo.CatchainSeqno)
			}
			if !info.ValidatorInfo.NextCCUpdated {
				t.Fatal("expected next catchain update flag")
			}
			if info.PrevBlocks == nil || info.PrevBlocks.AugmentedDictionary == nil {
				t.Fatal("expected prev blocks dictionary")
			}
			if info.AfterKeyBlock != tt.afterKeyBlock {
				t.Fatalf("unexpected after key block: got %t want %t", info.AfterKeyBlock, tt.afterKeyBlock)
			}
			if info.LastKeyBlock != nil {
				t.Fatal("expected empty last key block")
			}

			if tt.stats == nil {
				if info.BlockCreateStats != nil {
					t.Fatal("expected empty block create stats")
				}
			} else if info.BlockCreateStats == nil {
				t.Fatal("expected block create stats")
			} else if !bytes.Equal(info.BlockCreateStats.Hash(), tt.stats.Hash()) {
				t.Fatal("block create stats mismatch")
			}

			roundTrip, err := ToCell(info)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(roundTrip.Hash(), src.Hash()) {
				t.Fatal("roundtrip cell mismatch")
			}
		})
	}
}

func TestMcStateExtraBlockInfoRejectsUnsupportedFlags(t *testing.T) {
	src := makeMcStateExtraBlockInfoCell(2, false, nil)

	var info McStateExtraBlockInfo
	err := LoadFromCell(&info, src.MustBeginParse())
	if err == nil {
		t.Fatal("expected unsupported flags error")
	}
}

func makeMcStateExtraBlockInfoCell(flags uint16, afterKeyBlock bool, stats *cell.Cell) *cell.Cell {
	b := cell.BeginCell().
		MustStoreUInt(uint64(flags), 16).
		MustStoreUInt(0x11223344, 32).
		MustStoreUInt(0x55667788, 32).
		MustStoreBoolBit(true).
		MustStoreBuilder(emptyOldMcBlocksInfoCell().ToBuilder()).
		MustStoreBoolBit(afterKeyBlock).
		MustStoreBoolBit(false)

	if flags&1 == 1 {
		b.MustStoreBuilder(stats.ToBuilder())
	}

	return b.EndCell()
}

func emptyOldMcBlocksInfoCell() *cell.Cell {
	return cell.BeginCell().
		MustStoreUInt(0, 1).
		MustStoreBoolBit(false).
		MustStoreUInt(0, 64).
		EndCell()
}
