package funcs

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

// The dictionary walk distinguishes two malformed-config failures: an
// unparsable node label is a cell underflow (9), while a shape-violating fork
// is a dictionary error (10). A malformed config root crashes the reference
// emulator at setup, so this parity is pinned here instead of in the
// cross-emulator suite.
func TestConfigDictMalformedRootErrorCodes(t *testing.T) {
	// hml_short with a unary length of 40 > 32 key bits: unparsable label.
	badLabel := cell.BeginCell().MustStoreUInt(0, 1)
	for i := 0; i < 40; i++ {
		badLabel.MustStoreUInt(1, 1)
	}
	badLabel.MustStoreUInt(0, 1)

	// Valid empty label but a fork carrying junk bits and a single ref.
	badFork := cell.BeginCell().
		MustStoreUInt(0b00, 2).
		MustStoreUInt(0b1111, 4).
		MustStoreRef(cell.BeginCell().MustStoreUInt(0b00, 2).EndCell()).
		EndCell()

	for _, tc := range []struct {
		name string
		root *cell.Cell
		want int64
	}{
		{name: "unparsable label", root: badLabel.EndCell(), want: vmerr.CodeCellUnderflow},
		{name: "malformed fork", root: badFork, want: vmerr.CodeDict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := newFuncTestState(t, map[int]any{9: tc.root})
			if err := st.Stack.PushSmallInt(19); err != nil {
				t.Fatalf("push index: %v", err)
			}
			err := CONFIGPARAM().Interpret(st)
			code, ok := vmerr.ErrorCode(err)
			if !ok || code != tc.want {
				t.Fatalf("CONFIGPARAM error = %v (code %d), want %d", err, code, tc.want)
			}
		})
	}
}
