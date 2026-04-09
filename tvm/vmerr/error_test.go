package vmerr

import (
	"strings"
	"testing"
)

func TestErrorDefaultMessages(t *testing.T) {
	tests := []struct {
		code int64
		msg  string
	}{
		{CodeStackUnderflow, "stack underflow"},
		{CodeStackOverflow, "stack overflow"},
		{CodeIntOverflow, "integer overflow"},
		{CodeRangeCheck, "range check failed"},
		{CodeInvalidOpcode, "invalid opcode"},
		{CodeTypeCheck, "type check failed"},
		{CodeCellOverflow, "cell overflow"},
		{CodeCellUnderflow, "cell underflow"},
		{CodeDict, "dictionary error"},
	}

	prev := TVMTraceEnabled
	TVMTraceEnabled = false
	defer func() {
		TVMTraceEnabled = prev
	}()

	for _, tt := range tests {
		err := Error(tt.code)
		if err.Code != tt.code {
			t.Fatalf("code = %d, want %d", err.Code, tt.code)
		}
		if err.Msg != tt.msg {
			t.Fatalf("message for code %d = %q, want %q", tt.code, err.Msg, tt.msg)
		}
		if err.trace != "" {
			t.Fatalf("trace should be empty when disabled, got %q", err.trace)
		}
	}
}

func TestErrorCustomMessageAndFormatting(t *testing.T) {
	prev := TVMTraceEnabled
	defer func() {
		TVMTraceEnabled = prev
	}()

	TVMTraceEnabled = false
	custom := Error(CodeFatal, "boom")
	if custom.Msg != "boom" {
		t.Fatalf("custom message = %q, want boom", custom.Msg)
	}

	formatted := custom.Error()
	if !strings.Contains(formatted, "Code: 12") || !strings.Contains(formatted, "Text:boom") {
		t.Fatalf("formatted error missing expected content: %q", formatted)
	}

	TVMTraceEnabled = true
	withTrace := Error(CodeOutOfGas, "no gas")
	if withTrace.trace == "" {
		t.Fatal("expected stack trace when TVM trace is enabled")
	}
	if !strings.Contains(withTrace.Error(), "no gas") {
		t.Fatalf("formatted traced error missing message: %q", withTrace.Error())
	}
}
