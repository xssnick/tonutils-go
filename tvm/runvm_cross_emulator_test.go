//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorRunVM(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	type testCase struct {
		name    string
		code    *cell.Cell
		stack   []any
		c7      tuple.Tuple
		goLibs  []*cell.Cell
		refLibs *cell.Cell
	}

	childC7 := tuple.NewTupleValue(tuple.NewTupleValue(int64(11), int64(22)))
	returnedData := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	returnedActions := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	childLibTarget := cell.BeginCell().MustStoreUInt(0xA11CE, 20).EndCell()
	childLibCell := mustCrossLibraryCellForHash(t, childLibTarget.Hash())
	childLibraries := mustCrossLibraryCollection(t, childLibTarget)

	tests := []testCase{
		{
			name: "runvm_basic",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(0).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize()).MustBeginParse(),
			},
		},
		{
			name: "runvmx_basic",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t, stackop.PUSHINT(big.NewInt(9)).Serialize()).MustBeginParse(),
				int64(0),
			},
		},
		{
			name: "runvm_same_c3_push_zero",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(3).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t, stackop.DROP().Serialize()).MustBeginParse(),
			},
		},
		{
			name: "runvm_push_zero_without_same_c3",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(2).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t, stackop.DROP().Serialize()).MustBeginParse(),
			},
		},
		{
			name: "runvm_same_c3_calldict",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(3).Serialize())),
			stack: []any{
				int64(0),
				makeRunvmSameC3CallDictChild(t, 5, 9).MustBeginParse(),
			},
		},
		{
			name: "runvm_same_c3_jmpdict",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(3).Serialize())),
			stack: []any{
				int64(0),
				makeRunvmSameC3JmpDictChild(t, 8).MustBeginParse(),
			},
		},
		{
			name: "runvm_same_c3_preparedict",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(3).Serialize())),
			stack: []any{
				int64(0),
				makeRunvmSameC3PrepareDictChild(t, 9, 11).MustBeginParse(),
			},
		},
		{
			name: "runvm_loads_child_c7",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(16).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t, funcsop.GETPARAM(1).Serialize()).MustBeginParse(),
				childC7,
			},
		},
		{
			name: "runvm_returns_data_and_actions",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(4|32).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t,
					stackop.PUSHREF(returnedData).Serialize(),
					execop.POPCTR(4).Serialize(),
					stackop.PUSHREF(returnedActions).Serialize(),
					execop.POPCTR(5).Serialize(),
				).MustBeginParse(),
				cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell(),
			},
		},
		{
			name: "runvm_returns_data_actions_and_gas",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(4|8|32).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t,
					stackop.PUSHREF(returnedData).Serialize(),
					execop.POPCTR(4).Serialize(),
					stackop.PUSHREF(returnedActions).Serialize(),
					execop.POPCTR(5).Serialize(),
				).MustBeginParse(),
				cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell(),
				int64(10_000),
			},
		},
		{
			name: "runvm_return_values_exact",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(256).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t,
					stackop.PUSHINT(big.NewInt(7)).Serialize(),
					stackop.PUSHINT(big.NewInt(8)).Serialize(),
				).MustBeginParse(),
				int64(1),
			},
		},
		{
			name: "runvm_return_values_underflow",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(256).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize()).MustBeginParse(),
				int64(2),
			},
		},
		{
			name: "runvm_return_32_values_free_stack_depth",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(0).Serialize())),
			stack: []any{
				int64(0),
				makeRunvmPushIntsChild(t, 32).MustBeginParse(),
			},
		},
		{
			name: "runvm_return_33_values_stack_depth_charge",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(0).Serialize())),
			stack: []any{
				int64(0),
				makeRunvmPushIntsChild(t, 33).MustBeginParse(),
			},
		},
		{
			name: "runvm_child_inherits_libraries",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(0).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t,
					stackop.PUSHREF(childLibCell).Serialize(),
					cellsliceop.XLOADQ().Serialize(),
				).MustBeginParse(),
			},
			goLibs:  []*cell.Cell{childLibraries},
			refLibs: childLibraries,
		},
		{
			name: "runvm_commit_then_throw",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(4|8).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t,
					stackop.PUSHREF(returnedData).Serialize(),
					execop.POPCTR(4).Serialize(),
					funcsop.COMMIT().Serialize(),
					codeFromOpcodes(t, 0xF22A).ToBuilder(),
				).MustBeginParse(),
				cell.BeginCell().EndCell(),
				int64(10_000),
			},
		},
		{
			name: "runvm_invalid_flags",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(512).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
			},
		},
		{
			name: "runvmx_invalid_mode_from_stack",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				int64(4096),
			},
		},
		{
			name: "runvm_bad_c7_type",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(16).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				int64(1),
			},
		},
		{
			name: "runvm_bad_data_type",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(4).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				int64(1),
			},
		},
		{
			name: "runvm_bad_code_type",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(0).Serialize())),
			stack: []any{
				int64(0),
				int64(1),
			},
		},
		{
			name: "runvm_bad_stack_size_range",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(0).Serialize())),
			stack: []any{
				int64(1),
				codeFromBuilders(t).MustBeginParse(),
			},
		},
		{
			name: "runvm_oog_with_gas_max",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(8|64).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize()).MustBeginParse(),
				int64(0),
				int64(100),
			},
		},
		{
			name: "runvm_child_gas_limit_oog_without_gas_max",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(8).Serialize())),
			stack: []any{
				int64(0),
				makeRunvmPushIntsChild(t, 12).MustBeginParse(),
				int64(45),
			},
		},
		{
			name: "runvm_child_gas_limit_raised_to_limit_when_max_lower",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(8|64).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t,
					stackop.PUSHINT(big.NewInt(1)).Serialize(),
					stackop.PUSHINT(big.NewInt(2)).Serialize(),
					stackop.PUSHINT(big.NewInt(3)).Serialize(),
				).MustBeginParse(),
				int64(200),
				int64(40),
			},
		},
		{
			name: "runvm_child_zero_gas_reports_oog",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(8|64).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t, stackop.PUSHINT(big.NewInt(1)).Serialize()).MustBeginParse(),
				int64(0),
				int64(0),
			},
		},
		{
			name: "runvm_isolated_loaded_cells",
			code: prependRawMethodDrop(codeFromBuilders(t,
				stackop.PUSHREF(cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()).Serialize(),
				cellsliceop.CTOS().Serialize(),
				stackop.DROP().Serialize(),
				stackop.PUSHREF(cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()).Serialize(),
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				stackop.PUSHSLICEINLINE(codeFromBuilders(t, cellsliceop.CTOS().Serialize(), stackop.DROP().Serialize()).MustBeginParse()).Serialize(),
				execop.RUNVM(128).Serialize(),
			)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build go stack: %v", err)
			}
			refStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build reference stack: %v", err)
			}

			var goRes *crossRunResult
			if len(tt.goLibs) > 0 {
				goRes, err = runGoCrossCodeWithLibs(tt.code, cell.BeginCell().EndCell(), tt.c7, tt.goLibs, goStack)
			} else {
				goRes, err = runGoCrossCode(tt.code, cell.BeginCell().EndCell(), tt.c7, goStack)
			}
			if err != nil {
				t.Fatalf("go execution failed: %v", err)
			}
			var refRes *crossRunResult
			if tt.refLibs != nil {
				refRes, err = runReferenceCrossCodeWithLibs(tt.code, cell.BeginCell().EndCell(), tt.c7, tt.refLibs, refStack)
			} else {
				refRes, err = runReferenceCrossCode(tt.code, cell.BeginCell().EndCell(), tt.c7, refStack)
			}
			if err != nil {
				t.Fatalf("reference execution failed: %v", err)
			}

			if goRes.exitCode != refRes.exitCode {
				t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
			}
			switch tt.name {
			case "runvm_invalid_flags", "runvmx_invalid_mode_from_stack", "runvm_bad_stack_size_range":
				if goRes.exitCode != vmerr.CodeRangeCheck {
					t.Fatalf("unexpected range-check exit: go=%d", goRes.exitCode)
				}
			case "runvm_bad_c7_type", "runvm_bad_data_type", "runvm_bad_code_type":
				if goRes.exitCode != vmerr.CodeTypeCheck {
					t.Fatalf("unexpected type-check exit: go=%d", goRes.exitCode)
				}
			}
			if goRes.gasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.stack.Hash(), refRes.stack.Hash()) {
				t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goRes.stack.Dump(), refRes.stack.Dump())
			}
		})
	}
}

func makeRunvmPushIntsChild(t *testing.T, count int) *cell.Cell {
	t.Helper()

	builders := make([]*cell.Builder, 0, count)
	for i := 0; i < count; i++ {
		builders = append(builders, stackop.PUSHINT(big.NewInt(int64(i+1))).Serialize())
	}
	return codeFromBuilders(t, builders...)
}
