package tvm

import (
	"math/big"
	"testing"

	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestContOpsGoSemantics(t *testing.T) {
	t.Run("RETFromNestedCall", func(t *testing.T) {
		body := codeFromBuilders(t, execop.RET().Serialize())
		code := codeFromBuilders(
			t,
			stackop.PUSHCONT(body).Serialize(),
			execop.EXECUTE().Serialize(),
			stackop.PUSHINT(big.NewInt(7)).Serialize(),
		)

		stack, res, err := runRawCode(code)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		got, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("pop result: %v", err)
		}
		if got.Int64() != 7 {
			t.Fatalf("expected 7, got %s", got.String())
		}
	})

	t.Run("CALLXArgsTrimsReturnValues", func(t *testing.T) {
		body := codeFromBuilders(t)
		code := codeFromBuilders(
			t,
			stackop.PUSHCONT(body).Serialize(),
			execop.CALLXARGS(2, 1).Serialize(),
		)

		stack, res, err := runRawCode(code, int64(11), int64(22))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack
		if stack.Len() != 1 {
			t.Fatalf("expected one returned value, got %d", stack.Len())
		}

		got, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("pop result: %v", err)
		}
		if got.Int64() != 22 {
			t.Fatalf("expected 22, got %s", got.String())
		}
	})

	t.Run("CALLXArgsPReturnsAllValues", func(t *testing.T) {
		body := codeFromBuilders(t)
		code := codeFromBuilders(
			t,
			stackop.PUSHCONT(body).Serialize(),
			execop.CALLXARGSP(1).Serialize(),
		)

		stack, res, err := runRawCode(code, int64(55))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		got, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("pop result: %v", err)
		}
		if got.Int64() != 55 {
			t.Fatalf("expected 55, got %s", got.String())
		}
	})

	t.Run("CALLXVarArgsUsesDynamicCounts", func(t *testing.T) {
		body := codeFromBuilders(t)
		code := codeFromBuilders(
			t,
			stackop.PUSHCONT(body).Serialize(),
			stackop.XCHG0(2).Serialize(),
			stackop.XCHG0(1).Serialize(),
			execop.CALLXVARARGS().Serialize(),
		)

		stack, res, err := runRawCode(code, int64(11), int64(22), int64(2), int64(1))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack
		if stack.Len() != 1 {
			t.Fatalf("expected one returned value, got %d", stack.Len())
		}

		got, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("pop result: %v", err)
		}
		if got.Int64() != 22 {
			t.Fatalf("expected 22, got %s", got.String())
		}
	})

	t.Run("RETVARARGSTrimsReturnValues", func(t *testing.T) {
		stack, res, err := runRawCode(codeFromBuilders(t, execop.RETVARARGS().Serialize()), int64(11), int64(22), int64(1))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		got, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("pop result: %v", err)
		}
		if got.Int64() != 22 {
			t.Fatalf("expected 22, got %s", got.String())
		}
	})

	t.Run("CALLREFAndJMPREFDATA", func(t *testing.T) {
		callBody := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize())
		callStack, res, err := runRawCode(codeFromBuilders(t, execop.CALLREF(callBody).Serialize()))
		if err != nil {
			t.Fatalf("callref unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("callref unexpected exit code: %d", res.ExitCode)
		}
		callStack = res.Stack
		got, err := callStack.PopIntFinite()
		if err != nil {
			t.Fatalf("callref pop result: %v", err)
		}
		if got.Int64() != 7 {
			t.Fatalf("callref expected 7, got %s", got.String())
		}

		jumpBody := codeFromBuilders(t, stackop.DROP().Serialize(), stackop.PUSHINT(big.NewInt(9)).Serialize())
		jumpStack, res, err := runRawCode(codeFromBuilders(t, execop.JMPREFDATA(jumpBody).Serialize()))
		if err != nil {
			t.Fatalf("jmprefdata unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("jmprefdata unexpected exit code: %d", res.ExitCode)
		}
		jumpStack = res.Stack
		got, err = jumpStack.PopIntFinite()
		if err != nil {
			t.Fatalf("jmprefdata pop result: %v", err)
		}
		if got.Int64() != 9 {
			t.Fatalf("jmprefdata expected 9, got %s", got.String())
		}
	})

	t.Run("IFREFELSEAndIFREFELSEREF", func(t *testing.T) {
		trueBody := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize())
		falseBody := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(9)).Serialize())

		stack, res, err := runRawCode(
			codeFromBuilders(t, stackop.PUSHCONT(falseBody).Serialize(), execop.IFREFELSE(trueBody).Serialize()),
			int64(0),
		)
		if err != nil {
			t.Fatalf("ifrefelse unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("ifrefelse unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack
		got, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("ifrefelse pop result: %v", err)
		}
		if got.Int64() != 9 {
			t.Fatalf("ifrefelse expected 9, got %s", got.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, execop.IFREFELSEREF(trueBody, falseBody).Serialize()), int64(-1))
		if err != nil {
			t.Fatalf("ifrefelseref unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("ifrefelseref unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack
		got, err = stack.PopIntFinite()
		if err != nil {
			t.Fatalf("ifrefelseref pop result: %v", err)
		}
		if got.Int64() != 7 {
			t.Fatalf("ifrefelseref expected 7, got %s", got.String())
		}
	})

	t.Run("AgainAndBreakLoops", func(t *testing.T) {
		retAltBody := codeFromBuilders(t, execop.RETALT().Serialize())

		_, res, err := runRawCode(codeFromBuilders(t, stackop.PUSHCONT(retAltBody).Serialize(), execop.AGAIN().Serialize()))
		if code := exitCodeFromResult(res, err); code != 1 {
			t.Fatalf("again expected exit 1, got %d err=%v", code, err)
		}

		_, res, err = runRawCode(codeFromBuilders(t, execop.AGAINEND().Serialize(), execop.RETALT().Serialize()))
		if code := exitCodeFromResult(res, err); code != 1 {
			t.Fatalf("againend expected exit 1, got %d err=%v", code, err)
		}

		breakCode := codeFromBuilders(t, stackop.PUSHCONT(retAltBody).Serialize(), execop.REPEATBRK().Serialize(), stackop.PUSHINT(big.NewInt(7)).Serialize())
		stack, res, err := runRawCode(breakCode, int64(3))
		if err != nil {
			t.Fatalf("repeatbrk unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("repeatbrk unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack
		got, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("repeatbrk pop result: %v", err)
		}
		if got.Int64() != 7 {
			t.Fatalf("repeatbrk expected 7, got %s", got.String())
		}

		stack, res, err = runRawCode(breakCode, int64(0))
		if err != nil {
			t.Fatalf("repeatbrk zero unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("repeatbrk zero unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack
		got, err = stack.PopIntFinite()
		if err != nil {
			t.Fatalf("repeatbrk zero pop result: %v", err)
		}
		if got.Int64() != 7 {
			t.Fatalf("repeatbrk zero expected 7, got %s", got.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, stackop.PUSHCONT(retAltBody).Serialize(), execop.UNTILBRK().Serialize(), stackop.PUSHINT(big.NewInt(8)).Serialize()))
		if err != nil {
			t.Fatalf("untilbrk unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("untilbrk unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack
		got, err = stack.PopIntFinite()
		if err != nil {
			t.Fatalf("untilbrk pop result: %v", err)
		}
		if got.Int64() != 8 {
			t.Fatalf("untilbrk expected 8, got %s", got.String())
		}

		condBody := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(1)).Serialize())
		stack, res, err = runRawCode(
			codeFromBuilders(
				t,
				stackop.PUSHCONT(condBody).Serialize(),
				stackop.PUSHCONT(retAltBody).Serialize(),
				execop.WHILEBRK().Serialize(),
				stackop.PUSHINT(big.NewInt(9)).Serialize(),
			),
		)
		if err != nil {
			t.Fatalf("whilebrk unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("whilebrk unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack
		got, err = stack.PopIntFinite()
		if err != nil {
			t.Fatalf("whilebrk pop result: %v", err)
		}
		if got.Int64() != 9 {
			t.Fatalf("whilebrk expected 9, got %s", got.String())
		}
	})

	t.Run("CALLREFWithoutRefFailsWithInvalidOpcode", func(t *testing.T) {
		_, res, err := runRawCode(codeFromOpcodes(t, 0xDB3C))
		if code := exitCodeFromResult(res, err); code != vmerr.CodeInvalidOpcode {
			t.Fatalf("unexpected exit code: %d err=%v", code, err)
		}
	})

	t.Run("RETARGSAndIFNOTRETALT", func(t *testing.T) {
		body := codeFromBuilders(t, execop.RETARGS(1).Serialize())
		stack, res, err := runRawCode(
			codeFromBuilders(t, stackop.PUSHCONT(body).Serialize(), execop.EXECUTE().Serialize()),
			int64(11), int64(22),
		)
		if err != nil {
			t.Fatalf("retargs unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("retargs unexpected exit code: %d", res.ExitCode)
		}
		got, err := res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("retargs pop result: %v", err)
		}
		if got.Int64() != 22 {
			t.Fatalf("retargs expected 22, got %s", got.String())
		}

		_, res, err = runRawCode(codeFromBuilders(t, execop.IFNOTRETALT().Serialize()), int64(0))
		if code := exitCodeFromResult(res, err); code != 1 {
			t.Fatalf("ifnotretalt expected exit 1, got %d err=%v", code, err)
		}
		_, res, err = runRawCode(codeFromBuilders(t, execop.IFNOTRETALT().Serialize(), stackop.PUSHINT(big.NewInt(9)).Serialize()), int64(-1))
		if err != nil {
			t.Fatalf("ifnotretalt continue unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("ifnotretalt continue unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack
		got, err = stack.PopIntFinite()
		if err != nil {
			t.Fatalf("ifnotretalt continue pop result: %v", err)
		}
		if got.Int64() != 9 {
			t.Fatalf("ifnotretalt continue expected 9, got %s", got.String())
		}
	})

	t.Run("IFBITJMPFamilies", func(t *testing.T) {
		dropThen7 := codeFromBuilders(t, stackop.DROP().Serialize(), stackop.PUSHINT(big.NewInt(7)).Serialize())

		stack, res, err := runRawCode(
			codeFromBuilders(t, stackop.PUSHCONT(dropThen7).Serialize(), execop.IFBITJMP(1).Serialize()),
			int64(2),
		)
		_ = stack
		if err != nil {
			t.Fatalf("ifbitjmp unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("ifbitjmp unexpected exit code: %d", res.ExitCode)
		}
		got, err := res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("ifbitjmp pop result: %v", err)
		}
		if got.Int64() != 7 {
			t.Fatalf("ifbitjmp expected 7, got %s", got.String())
		}

		stack, res, err = runRawCode(
			codeFromBuilders(t, stackop.PUSHCONT(dropThen7).Serialize(), execop.IFNBITJMP(1).Serialize()),
			int64(0),
		)
		if err != nil {
			t.Fatalf("ifnbitjmp unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("ifnbitjmp unexpected exit code: %d", res.ExitCode)
		}
		got, err = res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("ifnbitjmp pop result: %v", err)
		}
		if got.Int64() != 7 {
			t.Fatalf("ifnbitjmp expected 7, got %s", got.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, execop.IFBITJMPREF(1, dropThen7).Serialize()), int64(2))
		if err != nil {
			t.Fatalf("ifbitjmpref unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("ifbitjmpref unexpected exit code: %d", res.ExitCode)
		}
		got, err = res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("ifbitjmpref pop result: %v", err)
		}
		if got.Int64() != 7 {
			t.Fatalf("ifbitjmpref expected 7, got %s", got.String())
		}
	})

	t.Run("SetContArgsAndReturnArgs", func(t *testing.T) {
		emptyBody := codeFromBuilders(t)

		stack, res, err := runRawCode(
			codeFromBuilders(t, stackop.PUSHCONT(emptyBody).Serialize(), execop.SETCONTARGS(1, 0).Serialize(), execop.EXECUTE().Serialize()),
			int64(55),
		)
		_ = stack
		if err != nil {
			t.Fatalf("setcontargs unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("setcontargs unexpected exit code: %d", res.ExitCode)
		}
		got, err := res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("setcontargs pop result: %v", err)
		}
		if got.Int64() != 55 {
			t.Fatalf("setcontargs expected 55, got %s", got.String())
		}

		stack, res, err = runRawCode(
			codeFromBuilders(
				t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				execop.SETCONTVARARGS().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			int64(66),
		)
		if err != nil {
			t.Fatalf("setcontvarargs unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("setcontvarargs unexpected exit code: %d", res.ExitCode)
		}
		got, err = res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("setcontvarargs pop result: %v", err)
		}
		if got.Int64() != 66 {
			t.Fatalf("setcontvarargs expected 66, got %s", got.String())
		}

		stack, res, err = runRawCode(
			codeFromBuilders(
				t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				execop.SETNUMVARARGS().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			int64(77),
		)
		if err != nil {
			t.Fatalf("setnumvarargs unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("setnumvarargs unexpected exit code: %d", res.ExitCode)
		}
		got, err = res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("setnumvarargs pop result: %v", err)
		}
		if got.Int64() != 77 {
			t.Fatalf("setnumvarargs expected 77, got %s", got.String())
		}

		body := codeFromBuilders(t, execop.RETURNARGS(1).Serialize())
		stack, res, err = runRawCode(
			codeFromBuilders(t, stackop.PUSHCONT(body).Serialize(), execop.EXECUTE().Serialize()),
			int64(11), int64(22),
		)
		if err != nil {
			t.Fatalf("returnargs unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("returnargs unexpected exit code: %d", res.ExitCode)
		}
		got, err = res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("returnargs pop top result: %v", err)
		}
		if got.Int64() != 22 {
			t.Fatalf("returnargs expected top 22, got %s", got.String())
		}
		got, err = res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("returnargs pop preserved result: %v", err)
		}
		if got.Int64() != 11 {
			t.Fatalf("returnargs expected preserved 11, got %s", got.String())
		}

		body = codeFromBuilders(t, execop.RETURNVARARGS().Serialize())
		stack, res, err = runRawCode(
			codeFromBuilders(t, stackop.PUSHCONT(body).Serialize(), execop.EXECUTE().Serialize()),
			int64(11), int64(22), int64(1),
		)
		if err != nil {
			t.Fatalf("returnvarargs unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("returnvarargs unexpected exit code: %d", res.ExitCode)
		}
		got, err = res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("returnvarargs pop top result: %v", err)
		}
		if got.Int64() != 22 {
			t.Fatalf("returnvarargs expected top 22, got %s", got.String())
		}
		got, err = res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("returnvarargs pop preserved result: %v", err)
		}
		if got.Int64() != 11 {
			t.Fatalf("returnvarargs expected preserved 11, got %s", got.String())
		}
	})

	t.Run("BlessFamilies", func(t *testing.T) {
		bodyCell := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize())
		bodySlice := bodyCell.BeginParse()

		stack, res, err := runRawCode(codeFromBuilders(t, execop.BLESS().Serialize(), execop.EXECUTE().Serialize()), bodySlice)
		_ = stack
		if err != nil {
			t.Fatalf("bless unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("bless unexpected exit code: %d", res.ExitCode)
		}
		got, err := res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("bless pop result: %v", err)
		}
		if got.Int64() != 7 {
			t.Fatalf("bless expected 7, got %s", got.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, execop.BLESSARGS(1, 0).Serialize(), execop.EXECUTE().Serialize()), int64(55), bodyCell.BeginParse())
		if err != nil {
			t.Fatalf("blessargs unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("blessargs unexpected exit code: %d", res.ExitCode)
		}
			got, err = res.Stack.PopIntFinite()
			if err != nil {
				t.Fatalf("blessargs pop result: %v", err)
			}
			if got.Int64() != 7 {
				t.Fatalf("blessargs expected top value 7, got %s", got.String())
			}
			got, err = res.Stack.PopIntFinite()
			if err != nil {
				t.Fatalf("blessargs pop captured arg: %v", err)
			}
			if got.Int64() != 55 {
				t.Fatalf("blessargs expected captured arg 55, got %s", got.String())
			}

		stack, res, err = runRawCode(
			codeFromBuilders(
				t,
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				execop.BLESSVARARGS().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			int64(66), bodyCell.BeginParse(),
		)
		if err != nil {
			t.Fatalf("blessvarargs unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("blessvarargs unexpected exit code: %d", res.ExitCode)
		}
			got, err = res.Stack.PopIntFinite()
			if err != nil {
				t.Fatalf("blessvarargs pop result: %v", err)
			}
			if got.Int64() != 7 {
				t.Fatalf("blessvarargs expected top value 7, got %s", got.String())
			}
			got, err = res.Stack.PopIntFinite()
			if err != nil {
				t.Fatalf("blessvarargs pop captured arg: %v", err)
			}
			if got.Int64() != 66 {
				t.Fatalf("blessvarargs expected captured arg 66, got %s", got.String())
			}
	})

	t.Run("CompositionAndExitOps", func(t *testing.T) {
		push7 := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize())
		emptyBody := codeFromBuilders(t)
		retAltBody := codeFromBuilders(t, execop.RETALT().Serialize())

		stack, res, err := runRawCode(codeFromBuilders(t, stackop.PUSHCONT(push7).Serialize(), execop.ATEXIT().Serialize()))
		_ = stack
		if err != nil {
			t.Fatalf("atexit unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("atexit unexpected exit code: %d", res.ExitCode)
		}
		got, err := res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("atexit pop result: %v", err)
		}
		if got.Int64() != 7 {
			t.Fatalf("atexit expected 7, got %s", got.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, stackop.PUSHCONT(push7).Serialize(), execop.ATEXITALT().Serialize(), execop.RETALT().Serialize()))
		if err != nil {
			t.Fatalf("atexitalt unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("atexitalt unexpected exit code: %d", res.ExitCode)
		}
		got, err = res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("atexitalt pop result: %v", err)
		}
		if got.Int64() != 7 {
			t.Fatalf("atexitalt expected 7, got %s", got.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, stackop.PUSHCONT(push7).Serialize(), execop.SETEXITALT().Serialize(), execop.RETALT().Serialize()))
		if err != nil {
			t.Fatalf("setexitalt unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("setexitalt unexpected exit code: %d", res.ExitCode)
		}
		got, err = res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("setexitalt pop result: %v", err)
		}
		if got.Int64() != 7 {
			t.Fatalf("setexitalt expected 7, got %s", got.String())
		}

		stack, res, err = runRawCode(
			codeFromBuilders(
				t,
				stackop.PUSHCONT(push7).Serialize(),
				execop.ATEXIT().Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.THENRET().Serialize(),
				execop.EXECUTE().Serialize(),
			),
		)
		if err != nil {
			t.Fatalf("thenret unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("thenret unexpected exit code: %d", res.ExitCode)
		}
		got, err = res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("thenret pop result: %v", err)
		}
		if got.Int64() != 7 {
			t.Fatalf("thenret expected 7, got %s", got.String())
		}

		stack, res, err = runRawCode(
			codeFromBuilders(
				t,
				stackop.PUSHCONT(push7).Serialize(),
				execop.POPCTR(1).Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.THENRETALT().Serialize(),
				execop.EXECUTE().Serialize(),
			),
		)
		if err != nil {
			t.Fatalf("thenretalt unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("thenretalt unexpected exit code: %d", res.ExitCode)
		}
		got, err = res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("thenretalt pop result: %v", err)
		}
		if got.Int64() != 7 {
			t.Fatalf("thenretalt expected 7, got %s", got.String())
		}

		_, res, err = runRawCode(codeFromBuilders(t, execop.INVERT().Serialize(), execop.RET().Serialize()))
		if code := exitCodeFromResult(res, err); code != 1 {
			t.Fatalf("invert expected exit 1, got %d err=%v", code, err)
		}

		stack, res, err = runRawCode(codeFromBuilders(t, stackop.PUSHCONT(execop.RET().Serialize().EndCell()).Serialize(), execop.BOOLEVAL().Serialize()))
		if err == nil && res != nil && res.ExitCode == 0 {
			got, err = res.Stack.PopIntFinite()
			if err != nil {
				t.Fatalf("booleval pop result: %v", err)
			}
			if got.Int64() != -1 {
				t.Fatalf("booleval expected -1, got %s", got.String())
			}
		} else {
			t.Fatalf("booleval unexpected result: res=%#v err=%v", res, err)
		}

		stack, res, err = runRawCode(codeFromBuilders(t, stackop.PUSHCONT(emptyBody).Serialize(), stackop.PUSHCONT(push7).Serialize(), execop.BOOLAND().Serialize(), execop.EXECUTE().Serialize()))
		if err != nil {
			t.Fatalf("booland unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("booland unexpected exit code: %d", res.ExitCode)
		}
		got, err = res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("booland pop result: %v", err)
		}
		if got.Int64() != 7 {
			t.Fatalf("booland expected 7, got %s", got.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, stackop.PUSHCONT(retAltBody).Serialize(), stackop.PUSHCONT(push7).Serialize(), execop.COMPOSBOTH().Serialize(), execop.EXECUTE().Serialize()))
		if err != nil {
			t.Fatalf("composboth unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("composboth unexpected exit code: %d", res.ExitCode)
		}
		got, err = res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("composboth pop result: %v", err)
		}
		if got.Int64() != 7 {
			t.Fatalf("composboth expected 7, got %s", got.String())
		}
	})

	t.Run("SetContCtrManyAndDictJumpOps", func(t *testing.T) {
		push7 := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize())
		retAltBody := codeFromBuilders(t, execop.RETALT().Serialize())
		emptyBody := codeFromBuilders(t)

		stack, res, err := runRawCode(
			codeFromBuilders(
				t,
				stackop.PUSHCONT(push7).Serialize(),
				execop.POPCTR(1).Serialize(),
				stackop.PUSHCONT(retAltBody).Serialize(),
				execop.SETCONTCTRMANY(1<<1).Serialize(),
				execop.EXECUTE().Serialize(),
			),
		)
		_ = stack
		if err != nil {
			t.Fatalf("setcontctrmany unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("setcontctrmany unexpected exit code: %d", res.ExitCode)
		}
		got, err := res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("setcontctrmany pop result: %v", err)
		}
		if got.Int64() != 7 {
			t.Fatalf("setcontctrmany expected 7, got %s", got.String())
		}

		stack, res, err = runRawCode(
			codeFromBuilders(
				t,
				stackop.PUSHCONT(push7).Serialize(),
				execop.POPCTR(1).Serialize(),
				stackop.PUSHCONT(retAltBody).Serialize(),
				stackop.PUSHINT(big.NewInt(1<<1)).Serialize(),
				execop.SETCONTCTRMANYX().Serialize(),
				execop.EXECUTE().Serialize(),
			),
		)
		if err != nil {
			t.Fatalf("setcontctrmanyx unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("setcontctrmanyx unexpected exit code: %d", res.ExitCode)
		}
		got, err = res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("setcontctrmanyx pop result: %v", err)
		}
		if got.Int64() != 7 {
			t.Fatalf("setcontctrmanyx expected 7, got %s", got.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, stackop.PUSHCONT(emptyBody).Serialize(), execop.POPCTR(3).Serialize(), execop.CALLDICT(5).Serialize()))
		if err != nil {
			t.Fatalf("calldict short unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("calldict short unexpected exit code: %d", res.ExitCode)
		}
		got, err = res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("calldict short pop result: %v", err)
		}
		if got.Int64() != 5 {
			t.Fatalf("calldict short expected 5, got %s", got.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, stackop.PUSHCONT(emptyBody).Serialize(), execop.POPCTR(3).Serialize(), execop.CALLDICT(300).Serialize()))
		if err != nil {
			t.Fatalf("calldict long unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("calldict long unexpected exit code: %d", res.ExitCode)
		}
		got, err = res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("calldict long pop result: %v", err)
		}
		if got.Int64() != 300 {
			t.Fatalf("calldict long expected 300, got %s", got.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, stackop.PUSHCONT(emptyBody).Serialize(), execop.POPCTR(3).Serialize(), execop.JMPDICT(8).Serialize()))
		if err != nil {
			t.Fatalf("jmpdict unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("jmpdict unexpected exit code: %d", res.ExitCode)
		}
		got, err = res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("jmpdict pop result: %v", err)
		}
		if got.Int64() != 8 {
			t.Fatalf("jmpdict expected 8, got %s", got.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, stackop.PUSHCONT(emptyBody).Serialize(), execop.POPCTR(3).Serialize(), execop.PREPAREDICT(9).Serialize(), execop.EXECUTE().Serialize()))
		if err != nil {
			t.Fatalf("preparedict unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("preparedict unexpected exit code: %d", res.ExitCode)
		}
		got, err = res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("preparedict pop result: %v", err)
		}
		if got.Int64() != 9 {
			t.Fatalf("preparedict expected 9, got %s", got.String())
		}
	})

	t.Run("BreakEndLoopVariants", func(t *testing.T) {
		_, res, err := runRawCode(codeFromBuilders(t, execop.REPEATENDBRK().Serialize(), execop.RETALT().Serialize()), int64(3))
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("repeatendbrk expected exit 0, got %d err=%v", code, err)
		}
		_, res, err = runRawCode(codeFromBuilders(t, execop.UNTILENDBRK().Serialize(), execop.RETALT().Serialize()))
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("untilendbrk expected exit 0, got %d err=%v", code, err)
		}
		condBody := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(1)).Serialize())
		_, res, err = runRawCode(codeFromBuilders(t, stackop.PUSHCONT(condBody).Serialize(), execop.WHILEENDBRK().Serialize(), execop.RETALT().Serialize()))
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("whileendbrk expected exit 0, got %d err=%v", code, err)
		}
		_, res, err = runRawCode(codeFromBuilders(t, stackop.PUSHCONT(codeFromBuilders(t, execop.RETALT().Serialize())).Serialize(), execop.AGAINBRK().Serialize()))
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("againbrk expected exit 0, got %d err=%v", code, err)
		}
		_, res, err = runRawCode(codeFromBuilders(t, execop.AGAINENDBRK().Serialize(), execop.RETALT().Serialize()))
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("againendbrk expected exit 0, got %d err=%v", code, err)
		}
	})
}
