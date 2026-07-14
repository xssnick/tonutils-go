package funcs

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestVarIntAndAddressErrorTextParity(t *testing.T) {
	empty := cell.BeginCell().ToSlice()
	short := cell.BeginCell().MustStoreUInt(1, 1).ToSlice()
	extAddr, ext := mustExtAddrSlice(t)
	trailing := cell.BeginCell().MustStoreAddr(extAddr).MustStoreUInt(1, 1).ToSlice()

	tests := []struct {
		name  string
		op    vm.OP
		stack []any
		msg   string
	}{
		{name: "LDVARINT16", op: LDVARINT16(), stack: []any{empty}, msg: "cannot deserialize a variable-length integer"},
		{name: "LDVARUINT32", op: LDVARUINT32(), stack: []any{empty}, msg: "cannot deserialize a variable-length integer"},
		{name: "LDVARINT32", op: LDVARINT32(), stack: []any{empty}, msg: "cannot deserialize a variable-length integer"},
		{name: "LDMSGADDR", op: LDMSGADDR(), stack: []any{short}, msg: "cannot load a MsgAddress"},
		{name: "LDSTDADDR", op: LDSTDADDR(), stack: []any{ext}, msg: "cannot load a MsgAddressInt"},
		{name: "LDOPTSTDADDR short tag", op: LDOPTSTDADDR(), stack: []any{short}, msg: "cell underflow"},
		{name: "LDOPTSTDADDR invalid kind", op: LDOPTSTDADDR(), stack: []any{ext}, msg: "cannot load a MsgAddressInt"},
		{name: "REWRITESTDADDR malformed", op: REWRITESTDADDR(), stack: []any{short}, msg: "cannot parse a MsgAddress"},
		{name: "REWRITESTDADDR trailing data", op: REWRITESTDADDR(), stack: []any{trailing}, msg: "cannot parse a MsgAddress"},
		{name: "REWRITESTDADDR invalid kind", op: REWRITESTDADDR(), stack: []any{ext}, msg: "cannot parse a MsgAddressInt"},
		{name: "REWRITEVARADDR malformed", op: REWRITEVARADDR(), stack: []any{short}, msg: "cannot parse a MsgAddress"},
		{name: "REWRITEVARADDR invalid kind", op: REWRITEVARADDR(), stack: []any{ext}, msg: "cannot parse a MsgAddressInt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newFuncTestState(t, nil)
			pushErrorTextStack(t, st, tt.stack...)

			assertCPPErrorMessage(t, tt.op.Interpret(st), vmerr.CodeCellUnderflow, tt.msg)
		})
	}
}

func TestRistrettoErrorTextParity(t *testing.T) {
	invalidPoint := new(big.Int).SetBytes(bytes.Repeat([]byte{0xFF}, 32))
	zero := big.NewInt(0)
	one := big.NewInt(1)

	tests := []struct {
		name    string
		op      vm.OP
		stack   []any
		version int
		msg     string
	}{
		{name: "VALIDATE NaN", op: RIST255_VALIDATE(), stack: []any{vm.NaN{}}, version: 13, msg: "x is not a valid encoded element"},
		{name: "VALIDATE malformed point", op: RIST255_VALIDATE(), stack: []any{invalidPoint}, version: 13, msg: "x is not a valid encoded element"},
		{name: "ADD NaN x", op: RIST255_ADD(), stack: []any{vm.NaN{}, zero}, version: 13, msg: "x and/or y are not valid encoded elements"},
		{name: "ADD malformed y", op: RIST255_ADD(), stack: []any{zero, invalidPoint}, version: 13, msg: "x and/or y are not valid encoded elements"},
		{name: "SUB malformed x", op: RIST255_SUB(), stack: []any{invalidPoint, zero}, version: 13, msg: "x and/or y are not valid encoded elements"},
		{name: "SUB NaN y", op: RIST255_SUB(), stack: []any{zero, vm.NaN{}}, version: 13, msg: "x and/or y are not valid encoded elements"},
		{name: "MUL malformed point v13", op: RIST255_MUL(), stack: []any{invalidPoint, one}, version: 13, msg: "invalid x or n"},
		{name: "MUL zero scalar validates point v14", op: RIST255_MUL(), stack: []any{invalidPoint, zero}, version: 14, msg: "invalid x or n"},
		{name: "MUL NaN x", op: RIST255_MUL(), stack: []any{vm.NaN{}, one}, version: 14, msg: "invalid x or n"},
		{name: "MUL NaN n", op: RIST255_MUL(), stack: []any{zero, vm.NaN{}}, version: 14, msg: "invalid x or n"},
		{name: "MULBASE NaN v13", op: RIST255_MULBASE(), stack: []any{vm.NaN{}}, version: 13, msg: "invalid n"},
		{name: "MULBASE NaN v14", op: RIST255_MULBASE(), stack: []any{vm.NaN{}}, version: 14, msg: "invalid n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newFuncTestState(t, nil)
			st.GlobalVersion = tt.version
			pushErrorTextStack(t, st, tt.stack...)

			assertCPPErrorMessage(t, tt.op.Interpret(st), vmerr.CodeRangeCheck, tt.msg)
		})
	}
}

func pushErrorTextStack(t *testing.T, st *vm.State, values ...any) {
	t.Helper()

	for _, value := range values {
		switch v := value.(type) {
		case *big.Int:
			value = new(big.Int).Set(v)
		case *cell.Slice:
			value = v.Copy()
		}
		if err := st.Stack.PushAny(value); err != nil {
			t.Fatalf("failed to push %T: %v", value, err)
		}
	}
}

func assertCPPErrorMessage(t *testing.T, err error, code int64, msg string) {
	t.Helper()

	vmErr, ok := vmerr.AsVMError(err)
	if !ok {
		t.Fatalf("error = %v, want VMError", err)
	}
	if vmErr.Code != code {
		t.Fatalf("error code = %d, want %d", vmErr.Code, code)
	}
	if vmErr.Msg != msg {
		t.Fatalf("error message = %q, want %q", vmErr.Msg, msg)
	}
}
