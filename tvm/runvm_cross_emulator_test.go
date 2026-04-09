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
		name  string
		code  *cell.Cell
		stack []any
		c7    tuple.Tuple
	}

	childC7 := *tuple.NewTuple(*tuple.NewTuple(int64(11), int64(22)))
	returnedData := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	returnedActions := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()

	tests := []testCase{
		{
			name: "runvm_basic",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(0).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize()).BeginParse(),
			},
		},
		{
			name: "runvmx_basic",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t, stackop.PUSHINT(big.NewInt(9)).Serialize()).BeginParse(),
				int64(0),
			},
		},
		{
			name: "runvm_same_c3_push_zero",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(3).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t, stackop.DROP().Serialize()).BeginParse(),
			},
		},
		{
			name: "runvm_push_zero_without_same_c3",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(2).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t, stackop.DROP().Serialize()).BeginParse(),
			},
		},
		{
			name: "runvm_same_c3_calldict",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(3).Serialize())),
			stack: []any{
				int64(0),
				makeRunvmSameC3CallDictChild(t, 5, 9).BeginParse(),
			},
		},
		{
			name: "runvm_same_c3_jmpdict",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(3).Serialize())),
			stack: []any{
				int64(0),
				makeRunvmSameC3JmpDictChild(t, 8).BeginParse(),
			},
		},
		{
			name: "runvm_same_c3_preparedict",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(3).Serialize())),
			stack: []any{
				int64(0),
				makeRunvmSameC3PrepareDictChild(t, 9, 11).BeginParse(),
			},
		},
		{
			name: "runvm_loads_child_c7",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(16).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t, funcsop.GETPARAM(1).Serialize()).BeginParse(),
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
				).BeginParse(),
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
				).BeginParse(),
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
				).BeginParse(),
				int64(1),
			},
		},
		{
			name: "runvm_return_values_underflow",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(256).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize()).BeginParse(),
				int64(2),
			},
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
				).BeginParse(),
				cell.BeginCell().EndCell(),
				int64(10_000),
			},
		},
		{
			name: "runvm_invalid_flags",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(512).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).BeginParse(),
			},
		},
		{
			name: "runvmx_invalid_mode_from_stack",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).BeginParse(),
				int64(4096),
			},
		},
		{
			name: "runvm_bad_c7_type",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(16).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).BeginParse(),
				int64(1),
			},
		},
		{
			name: "runvm_bad_data_type",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(4).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).BeginParse(),
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
				codeFromBuilders(t).BeginParse(),
			},
		},
		{
			name: "runvm_oog_with_gas_max",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(8|64).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize()).BeginParse(),
				int64(0),
				int64(100),
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
				stackop.PUSHSLICEINLINE(codeFromBuilders(t, cellsliceop.CTOS().Serialize(), stackop.DROP().Serialize()).BeginParse()).Serialize(),
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

			goRes, err := runGoCrossCode(tt.code, cell.BeginCell().EndCell(), tt.c7, goStack)
			if err != nil {
				t.Fatalf("go execution failed: %v", err)
			}
			refRes, err := runReferenceCrossCode(tt.code, cell.BeginCell().EndCell(), tt.c7, refStack)
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
