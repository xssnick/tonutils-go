package vm

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestLazyLibraryDictionaryNodeMatchesEager(t *testing.T) {
	dict := cell.NewDict(8)
	if err := dict.SetIntKey(big.NewInt(7), cell.BeginCell().MustStoreUInt(42, 8).EndCell()); err != nil {
		t.Fatal(err)
	}
	node := dict.AsCell()
	library := mustLibraryCellForHash(t, node.Hash())
	collection := makeLibraryRoot(t, node)
	key := cell.BeginCell().MustStoreUInt(7, 8).EndCell()

	for _, version := range []int{0, 3, 4, 5, MaxSupportedGlobalVersion} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			var eagerGas int64
			for _, lazy := range []bool{false, true} {
				root := collection
				if lazy {
					root = makeLazyLibraryRoot(t, root)
					payload, err := root.MustBeginParse().LoadRefCell()
					if err != nil {
						t.Fatal(err)
					}
					if !payload.IsLazy() {
						t.Fatal("library payload must remain lazy before lookup")
					}
				}
				state := NewExecutionState(version, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
				state.InitForExecution()
				defer state.Cells.FinishExecution()
				state.SetLibraries(root)
				lookup := library.AsDict(8)
				lookup.SetTrace(state.Cells.Trace())
				value, err := lookup.LoadValue(key)
				if err != nil {
					t.Fatalf("lazy=%t: dictionary lookup: %v", lazy, err)
				}
				if got := value.MustLoadUInt(8); got != 42 {
					t.Fatalf("lazy=%t: value=%d, want 42", lazy, got)
				}
				if !lazy {
					eagerGas = state.Gas.Used()
				} else if state.Gas.Used() != eagerGas {
					t.Fatalf("lazy gas=%d, eager=%d", state.Gas.Used(), eagerGas)
				}
			}
		})
	}
}

func TestLazyLibraryDictionarySpecialPayloadMatchesEager(t *testing.T) {
	dict := cell.NewDict(8)
	if err := dict.SetIntKey(big.NewInt(7), cell.BeginCell().MustStoreUInt(42, 8).EndCell()); err != nil {
		t.Fatal(err)
	}
	node := dict.AsCell()
	for _, tc := range []struct {
		name    string
		payload *cell.Cell
	}{
		{name: "nested library", payload: mustLibraryCellForHash(t, node.Hash())},
		{name: "pruned", payload: mustPrunedCell(t)},
	} {
		for _, version := range []int{0, 3, 4, 5, MaxSupportedGlobalVersion} {
			t.Run(fmt.Sprintf("%s/v%d", tc.name, version), func(t *testing.T) {
				var eagerGas int64
				for _, lazy := range []bool{false, true} {
					collections := []*cell.Cell{makeLibraryRoot(t, tc.payload), makeLibraryRoot(t, node)}
					if lazy {
						for i := range collections {
							var err error
							collections[i], err = cell.FromBOCWithOptions(collections[i].ToBOCWithOptions(cell.BOCSerializeOptions{
								WithTopHash: true, WithIntHashes: true,
							}), cell.BOCParseOptions{Lazy: true, TrustedHashes: true, AllowNonZeroLevelRoot: true})
							if err != nil {
								t.Fatal(err)
							}
						}
					}
					state := NewExecutionState(version, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
					state.InitForExecution()
					defer state.Cells.FinishExecution()
					state.SetLibraries(collections...)
					lookup := mustLibraryCellForHash(t, tc.payload.Hash()).AsDict(8)
					lookup.SetTrace(state.Cells.Trace())
					value, err := lookup.LoadValue(cell.BeginCell().MustStoreUInt(7, 8).EndCell())
					if tc.name == "pruned" || version >= 5 {
						if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeCellUnderflow {
							t.Fatalf("lazy=%t: error=%v, want cell underflow", lazy, err)
						}
					} else if err != nil {
						t.Fatalf("lazy=%t: nested library lookup: %v", lazy, err)
					} else if got := value.MustLoadUInt(8); got != 42 {
						t.Fatalf("lazy=%t: value=%d, want 42", lazy, got)
					}
					if !lazy {
						eagerGas = state.Gas.Used()
					} else if state.Gas.Used() != eagerGas {
						t.Fatalf("lazy gas=%d, eager=%d", state.Gas.Used(), eagerGas)
					}
				}
			})
		}
	}
}
