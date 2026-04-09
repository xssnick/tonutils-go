package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func recoverMainPanic(t *testing.T) any {
	t.Helper()

	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		main()
	}()
	return recovered
}

func runMainWithCodeAndLogs(t *testing.T, code *cell.Cell) (any, string) {
	t.Helper()

	origCode := MainContractCode
	MainContractCode = code
	defer func() {
		MainContractCode = origCode
	}()

	origWriter := log.Writer()
	origFlags := log.Flags()
	origPrefix := log.Prefix()

	var logs bytes.Buffer
	log.SetOutput(&logs)
	log.SetFlags(0)
	log.SetPrefix("")
	defer func() {
		log.SetOutput(origWriter)
		log.SetFlags(origFlags)
		log.SetPrefix(origPrefix)
	}()

	return recoverMainPanic(t), logs.String()
}

func testRunMethodParams(t *testing.T, code *cell.Cell) RunMethodParams {
	t.Helper()

	stack := vm.NewStack()
	if err := stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("seed stack: %v", err)
	}

	tlbStack, err := tlb.NewStackFromVM(stack)
	if err != nil {
		t.Fatalf("NewStackFromVM: %v", err)
	}
	stackCell, err := tlbStack.ToCell()
	if err != nil {
		t.Fatalf("stack ToCell: %v", err)
	}

	c7tuple, err := PrepareC7(
		address.MustParseAddr("EQAYqo4u7VF0fa4DPAebk4g9lBytj2VFny7pzXR0trjtXQaO"),
		time.Unix(123, 0),
		make([]byte, 32),
		big.NewInt(10_000_000),
		nil,
		code,
	)
	if err != nil {
		t.Fatalf("PrepareC7: %v", err)
	}

	c7stack := tlb.NewStack()
	c7stack.Push(c7tuple)
	c7cell, err := c7stack.ToCell()
	if err != nil {
		t.Fatalf("c7 ToCell: %v", err)
	}

	return RunMethodParams{
		Code:  code,
		Data:  cell.BeginCell().EndCell(),
		Stack: stackCell,
		Params: MethodConfig{
			C7:   c7cell,
			Libs: cell.BeginCell().EndCell(),
		},
		MethodID: int32(tlb.MethodNameHash("simpleRepeatWhile")),
	}
}

func codeFromBuilders(t *testing.T, builders ...*cell.Builder) *cell.Cell {
	t.Helper()

	code := cell.BeginCell()
	for _, builder := range builders {
		if err := code.StoreBuilder(builder); err != nil {
			t.Fatalf("failed to build code cell: %v", err)
		}
	}
	return code.EndCell()
}

func rawCodeCellFromHex(t *testing.T, src string) *cell.Cell {
	t.Helper()

	data, err := hex.DecodeString(src)
	if err != nil {
		t.Fatalf("decode hex failed: %v", err)
	}
	return cell.BeginCell().MustStoreSlice(data, uint(len(data))*8).EndCell()
}

func TestMainPanicsWhenRunGetMethodSetupIsInvalid(t *testing.T) {
	recovered, _ := runMainWithCodeAndLogs(t, nil)
	if recovered != 3 {
		t.Fatalf("main panic = %v, want 3", recovered)
	}
}

func TestMainPanicsWhenNativeResultStackContainsContinuation(t *testing.T) {
	code := codeFromBuilders(t, stackop.PUSHCONT(cell.BeginCell().EndCell()).Serialize())

	recovered, logs := runMainWithCodeAndLogs(t, code)
	if recovered != 4 {
		t.Fatalf("main panic = %v, want 4", recovered)
	}
	if logs != "" {
		t.Fatalf("expected panic before Go log output, got logs: %q", logs)
	}
}

func TestRunGetMethodReturnsErrorWhenNativeEmulatorRejectsRequest(t *testing.T) {
	code := rawCodeCellFromHex(t, "72E5ED40DB3603")

	_, err := RunGetMethod(testRunMethodParams(t, code), 1_000_000_000)
	if err == nil {
		t.Fatal("RunGetMethod should fail when native emulator returns nil")
	}
	if !strings.Contains(err.Error(), "failed to execute tvm, req:") {
		t.Fatalf("unexpected RunGetMethod error: %v", err)
	}
}

func TestMainPanicsWhenGoExecutionFailsAfterNativeSuccess(t *testing.T) {
	code := rawCodeCellFromHex(t, "90787FDB3B")

	recovered, logs := runMainWithCodeAndLogs(t, code)
	if !strings.Contains(logs, "C CALL COMPLETED") {
		t.Fatalf("expected native execution to complete, got logs: %q", logs)
	}
	if strings.Contains(logs, "GO CALL COMPLETED") {
		t.Fatalf("expected Go execution to fail before GO completion log, got logs: %q", logs)
	}
	if !strings.Contains(fmt.Sprint(recovered), "Code: 2 Text:stack underflow") {
		t.Fatalf("unexpected main panic: %v", recovered)
	}
}

func TestMainLogsMismatchWhenGoAndNativeStacksDiffer(t *testing.T) {
	code := rawCodeCellFromHex(t, "778B04216D73F43E018B04591277F473")

	recovered, logs := runMainWithCodeAndLogs(t, code)
	if recovered != nil {
		t.Fatalf("main panicked: %v", recovered)
	}
	if !strings.Contains(logs, "C CALL COMPLETED") || !strings.Contains(logs, "GO CALL COMPLETED") {
		t.Fatalf("expected both executions to complete, got logs: %q", logs)
	}
	if !strings.Contains(logs, "CPP\n") || !strings.Contains(logs, "\nGO\n") {
		t.Fatalf("expected mismatch logs, got: %q", logs)
	}
	if strings.Contains(logs, "OK, SAME") {
		t.Fatalf("expected mismatch path, got logs: %q", logs)
	}
}
