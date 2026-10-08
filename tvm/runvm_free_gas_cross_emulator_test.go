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
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

func runVMFreeGasSignatureBody(t *testing.T) *cell.Cell {
	t.Helper()

	sig := cell.BeginCell().MustStoreSlice(make([]byte, 64), 512).EndCell()
	return codeFromBuilders(t,
		stackop.PUSHINT(big.NewInt(0)).Serialize(),
		stackop.PUSHREFSLICE(sig.MustBeginParse()).Serialize(),
		stackop.PUSHINT(big.NewInt(0)).Serialize(),
		funcsop.CHKSIGNU().Serialize(),
		stackop.DROP().Serialize(),
	)
}

func runVMFreeGasRepeat(body *cell.Cell, n int) []*cell.Builder {
	return []*cell.Builder{
		stackop.PUSHINT(big.NewInt(int64(n))).Serialize(),
		stackop.PUSHREFCONT(body).Serialize(),
		execop.REPEAT().Serialize(),
	}
}

func runVMFreeGasChildCall(child *cell.Cell, mode int) []*cell.Builder {
	builders := []*cell.Builder{
		stackop.PUSHINT(big.NewInt(0)).Serialize(),
		stackop.PUSHREFSLICE(child.MustBeginParse()).Serialize(),
	}
	if mode&16 != 0 {
		builders = append(builders, execop.PUSHCTR(7).Serialize())
	}
	return append(builders, execop.RUNVM(mode).Serialize())
}

func assertRunVMFreeGasCrossParity(t *testing.T, code *cell.Cell, version int, c7 tuple.Tuple) {
	t.Helper()

	goStack, err := buildCrossStack()
	if err != nil {
		t.Fatal(err)
	}
	refStack, err := buildCrossStack()
	if err != nil {
		t.Fatal(err)
	}

	code = prependRawMethodDrop(code)
	goRes, err := runGoCrossCodeWithVersion(code, testEmptyCell(), c7, goStack, version)
	if err != nil {
		t.Fatal(err)
	}
	refRes, err := runReferenceCrossCode(code, testEmptyCell(), c7, refStack)
	if err != nil {
		t.Fatal(err)
	}

	if goRes.exitCode != 0 || refRes.exitCode != 0 || goRes.gasUsed != refRes.gasUsed || !bytes.Equal(goRes.stack.Hash(), refRes.stack.Hash()) {
		t.Fatalf("Go/C++ mismatch: exit %d/%d, gas %d/%d, stack\nGo: %s\nC++: %s", goRes.exitCode, refRes.exitCode, goRes.gasUsed, refRes.gasUsed, goRes.stack.Dump(), refRes.stack.Dump())
	}
}

func TestTVMCrossEmulatorRunVMFreeGasThresholds(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skip(err)
	}

	// The path for id 0 exceeds the 200-gas discount even after cell reloads,
	// making the fifth-to-sixth GETEXTRABALANCE transition observable.
	extra := cell.NewDict(32)
	if err := extra.SetIntKey(big.NewInt(0), cell.BeginCell().MustStoreVarUInt(12345, 32).EndCell()); err != nil {
		t.Fatal(err)
	}
	for i := uint(0); i < 32; i++ {
		if err := extra.SetIntKey(new(big.Int).Lsh(big.NewInt(1), i), cell.BeginCell().MustStoreVarUInt(1, 32).EndCell()); err != nil {
			t.Fatal(err)
		}
	}

	for _, op := range []struct {
		name       string
		body       *cell.Cell
		minVersion int
		threshold  int
		mode       int
	}{
		{name: "CHKSIGNU", body: runVMFreeGasSignatureBody(t), minVersion: 4, threshold: 10},
		{name: "GETEXTRABALANCE", body: codeFromBuilders(t,
			stackop.PUSHINT(big.NewInt(0)).Serialize(),
			funcsop.GETEXTRABALANCE().Serialize(),
			stackop.DROP().Serialize(),
		), minVersion: 10, threshold: 5, mode: 16},
	} {
		for _, isolate := range []int{0, 128} {
			mode := op.mode | isolate
			childTwo := codeFromBuilders(t, runVMFreeGasRepeat(op.body, 2)...)
			childThreshold := codeFromBuilders(t, runVMFreeGasRepeat(op.body, op.threshold)...)
			sibling := codeFromBuilders(t, runVMFreeGasRepeat(op.body, op.threshold/2+1)...)
			grandchild := codeFromBuilders(t, runVMFreeGasRepeat(op.body, op.threshold/2+1)...)
			nestedBuilders := runVMFreeGasRepeat(op.body, op.threshold/2)
			nestedBuilders = append(nestedBuilders, runVMFreeGasChildCall(grandchild, mode)...)
			nestedChild := codeFromBuilders(t, nestedBuilders...)

			cases := []struct {
				name         string
				parentBefore int
				firstChild   *cell.Cell
				secondChild  *cell.Cell
			}{
				{name: "parent_child_parent", parentBefore: op.threshold - 1, firstChild: childTwo},
				{name: "child_parent", firstChild: childThreshold},
				{name: "siblings", firstChild: sibling, secondChild: sibling},
				{name: "nested", parentBefore: 1, firstChild: nestedChild},
			}
			for _, tc := range cases {
				var builders []*cell.Builder
				if tc.parentBefore > 0 {
					builders = append(builders, runVMFreeGasRepeat(op.body, tc.parentBefore)...)
				}
				builders = append(builders, runVMFreeGasChildCall(tc.firstChild, mode)...)
				if tc.secondChild != nil {
					builders = append(builders, runVMFreeGasChildCall(tc.secondChild, mode)...)
				}
				builders = append(builders, runVMFreeGasRepeat(op.body, 1)...)
				code := codeFromBuilders(t, builders...)
				for version := op.minVersion; version <= vmcore.MaxSupportedGlobalVersion; version++ {
					t.Run(fmt.Sprintf("%s/mode%d/%s/v%d", op.name, mode, tc.name, version), func(t *testing.T) {
						c7 := makeTonopsTestC7(t, tonopsTestC7Config{
							ConfigRoot: tonopsCrossConfigWithGlobalVersion(t, uint32(version)),
							MyCode:     code,
							Balance:    tuple.NewTupleValue(big.NewInt(1_000_000), extra.AsCell()),
						})
						assertRunVMFreeGasCrossParity(t, code, version, c7)
					})
				}
			}
		}
	}
}

func TestTVMCrossEmulatorRunVMFailedFreeGasFlush(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skip(err)
	}

	empty := codeFromBuilders(t)
	child := codeFromBuilders(t, runVMFreeGasChildCall(empty, 128)...)
	builders := runVMFreeGasRepeat(runVMFreeGasSignatureBody(t), 1)
	builders = append(builders,
		stackop.PUSHINT(big.NewInt(0)).Serialize(),
		stackop.PUSHREFSLICE(child.MustBeginParse()).Serialize(),
		stackop.PUSHINT(big.NewInt(500)).Serialize(),
		execop.RUNVM(8).Serialize(),
	)
	builders = append(builders, runVMFreeGasChildCall(empty, 128)...)
	code := codeFromBuilders(t, builders...)

	// The first isolated grandchild flushes 4000 inherited free gas against
	// a 500-gas child limit and fails. The parent must recover that deferred
	// gas from the failed child and charge it on its next isolated RUNVM.
	for version := 4; version <= vmcore.MaxSupportedGlobalVersion; version++ {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			c7 := prepareCrossTestC7WithConfigRoot(tonopsCrossConfigWithGlobalVersion(t, uint32(version)), code)
			assertRunVMFreeGasCrossParity(t, code, version, c7)
		})
	}
}
