//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorSignatureAllowanceAfterCaughtPrecheck(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skip(err)
	}

	fullSignature := cell.BeginCell().MustStoreSlice(make([]byte, 64), 512).ToSlice()
	shortSignature := cell.BeginCell().MustStoreSlice(make([]byte, 63), 504).ToSlice()
	data := cell.BeginCell().ToSlice()
	failed := codeFromBuilders(t,
		stackop.PUSHSLICEINLINE(data).Serialize(),
		stackop.PUSHREFSLICE(shortSignature).Serialize(),
		stackop.PUSHINT(big.NewInt(0)).Serialize(),
		funcsop.CHKSIGNS().Serialize(),
	)
	handler := codeFromBuilders(t, stackop.NIP().Serialize()) // keep exception code
	unchecked := codeFromBuilders(t,
		stackop.PUSHINT(big.NewInt(0)).Serialize(),
		stackop.PUSHREFSLICE(fullSignature).Serialize(),
		stackop.PUSHINT(big.NewInt(0)).Serialize(),
		funcsop.CHKSIGNU().Serialize(),
		stackop.DROP().Serialize(),
	)

	// A short signature throws before registering the call. The following
	// CHKSIGNS is therefore the tenth shared CHKSIGNU/CHKSIGNS discounted call;
	// erroneously registering the failure would exhaust this gas limit. With
	// ten previous checks, CHKSIGNS must instead charge 4000 and run out of gas.
	for _, previous := range []int64{9, 10} {
		code := codeFromBuilders(t,
			stackop.PUSHINT(big.NewInt(previous)).Serialize(),
			stackop.PUSHREFCONT(unchecked).Serialize(),
			execop.REPEAT().Serialize(),
			stackop.PUSHREFCONT(failed).Serialize(),
			stackop.PUSHREFCONT(handler).Serialize(),
			execop.TRY().Serialize(),
			stackop.PUSHSLICEINLINE(data).Serialize(),
			stackop.PUSHREFSLICE(fullSignature).Serialize(),
			stackop.PUSHINT(big.NewInt(0)).Serialize(),
			funcsop.CHKSIGNS().Serialize(),
		)
		for _, version := range []int{4, 13, 14} {
			t.Run(fmt.Sprintf("v%d/previous%d", version, previous), func(t *testing.T) {
				exit := int32(0)
				want := []any{int64(9), int64(0)}
				if previous == 10 {
					exit = ^int32(vmerr.CodeOutOfGas)
					want = nil
				}
				c7 := prepareCrossTestC7WithConfigRoot(tonopsCrossConfigWithGlobalVersion(t, uint32(version)), code)
				assertTonopsBoundaryParity(t, code, c7, version, 2500, exit, want)
			})
		}
	}
}

func TestTVMCrossEmulatorExtraBalanceAllowanceAfterCaughtDecode(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skip(err)
	}

	extra := cell.NewDict(32)
	if err := extra.SetIntKey(big.NewInt(0), cell.BeginCell().MustStoreUInt(0, 4).EndCell()); err != nil {
		t.Fatal(err)
	}
	for i := uint(0); i < 32; i++ {
		if err := extra.SetIntKey(new(big.Int).Lsh(big.NewInt(1), i), cell.BeginCell().MustStoreVarUInt(1, 32).EndCell()); err != nil {
			t.Fatal(err)
		}
	}
	failed := codeFromBuilders(t,
		stackop.PUSHINT(big.NewInt(0)).Serialize(),
		funcsop.GETEXTRABALANCE().Serialize(),
	)
	handler := codeFromBuilders(t, stackop.NIP().Serialize())
	caught := codeFromBuilders(t,
		stackop.PUSHREFCONT(failed).Serialize(),
		stackop.PUSHREFCONT(handler).Serialize(),
		execop.TRY().Serialize(),
	)
	code := codeFromBuilders(t,
		stackop.PUSHINT(big.NewInt(5)).Serialize(),
		stackop.PUSHREFCONT(caught).Serialize(),
		execop.REPEAT().Serialize(),
		stackop.PUSHINT(big.NewInt(1)).Serialize(),
		funcsop.GETEXTRABALANCE().Serialize(),
	)

	// Value decoding happens after registering the lookup, so all five caught
	// failures consume allowance. The sixth lookup must charge the full path.
	for _, version := range []int{10, 14} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			c7 := makeTonopsTestC7(t, tonopsTestC7Config{
				ConfigRoot: tonopsCrossConfigWithGlobalVersion(t, uint32(version)),
				MyCode:     code,
				Balance:    tuple.NewTupleValue(big.NewInt(10_000_000), extra.AsCell()),
			})
			assertTonopsBoundaryParity(t, code, c7, version, referenceDefaultMaxGas, 0, []any{int64(9), int64(1)})
		})
	}
}

func assertTonopsBoundaryParity(t *testing.T, code *cell.Cell, c7 tuple.Tuple, version int, gasLimit int64, exit int32, want []any) {
	t.Helper()

	code = prependRawMethodDrop(code)
	stack, err := buildCrossStack()
	if err != nil {
		t.Fatal(err)
	}
	goRes, err := runGoCrossCodeWithVersionGasAndLibs(code, testEmptyCell(), c7, nil, stack, version, gasLimit)
	if err != nil {
		t.Fatal(err)
	}
	refRes, err := runReferenceCrossCodeWithGas(code, testEmptyCell(), c7, stack, gasLimit)
	if err != nil {
		t.Fatal(err)
	}
	if goRes.exitCode != exit || refRes.exitCode != exit || goRes.gasUsed != refRes.gasUsed || !bytes.Equal(goRes.stack.Hash(), refRes.stack.Hash()) {
		t.Fatalf("exit/gas Go=%d/%d C++=%d/%d, stack\nGo: %s\nC++: %s", goRes.exitCode, goRes.gasUsed, refRes.exitCode, refRes.gasUsed, goRes.stack.Dump(), refRes.stack.Dump())
	}
	if exit == ^int32(vmerr.CodeOutOfGas) {
		want = []any{goRes.gasUsed}
	}
	assertCrossSkippedGoStack(t, goRes.stack, want)
	assertCrossSkippedGoStack(t, refRes.stack, want)
}
