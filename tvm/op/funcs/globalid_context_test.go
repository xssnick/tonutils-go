package funcs

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

// TON 9f93888cf402f8421fef38406b67886be043ac58, tonops.cpp:214:
// GLOBALID reads c7[0][9], then looks up key 19 inside that config dictionary.
func TestGlobalIDVersionFourFiveConfigRoot(t *testing.T) {
	for _, version := range []int{4, 5} {
		for _, value := range []int32{-2147483648, -1, 0, 1, 2147483647} {
			for _, conflictingSlot := range []bool{false, true} {
				t.Run(fmt.Sprintf("v%d/id%d/slot19_%t", version, value, conflictingSlot), func(t *testing.T) {
					params := map[int]any{9: mustConfigRoot(t, 19, cell.BeginCell().MustStoreInt(int64(value), 32).EndCell())}
					if conflictingSlot {
						params[19] = big.NewInt(123) // This unrelated context slot must not be read.
					}
					state := newFuncTestState(t, params)
					state.GlobalVersion = version
					if err := GLOBALID().Interpret(state); err != nil {
						t.Fatal(err)
					}
					got, err := state.Stack.PopIntFinite()
					if err != nil || got.Int64() != int64(value) {
						t.Fatalf("GLOBALID=%v, err=%v, want %d", got, err, value)
					}
				})
			}
		}
	}
}

func TestGlobalIDVersionFourFiveInvalidConfig(t *testing.T) {
	for _, version := range []int{4, 5} {
		for name, root := range map[string]*cell.Cell{
			"missing": mustConfigRoot(t, 18, cell.BeginCell().EndCell()),
			"empty":   mustConfigRoot(t, 19, cell.BeginCell().EndCell()),
			"short":   mustConfigRoot(t, 19, cell.BeginCell().MustStoreUInt(0, 31).EndCell()),
		} {
			t.Run(fmt.Sprintf("v%d/%s", version, name), func(t *testing.T) {
				state := newFuncTestState(t, map[int]any{9: root})
				state.GlobalVersion = version
				err := GLOBALID().Interpret(state)
				if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeUnknown {
					t.Fatalf("error=%v (code %d), want unknown", err, code)
				}
				if state.Stack.Len() != 0 {
					t.Fatal("failed GLOBALID changed the stack")
				}
			})
		}
	}
}

func TestGlobalIDVersionSixUnpackedConfig(t *testing.T) {
	for _, version := range []int{6, 16} {
		for _, tc := range []struct {
			name     string
			value    any
			wantCode int64
		}{
			{name: "signed", value: cell.BeginCell().MustStoreInt(-239, 32).ToSlice()},
			{name: "missing", wantCode: vmerr.CodeTypeCheck},
			{name: "short", value: cell.BeginCell().MustStoreUInt(0, 31).ToSlice(), wantCode: vmerr.CodeCellUnderflow},
		} {
			t.Run(fmt.Sprintf("v%d/%s", version, tc.name), func(t *testing.T) {
				state := newFuncTestState(t, map[int]any{
					9: big.NewInt(9), 19: big.NewInt(19),
					14: tuple.NewTupleValue(nil, tc.value),
				})
				state.GlobalVersion = version
				err := GLOBALID().Interpret(state)
				if tc.wantCode != 0 {
					if code, ok := vmerr.ErrorCode(err); !ok || code != tc.wantCode {
						t.Fatalf("error=%v (code %d), want %d", err, code, tc.wantCode)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				got, err := state.Stack.PopIntFinite()
				if err != nil || got.Int64() != -239 {
					t.Fatalf("GLOBALID=%v, err=%v, want -239", got, err)
				}
				if tc.value.(*cell.Slice).BitsLeft() != 32 {
					t.Fatal("GLOBALID consumed the unpacked config slice")
				}
			})
		}
	}
}
