//go:build cgo && tvm_cross_emulator

package tvm

import (
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	dictop "github.com/xssnick/tonutils-go/tvm/op/dict"
)

// PrefixDictionary::lookup_prefix in the reference validates the complete
// HmLabel before comparing it with a possibly shorter input. A truncated
// explicit label must therefore raise cell underflow instead of becoming a
// quiet prefix miss.
func TestTVMCrossEmulatorPrefixDictTruncatedLabelParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	roots := []struct {
		name    string
		root    *cell.Cell
		keyBits int64
	}{
		{
			name:    "short",
			root:    cell.BeginCell().MustStoreUInt(0b010, 3).EndCell(), // hml_short len=1, payload absent
			keyBits: 1,
		},
		{
			name:    "long",
			root:    cell.BeginCell().MustStoreUInt(0b10101, 5).EndCell(), // hml_long len=5, payload absent
			keyBits: 5,
		},
	}
	ops := []struct {
		name string
		code string
	}{
		{name: "getq", code: "30F4A8"},
		{name: "get", code: "30F4A9"},
		{name: "getjmp", code: "30F4AA"},
		{name: "getexec", code: "30F4AB"},
	}

	for _, root := range roots {
		for _, op := range ops {
			t.Run(root.name+"-"+op.name, func(t *testing.T) {
				input := cell.BeginCell().EndCell().MustBeginParse()
				assertFlowParityCase(t, "prefix-dict-truncated-label", rawCodeCellFromHex(t, op.code),
					[]any{input, root.root, root.keyBits}, 13, 1_000_000)
			})
		}
	}

	t.Run("switch-long", func(t *testing.T) {
		input := cell.BeginCell().EndCell().MustBeginParse()
		code := prependRawMethodDrop(codeFromBuilders(t, dictop.PFXDICTSWITCH(roots[1].root, uint64(roots[1].keyBits)).Serialize()))
		assertFlowParityCase(t, "prefix-dict-switch-truncated-label", code, []any{input}, 13, 1_000_000)
	})
}
