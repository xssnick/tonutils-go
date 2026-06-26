//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	tupleop "github.com/xssnick/tonutils-go/tvm/op/tuple"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func runVMVersionCrossEmulatorVersions(t *testing.T) []int {
	t.Helper()

	return crossEmulatorVersionAuditVersions(t, "TVM_RUNVM_VERSION_AUDIT")
}

func TestTVMCrossEmulatorRunVMVersionAuditShardSelection(t *testing.T) {
	t.Setenv("TVM_RUNVM_VERSION_AUDIT_SHARDS", "")
	t.Setenv("TVM_RUNVM_VERSION_AUDIT_SHARD", "")

	all := runVMVersionCrossEmulatorVersions(t)
	wantLen := MaxSupportedGlobalVersion - MinSupportedGlobalVersion + 1
	if len(all) != wantLen {
		t.Fatalf("default version selection len = %d, want %d", len(all), wantLen)
	}
	if all[0] != MinSupportedGlobalVersion || all[len(all)-1] != MaxSupportedGlobalVersion {
		t.Fatalf("default version selection = %v, want range %d..%d", all, MinSupportedGlobalVersion, MaxSupportedGlobalVersion)
	}

	t.Setenv("TVM_RUNVM_VERSION_AUDIT_SHARDS", "4")
	t.Setenv("TVM_RUNVM_VERSION_AUDIT_SHARD", "1")
	got := runVMVersionCrossEmulatorVersions(t)
	want := []int{1, 5, 9, 13}
	if len(got) != len(want) {
		t.Fatalf("sharded version selection = %v, want %v", got, want)
	}
	for i, version := range want {
		if got[i] != version {
			t.Fatalf("sharded version selection = %v, want %v", got, want)
		}
	}
}

func TestTVMCrossEmulatorRunVM(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	type testCase struct {
		name     string
		code     *cell.Cell
		stack    []any
		c7       tuple.Tuple
		goLibs   []*cell.Cell
		refLibs  *cell.Cell
		gasLimit int64
	}

	childC7 := tuple.NewTupleValue(tuple.NewTupleValue(big.NewInt(11), big.NewInt(22)))
	returnedData := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	returnedActions := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	childLibTarget := cell.BeginCell().MustStoreUInt(0xA11CE, 20).EndCell()
	childLibCell := mustCrossLibraryCellForHash(t, childLibTarget.Hash())
	childLibraries := mustCrossLibraryCollection(t, childLibTarget)
	stackGasOOGStack := make([]any, 0, vmcore.FreeStackDepth+3)
	for i := int64(0); i < vmcore.FreeStackDepth+1; i++ {
		stackGasOOGStack = append(stackGasOOGStack, i)
	}
	stackGasOOGStack = append(stackGasOOGStack, int64(vmcore.FreeStackDepth+1), codeFromBuilders(t).MustBeginParse())

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
			name: "runvmx_same_c3_push_zero",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t, stackop.DROP().Serialize()).MustBeginParse(),
				int64(3),
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
			name: "runvmx_push_zero_without_same_c3",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t, stackop.DROP().Serialize()).MustBeginParse(),
				int64(2),
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
			name: "runvmx_same_c3_calldict",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				makeRunvmSameC3CallDictChild(t, 5, 9).MustBeginParse(),
				int64(3),
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
			name: "runvmx_same_c3_jmpdict",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				makeRunvmSameC3JmpDictChild(t, 8).MustBeginParse(),
				int64(3),
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
			name: "runvmx_same_c3_preparedict",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				makeRunvmSameC3PrepareDictChild(t, 9, 11).MustBeginParse(),
				int64(3),
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
			name: "runvmx_loads_child_c7",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t, funcsop.GETPARAM(1).Serialize()).MustBeginParse(),
				childC7,
				int64(16),
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
			name: "runvm_committed_actions_ctos",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(4|32).Serialize(), cellsliceop.CTOS().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t,
					stackop.PUSHREF(returnedData).Serialize(),
					execop.POPCTR(4).Serialize(),
					stackop.PUSHREF(returnedActions).Serialize(),
					execop.POPCTR(5).Serialize(),
					funcsop.COMMIT().Serialize(),
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
			name: "runvmx_returns_data_actions_and_gas",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
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
				int64(4 | 8 | 32),
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
			name: "runvmx_return_values_exact",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t,
					stackop.PUSHINT(big.NewInt(7)).Serialize(),
					stackop.PUSHINT(big.NewInt(8)).Serialize(),
				).MustBeginParse(),
				int64(1),
				int64(256),
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
			name: "runvmx_return_values_underflow",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize()).MustBeginParse(),
				int64(2),
				int64(256),
			},
		},
		{
			name: "runvm_bad_ret_count_negative",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(256).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				int64(-1),
			},
		},
		{
			name: "runvmx_bad_ret_count_negative",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				int64(-1),
				int64(256),
			},
		},
		{
			name: "runvm_bad_ret_count_large",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(256).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				int64(1<<30 + 1),
			},
		},
		{
			name: "runvmx_bad_ret_count_large",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				int64(1<<30 + 1),
				int64(256),
			},
		},
		{
			name: "runvm_bad_ret_count_nan",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(256).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				vmcore.NaN{},
			},
		},
		{
			name: "runvmx_bad_ret_count_nan",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				vmcore.NaN{},
				int64(256),
			},
		},
		{
			name: "runvm_bad_ret_count_type",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(256).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				cell.BeginCell().EndCell(),
			},
		},
		{
			name: "runvmx_bad_ret_count_type",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				cell.BeginCell().EndCell(),
				int64(256),
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
			name: "runvmx_child_inherits_libraries",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t,
					stackop.PUSHREF(childLibCell).Serialize(),
					cellsliceop.XLOADQ().Serialize(),
				).MustBeginParse(),
				int64(0),
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
			name: "runvmx_commit_then_throw",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
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
				int64(4 | 8),
			},
		},
		{
			name: "runvm_invalid_flags_512",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(512).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
			},
		},
		{
			name: "runvm_invalid_flags_513",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(513).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
			},
		},
		{
			name: "runvm_invalid_flags_4095",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(4095).Serialize())),
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
			name: "runvmx_bad_mode_type",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				cell.BeginCell().EndCell(),
			},
		},
		{
			name: "runvmx_invalid_flags_512_preserves_operands",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				int64(512),
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
			name: "runvmx_bad_c7_type",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				int64(1),
				int64(16),
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
			name: "runvmx_bad_data_type",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				int64(1),
				int64(4),
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
			name: "runvm_missing_stack_size_after_code",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(0).Serialize())),
			stack: []any{
				codeFromBuilders(t).MustBeginParse(),
			},
		},
		{
			name: "runvm_bad_gas_limit_type",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(8).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				cell.BeginCell().EndCell(),
			},
		},
		{
			name: "runvmx_bad_gas_limit_type",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				cell.BeginCell().EndCell(),
				int64(8),
			},
		},
		{
			name: "runvm_bad_gas_limit_negative",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(8).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				int64(-1),
			},
		},
		{
			name: "runvmx_bad_gas_limit_negative",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				int64(-1),
				int64(8),
			},
		},
		{
			name: "runvm_bad_gas_limit_large",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(8).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				new(big.Int).Lsh(big.NewInt(1), 63),
			},
		},
		{
			name: "runvmx_bad_gas_limit_large",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				new(big.Int).Lsh(big.NewInt(1), 63),
				int64(8),
			},
		},
		{
			name: "runvm_bad_gas_limit_nan",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(8).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				vmcore.NaN{},
			},
		},
		{
			name: "runvmx_bad_gas_limit_nan",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				vmcore.NaN{},
				int64(8),
			},
		},
		{
			name: "runvm_bad_gas_max_type",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(64).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				cell.BeginCell().EndCell(),
			},
		},
		{
			name: "runvmx_bad_gas_max_type",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				cell.BeginCell().EndCell(),
				int64(64),
			},
		},
		{
			name: "runvm_bad_gas_max_negative",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(64).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				int64(-1),
			},
		},
		{
			name: "runvmx_bad_gas_max_negative",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				int64(-1),
				int64(64),
			},
		},
		{
			name: "runvm_bad_gas_max_large",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(64).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				new(big.Int).Lsh(big.NewInt(1), 63),
			},
		},
		{
			name: "runvmx_bad_gas_max_large",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				new(big.Int).Lsh(big.NewInt(1), 63),
				int64(64),
			},
		},
		{
			name: "runvm_bad_gas_max_nan",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(64).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				vmcore.NaN{},
			},
		},
		{
			name: "runvmx_bad_gas_max_nan",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				vmcore.NaN{},
				int64(64),
			},
		},
		{
			name: "runvm_bad_gas_limit_with_max_negative",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(8|64).Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				int64(-1),
				int64(10_000),
			},
		},
		{
			name: "runvmx_bad_gas_limit_with_max_negative",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t).MustBeginParse(),
				int64(-1),
				int64(10_000),
				int64(8 | 64),
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
			name: "runvmx_bad_stack_size_range",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(1),
				codeFromBuilders(t).MustBeginParse(),
				int64(0),
			},
		},
		{
			name: "runvm_bad_stack_size_negative",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(0).Serialize())),
			stack: []any{
				int64(-1),
				codeFromBuilders(t).MustBeginParse(),
			},
		},
		{
			name: "runvmx_bad_stack_size_negative",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(-1),
				codeFromBuilders(t).MustBeginParse(),
				int64(0),
			},
		},
		{
			name: "runvm_bad_stack_size_nan",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(0).Serialize())),
			stack: []any{
				vmcore.NaN{},
				codeFromBuilders(t).MustBeginParse(),
			},
		},
		{
			name: "runvmx_bad_stack_size_nan",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				vmcore.NaN{},
				codeFromBuilders(t).MustBeginParse(),
				int64(0),
			},
		},
		{
			name: "runvm_bad_stack_size_type",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(0).Serialize())),
			stack: []any{
				cell.BeginCell().EndCell(),
				codeFromBuilders(t).MustBeginParse(),
			},
		},
		{
			name: "runvmx_bad_stack_size_type",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				cell.BeginCell().EndCell(),
				codeFromBuilders(t).MustBeginParse(),
				int64(0),
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
			name: "runvmx_oog_with_gas_max",
			code: prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize())),
			stack: []any{
				int64(0),
				codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize()).MustBeginParse(),
				int64(0),
				int64(100),
				int64(8 | 64),
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
			name:     "runvm_child_stack_gas_oog",
			code:     prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(0).Serialize())),
			stack:    stackGasOOGStack,
			gasLimit: 2*vmcore.InstructionBaseGasPrice + vmcore.RunvmGasPrice,
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
		{
			name: "runvmx_isolated_loaded_cells",
			code: prependRawMethodDrop(codeFromBuilders(t,
				stackop.PUSHREF(cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()).Serialize(),
				cellsliceop.CTOS().Serialize(),
				stackop.DROP().Serialize(),
				stackop.PUSHREF(cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()).Serialize(),
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				stackop.PUSHSLICEINLINE(codeFromBuilders(t, cellsliceop.CTOS().Serialize(), stackop.DROP().Serialize()).MustBeginParse()).Serialize(),
				stackop.PUSHINT(big.NewInt(128)).Serialize(),
				execop.RUNVMX().Serialize(),
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

			gasLimit := referenceDefaultMaxGas
			if tt.gasLimit != 0 {
				gasLimit = tt.gasLimit
			}

			goRes, err := runGoCrossCodeWithVersionGasAndLibs(tt.code, cell.BeginCell().EndCell(), tt.c7, tt.goLibs, goStack, referenceRawRunGlobalVersion, gasLimit)
			if err != nil {
				t.Fatalf("go execution failed: %v", err)
			}
			refRes, err := runReferenceCrossCodeWithLibsAndGas(tt.code, cell.BeginCell().EndCell(), tt.c7, tt.refLibs, refStack, gasLimit)
			if err != nil {
				t.Fatalf("reference execution failed: %v", err)
			}

			if goRes.exitCode != refRes.exitCode {
				t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
			}
			switch tt.name {
			case "runvm_invalid_flags_512", "runvm_invalid_flags_513", "runvm_invalid_flags_4095", "runvmx_invalid_mode_from_stack", "runvmx_invalid_flags_512_preserves_operands", "runvm_bad_stack_size_range", "runvmx_bad_stack_size_range", "runvm_bad_stack_size_negative", "runvmx_bad_stack_size_negative", "runvm_bad_stack_size_nan", "runvmx_bad_stack_size_nan", "runvm_bad_ret_count_negative", "runvmx_bad_ret_count_negative", "runvm_bad_ret_count_large", "runvmx_bad_ret_count_large", "runvm_bad_ret_count_nan", "runvmx_bad_ret_count_nan":
				if goRes.exitCode != vmerr.CodeRangeCheck {
					t.Fatalf("unexpected range-check exit: go=%d", goRes.exitCode)
				}
			case "runvm_bad_c7_type", "runvmx_bad_c7_type", "runvm_bad_data_type", "runvmx_bad_data_type", "runvm_bad_code_type", "runvmx_bad_mode_type", "runvm_bad_stack_size_type", "runvmx_bad_stack_size_type", "runvm_bad_ret_count_type", "runvmx_bad_ret_count_type":
				if goRes.exitCode != vmerr.CodeTypeCheck {
					t.Fatalf("unexpected type-check exit: go=%d", goRes.exitCode)
				}
			case "runvm_bad_gas_limit_type", "runvmx_bad_gas_limit_type", "runvm_bad_gas_max_type", "runvmx_bad_gas_max_type":
				if goRes.exitCode != vmerr.CodeTypeCheck {
					t.Fatalf("unexpected type-check exit: go=%d", goRes.exitCode)
				}
			case "runvm_bad_gas_limit_negative", "runvmx_bad_gas_limit_negative", "runvm_bad_gas_limit_large", "runvmx_bad_gas_limit_large", "runvm_bad_gas_limit_nan", "runvmx_bad_gas_limit_nan", "runvm_bad_gas_max_negative", "runvmx_bad_gas_max_negative", "runvm_bad_gas_max_large", "runvmx_bad_gas_max_large", "runvm_bad_gas_max_nan", "runvmx_bad_gas_max_nan", "runvm_bad_gas_limit_with_max_negative", "runvmx_bad_gas_limit_with_max_negative":
				if goRes.exitCode != vmerr.CodeRangeCheck {
					t.Fatalf("unexpected range-check exit: go=%d", goRes.exitCode)
				}
			case "runvm_missing_stack_size_after_code":
				if goRes.exitCode != vmerr.CodeStackUnderflow {
					t.Fatalf("unexpected stack-underflow exit: go=%d", goRes.exitCode)
				}
			case "runvm_child_stack_gas_oog":
				if goRes.exitCode != int32(^vmerr.CodeOutOfGas) {
					t.Fatalf("unexpected out-of-gas exit: go=%d", goRes.exitCode)
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

func TestTVMCrossEmulatorRunVMChildGlobalVersionInheritance(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range runVMVersionCrossEmulatorVersions(t) {
		version := version
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			runCrossEmulatorRunVMChildGlobalVersionInheritance(t, version)
		})
	}
}

func FuzzTVMCrossEmulatorRunVMChildGlobalVersionInheritance(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version))
	}
	f.Add(uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		runCrossEmulatorRunVMChildGlobalVersionInheritance(t, version)
	})
}

func runCrossEmulatorRunVMChildGlobalVersionInheritance(t *testing.T, version int) {
	t.Helper()

	code := prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(16).Serialize()))
	child := codeFromBuilders(t, funcsop.INMSGPARAMS().Serialize()).MustBeginParse()
	childC7 := makeRunvmInMsgParamsChildC7(t)

	goStack, err := buildCrossStack(int64(0), child.Copy(), childC7)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(int64(0), child.Copy(), childC7)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithVersion(code, cell.BeginCell().EndCell(), tuple.Tuple{}, goStack, version)
	if err != nil {
		t.Fatalf("go execution failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference execution failed: %v", err)
	}

	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.stack.Hash(), refRes.stack.Hash()) {
		t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goRes.stack.Dump(), refRes.stack.Dump())
	}
	if version < 4 && goRes.exitCode != vmerr.CodeInvalidOpcode {
		t.Fatalf("v%d parent exit = %d, want invalid opcode", version, goRes.exitCode)
	}
}

func TestTVMCrossEmulatorRunVMFailedDataActionsVersionMatrix(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := runVMFailedDataActionsVersionCases(t)
	for _, tt := range tests {
		tt := tt
		for _, version := range runVMVersionCrossEmulatorVersions(t) {
			version := version
			t.Run(tt.name+"_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
				runCrossEmulatorRunVMFailedDataActionsVersionCase(t, tt, version)
			})
		}
	}
}

func FuzzTVMCrossEmulatorRunVMFailedDataActionsVersionMatrix(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%runVMFailedDataActionsVersionCaseCount))
	}
	for i := 0; i < runVMFailedDataActionsVersionCaseCount; i++ {
		f.Add(uint8(MaxSupportedGlobalVersion), uint8(i))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := runVMFailedDataActionsVersionCases(t)
		if len(tests) != runVMFailedDataActionsVersionCaseCount {
			t.Fatalf("runvm failed data/actions case count = %d, want %d", len(tests), runVMFailedDataActionsVersionCaseCount)
		}
		tt := tests[int(rawCase)%len(tests)]
		runCrossEmulatorRunVMFailedDataActionsVersionCase(t, tt, version)
	})
}

const runVMFailedDataActionsVersionCaseCount = 10

type runVMFailedDataActionsVersionCase struct {
	name    string
	dynamic bool
	mode    int
	child   *cell.Cell
}

func runVMFailedDataActionsVersionCases(t *testing.T) []runVMFailedDataActionsVersionCase {
	t.Helper()

	throw5 := codeFromOpcodes(t, 0xF205)
	returnedData := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	returnedActions := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	setDataThenThrow := codeFromBuilders(t,
		stackop.PUSHREF(returnedData).Serialize(),
		execop.POPCTR(4).Serialize(),
		cell.BeginCell().MustStoreUInt(0xF205, 16),
	)
	setActionsThenThrow := codeFromBuilders(t,
		stackop.PUSHREF(returnedActions).Serialize(),
		execop.POPCTR(5).Serialize(),
		cell.BeginCell().MustStoreUInt(0xF205, 16),
	)
	stackUnderflow := codeFromBuilders(t, stackop.DROP().Serialize())
	typeCheck := codeFromBuilders(t,
		stackop.PUSHINT(big.NewInt(7)).Serialize(),
		tupleop.QTLEN().Serialize(),
	)

	return []runVMFailedDataActionsVersionCase{
		{name: "runvm_throw", mode: 4 | 32, child: throw5},
		{name: "runvmx_throw", dynamic: true, mode: 4 | 32, child: throw5},
		{name: "runvm_stack_underflow", mode: 4 | 32, child: stackUnderflow},
		{name: "runvmx_stack_underflow", dynamic: true, mode: 4 | 32, child: stackUnderflow},
		{name: "runvm_type_check", mode: 4 | 32, child: typeCheck},
		{name: "runvmx_type_check", dynamic: true, mode: 4 | 32, child: typeCheck},
		{name: "runvm_set_data_then_throw", mode: 4 | 32, child: setDataThenThrow},
		{name: "runvmx_set_data_then_throw", dynamic: true, mode: 4 | 32, child: setDataThenThrow},
		{name: "runvm_set_actions_then_throw", mode: 4 | 32, child: setActionsThenThrow},
		{name: "runvmx_set_actions_then_throw", dynamic: true, mode: 4 | 32, child: setActionsThenThrow},
	}
}

func runCrossEmulatorRunVMFailedDataActionsVersionCase(t *testing.T, tt runVMFailedDataActionsVersionCase, version int) {
	t.Helper()

	initialData := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	parentCode := prependRawMethodDrop(codeFromBuilders(t,
		execop.RUNVM(tt.mode).Serialize(),
		tupleop.ISNULL().Serialize(),
		stackop.XCHG0(1).Serialize(),
		tupleop.ISNULL().Serialize(),
	))
	if tt.dynamic {
		parentCode = prependRawMethodDrop(codeFromBuilders(t,
			execop.RUNVMX().Serialize(),
			tupleop.ISNULL().Serialize(),
			stackop.XCHG0(1).Serialize(),
			tupleop.ISNULL().Serialize(),
		))
	}

	stackValues := []any{int64(0), tt.child.MustBeginParse(), initialData}
	if tt.dynamic {
		stackValues = append(stackValues, int64(tt.mode))
	}
	goStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithVersion(parentCode, cell.BeginCell().EndCell(), tuple.Tuple{}, goStack, version)
	if err != nil {
		t.Fatalf("go execution failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	refRes, err := runReferenceCrossCodeViaEmulator(parentCode, cell.BeginCell().EndCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference execution failed: %v", err)
	}

	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
	}
	if version < 4 && goRes.exitCode != vmerr.CodeInvalidOpcode {
		t.Fatalf("v%d parent exit = %d, want invalid opcode", version, goRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.stack.Hash(), refRes.stack.Hash()) {
		t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goRes.stack.Dump(), refRes.stack.Dump())
	}
}

func TestTVMCrossEmulatorRunVMVersionedChildOpcodeMatrix(t *testing.T) {
	runCrossEmulatorRunVMVersionedChildOpcodeMatrix(t, false)
}

func TestTVMCrossEmulatorRunVMXVersionedChildOpcodeMatrix(t *testing.T) {
	runCrossEmulatorRunVMVersionedChildOpcodeMatrix(t, true)
}

func FuzzTVMCrossEmulatorRunVMVersionedChildOpcodeMatrix(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%runvmVersionedChildCaseCount), false)
		f.Add(uint8(version), uint8(version%runvmVersionedChildCaseCount), true)
	}
	for i := 0; i < runvmVersionedChildCaseCount; i++ {
		f.Add(uint8(MaxSupportedGlobalVersion), uint8(i), false)
		f.Add(uint8(MaxSupportedGlobalVersion), uint8(i), true)
	}
	f.Add(uint8(255), uint8(255), true)

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8, dynamicMode bool) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := runvmVersionedChildCases(t)
		if len(tests) != runvmVersionedChildCaseCount {
			t.Fatalf("runvm versioned child case count = %d, want %d", len(tests), runvmVersionedChildCaseCount)
		}
		tt := tests[int(rawCase)%len(tests)]
		runCrossEmulatorRunVMVersionedChildOpcodeCase(t, tt, version, dynamicMode)
	})
}

func runCrossEmulatorRunVMVersionedChildOpcodeMatrix(t *testing.T, dynamicMode bool) {
	t.Helper()

	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := runvmVersionedChildCases(t)
	versions := runVMVersionCrossEmulatorVersions(t)

	for _, tt := range tests {
		tt := tt
		for _, version := range versions {
			version := version
			t.Run(runVMOpcodeName(dynamicMode)+"_"+tt.name+"_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
				runCrossEmulatorRunVMVersionedChildOpcodeCase(t, tt, version, dynamicMode)
			})
		}
	}
}

func runCrossEmulatorRunVMVersionedChildOpcodeCase(t *testing.T, tt runvmVersionedChildCase, version int, dynamicMode bool) {
	t.Helper()

	parentCode := prependRawMethodDrop(codeFromBuilders(t, execop.RUNVM(tt.mode).Serialize()))
	if dynamicMode {
		parentCode = prependRawMethodDrop(codeFromBuilders(t, execop.RUNVMX().Serialize()))
	}

	stackValues := []any{int64(0), tt.child.MustBeginParse()}
	if tt.mode&16 != 0 {
		stackValues = append(stackValues, tt.c7)
	}
	if dynamicMode {
		stackValues = append(stackValues, int64(tt.mode))
	}
	goStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithVersion(parentCode, cell.BeginCell().EndCell(), tuple.Tuple{}, goStack, version)
	if err != nil {
		t.Fatalf("go execution failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	refRes, err := runReferenceCrossCodeViaEmulator(parentCode, cell.BeginCell().EndCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference execution failed: %v", err)
	}

	wantParentExit := int32(0)
	if version < 4 {
		wantParentExit = int32(vmerr.CodeInvalidOpcode)
	}
	if goRes.exitCode != wantParentExit || refRes.exitCode != wantParentExit {
		t.Fatalf("parent exit mismatch: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, wantParentExit)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.stack.Hash(), refRes.stack.Hash()) {
		t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goRes.stack.Dump(), refRes.stack.Dump())
	}

	if version < 4 {
		return
	}

	gotChildExit, err := popCrossStackLastInt(goRes.stack)
	if err != nil {
		t.Fatalf("failed to pop child exit: %v", err)
	}
	wantChildExit := int64(0)
	if version < tt.minVersion {
		wantChildExit = int64(vmerr.CodeInvalidOpcode)
	}
	if gotChildExit != wantChildExit {
		t.Fatalf("child exit = %d, want %d", gotChildExit, wantChildExit)
	}
}

func popCrossStackLastInt(stackCell *cell.Cell) (int64, error) {
	var stack tlb.Stack
	if err := tlb.Parse(&stack, stackCell); err != nil {
		return 0, err
	}

	var val any
	for stack.Depth() > 0 {
		var err error
		val, err = stack.Pop()
		if err != nil {
			return 0, err
		}
	}

	switch v := val.(type) {
	case int64:
		return v, nil
	case *big.Int:
		return v.Int64(), nil
	default:
		return 0, vmerr.Error(vmerr.CodeTypeCheck, "stack top is not an int")
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
