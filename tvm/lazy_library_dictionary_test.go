package tvm

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func lazyLibraryDictionaryFixture(t *testing.T) (library, collection *cell.Cell) {
	t.Helper()

	dict := cell.NewDict(8)
	if err := dict.SetIntKey(big.NewInt(7), cell.BeginCell().MustStoreUInt(42, 8).EndCell()); err != nil {
		t.Fatal(err)
	}
	node := dict.AsCell()
	library = mustLibraryCellForHash(t, node.Hash())
	libs := cell.NewDict(256)
	key := cell.BeginCell().MustStoreSlice(node.Hash(), 256).EndCell()
	if err := libs.Set(key, cell.BeginCell().MustStoreRef(node).EndCell()); err != nil {
		t.Fatal(err)
	}
	return library, libs.AsCell()
}

func lazyLibraryDictionaryCollection(t *testing.T, root *cell.Cell) *cell.Cell {
	t.Helper()

	boc := root.ToBOCWithOptions(cell.BOCSerializeOptions{WithTopHash: true, WithIntHashes: true})
	roots, _, err := cell.FromBOCMultiRootReader(cell.NewBOCNoCopyReader(boc), cell.BOCParseOptions{
		Lazy:          true,
		TrustedHashes: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("root count = %d, want 1", len(roots))
	}
	payload, err := roots[0].MustBeginParse().LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	if !payload.IsLazy() {
		t.Fatal("library dictionary payload must remain lazy before execution")
	}
	return roots[0]
}

func lazyLibraryDictionaryCode(signed bool, lookups int) *cell.Cell {
	code := cell.BeginCell().MustStoreUInt(0x30, 8) // DROP get-method id
	op := uint64(0xF40E)                            // DICTUGET
	if signed {
		op = 0xF40C // DICTIGET
	}
	for i := 0; i < lookups; i++ {
		if i > 0 {
			code.MustStoreUInt(0x3030, 16) // DROP flag; DROP previous value
		}
		code.MustStoreUInt(op, 16)
	}
	return code.EndCell()
}

func lazyLibraryDictionaryStack(t *testing.T, library *cell.Cell, lookups int) *vm.Stack {
	t.Helper()

	stack := vm.NewStack()
	for i := 0; i < lookups; i++ {
		for _, value := range []any{big.NewInt(7), library, big.NewInt(8)} {
			if err := stack.PushHostValue(value); err != nil {
				t.Fatal(err)
			}
		}
	}
	return stack
}

// A library target is materialized before its cell kind is checked. Lazy BOC
// placeholders must not change dictionary lookup results or load accounting.
func TestLazyLibraryDictionaryExecuteMatchesEager(t *testing.T) {
	library, collection := lazyLibraryDictionaryFixture(t)
	for _, version := range []int{0, 3, 4, 5, 16} {
		for _, signed := range []bool{false, true} {
			for _, lookups := range []int{1, 2} {
				t.Run(fmt.Sprintf("v%d/signed=%t/lookups=%d", version, signed, lookups), func(t *testing.T) {
					var eager *ExecutionResult
					for _, lazy := range []bool{false, true} {
						root := collection
						if lazy {
							root = lazyLibraryDictionaryCollection(t, root)
						}
						stack := lazyLibraryDictionaryStack(t, library, lookups)
						if err := stack.PushSmallInt(0); err != nil {
							t.Fatal(err)
						}
						cfg := testExecutionConfigWithVersion(t, uint32(version))
						cfg.Libraries = []*cell.Cell{root}
						res, err := NewTVM().Execute(lazyLibraryDictionaryCode(signed, lookups), cell.BeginCell().EndCell(),
							tuple.Tuple{}, vm.GasWithLimit(100_000), stack, cfg)
						if err != nil {
							t.Fatal(err)
						}
						if res.ExitCode != 0 || res.Stack.Len() != 2 || res.MissingLibrary != nil {
							t.Fatalf("lazy=%t: exit=%d stack size=%d missing=%v", lazy, res.ExitCode, res.Stack.Len(), res.MissingLibrary)
						}
						found, err := res.Stack.PopBool()
						if err != nil || !found {
							t.Fatalf("lazy=%t: found=%t err=%v", lazy, found, err)
						}
						value, err := res.Stack.PopSlice()
						if err != nil {
							t.Fatal(err)
						}
						if value.BitsLeft() != 8 || value.RefsNum() != 0 || value.MustLoadUInt(8) != 42 {
							t.Fatalf("lazy=%t: unexpected dictionary value %s", lazy, value.String())
						}
						if res.Steps != uint64(lookups*3) {
							t.Fatalf("lazy=%t: steps=%d, want %d", lazy, res.Steps, lookups*3)
						}
						if eager == nil {
							eager = res
						} else if res.GasUsed != eager.GasUsed || res.Steps != eager.Steps {
							t.Fatalf("lazy gas/steps=%d/%d, eager=%d/%d", res.GasUsed, res.Steps, eager.GasUsed, eager.Steps)
						}
					}
				})
			}
		}
	}
}
