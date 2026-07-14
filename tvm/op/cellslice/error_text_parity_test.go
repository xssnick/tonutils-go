package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestLDGRAMSErrorTextParity(t *testing.T) {
	st := newCellSliceState()
	pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(1, 4).EndCell().MustBeginParse())

	err := LDGRAMS().Interpret(st)
	vmErr, ok := vmerr.AsVMError(err)
	if !ok {
		t.Fatalf("error = %v, want VMError", err)
	}
	if vmErr.Code != vmerr.CodeCellUnderflow {
		t.Fatalf("error code = %d, want %d", vmErr.Code, vmerr.CodeCellUnderflow)
	}
	const want = "cannot deserialize a variable-length integer"
	if vmErr.Msg != want {
		t.Fatalf("error message = %q, want %q", vmErr.Msg, want)
	}
}
