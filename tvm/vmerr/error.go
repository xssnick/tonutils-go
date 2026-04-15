package vmerr

import (
	"errors"
	"fmt"
	"runtime/debug"
)

var TVMTraceEnabled = true

type VMError struct {
	Code  int64
	Msg   string
	trace string
}

func (e VMError) Error() string {
	return "[VMError] Code: " + fmt.Sprint(e.Code) + " Text:" + e.Msg + "\n" + e.trace
}

func (e VMError) VMCode() int64 {
	return e.Code
}

const (
	CodeStackUnderflow = 2
	CodeStackOverflow  = 3
	CodeIntOverflow    = 4
	CodeRangeCheck     = 5
	CodeInvalidOpcode  = 6
	CodeTypeCheck      = 7
	CodeCellOverflow   = 8
	CodeCellUnderflow  = 9
	CodeDict           = 10
	CodeUnknown        = 11
	CodeFatal          = 12
	CodeOutOfGas       = 13
	CodeVirtualization = 14
)

type VirtualizationError struct {
	Virtualization int
	Msg            string
	trace          string
}

func (e VirtualizationError) Error() string {
	return "[VMVirtError] Code: " + fmt.Sprint(CodeVirtualization) + " Text:" + e.Msg + "\n" + e.trace
}

func (e VirtualizationError) VMCode() int64 {
	return CodeVirtualization
}

func Virtualization(virtualization int, msg ...string) VirtualizationError {
	e := VirtualizationError{
		Virtualization: virtualization,
		Msg:            "pruned branch",
	}
	if len(msg) > 0 && msg[0] != "" {
		e.Msg = msg[0]
	}
	if TVMTraceEnabled {
		e.trace = string(debug.Stack())
	}
	return e
}

type codeError interface {
	VMCode() int64
}

func ErrorCode(err error) (int64, bool) {
	var coded codeError
	if errors.As(err, &coded) {
		return coded.VMCode(), true
	}
	return 0, false
}

func Error(code int64, msg ...string) VMError {
	e := VMError{
		Code: code,
	}

	if len(msg) == 0 {
		switch code {
		case CodeStackUnderflow:
			e.Msg = "stack underflow"
		case CodeStackOverflow:
			e.Msg = "stack overflow"
		case CodeIntOverflow:
			e.Msg = "integer overflow"
		case CodeRangeCheck:
			e.Msg = "range check failed"
		case CodeInvalidOpcode:
			e.Msg = "invalid opcode"
		case CodeTypeCheck:
			e.Msg = "type check failed"
		case CodeCellOverflow:
			e.Msg = "cell overflow"
		case CodeCellUnderflow:
			e.Msg = "cell underflow"
		case CodeDict:
			e.Msg = "dictionary error"
		}
	} else {
		e.Msg = msg[0]
	}

	if TVMTraceEnabled {
		e.trace = string(debug.Stack())
	}

	return e
}
