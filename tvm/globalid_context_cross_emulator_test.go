//go:build cgo && tvm_cross_emulator

package tvm

import (
	"fmt"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorGlobalIDUnpackedContext(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library unavailable: %v", err)
	}
	code := prependRawMethodDrop(codeFromBuilders(t, funcsop.GLOBALID().Serialize()))
	for _, version := range []int{6, 16} {
		for _, tc := range []struct {
			name  string
			value any
			exit  int32
		}{
			{name: "signed", value: cell.BeginCell().MustStoreInt(-239, 32).ToSlice()},
			{name: "minimum", value: cell.BeginCell().MustStoreInt(-2147483648, 32).ToSlice()},
			{name: "maximum", value: cell.BeginCell().MustStoreUInt(2147483647, 32).ToSlice()},
			{name: "missing", exit: vmerr.CodeTypeCheck},
			{name: "short", value: cell.BeginCell().MustStoreUInt(0, 31).ToSlice(), exit: vmerr.CodeCellUnderflow},
		} {
			t.Run(fmt.Sprintf("v%d/%s", version, tc.name), func(t *testing.T) {
				c7 := makeTonopsTestC7(t, tonopsTestC7Config{UnpackedConfig: tuple.NewTupleValue(nil, tc.value)})
				goRes, err := runGoCrossCodeWithVersion(code, cell.BeginCell().EndCell(), c7, vm.NewStack(), version)
				if err != nil {
					t.Fatal(err)
				}
				// The raw native runner uses its modern version. GLOBALID's
				// unpacked-config behavior is unchanged from v6 onward.
				ref, err := runReferenceCrossCode(code, cell.BeginCell().EndCell(), c7, vm.NewStack())
				if err != nil {
					t.Fatal(err)
				}
				if goRes.exitCode != tc.exit || ref.exitCode != tc.exit || goRes.gasUsed != ref.gasUsed {
					t.Fatalf("Go exit/gas=%d/%d, reference=%d/%d, want exit=%d", goRes.exitCode, goRes.gasUsed, ref.exitCode, ref.gasUsed, tc.exit)
				}
				goStack, err := normalizeStackCell(goRes.stack)
				if err != nil {
					t.Fatal(err)
				}
				refStack, err := normalizeStackCell(ref.stack)
				if err != nil {
					t.Fatal(err)
				}
				if goStack.HashKey() != refStack.HashKey() {
					t.Fatal("GLOBALID result stack differs from native reference")
				}
			})
		}
	}
}
