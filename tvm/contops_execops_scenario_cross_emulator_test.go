//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	mathop "github.com/xssnick/tonutils-go/tvm/op/math"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	tupleop "github.com/xssnick/tonutils-go/tvm/op/tuple"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorContExecScenarios(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	intCode := func(v int64) *cell.Builder {
		return stackop.PUSHINT(big.NewInt(v)).Serialize()
	}
	body := func(builders ...*cell.Builder) *cell.Cell {
		return codeFromBuilders(t, builders...)
	}

	cellA := cell.BeginCell().MustStoreUInt(0xA, 4).EndCell()
	cellB := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	cellC := cell.BeginCell().MustStoreUInt(0xC0DE, 16).EndCell()
	defaultData := cell.BeginCell().EndCell()
	nonEmptyC7 := tuple.NewTupleValue(big.NewInt(10), tuple.NewTupleValue(big.NewInt(20)), nil)

	push7Body := body(intCode(7))
	push9Body := body(intCode(9))
	trueBranch := body(intCode(11))
	falseBranch := body(intCode(22))
	incBody := body(mathop.INC().Serialize())
	untilBody := body(
		mathop.INC().Serialize(),
		stackop.DUP().Serialize(),
		mathop.GTINT(2).Serialize(),
	)
	whileCond := body(
		stackop.DUP().Serialize(),
		mathop.LESSINT(4).Serialize(),
	)
	whileBody := body(mathop.INC().Serialize())
	againBody := body(
		intCode(44),
		execop.RETALT().Serialize(),
	)
	tryHandler := body(intCode(0xCAFE))
	tryThrowArgBody := body(
		intCode(321),
		cell.BeginCell().MustStoreUInt(0xF2C955, 24),
	)
	trySuccessBody := body(
		mathop.INC().Serialize(),
		execop.RETARGS(1).Serialize(),
	)
	nestedTryInnerCaughtBody := body(
		stackop.PUSHCONT(tryThrowArgBody).Serialize(),
		stackop.PUSHCONT(body(intCode(0x111))).Serialize(),
		execop.TRY().Serialize(),
		intCode(0x222),
	)
	nestedTryInnerRethrowBody := body(
		stackop.PUSHCONT(tryThrowArgBody).Serialize(),
		stackop.PUSHCONT(body(codeFromOpcodes(t, 0xF22A).ToBuilder())).Serialize(),
		execop.TRY().Serialize(),
		intCode(0x333),
	)
	nestedTryOuterRethrowBody := body(codeFromOpcodes(t, 0xF22B).ToBuilder())
	sumReturnBody := body(
		mathop.SUM().Serialize(),
		execop.RETARGS(1).Serialize(),
	)
	retVarArgsBody := body(
		intCode(123),
		intCode(1),
		execop.RETVARARGS().Serialize(),
	)
	callCCBody := body(
		intCode(42),
		stackop.SWAP().Serialize(),
		execop.EXECUTE().Serialize(),
	)
	callCCArgsBody := body(
		stackop.SWAP().Serialize(),
		mathop.INC().Serialize(),
		stackop.SWAP().Serialize(),
		execop.EXECUTE().Serialize(),
	)
	retAltBody := body(execop.RETALT().Serialize())
	setRetCtrBody := body(
		execop.SETRETCTR(4).Serialize(),
		execop.RET().Serialize(),
	)
	popSaveCtrBody := body(
		execop.POPSAVECTR(4).Serialize(),
		execop.RET().Serialize(),
	)
	pushC4Body := body(execop.PUSHCTR(4).Serialize())
	pushC5Body := body(execop.PUSHCTR(5).Serialize())
	pushC7LenBody := body(
		execop.PUSHCTR(7).Serialize(),
		tupleop.QTLEN().Serialize(),
	)

	type testCase struct {
		name  string
		code  *cell.Cell
		data  *cell.Cell
		c7    tuple.Tuple
		stack []any
		exit  int32
	}

	jumpDict := cell.NewDict(8)
	if err := jumpDict.SetIntKey(big.NewInt(3), body(intCode(55))); err != nil {
		t.Fatalf("failed to build DICTIGETJMPZ dict: %v", err)
	}

	tests := []testCase{
		{
			name: "if_ifnot_continue_after_taken_branches",
			code: body(
				stackop.PUSHCONT(body(intCode(10))).Serialize(),
				execop.IF().Serialize(),
				intCode(0),
				stackop.PUSHCONT(body(intCode(20))).Serialize(),
				execop.IFNOT().Serialize(),
				intCode(30),
			),
			stack: []any{int64(-1)},
			exit:  0,
		},
		{
			name: "ifjmp_skips_fallthrough",
			code: body(
				stackop.PUSHCONT(body(intCode(77))).Serialize(),
				execop.IFJMP().Serialize(),
				intCode(13),
			),
			stack: []any{int64(-1)},
			exit:  0,
		},
		{
			name: "ifnotjmp_false_skips_fallthrough",
			code: body(
				stackop.PUSHCONT(body(intCode(78))).Serialize(),
				execop.IFNOTJMP().Serialize(),
				intCode(13),
			),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name: "ifnotret_false_returns_before_tail",
			code: body(
				execop.IFNOTRET().Serialize(),
				intCode(13),
			),
			stack: []any{int64(99), int64(0)},
			exit:  0,
		},
		{
			name: "ifelse_true_branch_returns_to_tail",
			code: body(
				stackop.PUSHCONT(trueBranch).Serialize(),
				stackop.PUSHCONT(falseBranch).Serialize(),
				execop.IFELSE().Serialize(),
				intCode(33),
			),
			stack: []any{int64(-1)},
			exit:  0,
		},
		{
			name: "ifelse_false_branch_returns_to_tail",
			code: body(
				stackop.PUSHCONT(trueBranch).Serialize(),
				stackop.PUSHCONT(falseBranch).Serialize(),
				execop.IFELSE().Serialize(),
				intCode(33),
			),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name: "repeat_accumulates_three_iterations",
			code: body(
				stackop.PUSHCONT(incBody).Serialize(),
				execop.REPEAT().Serialize(),
				intCode(99),
			),
			stack: []any{int64(0), int64(3)},
			exit:  0,
		},
		{
			name: "repeat_zero_skips_body",
			code: body(
				stackop.PUSHCONT(incBody).Serialize(),
				execop.REPEAT().Serialize(),
				intCode(99),
			),
			stack: []any{int64(5), int64(0)},
			exit:  0,
		},
		{
			name:  "repeat_top_not_continuation_consumes_top_only",
			code:  body(execop.REPEAT().Serialize()),
			stack: []any{int64(3), int64(11)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "repeat_bad_count_consumes_body_and_count",
			code: body(
				stackop.PUSHCONT(incBody).Serialize(),
				execop.REPEAT().Serialize(),
			),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "repeat_count_range_consumes_body_and_count",
			code: body(
				stackop.PUSHCONT(incBody).Serialize(),
				execop.REPEAT().Serialize(),
			),
			stack: []any{int64(1 << 31)},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name: "repeatend_reuses_remaining_code_as_body",
			code: body(
				intCode(0),
				intCode(3),
				execop.REPEATEND().Serialize(),
				mathop.INC().Serialize(),
			),
			exit: 0,
		},
		{
			name: "until_runs_until_body_returns_true",
			code: body(
				stackop.PUSHCONT(untilBody).Serialize(),
				execop.UNTIL().Serialize(),
				intCode(99),
			),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name:  "until_top_not_continuation_consumes_top_only",
			code:  body(execop.UNTIL().Serialize()),
			stack: []any{int64(11), int64(22)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "untilend_reuses_remaining_code_as_body",
			code: body(
				intCode(0),
				execop.UNTILEND().Serialize(),
				mathop.INC().Serialize(),
				stackop.DUP().Serialize(),
				mathop.GTINT(2).Serialize(),
			),
			exit: 0,
		},
		{
			name: "while_runs_cond_then_body_until_false",
			code: body(
				stackop.PUSHCONT(whileCond).Serialize(),
				stackop.PUSHCONT(whileBody).Serialize(),
				execop.WHILE().Serialize(),
				intCode(99),
			),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name:  "while_top_not_continuation_consumes_top_only",
			code:  body(execop.WHILE().Serialize()),
			stack: []any{int64(11), int64(22)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "while_cond_not_continuation_consumes_both",
			code: body(
				stackop.PUSHCONT(whileBody).Serialize(),
				execop.WHILE().Serialize(),
			),
			stack: []any{int64(11)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "whileend_reuses_remaining_code_as_body",
			code: body(
				intCode(0),
				stackop.PUSHCONT(whileCond).Serialize(),
				execop.WHILEEND().Serialize(),
				mathop.INC().Serialize(),
			),
			exit: 0,
		},
		{
			name: "again_exits_through_alt_continuation",
			code: body(
				stackop.PUSHCONT(againBody).Serialize(),
				execop.AGAIN().Serialize(),
				intCode(99),
			),
			exit: 1,
		},
		{
			name: "try_catches_throwarg_and_continues",
			code: body(
				stackop.PUSHCONT(tryThrowArgBody).Serialize(),
				stackop.PUSHCONT(tryHandler).Serialize(),
				execop.TRY().Serialize(),
				intCode(7),
			),
			stack: []any{int64(888)},
			exit:  0,
		},
		{
			name: "tryargs_success_returns_selected_arg",
			code: body(
				stackop.PUSHCONT(trySuccessBody).Serialize(),
				stackop.PUSHCONT(tryHandler).Serialize(),
				execop.TRYARGS(1, 1).Serialize(),
				intCode(99),
			),
			stack: []any{int64(10)},
			exit:  0,
		},
		{
			name: "tryargs_max_params_short_stack_underflow",
			code: body(
				stackop.PUSHCONT(push7Body).Serialize(),
				stackop.PUSHCONT(tryHandler).Serialize(),
				execop.TRYARGS(15, 15).Serialize(),
			),
			stack: []any{int64(1), int64(2)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name: "nested_try_inner_caught_outer_continues",
			code: body(
				stackop.PUSHREFCONT(nestedTryInnerCaughtBody).Serialize(),
				stackop.PUSHCONT(body(intCode(0xCAFE))).Serialize(),
				execop.TRY().Serialize(),
				intCode(0x444),
			),
			stack: []any{int64(888)},
			exit:  0,
		},
		{
			name: "nested_try_inner_rethrow_outer_catches",
			code: body(
				stackop.PUSHREFCONT(nestedTryInnerRethrowBody).Serialize(),
				stackop.PUSHCONT(body(intCode(0xCAFE))).Serialize(),
				execop.TRY().Serialize(),
				intCode(0x444),
			),
			stack: []any{int64(888)},
			exit:  0,
		},
		{
			name: "nested_try_outer_rethrow_uncaught",
			code: body(
				stackop.PUSHREFCONT(nestedTryInnerRethrowBody).Serialize(),
				stackop.PUSHCONT(nestedTryOuterRethrowBody).Serialize(),
				execop.TRY().Serialize(),
				intCode(0x444),
			),
			stack: []any{int64(888)},
			exit:  0x2B,
		},
		{
			name: "callx_retargs_sum_then_tail",
			code: body(
				stackop.PUSHCONT(sumReturnBody).Serialize(),
				execop.CALLXARGS(2, 1).Serialize(),
				intCode(9),
			),
			stack: []any{int64(5), int64(6)},
			exit:  0,
		},
		{
			name: "callxvarargs_dynamic_params_and_returns",
			code: body(
				stackop.PUSHCONT(sumReturnBody).Serialize(),
				intCode(2),
				intCode(1),
				execop.CALLXVARARGS().Serialize(),
				intCode(9),
			),
			stack: []any{int64(5), int64(6)},
			exit:  0,
		},
		{
			name: "jmpxvarargs_dynamic_params",
			code: body(
				stackop.PUSHCONT(sumReturnBody).Serialize(),
				intCode(2),
				execop.JMPXVARARGS().Serialize(),
			),
			stack: []any{int64(5), int64(6)},
			exit:  0,
		},
		{
			name: "retvarargs_from_called_continuation",
			code: body(
				stackop.PUSHCONT(retVarArgsBody).Serialize(),
				execop.CALLXARGS(0, 1).Serialize(),
				intCode(66),
			),
			exit: 0,
		},
		{
			name: "returnargs_moves_excess_values_into_return_continuation",
			code: body(
				execop.RETURNARGS(1).Serialize(),
				intCode(44),
				execop.RET().Serialize(),
			),
			stack: []any{int64(11), int64(22), int64(33)},
			exit:  0,
		},
		{
			name: "returnvarargs_dynamic_count",
			code: body(
				intCode(1),
				execop.RETURNVARARGS().Serialize(),
				intCode(44),
				execop.RET().Serialize(),
			),
			stack: []any{int64(11), int64(22), int64(33)},
			exit:  0,
		},
		{
			name: "callcc_executes_captured_current_continuation",
			code: body(
				stackop.PUSHCONT(callCCBody).Serialize(),
				execop.CALLCC().Serialize(),
				intCode(8),
			),
			exit: 0,
		},
		{
			name: "callccargs_executes_captured_current_continuation",
			code: body(
				stackop.PUSHCONT(callCCArgsBody).Serialize(),
				execop.CALLCCARGS(1, 1).Serialize(),
				intCode(70),
			),
			stack: []any{int64(41)},
			exit:  0,
		},
		{
			name: "callccvarargs_dynamic_current_continuation",
			code: body(
				stackop.PUSHCONT(callCCArgsBody).Serialize(),
				intCode(1),
				intCode(1),
				execop.CALLCCVARARGS().Serialize(),
				intCode(70),
			),
			stack: []any{int64(41)},
			exit:  0,
		},
		{
			name: "control_pop_push_c4_cell",
			code: body(
				execop.POPCTR(4).Serialize(),
				execop.PUSHCTR(4).Serialize(),
			),
			stack: []any{cellA},
			exit:  0,
		},
		{
			name: "control_push_c0_execute",
			code: body(
				execop.PUSHCTR(0).Serialize(),
				execop.EXECUTE().Serialize(),
				intCode(13),
			),
			exit: 0,
		},
		{
			name: "control_push_c1_execute",
			code: body(
				execop.PUSHCTR(1).Serialize(),
				execop.EXECUTE().Serialize(),
				intCode(13),
			),
			exit: 1,
		},
		{
			name: "control_pop_push_c3_execute",
			code: body(
				stackop.PUSHCONT(body(intCode(0x33))).Serialize(),
				execop.POPCTR(3).Serialize(),
				execop.PUSHCTR(3).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "control_setcontctr_applies_when_executed",
			code: body(
				stackop.PUSHREF(cellB).Serialize(),
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.SETCONTCTR(4).Serialize(),
				stackop.PUSHREF(cellC).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "control_popctrx_pushctrx_c5_cell",
			code: body(
				intCode(5),
				execop.POPCTRX().Serialize(),
				intCode(5),
				execop.PUSHCTRX().Serialize(),
			),
			stack: []any{cellB},
			exit:  0,
		},
		{
			name: "control_push_c7_tuple_len",
			code: body(
				execop.PUSHCTR(7).Serialize(),
				tupleop.QTLEN().Serialize(),
			),
			c7:   nonEmptyC7,
			exit: 0,
		},
		{
			name: "control_popctrx_rejects_c6_gap",
			code: body(
				intCode(6),
				execop.POPCTRX().Serialize(),
			),
			stack: []any{cellB},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name: "control_popctrx_c0_bad_value_consumes_value_and_idx",
			code: body(
				intCode(0),
				execop.POPCTRX().Serialize(),
			),
			stack: []any{cellB},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "control_pushctrx_rejects_c8_gap",
			code: body(
				intCode(8),
				execop.PUSHCTRX().Serialize(),
			),
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "control_setcontctrx_rejects_c6_gap",
			code: body(
				stackop.PUSHREF(cellB).Serialize(),
				stackop.PUSHCONT(pushC4Body).Serialize(),
				intCode(6),
				execop.SETCONTCTRX().Serialize(),
			),
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "control_setcontctrx_bad_continuation_consumes_idx_and_bad_continuation",
			code: body(
				intCode(11),
				intCode(4),
				execop.SETCONTCTRX().Serialize(),
			),
			stack: []any{cellB},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "control_setcontctrx_bad_c4_value_consumes_all_inputs",
			code: body(
				stackop.PUSHCONT(pushC4Body).Serialize(),
				intCode(4),
				execop.SETCONTCTRX().Serialize(),
			),
			stack: []any{int64(11)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "control_setcontctr_c0_saved_continuation_executes",
			code: body(
				stackop.PUSHCONT(body(intCode(0x51))).Serialize(),
				stackop.PUSHCONT(body(execop.PUSHCTR(0).Serialize(), execop.EXECUTE().Serialize())).Serialize(),
				execop.SETCONTCTR(0).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "control_savectr_restores_on_return",
			code: body(
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.POPCTR(0).Serialize(),
				stackop.PUSHREF(cellB).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.SAVECTR(4).Serialize(),
				stackop.PUSHREF(cellC).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.RET().Serialize(),
			),
			exit: 0,
		},
		{
			name: "control_savectr_restores_c5_actions_on_return",
			code: body(
				stackop.PUSHCONT(pushC5Body).Serialize(),
				execop.POPCTR(0).Serialize(),
				stackop.PUSHREF(cellB).Serialize(),
				execop.POPCTR(5).Serialize(),
				execop.SAVECTR(5).Serialize(),
				stackop.PUSHREF(cellC).Serialize(),
				execop.POPCTR(5).Serialize(),
				execop.RET().Serialize(),
			),
			exit: 0,
		},
		{
			name: "control_savectr_restores_c7_tuple_on_return",
			code: body(
				stackop.PUSHCONT(pushC7LenBody).Serialize(),
				execop.POPCTR(0).Serialize(),
				execop.SAVECTR(7).Serialize(),
				execop.POPCTR(7).Serialize(),
				execop.RET().Serialize(),
			),
			c7:    nonEmptyC7,
			stack: []any{tuple.NewTupleValue(big.NewInt(99))},
			exit:  0,
		},
		{
			name: "control_savealtctr_restores_on_retalt",
			code: body(
				stackop.PUSHREF(cellB).Serialize(),
				execop.POPCTR(4).Serialize(),
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.POPCTR(1).Serialize(),
				execop.SAVEALTCTR(4).Serialize(),
				stackop.PUSHREF(cellC).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.RETALT().Serialize(),
			),
			exit: 0,
		},
		{
			name: "control_savebothctr_restores_on_return",
			code: body(
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.POPCTR(0).Serialize(),
				stackop.PUSHREF(cellB).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.SAVEBOTHCTR(4).Serialize(),
				stackop.PUSHREF(cellC).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.RET().Serialize(),
			),
			exit: 0,
		},
		{
			name: "control_setretctr_applies_on_return",
			code: body(
				stackop.PUSHCONT(setRetCtrBody).Serialize(),
				execop.CALLXARGS(1, 0).Serialize(),
				execop.PUSHCTR(4).Serialize(),
			),
			stack: []any{cellB},
			exit:  0,
		},
		{
			name: "control_popsavectr_restores_on_return",
			code: body(
				stackop.PUSHCONT(popSaveCtrBody).Serialize(),
				execop.CALLXARGS(1, 0).Serialize(),
				execop.PUSHCTR(4).Serialize(),
			),
			data:  cellA,
			stack: []any{cellB},
			exit:  0,
		},
		{
			name: "control_setaltctr_applies_on_retalt",
			code: body(
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.POPCTR(1).Serialize(),
				stackop.PUSHREF(cellB).Serialize(),
				execop.SETALTCTR(4).Serialize(),
				execop.RETALT().Serialize(),
			),
			exit: 0,
		},
		{
			name: "control_setcontctrx_applies_when_executed",
			code: body(
				stackop.PUSHCONT(pushC4Body).Serialize(),
				intCode(4),
				execop.SETCONTCTRX().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{cellC},
			exit:  0,
		},
		{
			name: "control_setcontctrmany_applies_when_executed",
			code: body(
				stackop.PUSHREF(cellB).Serialize(),
				execop.POPCTR(4).Serialize(),
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.SETCONTCTRMANY(1<<4).Serialize(),
				stackop.PUSHREF(cellC).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "control_setcontctrmanyx_applies_when_executed",
			code: body(
				stackop.PUSHREF(cellB).Serialize(),
				execop.POPCTR(4).Serialize(),
				stackop.PUSHCONT(pushC4Body).Serialize(),
				intCode(1<<4),
				execop.SETCONTCTRMANYX().Serialize(),
				stackop.PUSHREF(cellC).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "control_setcontctrmany_rejects_c6_mask",
			code: body(
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.SETCONTCTRMANY(1<<6).Serialize(),
			),
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "control_setcontctrmanyx_rejects_c6_mask",
			code: body(
				stackop.PUSHCONT(pushC4Body).Serialize(),
				intCode(1<<6),
				execop.SETCONTCTRMANYX().Serialize(),
			),
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "samealt_copies_return_continuation_to_alt",
			code: body(
				stackop.PUSHCONT(push7Body).Serialize(),
				execop.POPCTR(0).Serialize(),
				execop.SAMEALT().Serialize(),
				execop.RETALT().Serialize(),
			),
			exit: 0,
		},
		{
			name: "samealtsave_preserves_previous_alt_in_return_continuation",
			code: body(
				stackop.PUSHCONT(retAltBody).Serialize(),
				execop.POPCTR(0).Serialize(),
				stackop.PUSHCONT(push9Body).Serialize(),
				execop.POPCTR(1).Serialize(),
				execop.SAMEALTSAVE().Serialize(),
				execop.RET().Serialize(),
			),
			exit: 0,
		},
		{
			name: "boolor_composes_alt_continuation",
			code: body(
				stackop.PUSHCONT(retAltBody).Serialize(),
				stackop.PUSHCONT(push7Body).Serialize(),
				execop.BOOLOR().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name:  "boolor_top_not_continuation_consumes_top_only",
			code:  body(execop.BOOLOR().Serialize()),
			stack: []any{int64(11), int64(22)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "boolor_second_not_continuation_consumes_both",
			code: body(
				stackop.PUSHCONT(push7Body).Serialize(),
				execop.BOOLOR().Serialize(),
			),
			stack: []any{int64(11)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "ifnbitjmpref_true",
			code: body(
				execop.IFNBITJMPREF(1, push7Body).Serialize(),
				intCode(13),
			),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name:  "dictigetjmpz_hit_jumps_to_value_continuation",
			code:  body(execop.DICTIGETJMPZ().Serialize()),
			stack: []any{int64(3), jumpDict.AsCell(), int64(8)},
			exit:  0,
		},
		{
			name:  "dictigetjmpz_miss_keeps_key",
			code:  body(execop.DICTIGETJMPZ().Serialize()),
			stack: []any{int64(4), jumpDict.AsCell(), int64(8)},
			exit:  0,
		},
		{
			name:  "dictigetjmpz_bad_bits_consumes_bits_only",
			code:  body(execop.DICTIGETJMPZ().Serialize()),
			stack: []any{int64(11), testEmptyCell(), int64(1024)},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name:  "dictigetjmpz_bad_root_consumes_bits_and_root",
			code:  body(execop.DICTIGETJMPZ().Serialize()),
			stack: []any{int64(11), int64(99), int64(8)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "dictigetjmpz_bad_key_consumes_all_inputs",
			code:  body(execop.DICTIGETJMPZ().Serialize()),
			stack: []any{testEmptyCell(), testEmptyCell(), int64(8)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "dictigetjmpz_malformed_dict_consumes_all_inputs",
			code:  body(execop.DICTIGETJMPZ().Serialize()),
			stack: []any{int64(11), testEmptyCell(), int64(8)},
			exit:  vmerr.CodeCellUnderflow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := prependRawMethodDrop(tt.code)
			data := tt.data
			if data == nil {
				data = defaultData
			}

			goStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build go stack: %v", err)
			}
			refStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build reference stack: %v", err)
			}

			goRes, err := runGoCrossCode(code, data, tt.c7, goStack)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}
			refRes, err := runReferenceCrossCode(code, data, tt.c7, refStack)
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

type contExecScenarioVersionCase struct {
	name       string
	code       *cell.Cell
	stack      []any
	exit       int32
	minVersion int
}

func (tt contExecScenarioVersionCase) exitForVersion(version int) int32 {
	if tt.minVersion > 0 && version < tt.minVersion {
		return vmerr.CodeInvalidOpcode
	}
	return tt.exit
}

func TestTVMCrossEmulatorContExecScenariosAllGlobalVersionsSmoke(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := contExecScenarioVersionCases(t)
	versions := crossEmulatorVersionAuditVersions(t, "TVM_CONTEXEC_SCENARIO_VERSION_AUDIT")
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runContVersionedParityCase(t, tt.code, tt.stack, version, tt.exitForVersion(version), "", nil)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorContExecScenariosGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%contExecScenarioVersionCaseCount))
	}
	for i := 0; i < contExecScenarioVersionCaseCount; i++ {
		f.Add(uint8(MaxSupportedGlobalVersion), uint8(i))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := contExecScenarioVersionCases(t)
		if len(tests) != contExecScenarioVersionCaseCount {
			t.Fatalf("cont/exec scenario case count = %d, want %d", len(tests), contExecScenarioVersionCaseCount)
		}
		tt := tests[int(rawCase)%len(tests)]
		runContVersionedParityCase(t, tt.code, tt.stack, version, tt.exitForVersion(version), "", nil)
	})
}

const contExecScenarioVersionCaseCount = 71

func contExecScenarioVersionCases(t *testing.T) []contExecScenarioVersionCase {
	t.Helper()

	intCode := func(v int64) *cell.Builder {
		return stackop.PUSHINT(big.NewInt(v)).Serialize()
	}
	body := func(builders ...*cell.Builder) *cell.Cell {
		return codeFromBuilders(t, builders...)
	}

	cellA := cell.BeginCell().MustStoreUInt(0xA, 4).EndCell()
	cellB := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	cellC := cell.BeginCell().MustStoreUInt(0xC0DE, 16).EndCell()

	push7Body := body(intCode(7))
	push9Body := body(intCode(9))
	trueBranch := body(intCode(11))
	falseBranch := body(intCode(22))
	incBody := body(mathop.INC().Serialize())
	untilBody := body(
		mathop.INC().Serialize(),
		stackop.DUP().Serialize(),
		mathop.GTINT(2).Serialize(),
	)
	whileCond := body(
		stackop.DUP().Serialize(),
		mathop.LESSINT(4).Serialize(),
	)
	whileBody := body(mathop.INC().Serialize())
	againBody := body(
		intCode(44),
		execop.RETALT().Serialize(),
	)
	tryHandler := body(intCode(0xCAFE))
	tryThrowArgBody := body(
		intCode(321),
		cell.BeginCell().MustStoreUInt(0xF2C955, 24),
	)
	trySuccessBody := body(
		mathop.INC().Serialize(),
		execop.RETARGS(1).Serialize(),
	)
	nestedTryInnerCaughtBody := body(
		stackop.PUSHCONT(tryThrowArgBody).Serialize(),
		stackop.PUSHCONT(body(intCode(0x111))).Serialize(),
		execop.TRY().Serialize(),
		intCode(0x222),
	)
	nestedTryInnerRethrowBody := body(
		stackop.PUSHCONT(tryThrowArgBody).Serialize(),
		stackop.PUSHCONT(body(codeFromOpcodes(t, 0xF22A).ToBuilder())).Serialize(),
		execop.TRY().Serialize(),
		intCode(0x333),
	)
	nestedTryOuterRethrowBody := body(codeFromOpcodes(t, 0xF22B).ToBuilder())
	sumReturnBody := body(
		mathop.SUM().Serialize(),
		execop.RETARGS(1).Serialize(),
	)
	retVarArgsBody := body(
		intCode(123),
		intCode(1),
		execop.RETVARARGS().Serialize(),
	)
	callCCBody := body(
		intCode(42),
		stackop.SWAP().Serialize(),
		execop.EXECUTE().Serialize(),
	)
	callCCArgsBody := body(
		stackop.SWAP().Serialize(),
		mathop.INC().Serialize(),
		stackop.SWAP().Serialize(),
		execop.EXECUTE().Serialize(),
	)
	retAltBody := body(execop.RETALT().Serialize())
	setRetCtrBody := body(
		execop.SETRETCTR(4).Serialize(),
		execop.RET().Serialize(),
	)
	pushC4Body := body(execop.PUSHCTR(4).Serialize())
	pushC5Body := body(execop.PUSHCTR(5).Serialize())

	jumpDict := cell.NewDict(8)
	if err := jumpDict.SetIntKey(big.NewInt(3), body(intCode(55))); err != nil {
		t.Fatalf("failed to build DICTIGETJMPZ dict: %v", err)
	}

	tests := []contExecScenarioVersionCase{
		{
			name: "if_ifnot_continue_after_taken_branches",
			code: body(
				stackop.PUSHCONT(body(intCode(10))).Serialize(),
				execop.IF().Serialize(),
				intCode(0),
				stackop.PUSHCONT(body(intCode(20))).Serialize(),
				execop.IFNOT().Serialize(),
				intCode(30),
			),
			stack: []any{int64(-1)},
			exit:  0,
		},
		{
			name: "ifjmp_skips_fallthrough",
			code: body(
				stackop.PUSHCONT(body(intCode(77))).Serialize(),
				execop.IFJMP().Serialize(),
				intCode(13),
			),
			stack: []any{int64(-1)},
			exit:  0,
		},
		{
			name: "ifnotjmp_false_skips_fallthrough",
			code: body(
				stackop.PUSHCONT(body(intCode(78))).Serialize(),
				execop.IFNOTJMP().Serialize(),
				intCode(13),
			),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name: "ifnotret_false_returns_before_tail",
			code: body(
				execop.IFNOTRET().Serialize(),
				intCode(13),
			),
			stack: []any{int64(99), int64(0)},
			exit:  0,
		},
		{
			name: "ifelse_true_branch_returns_to_tail",
			code: body(
				stackop.PUSHCONT(trueBranch).Serialize(),
				stackop.PUSHCONT(falseBranch).Serialize(),
				execop.IFELSE().Serialize(),
				intCode(33),
			),
			stack: []any{int64(-1)},
			exit:  0,
		},
		{
			name: "ifelse_false_branch_returns_to_tail",
			code: body(
				stackop.PUSHCONT(trueBranch).Serialize(),
				stackop.PUSHCONT(falseBranch).Serialize(),
				execop.IFELSE().Serialize(),
				intCode(33),
			),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name: "repeat_accumulates_three_iterations",
			code: body(
				stackop.PUSHCONT(incBody).Serialize(),
				execop.REPEAT().Serialize(),
				intCode(99),
			),
			stack: []any{int64(0), int64(3)},
			exit:  0,
		},
		{
			name: "repeat_zero_skips_body",
			code: body(
				stackop.PUSHCONT(incBody).Serialize(),
				execop.REPEAT().Serialize(),
				intCode(99),
			),
			stack: []any{int64(5), int64(0)},
			exit:  0,
		},
		{
			name:  "repeat_top_not_continuation_consumes_top_only",
			code:  body(execop.REPEAT().Serialize()),
			stack: []any{int64(3), int64(11)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "repeat_bad_count_consumes_body_and_count",
			code: body(
				stackop.PUSHCONT(incBody).Serialize(),
				execop.REPEAT().Serialize(),
			),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "repeat_count_range_consumes_body_and_count",
			code: body(
				stackop.PUSHCONT(incBody).Serialize(),
				execop.REPEAT().Serialize(),
			),
			stack: []any{int64(1 << 31)},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name: "repeatend_reuses_remaining_code_as_body",
			code: body(
				intCode(0),
				intCode(3),
				execop.REPEATEND().Serialize(),
				mathop.INC().Serialize(),
			),
			exit: 0,
		},
		{
			name: "until_runs_until_body_returns_true",
			code: body(
				stackop.PUSHCONT(untilBody).Serialize(),
				execop.UNTIL().Serialize(),
				intCode(99),
			),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name:  "until_top_not_continuation_consumes_top_only",
			code:  body(execop.UNTIL().Serialize()),
			stack: []any{int64(11), int64(22)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "untilend_reuses_remaining_code_as_body",
			code: body(
				intCode(0),
				execop.UNTILEND().Serialize(),
				mathop.INC().Serialize(),
				stackop.DUP().Serialize(),
				mathop.GTINT(2).Serialize(),
			),
			exit: 0,
		},
		{
			name: "while_runs_cond_then_body_until_false",
			code: body(
				stackop.PUSHCONT(whileCond).Serialize(),
				stackop.PUSHCONT(whileBody).Serialize(),
				execop.WHILE().Serialize(),
				intCode(99),
			),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name:  "while_top_not_continuation_consumes_top_only",
			code:  body(execop.WHILE().Serialize()),
			stack: []any{int64(11), int64(22)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "while_cond_not_continuation_consumes_both",
			code: body(
				stackop.PUSHCONT(whileBody).Serialize(),
				execop.WHILE().Serialize(),
			),
			stack: []any{int64(11)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "whileend_reuses_remaining_code_as_body",
			code: body(
				intCode(0),
				stackop.PUSHCONT(whileCond).Serialize(),
				execop.WHILEEND().Serialize(),
				mathop.INC().Serialize(),
			),
			exit: 0,
		},
		{
			name: "again_exits_through_alt_continuation",
			code: body(
				stackop.PUSHCONT(againBody).Serialize(),
				execop.AGAIN().Serialize(),
				intCode(99),
			),
			exit: 1,
		},
		{
			name: "try_catches_throwarg_and_continues",
			code: body(
				stackop.PUSHCONT(tryThrowArgBody).Serialize(),
				stackop.PUSHCONT(tryHandler).Serialize(),
				execop.TRY().Serialize(),
				intCode(7),
			),
			stack: []any{int64(888)},
			exit:  0,
		},
		{
			name: "tryargs_success_returns_selected_arg",
			code: body(
				stackop.PUSHCONT(trySuccessBody).Serialize(),
				stackop.PUSHCONT(tryHandler).Serialize(),
				execop.TRYARGS(1, 1).Serialize(),
				intCode(99),
			),
			stack: []any{int64(10)},
			exit:  0,
		},
		{
			name: "tryargs_max_params_short_stack_underflow",
			code: body(
				stackop.PUSHCONT(push7Body).Serialize(),
				stackop.PUSHCONT(tryHandler).Serialize(),
				execop.TRYARGS(15, 15).Serialize(),
			),
			stack: []any{int64(1), int64(2)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name: "nested_try_inner_caught_outer_continues",
			code: body(
				stackop.PUSHREFCONT(nestedTryInnerCaughtBody).Serialize(),
				stackop.PUSHCONT(body(intCode(0xCAFE))).Serialize(),
				execop.TRY().Serialize(),
				intCode(0x444),
			),
			stack: []any{int64(888)},
			exit:  0,
		},
		{
			name: "nested_try_inner_rethrow_outer_catches",
			code: body(
				stackop.PUSHREFCONT(nestedTryInnerRethrowBody).Serialize(),
				stackop.PUSHCONT(body(intCode(0xCAFE))).Serialize(),
				execop.TRY().Serialize(),
				intCode(0x444),
			),
			stack: []any{int64(888)},
			exit:  0,
		},
		{
			name: "nested_try_outer_rethrow_uncaught",
			code: body(
				stackop.PUSHREFCONT(nestedTryInnerRethrowBody).Serialize(),
				stackop.PUSHCONT(nestedTryOuterRethrowBody).Serialize(),
				execop.TRY().Serialize(),
				intCode(0x444),
			),
			stack: []any{int64(888)},
			exit:  0x2B,
		},
		{
			name: "callx_retargs_sum_then_tail",
			code: body(
				stackop.PUSHCONT(sumReturnBody).Serialize(),
				execop.CALLXARGS(2, 1).Serialize(),
				intCode(9),
			),
			stack: []any{int64(5), int64(6)},
			exit:  0,
		},
		{
			name: "callxvarargs_dynamic_params_and_returns",
			code: body(
				stackop.PUSHCONT(sumReturnBody).Serialize(),
				intCode(2),
				intCode(1),
				execop.CALLXVARARGS().Serialize(),
				intCode(9),
			),
			stack: []any{int64(5), int64(6)},
			exit:  0,
		},
		{
			name: "jmpxvarargs_dynamic_params",
			code: body(
				stackop.PUSHCONT(sumReturnBody).Serialize(),
				intCode(2),
				execop.JMPXVARARGS().Serialize(),
			),
			stack: []any{int64(5), int64(6)},
			exit:  0,
		},
		{
			name: "retvarargs_from_called_continuation",
			code: body(
				stackop.PUSHCONT(retVarArgsBody).Serialize(),
				execop.CALLXARGS(0, 1).Serialize(),
				intCode(66),
			),
			exit: 0,
		},
		{
			name: "returnargs_moves_excess_values_into_return_continuation",
			code: body(
				execop.RETURNARGS(1).Serialize(),
				intCode(44),
				execop.RET().Serialize(),
			),
			stack: []any{int64(11), int64(22), int64(33)},
			exit:  0,
		},
		{
			name: "returnvarargs_dynamic_count",
			code: body(
				intCode(1),
				execop.RETURNVARARGS().Serialize(),
				intCode(44),
				execop.RET().Serialize(),
			),
			stack: []any{int64(11), int64(22), int64(33)},
			exit:  0,
		},
		{
			name: "callcc_executes_captured_current_continuation",
			code: body(
				stackop.PUSHCONT(callCCBody).Serialize(),
				execop.CALLCC().Serialize(),
				intCode(8),
			),
			exit: 0,
		},
		{
			name: "callccargs_executes_captured_current_continuation",
			code: body(
				stackop.PUSHCONT(callCCArgsBody).Serialize(),
				execop.CALLCCARGS(1, 1).Serialize(),
				intCode(70),
			),
			stack: []any{int64(41)},
			exit:  0,
		},
		{
			name: "callccvarargs_dynamic_current_continuation",
			code: body(
				stackop.PUSHCONT(callCCArgsBody).Serialize(),
				intCode(1),
				intCode(1),
				execop.CALLCCVARARGS().Serialize(),
				intCode(70),
			),
			stack: []any{int64(41)},
			exit:  0,
		},
		{
			name: "control_pop_push_c4_cell",
			code: body(
				execop.POPCTR(4).Serialize(),
				execop.PUSHCTR(4).Serialize(),
			),
			stack: []any{cellA},
			exit:  0,
		},
		{
			name: "control_push_c0_execute",
			code: body(
				execop.PUSHCTR(0).Serialize(),
				execop.EXECUTE().Serialize(),
				intCode(13),
			),
			exit: 0,
		},
		{
			name: "control_push_c1_execute",
			code: body(
				execop.PUSHCTR(1).Serialize(),
				execop.EXECUTE().Serialize(),
				intCode(13),
			),
			exit: 1,
		},
		{
			name: "control_pop_push_c3_execute",
			code: body(
				stackop.PUSHCONT(body(intCode(0x33))).Serialize(),
				execop.POPCTR(3).Serialize(),
				execop.PUSHCTR(3).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "control_setcontctr_applies_when_executed",
			code: body(
				stackop.PUSHREF(cellB).Serialize(),
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.SETCONTCTR(4).Serialize(),
				stackop.PUSHREF(cellC).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "control_popctrx_pushctrx_c5_cell",
			code: body(
				intCode(5),
				execop.POPCTRX().Serialize(),
				intCode(5),
				execop.PUSHCTRX().Serialize(),
			),
			stack: []any{cellB},
			exit:  0,
		},
		{
			name: "control_popctrx_rejects_c6_gap",
			code: body(
				intCode(6),
				execop.POPCTRX().Serialize(),
			),
			stack: []any{cellB},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name: "control_popctrx_c0_bad_value_consumes_value_and_idx",
			code: body(
				intCode(0),
				execop.POPCTRX().Serialize(),
			),
			stack: []any{cellB},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "control_pushctrx_rejects_c8_gap",
			code: body(
				intCode(8),
				execop.PUSHCTRX().Serialize(),
			),
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "control_setcontctrx_rejects_c6_gap",
			code: body(
				stackop.PUSHREF(cellB).Serialize(),
				stackop.PUSHCONT(pushC4Body).Serialize(),
				intCode(6),
				execop.SETCONTCTRX().Serialize(),
			),
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "control_setcontctrx_bad_continuation_consumes_idx_and_bad_continuation",
			code: body(
				intCode(11),
				intCode(4),
				execop.SETCONTCTRX().Serialize(),
			),
			stack: []any{cellB},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "control_setcontctrx_bad_c4_value_consumes_all_inputs",
			code: body(
				stackop.PUSHCONT(pushC4Body).Serialize(),
				intCode(4),
				execop.SETCONTCTRX().Serialize(),
			),
			stack: []any{int64(11)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "control_setcontctr_c0_saved_continuation_executes",
			code: body(
				stackop.PUSHCONT(body(intCode(0x51))).Serialize(),
				stackop.PUSHCONT(body(execop.PUSHCTR(0).Serialize(), execop.EXECUTE().Serialize())).Serialize(),
				execop.SETCONTCTR(0).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "control_savectr_restores_on_return",
			code: body(
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.POPCTR(0).Serialize(),
				stackop.PUSHREF(cellB).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.SAVECTR(4).Serialize(),
				stackop.PUSHREF(cellC).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.RET().Serialize(),
			),
			exit: 0,
		},
		{
			name: "control_savectr_restores_c5_actions_on_return",
			code: body(
				stackop.PUSHCONT(pushC5Body).Serialize(),
				execop.POPCTR(0).Serialize(),
				stackop.PUSHREF(cellB).Serialize(),
				execop.POPCTR(5).Serialize(),
				execop.SAVECTR(5).Serialize(),
				stackop.PUSHREF(cellC).Serialize(),
				execop.POPCTR(5).Serialize(),
				execop.RET().Serialize(),
			),
			exit: 0,
		},
		{
			name: "control_savealtctr_restores_on_retalt",
			code: body(
				stackop.PUSHREF(cellB).Serialize(),
				execop.POPCTR(4).Serialize(),
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.POPCTR(1).Serialize(),
				execop.SAVEALTCTR(4).Serialize(),
				stackop.PUSHREF(cellC).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.RETALT().Serialize(),
			),
			exit: 0,
		},
		{
			name: "control_savebothctr_restores_on_return",
			code: body(
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.POPCTR(0).Serialize(),
				stackop.PUSHREF(cellB).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.SAVEBOTHCTR(4).Serialize(),
				stackop.PUSHREF(cellC).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.RET().Serialize(),
			),
			exit: 0,
		},
		{
			name: "control_setretctr_applies_on_return",
			code: body(
				stackop.PUSHCONT(setRetCtrBody).Serialize(),
				execop.CALLXARGS(1, 0).Serialize(),
				execop.PUSHCTR(4).Serialize(),
			),
			stack: []any{cellB},
			exit:  0,
		},
		{
			name: "control_setaltctr_applies_on_retalt",
			code: body(
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.POPCTR(1).Serialize(),
				stackop.PUSHREF(cellB).Serialize(),
				execop.SETALTCTR(4).Serialize(),
				execop.RETALT().Serialize(),
			),
			exit: 0,
		},
		{
			name: "control_setcontctrx_applies_when_executed",
			code: body(
				stackop.PUSHCONT(pushC4Body).Serialize(),
				intCode(4),
				execop.SETCONTCTRX().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{cellC},
			exit:  0,
		},
		{
			name: "control_setcontctrmany_applies_when_executed",
			code: body(
				stackop.PUSHREF(cellB).Serialize(),
				execop.POPCTR(4).Serialize(),
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.SETCONTCTRMANY(1<<4).Serialize(),
				stackop.PUSHREF(cellC).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit:       0,
			minVersion: 9,
		},
		{
			name: "control_setcontctrmanyx_applies_when_executed",
			code: body(
				stackop.PUSHREF(cellB).Serialize(),
				execop.POPCTR(4).Serialize(),
				stackop.PUSHCONT(pushC4Body).Serialize(),
				intCode(1<<4),
				execop.SETCONTCTRMANYX().Serialize(),
				stackop.PUSHREF(cellC).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit:       0,
			minVersion: 9,
		},
		{
			name: "control_setcontctrmany_rejects_c6_mask",
			code: body(
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.SETCONTCTRMANY(1<<6).Serialize(),
			),
			exit:       vmerr.CodeRangeCheck,
			minVersion: 9,
		},
		{
			name: "control_setcontctrmanyx_rejects_c6_mask",
			code: body(
				stackop.PUSHCONT(pushC4Body).Serialize(),
				intCode(1<<6),
				execop.SETCONTCTRMANYX().Serialize(),
			),
			exit:       vmerr.CodeRangeCheck,
			minVersion: 9,
		},
		{
			name: "samealt_copies_return_continuation_to_alt",
			code: body(
				stackop.PUSHCONT(push7Body).Serialize(),
				execop.POPCTR(0).Serialize(),
				execop.SAMEALT().Serialize(),
				execop.RETALT().Serialize(),
			),
			exit: 0,
		},
		{
			name: "samealtsave_preserves_previous_alt_in_return_continuation",
			code: body(
				stackop.PUSHCONT(retAltBody).Serialize(),
				execop.POPCTR(0).Serialize(),
				stackop.PUSHCONT(push9Body).Serialize(),
				execop.POPCTR(1).Serialize(),
				execop.SAMEALTSAVE().Serialize(),
				execop.RET().Serialize(),
			),
			exit: 0,
		},
		{
			name: "boolor_composes_alt_continuation",
			code: body(
				stackop.PUSHCONT(retAltBody).Serialize(),
				stackop.PUSHCONT(push7Body).Serialize(),
				execop.BOOLOR().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name:  "boolor_top_not_continuation_consumes_top_only",
			code:  body(execop.BOOLOR().Serialize()),
			stack: []any{int64(11), int64(22)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "boolor_second_not_continuation_consumes_both",
			code: body(
				stackop.PUSHCONT(push7Body).Serialize(),
				execop.BOOLOR().Serialize(),
			),
			stack: []any{int64(11)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "ifnbitjmpref_true",
			code: body(
				execop.IFNBITJMPREF(1, push7Body).Serialize(),
				intCode(13),
			),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name:  "dictigetjmpz_hit_jumps_to_value_continuation",
			code:  body(execop.DICTIGETJMPZ().Serialize()),
			stack: []any{int64(3), jumpDict.AsCell(), int64(8)},
			exit:  0,
		},
		{
			name:  "dictigetjmpz_miss_keeps_key",
			code:  body(execop.DICTIGETJMPZ().Serialize()),
			stack: []any{int64(4), jumpDict.AsCell(), int64(8)},
			exit:  0,
		},
		{
			name:  "dictigetjmpz_bad_bits_consumes_bits_only",
			code:  body(execop.DICTIGETJMPZ().Serialize()),
			stack: []any{int64(11), testEmptyCell(), int64(1024)},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name:  "dictigetjmpz_bad_root_consumes_bits_and_root",
			code:  body(execop.DICTIGETJMPZ().Serialize()),
			stack: []any{int64(11), int64(99), int64(8)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "dictigetjmpz_bad_key_consumes_all_inputs",
			code:  body(execop.DICTIGETJMPZ().Serialize()),
			stack: []any{testEmptyCell(), testEmptyCell(), int64(8)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "dictigetjmpz_malformed_dict_consumes_all_inputs",
			code:  body(execop.DICTIGETJMPZ().Serialize()),
			stack: []any{int64(11), testEmptyCell(), int64(8)},
			exit:  vmerr.CodeCellUnderflow,
		},
	}

	return tests
}
