package vmerr

import "fmt"

type VMError struct {
	Code int64
	Msg  string
}

func (v VMError) Error() string {
	return "[VMError] Code: " + fmt.Sprint(v.Code) + " Text:" + v.Msg
}

var ErrStackUnderflow = VMError{
	Code: 2,
	Msg:  "stack underflow",
}

var ErrStackOverflow = VMError{
	Code: 3,
	Msg:  "stack overflow",
}

var ErrIntOverflow = VMError{
	Code: 4,
	Msg:  "integer overflow",
}

var ErrRangeCheck = VMError{
	Code: 5,
	Msg:  "range check failed",
}

var ErrInvalidOpcode = VMError{
	Code: 6,
	Msg:  "invalid opcode",
}

var ErrTypeCheck = VMError{
	Code: 7,
	Msg:  "type check failed",
}

var ErrCellOverflow = VMError{
	Code: 8,
	Msg:  "cell overflow",
}

var ErrCellUnderflow = VMError{
	Code: 9,
	Msg:  "cell underflow",
}

var ErrDict = VMError{
	Code: 10,
	Msg:  "dictionary error",
}

var ErrUnknown = VMError{
	Code: 11,
	Msg:  "unknown",
}

var ErrFatal = VMError{
	Code: 12,
	Msg:  "fatal",
}

var ErrOutOfGas = VMError{
	Code: 13,
	Msg:  "out of gas",
}

var ErrVirtualization = VMError{
	Code: 14,
	Msg:  "virtualization error",
}
