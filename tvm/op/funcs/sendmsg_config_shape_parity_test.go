package funcs

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestSendMsgNonSliceConfigSlotsParity(t *testing.T) {
	t.Run("size limits use defaults", func(t *testing.T) {
		for _, value := range []any{nil, int64(42), cell.BeginCell().EndCell(), (*cell.Slice)(nil)} {
			cfg := tuple.NewTupleSized(7)
			if err := cfg.Set(6, value); err != nil {
				t.Fatalf("set size-limits slot: %v", err)
			}
			state := newFuncTestState(t, map[int]any{paramIdxUnpackedConfig: cfg})

			got, err := getSizeLimitsMaxMsgCells(state)
			if err != nil || got != 1<<13 {
				t.Fatalf("getSizeLimitsMaxMsgCells(%T) = (%d, %v), want (%d, nil)", value, got, err, 1<<13)
			}
		}
	})

	t.Run("prices report unknown", func(t *testing.T) {
		for _, tc := range []struct {
			name          string
			idx           int
			isMasterchain bool
		}{
			{name: "basechain", idx: 5},
			{name: "masterchain", idx: 4, isMasterchain: true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				cfg := tuple.NewTupleSized(7)
				if err := cfg.Set(tc.idx, int64(42)); err != nil {
					t.Fatalf("set prices slot: %v", err)
				}
				state := newFuncTestState(t, map[int]any{paramIdxUnpackedConfig: cfg})

				_, err := getSendMsgPrices(state, tc.isMasterchain)
				if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeUnknown {
					t.Fatalf("getSendMsgPrices error = %v, want unknown(%d)", err, vmerr.CodeUnknown)
				}
			})
		}
	})

	t.Run("malformed price slice reports cell underflow", func(t *testing.T) {
		cfg := tuple.NewTupleSized(7)
		if err := cfg.Set(5, cell.BeginCell().MustStoreUInt(0, 8).ToSlice()); err != nil {
			t.Fatalf("set prices slot: %v", err)
		}
		state := newFuncTestState(t, map[int]any{paramIdxUnpackedConfig: cfg})

		_, err := getSendMsgPrices(state, false)
		if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeCellUnderflow {
			t.Fatalf("getSendMsgPrices error = %v, want cell underflow(%d)", err, vmerr.CodeCellUnderflow)
		}
	})

	t.Run("legacy non-cell config root reports unknown", func(t *testing.T) {
		state := newFuncTestState(t, map[int]any{9: int64(42)})
		state.GlobalVersion = 5

		_, err := getSendMsgPrices(state, false)
		if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeUnknown {
			t.Fatalf("getSendMsgPrices error = %v, want unknown(%d)", err, vmerr.CodeUnknown)
		}
	})
}
