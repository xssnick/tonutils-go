package tvm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func mustVirtualizedPrunedCellForTVM(t *testing.T) (*cell.Cell, *cell.Cell) {
	t.Helper()

	raw := mustPrunedCellForXLoad(t)
	return raw, raw.Virtualize(0)
}

func mustVirtualizedProofBodyForTVM(t *testing.T) (*cell.Cell, *cell.Cell, *cell.Cell, *cell.Cell) {
	t.Helper()

	branch := cell.BeginCell().
		MustStoreUInt(0xBEEF, 16).
		MustStoreRef(cell.BeginCell().MustStoreUInt(1, 1).EndCell()).
		EndCell()
	root := cell.BeginCell().
		MustStoreUInt(0, 1).
		MustStoreRef(branch).
		EndCell()

	proof, err := root.CreateProof(cell.CreateProofSkeleton())
	if err != nil {
		t.Fatalf("create proof: %v", err)
	}

	rawBody, err := cell.UnwrapProof(proof, root.Hash())
	if err != nil {
		t.Fatalf("unwrap raw proof: %v", err)
	}
	virtBody, err := cell.UnwrapProofVirtualized(proof, root.Hash())
	if err != nil {
		t.Fatalf("unwrap virtualized proof: %v", err)
	}

	rawPruned, err := rawBody.PeekRef(0)
	if err != nil {
		t.Fatalf("peek raw child: %v", err)
	}
	virtPruned, err := virtBody.PeekRef(0)
	if err != nil {
		t.Fatalf("peek virtualized child: %v", err)
	}
	return rawBody, virtBody, rawPruned, virtPruned
}

func TestVirtualizationErrorsBypassC2(t *testing.T) {
	code := codeFromBuilders(t, cellsliceop.CTOS().Serialize())
	tvm := NewTVM()

	run := func(cl *cell.Cell) (int64, error) {
		state := vmcore.NewExecutionState(vmcore.DefaultGlobalVersion, vmcore.GasWithLimit(100_000), cell.BeginCell().EndCell(), tuple.Tuple{}, vmcore.NewStack())
		state.Reg.C[2] = &vmcore.QuitContinuation{ExitCode: 0}
		if err := state.Stack.PushCell(cl); err != nil {
			t.Fatalf("push cell: %v", err)
		}
		state.PrepareExecution(code.BeginParse())
		return tvm.runState(state)
	}

	rawPruned, virtualizedPruned := mustVirtualizedPrunedCellForTVM(t)

	exitCode, err := run(rawPruned)
	if err != nil {
		t.Fatalf("expected raw pruned error to be handled via c2, got exit=%d err=%v", exitCode, err)
	}
	if exitCode != 0 {
		t.Fatalf("expected c2 to catch raw pruned underflow with exit 0, got %d", exitCode)
	}

	exitCode, err = run(virtualizedPruned)
	if err == nil {
		t.Fatal("expected virtualization error to bypass c2")
	}
	if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeVirtualization {
		t.Fatalf("unexpected virtualization error code: %v (ok=%v), err=%v", code, ok, err)
	}
	if exitCode != vmerr.CodeVirtualization {
		t.Fatalf("unexpected virtualization exit code: %d", exitCode)
	}
}

func TestVirtualizedGetterOpcodesClampToEffectiveLevel(t *testing.T) {
	rawBody, virtBody, rawPruned, virtPruned := mustVirtualizedProofBodyForTVM(t)

	tests := []struct {
		name string
		raw  *cell.Cell
		virt *cell.Cell
	}{
		{name: "body", raw: rawBody, virt: virtBody},
		{name: "pruned", raw: rawPruned, virt: virtPruned},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			levelStack, _, err := runRawCode(codeFromBuilders(t, cellsliceop.CLEVEL().Serialize()), tt.virt)
			if err != nil {
				t.Fatalf("CLEVEL failed: %v", err)
			}
			level, err := levelStack.PopIntFinite()
			if err != nil {
				t.Fatalf("pop CLEVEL: %v", err)
			}
			if got, want := level.Int64(), int64(tt.virt.Level()); got != want {
				t.Fatalf("unexpected CLEVEL: got=%d want=%d", got, want)
			}

			maskStack, _, err := runRawCode(codeFromBuilders(t, cellsliceop.CLEVELMASK().Serialize()), tt.virt)
			if err != nil {
				t.Fatalf("CLEVELMASK failed: %v", err)
			}
			mask, err := maskStack.PopIntFinite()
			if err != nil {
				t.Fatalf("pop CLEVELMASK: %v", err)
			}
			if got, want := mask.Int64(), int64(tt.virt.LevelMask().Mask); got != want {
				t.Fatalf("unexpected CLEVELMASK: got=%d want=%d", got, want)
			}

			depthStack, _, err := runRawCode(codeFromBuilders(t, cellsliceop.CDEPTH().Serialize()), tt.virt)
			if err != nil {
				t.Fatalf("CDEPTH failed: %v", err)
			}
			depth, err := depthStack.PopIntFinite()
			if err != nil {
				t.Fatalf("pop CDEPTH: %v", err)
			}
			if got, want := depth.Int64(), int64(tt.virt.Depth()); got != want {
				t.Fatalf("unexpected CDEPTH: got=%d want=%d", got, want)
			}

			for _, level := range []int{0, 1, 3} {
				wantLevel := min(level, tt.virt.EffectiveLevel())

				hashStack, _, err := runRawCode(codeFromBuilders(t, cellsliceop.CHASHI(level).Serialize()), tt.virt)
				if err != nil {
					t.Fatalf("CHASHI %d failed: %v", level, err)
				}
				hashInt, err := hashStack.PopIntFinite()
				if err != nil {
					t.Fatalf("pop CHASHI %d: %v", level, err)
				}
				wantHash := new(big.Int).SetBytes(tt.raw.Hash(wantLevel))
				if hashInt.Cmp(wantHash) != 0 {
					t.Fatalf("unexpected CHASHI %d result (clamped to %d)", level, wantLevel)
				}

				depthStack, _, err := runRawCode(codeFromBuilders(t, cellsliceop.CDEPTHI(level).Serialize()), tt.virt)
				if err != nil {
					t.Fatalf("CDEPTHI %d failed: %v", level, err)
				}
				depthInt, err := depthStack.PopIntFinite()
				if err != nil {
					t.Fatalf("pop CDEPTHI %d: %v", level, err)
				}
				if got, want := depthInt.Int64(), int64(tt.raw.Depth(wantLevel)); got != want {
					t.Fatalf("unexpected CDEPTHI %d (clamped to %d): got=%d want=%d", level, wantLevel, got, want)
				}
			}
		})
	}
}

func TestVirtualizedSliceDepthOpcodeMatchesVisibleRefs(t *testing.T) {
	_, virtBody, _, virtPruned := mustVirtualizedProofBodyForTVM(t)

	stack, _, err := runRawCode(codeFromBuilders(t, cellsliceop.SDEPTH().Serialize()), virtBody.BeginParse())
	if err != nil {
		t.Fatalf("SDEPTH failed: %v", err)
	}
	depth, err := stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop SDEPTH: %v", err)
	}
	if got, want := depth.Int64(), int64(virtPruned.Depth()+1); got != want {
		t.Fatalf("unexpected SDEPTH: got=%d want=%d", got, want)
	}
}
