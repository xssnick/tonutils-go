package vmerr

import (
	"fmt"
)

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
	return e
}

type codeError interface {
	VMCode() int64
}

// ErrorCode walks the unwrap chain manually: unlike errors.As it does not box
// a target on every call, which matters on the VM hot path where quit
// continuations report exit codes through errors.
func ErrorCode(err error) (int64, bool) {
	for err != nil {
		if coded, ok := err.(codeError); ok {
			return coded.VMCode(), true
		}
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			err = x.Unwrap()
		case interface{ Unwrap() []error }:
			for _, sub := range x.Unwrap() {
				if code, ok := ErrorCode(sub); ok {
					return code, true
				}
			}
			return 0, false
		default:
			return 0, false
		}
	}
	return 0, false
}

// AsVMError is an allocation-free errors.As for the VMError concrete type.
func AsVMError(err error) (VMError, bool) {
	for err != nil {
		if e, ok := err.(VMError); ok {
			return e, true
		}
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			err = x.Unwrap()
		case interface{ Unwrap() []error }:
			for _, sub := range x.Unwrap() {
				if e, ok := AsVMError(sub); ok {
					return e, true
				}
			}
			return VMError{}, false
		default:
			return VMError{}, false
		}
	}
	return VMError{}, false
}

// AsVirtualization is an allocation-free errors.As for VirtualizationError.
func AsVirtualization(err error) (VirtualizationError, bool) {
	for err != nil {
		if e, ok := err.(VirtualizationError); ok {
			return e, true
		}
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			err = x.Unwrap()
		case interface{ Unwrap() []error }:
			for _, sub := range x.Unwrap() {
				if e, ok := AsVirtualization(sub); ok {
					return e, true
				}
			}
			return VirtualizationError{}, false
		default:
			return VirtualizationError{}, false
		}
	}
	return VirtualizationError{}, false
}

// boxedErrors keeps pre-boxed default-message errors for small codes, so hot
// paths (every quit continuation jump, gas checks) do not re-box a VMError
// into the error interface on each return.
var boxedErrors = makeBoxedErrors()

func makeBoxedErrors() [16]error {
	var out [16]error
	for code := range out {
		out[code] = VMError{Code: int64(code), Msg: defaultErrorMsg(int64(code))}
	}
	return out
}

// Err returns an error for code with the default message. For small codes it
// returns a shared pre-boxed value.
func Err(code int64) error {
	if code >= 0 && code < int64(len(boxedErrors)) {
		return boxedErrors[code]
	}
	return Error(code)
}

func Error(code int64, msg ...string) VMError {
	e := VMError{
		Code: code,
	}

	if len(msg) == 0 {
		e.Msg = defaultErrorMsg(code)
	} else {
		e.Msg = msg[0]
	}

	return e
}

func defaultErrorMsg(code int64) string {
	switch code {
	case CodeStackUnderflow:
		return "stack underflow"
	case CodeStackOverflow:
		return "stack overflow"
	case CodeIntOverflow:
		return "integer overflow"
	case CodeRangeCheck:
		return "integer out of range"
	case CodeInvalidOpcode:
		return "invalid opcode"
	case CodeTypeCheck:
		return "type check error"
	case CodeCellOverflow:
		return "cell overflow"
	case CodeCellUnderflow:
		return "cell underflow"
	case CodeDict:
		return "dictionary error"
	case CodeUnknown:
		return "unknown error"
	case CodeFatal:
		return "fatal error"
	}
	return ""
}
