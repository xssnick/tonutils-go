//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorContOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	intBody := func(v int64) *cell.Cell {
		return codeFromBuilders(t, stackop.PUSHINT(big.NewInt(v)).Serialize())
	}

	retAltBody := codeFromBuilders(t, execop.RETALT().Serialize())
	retBody := codeFromBuilders(t, execop.RET().Serialize())
	dropThen7Body := codeFromBuilders(t, stackop.DROP().Serialize(), stackop.PUSHINT(big.NewInt(7)).Serialize())
	emptyBody := codeFromBuilders(t)
	remainingCodeBody := codeFromBuilders(t, execop.RETDATA().Serialize(), stackop.PUSHINT(big.NewInt(3)).Serialize())
	push7Body := intBody(7)
	push7Slice := push7Body.BeginParse()

	type testCase struct {
		name  string
		code  *cell.Cell
		stack []any
		exit  int32
	}

	tests := []testCase{
		{
			name: "ret_via_execute",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(codeFromBuilders(t, execop.RET().Serialize())).Serialize(),
				execop.EXECUTE().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			exit: 0,
		},
		{
			name:  "ifret_true_exit0",
			code:  codeFromBuilders(t, execop.IFRET().Serialize(), stackop.PUSHINT(big.NewInt(9)).Serialize()),
			stack: []any{int64(-1)},
			exit:  0,
		},
		{
			name:  "ifret_false_continue",
			code:  codeFromBuilders(t, execop.IFRET().Serialize(), stackop.PUSHINT(big.NewInt(9)).Serialize()),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name:  "ifretalt_true_exit1",
			code:  codeFromBuilders(t, execop.IFRETALT().Serialize(), stackop.PUSHINT(big.NewInt(9)).Serialize()),
			stack: []any{int64(-1)},
			exit:  1,
		},
		{
			name:  "ifnotretalt_false_exit1",
			code:  codeFromBuilders(t, execop.IFNOTRETALT().Serialize()),
			stack: []any{int64(0)},
			exit:  1,
		},
		{
			name: "retbool_false_exit1",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(codeFromBuilders(t, execop.RETBOOL().Serialize())).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{int64(0)},
			exit:  1,
		},
		{
			name: "retdata_pushes_remaining_code",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(remainingCodeBody).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "retargs_trim",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(codeFromBuilders(t, execop.RETARGS(1).Serialize())).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{int64(11), int64(22)},
			exit:  0,
		},
		{
			name: "callxargs_trim",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.CALLXARGS(2, 1).Serialize(),
			),
			stack: []any{int64(11), int64(22)},
			exit:  0,
		},
		{
			name: "callxargs_p_all",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.CALLXARGSP(1).Serialize(),
			),
			stack: []any{int64(55)},
			exit:  0,
		},
		{
			name: "callcc_pushes_cc",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(dropThen7Body).Serialize(),
				execop.CALLCC().Serialize(),
			),
			exit: 0,
		},
		{
			name: "callccargs_preserves_arg",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(dropThen7Body).Serialize(),
				execop.CALLCCARGS(1, 2).Serialize(),
			),
			stack: []any{int64(55)},
			exit:  0,
		},
		{
			name: "callxvarargs_dynamic",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.XCHG0(2).Serialize(),
				stackop.XCHG0(1).Serialize(),
				execop.CALLXVARARGS().Serialize(),
			),
			stack: []any{int64(11), int64(22), int64(2), int64(1)},
			exit:  0,
		},
		{
			name:  "retvarargs_trim",
			code:  codeFromBuilders(t, execop.RETVARARGS().Serialize()),
			stack: []any{int64(11), int64(22), int64(1)},
			exit:  0,
		},
		{
			name: "callccvarargs_dynamic",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(dropThen7Body).Serialize(),
				stackop.XCHG0(2).Serialize(),
				stackop.XCHG0(1).Serialize(),
				execop.CALLCCVARARGS().Serialize(),
			),
			stack: []any{int64(55), int64(1), int64(2)},
			exit:  0,
		},
		{
			name: "ifbitjmp_true",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(dropThen7Body).Serialize(),
				execop.IFBITJMP(1).Serialize(),
			),
			stack: []any{int64(2)},
			exit:  0,
		},
		{
			name: "ifnbitjmp_true",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(dropThen7Body).Serialize(),
				execop.IFNBITJMP(1).Serialize(),
			),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name:  "ifbitjmpref_true",
			code:  codeFromBuilders(t, execop.IFBITJMPREF(1, dropThen7Body).Serialize()),
			stack: []any{int64(2)},
			exit:  0,
		},
		{
			name: "callref",
			code: codeFromBuilders(t, execop.CALLREF(intBody(7)).Serialize()),
			exit: 0,
		},
		{
			name: "jmpref",
			code: codeFromBuilders(t, execop.JMPREF(intBody(8)).Serialize()),
			exit: 0,
		},
		{
			name: "jmprefdata",
			code: codeFromBuilders(t, execop.JMPREFDATA(dropThen7Body).Serialize()),
			exit: 0,
		},
		{
			name:  "ifref_true",
			code:  codeFromBuilders(t, execop.IFREF(intBody(7)).Serialize()),
			stack: []any{int64(-1)},
			exit:  0,
		},
		{
			name:  "ifnotref_false",
			code:  codeFromBuilders(t, execop.IFNOTREF(intBody(7)).Serialize()),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name:  "ifjmpref_true",
			code:  codeFromBuilders(t, execop.IFJMPREF(intBody(7)).Serialize()),
			stack: []any{int64(-1)},
			exit:  0,
		},
		{
			name:  "ifnotjmpref_false",
			code:  codeFromBuilders(t, execop.IFNOTJMPREF(intBody(7)).Serialize()),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name: "ifrefelse_false",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(intBody(9)).Serialize(),
				execop.IFREFELSE(intBody(7)).Serialize(),
			),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name: "ifelseref_false",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(intBody(7)).Serialize(),
				execop.IFELSEREF(intBody(9)).Serialize(),
			),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name:  "ifrefelseref_true",
			code:  codeFromBuilders(t, execop.IFREFELSEREF(intBody(7), intBody(9)).Serialize()),
			stack: []any{int64(-1)},
			exit:  0,
		},
		{
			name: "setcontargs_execute",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.SETCONTARGS(1, 0).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{int64(55)},
			exit:  0,
		},
		{
			name: "setcontvarargs_execute",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				execop.SETCONTVARARGS().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{int64(66)},
			exit:  0,
		},
		{
			name: "setnumvarargs_execute",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				execop.SETNUMVARARGS().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{int64(77)},
			exit:  0,
		},
		{
			name:  "bless_execute",
			code:  codeFromBuilders(t, execop.BLESS().Serialize(), execop.EXECUTE().Serialize()),
			stack: []any{push7Slice},
			exit:  0,
		},
		{
			name:  "blessargs_execute",
			code:  codeFromBuilders(t, execop.BLESSARGS(1, 0).Serialize(), execop.EXECUTE().Serialize()),
			stack: []any{int64(55), push7Body.BeginParse()},
			exit:  0,
		},
		{
			name: "blessvarargs_execute",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				execop.BLESSVARARGS().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{int64(66), push7Body.BeginParse()},
			exit:  0,
		},
		{
			name: "if_non_int_condition_typecheck",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.IF().Serialize(),
			),
			stack: []any{cell.BeginCell().EndCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "ifelse_second_branch_not_continuation",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.IFELSE().Serialize(),
			),
			stack: []any{int64(-1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "jmpx_non_continuation_typecheck",
			code:  codeFromBuilders(t, execop.JMPX().Serialize()),
			stack: []any{int64(1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "jmpxdata_non_continuation_typecheck",
			code:  codeFromBuilders(t, execop.JMPXDATA().Serialize()),
			stack: []any{int64(1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "jmpxvarargs_underflow",
			code:  codeFromBuilders(t, execop.JMPXVARARGS().Serialize()),
			stack: []any{int64(1)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name: "callccargs_underflow",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.CALLCCARGS(2, 1).Serialize(),
			),
			stack: []any{int64(55)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "callxvarargs_bad_params_range",
			code:  codeFromBuilders(t, execop.CALLXVARARGS().Serialize()),
			stack: []any{push7Slice, int64(255), int64(0)},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name: "setcontargs_closure_overflow",
			code: codeFromBuilders(t,
				execop.BLESSARGS(0, 0).Serialize(),
				execop.SETCONTARGS(1, -1).Serialize(),
			),
			stack: []any{int64(55), push7Slice},
			exit:  vmerr.CodeStackOverflow,
		},
		{
			name: "setnumvarargs_bad_range",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(256)).Serialize(),
				execop.SETNUMVARARGS().Serialize(),
			),
			exit: vmerr.CodeRangeCheck,
		},
		{
			name:  "bless_bad_code_type",
			code:  codeFromBuilders(t, execop.BLESS().Serialize()),
			stack: []any{int64(1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "again_exit_alt",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(retAltBody).Serialize(),
				execop.AGAIN().Serialize(),
			),
			exit: 1,
		},
		{
			name: "againend_exit_alt",
			code: codeFromBuilders(t,
				execop.AGAINEND().Serialize(),
				execop.RETALT().Serialize(),
			),
			exit: 1,
		},
		{
			name: "repeatbrk_break",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(retAltBody).Serialize(),
				execop.REPEATBRK().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(3)},
			exit:  0,
		},
		{
			name:  "repeatendbrk_break",
			code:  codeFromBuilders(t, execop.REPEATENDBRK().Serialize(), execop.RETALT().Serialize()),
			stack: []any{int64(3)},
			exit:  0,
		},
		{
			name: "untilbrk_break",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(retAltBody).Serialize(),
				execop.UNTILBRK().Serialize(),
				stackop.PUSHINT(big.NewInt(8)).Serialize(),
			),
			exit: 0,
		},
		{
			name: "untilendbrk_break",
			code: codeFromBuilders(t,
				execop.UNTILENDBRK().Serialize(),
				execop.RETALT().Serialize(),
			),
			exit: 0,
		},
		{
			name: "whilebrk_break",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(codeFromBuilders(t, stackop.PUSHINT(big.NewInt(1)).Serialize())).Serialize(),
				stackop.PUSHCONT(retAltBody).Serialize(),
				execop.WHILEBRK().Serialize(),
				stackop.PUSHINT(big.NewInt(9)).Serialize(),
			),
			exit: 0,
		},
		{
			name: "whileendbrk_break",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(codeFromBuilders(t, stackop.PUSHINT(big.NewInt(1)).Serialize())).Serialize(),
				execop.WHILEENDBRK().Serialize(),
				execop.RETALT().Serialize(),
			),
			exit: 0,
		},
		{
			name: "againbrk_break",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(retAltBody).Serialize(),
				execop.AGAINBRK().Serialize(),
			),
			exit: 0,
		},
		{
			name: "againendbrk_break",
			code: codeFromBuilders(t,
				execop.AGAINENDBRK().Serialize(),
				execop.RETALT().Serialize(),
			),
			exit: 0,
		},
		{
			name: "atexit_runs",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(push7Body).Serialize(),
				execop.ATEXIT().Serialize(),
			),
			exit: 0,
		},
		{
			name: "atexitalt_runs",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(push7Body).Serialize(),
				execop.ATEXITALT().Serialize(),
				execop.RETALT().Serialize(),
			),
			exit: 0,
		},
		{
			name: "setexitalt_runs",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(push7Body).Serialize(),
				execop.SETEXITALT().Serialize(),
				execop.RETALT().Serialize(),
			),
			exit: 0,
		},
		{
			name: "thenret_runs",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(push7Body).Serialize(),
				execop.ATEXIT().Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.THENRET().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "thenretalt_runs",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(push7Body).Serialize(),
				execop.POPCTR(1).Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.THENRETALT().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "invert_ret",
			code: codeFromBuilders(t,
				execop.INVERT().Serialize(),
				execop.RET().Serialize(),
			),
			exit: 1,
		},
		{
			name: "booleval_true",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(retBody).Serialize(),
				execop.BOOLEVAL().Serialize(),
			),
			exit: 0,
		},
		{
			name: "booland_runs",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHCONT(push7Body).Serialize(),
				execop.BOOLAND().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "composboth_runs",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(retAltBody).Serialize(),
				stackop.PUSHCONT(push7Body).Serialize(),
				execop.COMPOSBOTH().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "calldict_short",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.POPCTR(3).Serialize(),
				execop.CALLDICT(5).Serialize(),
			),
			exit: 0,
		},
		{
			name: "calldict_long",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.POPCTR(3).Serialize(),
				execop.CALLDICT(300).Serialize(),
			),
			exit: 0,
		},
		{
			name: "jmpdict",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.POPCTR(3).Serialize(),
				execop.JMPDICT(8).Serialize(),
			),
			exit: 0,
		},
		{
			name: "preparedict",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.POPCTR(3).Serialize(),
				execop.PREPAREDICT(9).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "try_handler_typecheck",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				execop.TRY().Serialize(),
			),
			stack: []any{push7Slice},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "try_body_typecheck",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.TRY().Serialize(),
			),
			stack: []any{int64(1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "retvarargs_bad_range",
			code:  codeFromBuilders(t, execop.RETVARARGS().Serialize()),
			stack: []any{int64(255)},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name:  "condselchk_true",
			code:  codeFromBuilders(t, execop.CONDSELCHK().Serialize()),
			stack: []any{int64(-1), int64(11), int64(22)},
			exit:  0,
		},
		{
			name:  "condselchk_type_mismatch",
			code:  codeFromBuilders(t, execop.CONDSELCHK().Serialize()),
			stack: []any{int64(-1), int64(11), cell.BeginCell().EndCell()},
			exit:  7,
		},
		{
			name: "callref_missing_ref_invalid",
			code: codeFromOpcodes(t, 0xDB3C),
			exit: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := prependRawMethodDrop(tt.code)
			goStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build go stack: %v", err)
			}
			refStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build reference stack: %v", err)
			}

			goRes, err := runGoCrossCode(code, cell.BeginCell().EndCell(), tuple.Tuple{}, goStack)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}

			refRes, err := runReferenceCrossCode(code, cell.BeginCell().EndCell(), tuple.Tuple{}, refStack)
			if err != nil {
				t.Fatalf("reference tvm execution failed: %v", err)
			}

			if goRes.exitCode != tt.exit || refRes.exitCode != tt.exit {
				t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, tt.exit)
			}
			if goRes.exitCode != refRes.exitCode {
				t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
			}
			if goRes.gasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
			}

			goStackCell, err := normalizeStackCell(goRes.stack)
			if err != nil {
				t.Fatalf("failed to normalize go stack: %v", err)
			}
			refStackCell, err := normalizeStackCell(refRes.stack)
			if err != nil {
				t.Fatalf("failed to normalize reference stack: %v", err)
			}
			if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
				t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
			}
		})
	}
}
